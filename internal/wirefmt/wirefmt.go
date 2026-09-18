// Package wirefmt defines the on-disk cassette format: a deterministic, readable,
// git-diffable YAML document. Serialization is byte-stable (record the same
// session twice and the files are identical) and body round-trips are byte-exact
// (text stored verbatim when UTF-8-safe, base64 otherwise) so SSE/JSON-RPC frames
// replay byte-for-byte on every OS.
package wirefmt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only format version understood by this build.
const SchemaVersion = 1

// File is a whole cassette: an ordered list of interactions.
type File struct {
	SchemaVersion int            `yaml:"schema_version"`
	Notice        string         `yaml:"notice,omitempty"`
	Expect        *Expect        `yaml:"expect,omitempty"`
	Match         *MatchSpec     `yaml:"match,omitempty"`
	Provenance    []Provenance   `yaml:"provenance,omitempty"`
	Interactions  []*Interaction `yaml:"interactions"`
}

// Provenance is one link in a derived cassette's lineage chain: the operation that
// produced this cassette and the BEHAVIORAL digest of the source it was derived
// from. Because the digest is over semantics (not volatile bytes), the chain is
// stable across benign re-records and lets a reader prove "this recording was
// produced by <op> from a recording whose behavior digests to <from>". Appended by
// the derivation commands (port, migrate, graft, distill); never carries secrets.
type Provenance struct {
	Op   string `yaml:"op"`             // "port" | "migrate" | "graft" | "distill" | ...
	From string `yaml:"from"`           // source recording's combined semantic digest
	Note string `yaml:"note,omitempty"` // optional detail, e.g. "→ openai-chat" or "turn 2"
}

// MatchSpec is the persistable match-normalization a cassette was recorded with, so
// every reader (replay, conformance, lint, rekey) reproduces the same match keys
// without the caller re-supplying it. Only the data-only knobs are persistable;
// function hooks (PathTransform/BodyTransform/KeyFunc) are not.
type MatchSpec struct {
	VolatileJSONPaths []string `yaml:"volatile_json_paths,omitempty"`
	HeaderAllowlist   []string `yaml:"header_allowlist,omitempty"`
}

// Expect is an optional behavioral contract co-located inside the recording, so
// `cassette assert` can run zero-arg in CI and the contract survives re-record.
type Expect struct {
	NoTools          []string `yaml:"no_tools,omitempty"`
	NoDuplicateTools bool     `yaml:"no_duplicate_tools,omitempty"`
	FinishesClean    bool     `yaml:"finishes_clean,omitempty"`
}

// Interaction is a single recorded request/response pair. Kind is "http" or "mcp".
type Interaction struct {
	Kind     string   `yaml:"kind"`
	Request  Request  `yaml:"request"`
	Response Response `yaml:"response"`
}

// Request is the recorded request side. HTTP uses Method/URL/Headers/Body; MCP
// uses MCPMethod/MCPTool plus Body (the canonicalized params/arguments JSON).
type Request struct {
	Method     string  `yaml:"method,omitempty"`
	URL        string  `yaml:"url,omitempty"` // path only; host scrubbed
	Headers    Headers `yaml:"headers,omitempty"`
	MCPMethod  string  `yaml:"mcp_method,omitempty"`
	MCPTool    string  `yaml:"mcp_tool,omitempty"`
	Body       *Body   `yaml:"body,omitempty"`
	BodySHA256 string  `yaml:"body_sha256,omitempty"`
	// MatchKey is the match key computed at record time over the live (gunzipped,
	// pre-scrub) request, so replay matching is independent of body scrubbing and
	// stored-body encoding. Empty in hand-written cassettes (matching then falls
	// back to recomputing from the stored request).
	MatchKey string `yaml:"match_key,omitempty"`
}

// Response is the recorded response side. For streaming responses Streaming is
// true and Body holds the verbatim SSE byte stream (original framing preserved).
type Response struct {
	Status    int     `yaml:"status,omitempty"`
	Headers   Headers `yaml:"headers,omitempty"`
	Streaming bool    `yaml:"streaming,omitempty"`
	Body      *Body   `yaml:"body,omitempty"`
	Error     string  `yaml:"error,omitempty"`
	// StreamTiming holds per-SSE-frame inter-arrival deltas in milliseconds
	// (frame i appears StreamTiming[i] ms after the previous frame; [0] is from
	// stream start). Recorded only when timing capture is enabled; omitted
	// otherwise so byte-stable re-record is preserved. Approximate arrival timing
	// (SDK read boundaries), not exact server-flush reproduction.
	StreamTiming []int64 `yaml:"stream_timing,omitempty"`
}

// HeaderField is one header name and its ordered values.
type HeaderField struct {
	Name   string
	Values []string
}

// Headers is an ordered list of header fields, kept sorted by name for stable
// serialization. It marshals to a readable YAML mapping.
type Headers []HeaderField

