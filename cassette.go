// Package cassette is a record/replay "VCR" for LLM (HTTP) and MCP (stdio
// JSON-RPC) calls in Go. Wrap an agent's HTTP client once; RECORD real model and
// tool interactions to a readable, git-diffable YAML fixture; then REPLAY them
// deterministically with no API key and no network, so agent tests run fast,
// free, and green in CI.
//
// Interception is via a custom http.RoundTripper (never monkeypatching). In
// replay the transport returns synthetic responses reconstructed from the
// cassette and a cassette miss is a hard error — the network is never reached,
// and a dial counter proves it.
package cassette

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// sleepCtx sleeps for d unless ctx is cancelled first, in which case it returns
// ctx.Err() promptly. The default pacing sleeper.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Mode selects record vs replay behavior.
type Mode int

const (
	// ModeUnset is the zero value: no mode was chosen. Open resolves it to
	// ModeReplay; the test helper resolves it from CASSETTE_MODE/-update. Having a
	// distinct zero value lets the helper tell "caller left it default" apart from
	// an explicit ModeReplay (which must NOT be overridden by the environment).
	ModeUnset Mode = iota
	// ModeReplay (default) serves recorded interactions; the network is blocked.
	ModeReplay
	// ModeRecord proxies to the real transport and captures wire bytes.
	ModeRecord
	// ModeAuto records if the cassette file does not yet exist, else replays.
	ModeAuto
	// ModeBranch replays the recorded prefix and, on the first request that
	// matches no recording (the divergence the agent reaches after a graft), dials
	// Options.Live, records, and appends — capturing a live "forward" branch. It
	// is the basis of edit-a-step-and-re-run-forward.
	ModeBranch
)

func (m Mode) String() string {
	switch m {
	case ModeRecord:
		return "record"
	case ModeAuto:
		return "auto"
	case ModeBranch:
		return "branch"
	default:
		return "replay"
	}
}

// ExhaustPolicy controls replay behavior once every recorded entry for a match
// key has been consumed.
type ExhaustPolicy int

const (
	// ExhaustError (default) returns an error on over-consumption.
	ExhaustError ExhaustPolicy = iota
	// ExhaustRepeatLast keeps returning the last recorded entry for the key.
	ExhaustRepeatLast
	// ExhaustCycle wraps around to the first entry for the key.
	ExhaustCycle
)

// ErrNetworkBlocked is returned by the replay dialer if any code path attempts a
// real network dial during replay. Reaching it is a bug — replay must return
// synthetic responses without touching the transport.
var ErrNetworkBlocked = errors.New("cassette: network egress blocked in replay")

// ErrNoMatch is returned (wrapped) when a replayed request matches no recorded
// interaction. Misses are loud: there is never a silent passthrough.
var ErrNoMatch = errors.New("cassette: no recorded interaction matches request")

// ErrExhausted is returned when a key's recorded entries are exhausted under the
// default ExhaustError policy.
var ErrExhausted = errors.New("cassette: recorded interactions for request exhausted")

// Options configures a Cassette.
type Options struct {
	// Mode is the record/replay mode. The zero value (ModeUnset) resolves to
	// ModeReplay in Open.
	Mode Mode
	// Match configures match-key derivation.
	Match MatchConfig
	// Scrub configures secret redaction applied before writing. When left as the
	// zero value, Open installs DefaultScrubConfig() so secrets are redacted by
	// default — set DisableScrub to opt out.
	Scrub ScrubConfig
	// DisableScrub turns off all redaction/stamping (NOT recommended; cassettes
	// may then contain real secrets). Only honored when explicitly set.
	DisableScrub bool
	// OnExhausted is the replay over-consumption policy. Default ExhaustError.
	OnExhausted ExhaustPolicy
	// CaptureTiming records per-SSE-frame inter-arrival deltas during record.
	// Default OFF so cassette bytes are byte-stable across re-records.
	CaptureTiming bool
	// PaceStreaming re-emits recorded SSE frames with their recorded inter-frame
	// delays during replay (honoring context cancellation). Default OFF (frames
	// are delivered as fast as the consumer reads).
	PaceStreaming bool
	// Faults, when set, is a chaos overlay applied at response-reconstruction time
	// without mutating the cassette (synthetic failures + stream corruptions),
	// keyed by request method/path and per-key attempt ordinal. Replay only.
	Faults FaultSet
	// Notice is written to the cassette header in record mode.
	Notice string
	// Live is the real upstream transport used by ModeBranch to dial the provider
	// when a request diverges from the recorded prefix. Required in branch mode
	// (defaults to a standard transport if nil). Use LiveTransport to point a
	// path-only recording at a real base URL and re-inject auth.
	Live http.RoundTripper
}

