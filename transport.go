package cassette

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// HTTPClient returns an *http.Client whose transport records or replays through
// this cassette. Pass it to an SDK via option.WithHTTPClient. In replay the
// client's base transport can never reach the network.
func (c *Cassette) HTTPClient() *http.Client {
	return &http.Client{Transport: c.Transport(nil)}
}

// Transport returns the cassette's http.RoundTripper. In record mode it wraps
// base (or a default transport) to reach the real server; in replay it wraps a
// dial-blocking transport so a logic bug that falls through to the network is
// caught loudly and counted.
func (c *Cassette) Transport(base http.RoundTripper) http.RoundTripper {
	if c.mode == ModeRecord {
		if base == nil {
			base = newRecordBaseTransport()
		}
		return &cassetteTransport{c: c, next: base}
	}
	if c.mode == ModeBranch {
		// Live is the egress path, used only when a request diverges from the
		// recorded prefix; the prefix itself is served from the recording.
		next := c.opts.Live
		if next == nil {
			next = newRecordBaseTransport()
		}
		return &cassetteTransport{c: c, next: next}
	}
	return &cassetteTransport{c: c, next: c.blockingTransport()}
}

// newRecordBaseTransport builds a fresh transport (not DefaultTransport, to avoid
// global state) that captures exactly the bytes on the wire, below SDK retries.
func newRecordBaseTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// blockingTransport returns a transport whose dialer can never reach the network:
// it increments the dial counter and returns ErrNetworkBlocked.
func (c *Cassette) blockingTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			c.dials.Add(1)
			return nil, fmt.Errorf("%w: dial %s %s", ErrNetworkBlocked, network, addr)
		},
		DialTLSContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			c.dials.Add(1)
			return nil, fmt.Errorf("%w: dial-tls %s %s", ErrNetworkBlocked, network, addr)
		},
	}
}

type cassetteTransport struct {
	c    *Cassette
	next http.RoundTripper
}

func (ct *cassetteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the request body once and restore it (body, ContentLength, GetBody)
	// so reading it for the match key never starves the real send or a retry.
	body, err := bufferRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("cassette: read request body: %w", err)
	}

	switch ct.c.mode {
	case ModeRecord:
		return ct.c.record(req, body, ct.next)
	case ModeBranch:
		return ct.c.branch(req, body, ct.next)
	default:
		return ct.c.replay(req, body)
	}
}

// branch serves the recorded prefix and, on the first miss (the divergence the
// agent reaches because it saw an edit), dials live and records the new tail —
// the forward re-run that graft alone cannot do. The appended live interaction
// is NOT added to c.keys, so a genuinely-new later request misses again (correct
// for a single forward run) and the suffix never shadows the prefix.
func (c *Cassette) branch(req *http.Request, reqBody []byte, next http.RoundTripper) (*http.Response, error) {
	stored, ok, err := c.matchResponse(req, reqBody)
	if err != nil {
		return nil, err // e.g. context cancelled — do not dial
	}
	if ok {
		return c.synthesize(stored, req), nil
	}
	// Divergence: go live and capture, appending to the in-memory cassette.
	return c.record(req, reqBody, next)
}

// LiveTransport returns a RoundTripper for ModeBranch that rewrites a path-only
// recorded request to a real base URL and re-injects auth from the caller
// (recorded match keys exclude host, and SDK clients are usually pointed at a
// placeholder base URL for replay). Secrets stay out of the saved cassette
// because saveLocked scrubs on write.
func LiveTransport(baseURL string, auth func(*http.Request)) http.RoundTripper {
	return &liveTransport{baseURL: strings.TrimRight(baseURL, "/"), auth: auth, base: newRecordBaseTransport()}
}

type liveTransport struct {
	baseURL string
	auth    func(*http.Request)
	base    http.RoundTripper
}

func (lt *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(lt.baseURL)
	if err != nil {
		return nil, fmt.Errorf("cassette: bad live base URL %q: %w", lt.baseURL, err)
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = u.Scheme
	clone.URL.Host = u.Host
	clone.Host = u.Host
	if lt.auth != nil {
		lt.auth(clone)
	}
	return lt.base.RoundTrip(clone)
}

// bufferRequestBody reads and restores req.Body, returning the raw bytes. It sets
// GetBody so the standard library can replay the body on retries/redirects.
func bufferRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	buf, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, err
	}
	restore := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	req.Body = io.NopCloser(bytes.NewReader(buf))
	req.ContentLength = int64(len(buf))
	req.GetBody = restore
	return buf, nil
}

// ---- Record ----------------------------------------------------------------