// HeadersFromHTTP converts an http.Header into deterministic, sorted Headers.
func HeadersFromHTTP(h http.Header) Headers {
	if len(h) == 0 {
		return nil
	}
	out := make(Headers, 0, len(h))
	for name, vals := range h {
		out = append(out, HeaderField{Name: name, Values: append([]string(nil), vals...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ToHTTP converts Headers back into an http.Header.
func (hs Headers) ToHTTP() http.Header {
	h := make(http.Header, len(hs))
	for _, f := range hs {
		h[http.CanonicalHeaderKey(f.Name)] = append([]string(nil), f.Values...)
	}
	return h
}

// Get returns the first value of the named header (canonical match), or "".
func (hs Headers) Get(name string) string {
	cn := http.CanonicalHeaderKey(name)
	for _, f := range hs {
		if http.CanonicalHeaderKey(f.Name) == cn && len(f.Values) > 0 {
			return f.Values[0]
		}
	}
	return ""
}

// MarshalYAML renders Headers as a mapping (name -> flow sequence of values),
// emitted in sorted name order for byte-stable output.
func (hs Headers) MarshalYAML() (any, error) {
	sorted := append(Headers(nil), hs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, f := range sorted {
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: f.Name}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, v := range f.Values {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v})
		}
		node.Content = append(node.Content, key, seq)
	}
	return node, nil
}

// UnmarshalYAML reads the mapping form back into ordered, sorted Headers.
func (hs *Headers) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("wirefmt: headers must be a mapping, got kind %d", node.Kind)
	}
	var out Headers
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		f := HeaderField{Name: k.Value}
		switch v.Kind {
		case yaml.SequenceNode:
			for _, item := range v.Content {
				f.Values = append(f.Values, item.Value)
			}
		case yaml.ScalarNode:
			f.Values = []string{v.Value}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	*hs = out
	return nil
}

// Body holds a recorded body. It serializes as a plain YAML string when the
// content is UTF-8-safe (readable, git-diffable) and as a {encoding: base64,
// data: ...} mapping otherwise — guaranteeing byte-exact round-trips for binary
// and non-UTF-8 payloads.
type Body struct {
	Data []byte
}

// NewBody wraps b in a *Body, or returns nil when b is empty (so the field is
// omitted from the cassette).
func NewBody(b []byte) *Body {
	if len(b) == 0 {
		return nil
	}
	return &Body{Data: append([]byte(nil), b...)}
}

// Bytes returns the body bytes, or nil for a nil *Body.
func (b *Body) Bytes() []byte {
	if b == nil {
		return nil
	}
	return b.Data
}

// MarshalYAML chooses a text or base64 representation.
func (b Body) MarshalYAML() (any, error) {
	if yamlSafeText(b.Data) {
		// Force the !!str tag so values like "null", "true", or "123" are stored
		// (and quoted) as strings and never reinterpreted by YAML type resolution.
		n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(b.Data)}
		if bytes.ContainsRune(b.Data, '\n') {
			n.Style = yaml.LiteralStyle
		}
		return n, nil
	}
	return map[string]string{
		"encoding": "base64",
		"data":     base64.StdEncoding.EncodeToString(b.Data),
	}, nil
}

// UnmarshalYAML reads either the scalar (text) or mapping (base64) form.
func (b *Body) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		b.Data = []byte(node.Value)
		return nil
	case yaml.MappingNode:
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return err
		}
		if m["encoding"] != "base64" {
			return fmt.Errorf("wirefmt: unknown body encoding %q", m["encoding"])
		}
		data, err := base64.StdEncoding.DecodeString(m["data"])
		if err != nil {
			return fmt.Errorf("wirefmt: decode base64 body: %w", err)
		}
		b.Data = data
		return nil
	default:
		return fmt.Errorf("wirefmt: body must be a scalar or mapping, got kind %d", node.Kind)
	}
}

// yamlSafeText reports whether data can be stored as a YAML string and recovered
// byte-for-byte. We require valid UTF-8 and no control characters other than \n
// (and \t), and no trailing space on a line which YAML block scalars would clip.
func yamlSafeText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	if bytes.Contains(data, []byte{'\r'}) {
		return false // CR would be normalized by YAML line handling
	}
	for _, r := range string(data) {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	// Trailing space before a newline (or at end) is not preserved by literal
	// block scalars; fall back to base64 to stay exact.
	if bytes.Contains(data, []byte(" \n")) || bytes.HasSuffix(data, []byte(" ")) {
		return false
	}
	return true
}

// Marshal serializes f to deterministic YAML bytes.
func Marshal(f *File) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("wirefmt: marshal: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("wirefmt: marshal close: %w", err)
	}
	return buf.Bytes(), nil
}

// Unmarshal parses cassette bytes into a File and validates the schema version.
func Unmarshal(data []byte) (*File, error) {
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("wirefmt: unmarshal: %w", err)
	}
	if f.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("wirefmt: unsupported schema_version %d (want %d)", f.SchemaVersion, SchemaVersion)
	}
	return &f, nil
}

// MaxCassetteBytes caps how much a single cassette file may occupy in memory, so
// a hostile or corrupt file can't OOM the process. Real cassettes are KB–MB; the
// cap is generously above any legitimate recording.
const MaxCassetteBytes = 256 << 20 // 256 MiB

// Load reads and parses a cassette file, rejecting anything larger than
// MaxCassetteBytes before unmarshaling.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxCassetteBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxCassetteBytes {
		return nil, fmt.Errorf("wirefmt: cassette %s exceeds %d bytes", path, MaxCassetteBytes)
	}
	return Unmarshal(data)
}

// Save writes f to path atomically (temp file + rename) with binary I/O, so no
// newline translation corrupts SSE/JSON-RPC frames on any OS.
func Save(path string, f *File) error {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}
	data, err := Marshal(f)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cassette-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