// MatchInput is the normalized, transport-decoded view of a request passed to a
// MatchConfig.KeyFunc. Path is the URL path only (host excluded so a cassette is
// portable across base URLs); Body is already decompressed.
type MatchInput struct {
	Method      string
	Path        string
	Body        []byte
	ContentType string
	Header      http.Header
}

// MatchConfig controls how the match key is derived from a request. The zero value
// is the default (path + canonical body, no headers). All fields must be
// deterministic: they run identically at record-index and replay-lookup time.
type MatchConfig struct {
	// HeaderAllowlist lists header names (canonical form) whose values enter the key.
	HeaderAllowlist []string
	// VolatileJSONPaths lists dotted JSON paths dropped from the request body before
	// digesting (match key only; the stored body is untouched).
	VolatileJSONPaths []string
	// PathTransform, when set, normalizes the URL path before it enters the key —
	// e.g. to collapse a volatile Azure deployment name or tenant id.
	PathTransform func(method, path string) string
	// BodyTransform, when set, is applied to the request body before it is digested
	// into the key. The path is the URL path (HTTP) or "mcp:<method>:<tool>" (MCP).
	BodyTransform func(path string, body []byte, contentType string) []byte
	// KeyFunc, when set, fully overrides key derivation for HTTP requests (MCP keys
	// are always tool name + canonical arguments). The caller owns determinism.
	KeyFunc func(MatchInput) (string, error)
}

func (mc MatchConfig) toInternal() match.Config {
	cfg := match.Config{
		HeaderAllowlist:   mc.HeaderAllowlist,
		VolatileJSONPaths: mc.VolatileJSONPaths,
		PathTransform:     mc.PathTransform,
		BodyTransform:     mc.BodyTransform,
	}
	if mc.KeyFunc != nil {
		kf := mc.KeyFunc
		cfg.KeyFunc = func(in match.Input) (string, error) {
			return kf(MatchInput{
				Method:      in.Method,
				Path:        in.Path,
				Body:        in.Body,
				ContentType: in.ContentType,
				Header:      in.Header,
			})
		}
	}
	return cfg
}

// AzureMatchConfig returns a MatchConfig that collapses the Azure OpenAI deployment
// name in the request path, so one cassette replays across deployments.
func AzureMatchConfig() MatchConfig {
	a := match.AzureConfig()
	return MatchConfig{PathTransform: a.PathTransform}
}

// ScrubConfig controls secret redaction and volatile-field stamping applied before
// a cassette is written. The zero value scrubs nothing; Open installs
// DefaultScrubConfig() when Options.Scrub is left zero (unless DisableScrub is set).
type ScrubConfig struct {
	// RedactHeaders lists header names (canonical-insensitive) whose values are
	// replaced with "REDACTED" in both request and response headers.
	RedactHeaders []string
	// BodyPatterns are regexps whose matches in any stored body are replaced.
	BodyPatterns []*regexp.Regexp
	// ResponseStamps maps a JSON path (gjson/sjson syntax) to a sentinel value,
	// applied to non-streaming JSON response bodies where the path exists.
	ResponseStamps map[string]any
}

func (sc ScrubConfig) isZero() bool {
	return sc.RedactHeaders == nil && sc.BodyPatterns == nil && sc.ResponseStamps == nil
}