func (c *Cassette) record(req *http.Request, reqBody []byte, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	it := &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{
			Method:  req.Method,
			URL:     req.URL.Path,
			Headers: wirefmt.HeadersFromHTTP(req.Header),
			Body:    wirefmt.NewBody(reqBody),
		},
	}
	if _, digest, derr := match.BodyDigest(reqBody, req.Header.Get("Content-Type"), nil); derr == nil && len(reqBody) > 0 {
		it.Request.BodySHA256 = digest
	}
	// Persist the match key now, computed from the live (gunzipped) request via
	// the SAME match logic used at replay-lookup time. This makes replay matching
	// independent of body scrubbing and stored-body encoding.
	if in, kerr := match.FromRequest(req, reqBody); kerr == nil {
		if key, kerr := match.Key(in, c.match); kerr == nil {
			it.Request.MatchKey = key
		}
	}

	// Fill the response envelope BEFORE publishing `it` via appendInteraction: once
	// appended, `it` is reachable by matchResponse/saveLocked under c.mu, so writing
	// these fields afterward (unlocked) is a data race (ModeBranch, -race).
	respHeaders, streaming := captureResponseMeta(resp)
	it.Response.Status = resp.StatusCode
	it.Response.Headers = respHeaders
	it.Response.Streaming = streaming

	// Append the interaction now (request order); the response body is filled in
	// either synchronously (unary) or on Close (streaming).
	idx := c.appendInteraction(it)

	if streaming {
		// Tee the live stream so the SDK consumes it incrementally while we
		// capture the verbatim bytes; finalize into the cassette on Close.
		resp.Body = c.newStreamCapture(resp.Body, idx, isGzip(resp.Header))
		return resp, nil
	}

	// Unary: fully buffer (after decompression), store, and hand the SDK a fresh
	// reader over the decoded bytes.
	decoded, err := readDecoded(resp.Body, isGzip(resp.Header))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("cassette: read response body: %w", err)
	}
	c.setResponseBody(idx, decoded)
	resp.Body = io.NopCloser(bytes.NewReader(decoded))
	resp.ContentLength = int64(len(decoded))
	stripStoredEncodingHeaders(resp.Header)
	return resp, nil
}

func (c *Cassette) appendInteraction(it *wirefmt.Interaction) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file.Interactions = append(c.file.Interactions, it)
	return len(c.file.Interactions) - 1
}

func (c *Cassette) setResponseBody(idx int, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file.Interactions[idx].Response.Body = wirefmt.NewBody(body)
}

func (c *Cassette) setStreamCapture(idx int, body []byte, timing []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file.Interactions[idx].Response.Body = wirefmt.NewBody(body)
	if c.opts.CaptureTiming && len(timing) > 0 {
		c.file.Interactions[idx].Response.StreamTiming = timing
	}
}

// setStreamError records why a stream capture is incomplete (e.g. a truncated gzip
// member). A separate setter keeps setStreamCapture's signature and callers intact.
func (c *Cassette) setStreamError(idx int, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.file.Interactions[idx].Response.Error = msg
}

// splitSSEFrames splits an SSE byte stream into frames on the blank-line
// separator, keeping the separator with each frame. The trailing remainder (if
// any) is its own frame.
func splitSSEFrames(body []byte) [][]byte {
	sep := []byte("\n\n")
	var frames [][]byte
	rest := body
	for {
		i := bytes.Index(rest, sep)
		if i < 0 {
			if len(rest) > 0 {
				frames = append(frames, rest)
			}
			break
		}
		frames = append(frames, rest[:i+len(sep)])
		rest = rest[i+len(sep):]
	}
	return frames
}

// pacedReader re-emits SSE frames with recorded inter-frame delays, honoring
// context cancellation.
type pacedReader struct {
	ctx    context.Context
	sleep  func(context.Context, time.Duration) error
	frames [][]byte
	delays []int64 // ms per frame
	i      int     // next frame index
	cur    []byte  // unread bytes of the current frame
}

func (p *pacedReader) Read(b []byte) (int, error) {
	for len(p.cur) == 0 {
		if p.i >= len(p.frames) {
			return 0, io.EOF
		}
		var d time.Duration
		if p.i < len(p.delays) {
			d = time.Duration(p.delays[p.i]) * time.Millisecond
		}
		if err := p.sleep(p.ctx, d); err != nil {
			return 0, err
		}
		p.cur = p.frames[p.i]
		p.i++
	}
	n := copy(b, p.cur)
	p.cur = p.cur[n:]
	return n, nil
}

func (p *pacedReader) Close() error { return nil }

// captureResponseMeta returns stored headers (Content-Encoding/Length removed,
// since we store decoded bodies) and whether the response is an SSE stream.
func captureResponseMeta(resp *http.Response) (wirefmt.Headers, bool) {
	streaming := strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	h := resp.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	stripStoredEncodingHeaders(h)
	return wirefmt.HeadersFromHTTP(h), streaming
}

// stripStoredEncodingHeaders drops the Content-Encoding/Content-Length envelope:
// bodies are stored DECODED, and the envelope is reconstructed on replay, so keeping
// the recorded values would be wrong. One place for the invariant that record,
// captureResponseMeta, and synthesize all share.
func stripStoredEncodingHeaders(h http.Header) {
	h.Del("Content-Encoding")
	h.Del("Content-Length")
}

func isGzip(h http.Header) bool {
	return strings.Contains(strings.ToLower(h.Get("Content-Encoding")), "gzip")
}

func readDecoded(r io.Reader, gz bool) ([]byte, error) {
	if gz {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	return io.ReadAll(r)
}

// streamCapture tees an SSE response body: every byte the SDK reads is copied to
// an internal buffer, and on Close the captured bytes are written to the
// cassette interaction. Reads are unbuffered (no ReadAll) to preserve the SDK's
// incremental, first-token-latency behavior.
type streamCapture struct {
	c   *Cassette
	idx int
	src io.ReadCloser
	gz  bool

	timing bool // record per-frame inter-arrival deltas
	clock  func() time.Time
	start  time.Time
	framed int     // number of complete SSE frames timestamped so far
	cumMS  []int64 // cumulative ms at each frame boundary

	mu   sync.Mutex // guards buf against concurrent Read (tee) vs Close (finalize)
	buf  bytes.Buffer
	once sync.Once
}

func (c *Cassette) newStreamCapture(src io.ReadCloser, idx int, gz bool) *streamCapture {
	// Timing is captured over the bytes as read. For a gzip-encoded stream those
	// are compressed bytes (decompressed only at finalize), where SSE frame
	// boundaries are meaningless — so timing capture is disabled for gzipped
	// streams rather than recording misleading deltas.
	return &streamCapture{c: c, idx: idx, src: src, gz: gz, timing: c.opts.CaptureTiming && !gz, clock: c.clock}
}

func (s *streamCapture) Read(p []byte) (int, error) {
	n, err := s.src.Read(p)
	if n > 0 {
		// Manual, mutex-guarded tee (not io.TeeReader): the http.Response.Body
		// contract permits Close from another goroutine to unblock a Read, so the
		// capture write and finalize's read of buf must be serialized.
		s.mu.Lock()
		s.buf.Write(p[:n])
		if s.timing {
			s.markFramesLocked()
		}
		s.mu.Unlock()
	}
	// Finalize as soon as the stream is fully drained — the SDK may consume the
	// stream to EOF without ever calling Close.
	if err == io.EOF {
		s.finalize()
	}
	return n, err
}

// markFramesLocked timestamps any newly-completed SSE frames (delimited by a
// blank line) at the current elapsed time. Caller holds s.mu.
func (s *streamCapture) markFramesLocked() {
	if s.start.IsZero() {
		s.start = s.clock()
	}
	total := bytes.Count(s.buf.Bytes(), []byte("\n\n"))
	if total <= s.framed {
		return
	}
	ms := s.clock().Sub(s.start).Milliseconds()
	for ; s.framed < total; s.framed++ {
		s.cumMS = append(s.cumMS, ms)
	}
}

func (s *streamCapture) Close() error {
	err := s.src.Close()
	s.finalize()
	return err
}

func (s *streamCapture) finalize() {
	s.once.Do(func() {
		s.mu.Lock()
		captured := append([]byte(nil), s.buf.Bytes()...)
		cum := append([]int64(nil), s.cumMS...)
		s.mu.Unlock()
		var capErr string
		if s.gz {
			dec, derr := readDecoded(bytes.NewReader(captured), true)
			if derr != nil {
				// Truncated/invalid gzip member (e.g. a context-cancelled SSE stream).
				// The stored headers have Content-Encoding stripped and replay serves
				// the body verbatim, so storing the still-compressed bytes would replay
				// as undecodable text. Drop the body and record why — keep it honest.
				captured = nil
				capErr = fmt.Sprintf("incomplete gzip stream capture: %v", derr)
			} else {
				captured = dec
			}
		}
		s.c.setStreamCapture(s.idx, captured, cumToDeltas(cum))
		if capErr != "" {
			s.c.setStreamError(s.idx, capErr)
		}
	})
}

// cumToDeltas converts cumulative per-frame millisecond timestamps into
// per-frame inter-arrival deltas.
func cumToDeltas(cum []int64) []int64 {
	if len(cum) == 0 {
		return nil
	}
	d := make([]int64, len(cum))
	var prev int64
	for i, v := range cum {
		d[i] = v - prev
		if d[i] < 0 {
			d[i] = 0
		}
		prev = v
	}
	return d
}

// ---- Replay -----------------------------------------------------------------

func (c *Cassette) replay(req *http.Request, reqBody []byte) (*http.Response, error) {
	stored, ok, err := c.matchResponse(req, reqBody)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s %s", ErrNoMatch, req.Method, req.URL.Path)
	}
	return c.synthesize(stored, req), nil
}