// DefaultScrubConfig returns the provider-aware default redaction config: redact
// common secret headers, scrub key-shaped tokens from bodies, and stamp volatile
// response identifiers. Use it as a base when extending with your own patterns.
func DefaultScrubConfig() ScrubConfig {
	d := scrub.DefaultConfig()
	return ScrubConfig{
		RedactHeaders:  d.RedactHeaders,
		BodyPatterns:   d.BodyPatterns,
		ResponseStamps: d.ResponseStamps,
	}
}

// resolveScrub picks the internal scrub config for opts: nothing when DisableScrub,
// the provider-aware default when Scrub is left zero (secrets-by-default so direct
// Open callers never write real credentials), else the caller's config MERGED onto
// the defaults. Merging field-by-field means setting one field (say BodyPatterns)
// no longer silently drops the default secret-header redaction and response-stamp
// determinism — a footgun, since the dropped stamps break byte-stability. A field
// the caller DID set replaces that field's default (so the documented "start from
// DefaultScrubConfig() and extend" pattern still works without double-merging).
func resolveScrub(opts Options) scrub.Config {
	switch {
	case opts.DisableScrub:
		return scrub.Config{}
	case opts.Scrub.isZero():
		return scrub.DefaultConfig()
	default:
		merged := scrub.DefaultConfig()
		if opts.Scrub.RedactHeaders != nil {
			merged.RedactHeaders = opts.Scrub.RedactHeaders
		}
		if opts.Scrub.BodyPatterns != nil {
			merged.BodyPatterns = opts.Scrub.BodyPatterns
		}
		if opts.Scrub.ResponseStamps != nil {
			merged.ResponseStamps = opts.Scrub.ResponseStamps
		}
		return merged
	}
}

// Cassette records or replays HTTP and MCP interactions backed by a single YAML
// file. It is safe for concurrent use in replay.
type Cassette struct {
	path string
	mode Mode
	opts Options

	// match and scrub are the public Options.Match/Scrub translated to the internal
	// configs once at construction, so internal call sites work with the internal
	// types while the public API stays nameable by external callers.
	match match.Config
	scrub scrub.Config

	mu       sync.Mutex
	file     *wirefmt.File
	keys     map[string][]int // match key -> ordered interaction indices (http + mcp)
	cursor   map[string]int   // match key -> next index within keys[key]
	consumed map[int]bool     // interaction index -> played at least once
	attempts map[string]int   // match key -> times served (for fault overlays)

	dials atomic.Int64

	// clock and sleep are injectable for deterministic timing tests; defaulted in
	// Open to time.Now and a context-aware sleep.
	clock func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Open creates a Cassette bound to path. In ModeReplay (or ModeAuto when the file
// exists) it loads and indexes the cassette; in ModeRecord it starts empty.
// newCassette builds a Cassette with the fields + maps common to both constructors.
// Keeping the struct literal (and the three map inits) in one place stops Open and
// OpenBytes from drifting.
func newCassette(path string, mode Mode, opts Options) *Cassette {
	return &Cassette{
		path:     path,
		mode:     mode,
		opts:     opts,
		match:    opts.Match.toInternal(),
		scrub:    resolveScrub(opts),
		cursor:   map[string]int{},
		consumed: map[int]bool{},
		attempts: map[string]int{},
		clock:    time.Now,
		sleep:    sleepCtx,
	}
}

func Open(path string, opts Options) (*Cassette, error) {
	c := newCassette(path, opts.Mode, opts)

	if c.mode == ModeUnset {
		c.mode = ModeReplay // zero value resolves to the safe, network-blocked default
	}

	if c.mode == ModeAuto {
		if _, err := os.Stat(path); err == nil {
			c.mode = ModeReplay
		} else {
			c.mode = ModeRecord
		}
	}

	if c.mode == ModeReplay || c.mode == ModeBranch {
		// Branch loads + indexes the seed like replay, but leaves c.file appendable
		// like record so the live suffix can be captured into it.
		f, err := wirefmt.Load(path)
		if err != nil {
			return nil, fmt.Errorf("cassette: load %s: %w", path, err)
		}
		c.file = f
		c.applyFileMatch()
		if err := c.index(); err != nil {
			return nil, err
		}
	} else {
		c.file = &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Notice: opts.Notice}
	}
	return c, nil
}

// applyFileMatch folds a cassette's persisted MatchSpec into the resolved match
// config, so a recorded cassette reproduces its own match keys (replay, conformance,
// lint) without the caller re-supplying --volatile. Explicit Options.Match wins.
func (c *Cassette) applyFileMatch() {
	if c.file == nil || c.file.Match == nil {
		return
	}
	if len(c.match.VolatileJSONPaths) == 0 {
		c.match.VolatileJSONPaths = c.file.Match.VolatileJSONPaths
	}
	if len(c.match.HeaderAllowlist) == 0 {
		c.match.HeaderAllowlist = c.file.Match.HeaderAllowlist
	}
}

// OpenBytes creates a replay-only Cassette from in-memory cassette bytes (the
// YAML produced by wirefmt.Marshal/Save), without touching the filesystem. name
// is a label used only for Path()/diagnostics. This is the constructor used by a
// `cassette pack` self-contained binary, which carries the recording embedded in
// its own image rather than as a file on disk.
func OpenBytes(name string, data []byte, opts Options) (*Cassette, error) {
	f, err := wirefmt.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("cassette: parse %s: %w", name, err)
	}
	opts.Mode = ModeReplay
	c := newCassette(name, ModeReplay, opts)
	c.file = f
	c.applyFileMatch()
	if err := c.index(); err != nil {
		return nil, err
	}
	return c, nil
}