// matchResponse is the shared lookup core used by both the replay RoundTripper
// and the serve listener: it derives the match key for req, advances the per-key
// cursor (marking the interaction consumed), and returns the recorded response.
// ok is false on a miss. It honors context cancellation and never touches the
// network.
func (c *Cassette) matchResponse(req *http.Request, reqBody []byte) (wirefmt.Response, bool, error) {
	if err := req.Context().Err(); err != nil {
		return wirefmt.Response{}, false, err
	}
	in, err := match.FromRequest(req, reqBody)
	if err != nil {
		return wirefmt.Response{}, false, err
	}
	key, err := match.Key(in, c.match)
	if err != nil {
		return wirefmt.Response{}, false, err
	}

	c.mu.Lock()
	// Chaos overlay: a synthetic fault (Status>0) injects a failure WITHOUT
	// consuming a recorded interaction, so a later retry can still reach the real
	// recording; a modify fault is applied to the consumed response below.
	var fault *Fault
	if len(c.opts.Faults) > 0 {
		c.attempts[key]++
		fault = c.opts.Faults.lookup(req.Method, req.URL.Path, c.attempts[key])
		if fault != nil && fault.synthetic() {
			c.mu.Unlock()
			return fault.synthResponse(), true, nil
		}
	}
	idx, ok := c.nextIndexLocked(key)
	var resp wirefmt.Response
	if ok {
		c.consumed[idx] = true
		// Read the recorded response WHILE holding c.mu: in ModeBranch a concurrent
		// divergent request appends to c.file.Interactions (reallocating the backing
		// array), which would race an unlocked indexed read here.
		resp = c.file.Interactions[idx].Response
	}
	c.mu.Unlock()
	if !ok {
		return wirefmt.Response{}, false, nil
	}
	if fault != nil {
		resp = applyModify(resp, *fault)
	}
	return resp, true, nil
}

// nextIndexLocked returns the interaction index to serve for key, advancing the
// per-key cursor and applying the exhaustion policy. Caller holds c.mu.
func (c *Cassette) nextIndexLocked(key string) (int, bool) {
	group := c.keys[key]
	if len(group) == 0 {
		return 0, false
	}
	n := c.cursor[key]
	if n < len(group) {
		c.cursor[key] = n + 1
		return group[n], true
	}
	switch c.opts.OnExhausted {
	case ExhaustRepeatLast:
		return group[len(group)-1], true
	case ExhaustCycle:
		c.cursor[key] = n + 1
		return group[n%len(group)], true
	default: // ExhaustError
		return 0, false
	}
}

// synthesize reconstructs a full *http.Response from a recorded response. Never
// returns a nil Body; Content-Type drives the SDK's decoder dispatch (JSON vs
// text/event-stream); no Content-Encoding is set (bodies are stored decoded).
func (c *Cassette) synthesize(stored wirefmt.Response, req *http.Request) *http.Response {
	body := stored.Body.Bytes()
	header := stored.Headers.ToHTTP()
	if header == nil {
		header = http.Header{}
	}
	stripStoredEncodingHeaders(header)

	status := stored.Status
	if status == 0 {
		status = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
	if stored.Streaming {
		resp.ContentLength = -1
		// Opt-in: re-emit frames with recorded inter-frame delays. Falls back to
		// the plain reader if frame/timing counts disagree.
		if c.opts.PaceStreaming && len(stored.StreamTiming) > 0 {
			frames := splitSSEFrames(body)
			delays := stored.StreamTiming
			// A final frame not terminated by a blank line yields one extra frame
			// from splitSSEFrames than the boundary-counted timing; pad it with a
			// zero delay so pacing still applies instead of silently falling back.
			if len(frames) == len(delays)+1 {
				delays = append(append([]int64(nil), delays...), 0)
			}
			if len(frames) == len(delays) {
				resp.Body = &pacedReader{
					ctx:    req.Context(),
					sleep:  c.sleep,
					frames: frames,
					delays: delays,
				}
			}
		}
	} else {
		resp.ContentLength = int64(len(body))
	}
	return resp
}