// Mode reports the resolved mode (ModeAuto is resolved to record or replay).
func (c *Cassette) Mode() Mode { return c.mode }

// Path returns the cassette file path.
func (c *Cassette) Path() string { return c.path }

// Dials returns the number of blocked network dials observed (must be 0 after a
// correct replay run).
func (c *Cassette) Dials() int64 { return c.dials.Load() }

// index groups loaded interactions (http and mcp) by match key, preserving
// recorded order so duplicate keys replay in sequence.
func (c *Cassette) index() error {
	c.keys = map[string][]int{}
	for i, it := range c.file.Interactions {
		var (
			key string
			err error
		)
		// Prefer the key persisted at record time: it was computed over the live,
		// gunzipped, pre-scrub request, so it is immune to body scrubbing and
		// stored-body encoding. Fall back to recomputation for hand-written
		// cassettes that omit it.
		if it.Request.MatchKey != "" {
			c.keys[it.Request.MatchKey] = append(c.keys[it.Request.MatchKey], i)
			continue
		}
		switch it.Kind {
		case "http":
			key, err = c.requestKey(it.Request)
		case "mcp":
			key = rekey.MCPKey(it.Request, c.match)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("cassette: index interaction %d: %w", i, err)
		}
		c.keys[key] = append(c.keys[key], i)
	}
	return nil
}

// requestKey computes the match key for a stored request using the SAME logic as
// live requests (match.Key), so indexing and lookup are identical.
func (c *Cassette) requestKey(r wirefmt.Request) (string, error) {
	return rekey.HTTPKey(r, c.match)
}

// Save writes the cassette to disk atomically, through the scrubber. Used in
// record mode.
func (c *Cassette) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Cassette) saveLocked() error {
	if c.file.SchemaVersion == 0 {
		c.file.SchemaVersion = wirefmt.SchemaVersion
	}
	// Preserve an existing behavioral contract (Expect) across re-record so a
	// `cassette expect` contract survives `go test -update`.
	if c.file.Expect == nil {
		if prior, err := wirefmt.Load(c.path); err == nil && prior.Expect != nil {
			c.file.Expect = prior.Expect
		}
	}
	// Persist the match normalization so the cassette reproduces its own keys for
	// every reader (replay/conformance/lint) without re-supplying --volatile.
	if len(c.match.VolatileJSONPaths) > 0 || len(c.match.HeaderAllowlist) > 0 {
		c.file.Match = &wirefmt.MatchSpec{
			VolatileJSONPaths: c.match.VolatileJSONPaths,
			HeaderAllowlist:   c.match.HeaderAllowlist,
		}
	}
	// Scrub a deep copy so in-memory state used by ongoing replay is untouched.
	scrubbed := cloneFile(c.file)
	c.scrub.File(scrubbed)
	// Backstop: a named credential shape must never reach disk. Scrubbing already
	// redacts these from bodies, so a hit here means one survived in a field the
	// scrubber doesn't touch (e.g. an unexpected header) — refuse the write rather
	// than persist a secret. Closes the scrub↔scan loop at the point that matters.
	// Skipped when the caller has explicitly opted out of scrubbing (DisableScrub),
	// which is the deliberate "record raw" escape hatch.
	if !c.opts.DisableScrub {
		data, err := wirefmt.Marshal(scrubbed)
		if err != nil {
			return err
		}
		if hits := scrub.SecretScan(data); len(hits) > 0 {
			return fmt.Errorf("cassette: refusing to write %s — secret pattern(s) survived scrubbing: %v", c.path, hits)
		}
	}
	return wirefmt.Save(c.path, scrubbed)
}

// VerifyError returns a non-nil error describing any verification failure:
// unconsumed interactions or non-zero dials in replay. In record mode it saves
// the cassette and returns any write error.
func (c *Cassette) VerifyError() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.mode == ModeRecord {
		return c.saveLocked()
	}

	// Branch mode dials on purpose and intentionally diverges mid-prefix, so it
	// neither fails on dials nor flags the unreplayed prefix tail; it just persists
	// the grown branch (edited prefix + live suffix).
	if c.mode == ModeBranch {
		return c.saveLocked()
	}

	if d := c.dials.Load(); d != 0 {
		return fmt.Errorf("cassette: replay made %d network dial(s); expected 0", d)
	}
	var unconsumed []int
	for i, it := range c.file.Interactions {
		if it.Kind != "http" && it.Kind != "mcp" {
			continue
		}
		if !c.consumed[i] {
			// An unbounded synthetic fault intentionally shadows this key on every
			// attempt, so the recording is never reached — that is expected, not a
			// verification failure.
			if c.opts.Faults.shadowsEveryAttempt(it.Request.Method, it.Request.URL) {
				continue
			}
			unconsumed = append(unconsumed, i)
		}
	}
	if len(unconsumed) > 0 {
		return fmt.Errorf("cassette: %d recorded interaction(s) never replayed: %v", len(unconsumed), unconsumed)
	}
	return nil
}

func cloneFile(f *wirefmt.File) *wirefmt.File {
	out := &wirefmt.File{SchemaVersion: f.SchemaVersion, Notice: f.Notice, Expect: f.Expect, Match: f.Match, Provenance: f.Provenance}
	for _, it := range f.Interactions {
		ci := &wirefmt.Interaction{
			Kind:     it.Kind,
			Request:  cloneRequest(it.Request),
			Response: cloneResponse(it.Response),
		}
		out.Interactions = append(out.Interactions, ci)
	}
	return out
}

func cloneRequest(r wirefmt.Request) wirefmt.Request {
	return wirefmt.Request{
		Method:     r.Method,
		URL:        r.URL,
		Headers:    cloneHeaders(r.Headers),
		MCPMethod:  r.MCPMethod,
		MCPTool:    r.MCPTool,
		Body:       cloneBody(r.Body),
		BodySHA256: r.BodySHA256,
		MatchKey:   r.MatchKey,
	}
}

func cloneResponse(r wirefmt.Response) wirefmt.Response {
	return wirefmt.Response{
		Status:       r.Status,
		Headers:      cloneHeaders(r.Headers),
		Streaming:    r.Streaming,
		Body:         cloneBody(r.Body),
		Error:        r.Error,
		StreamTiming: append([]int64(nil), r.StreamTiming...),
	}
}

func cloneHeaders(hs wirefmt.Headers) wirefmt.Headers {
	if hs == nil {
		return nil
	}
	out := make(wirefmt.Headers, len(hs))
	for i, f := range hs {
		out[i] = wirefmt.HeaderField{Name: f.Name, Values: append([]string(nil), f.Values...)}
	}
	return out
}

func cloneBody(b *wirefmt.Body) *wirefmt.Body {
	if b == nil {
		return nil
	}
	return &wirefmt.Body{Data: append([]byte(nil), b.Data...)}
}
