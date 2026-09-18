package rekey

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"testing"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func httpReq(method, url, ct, body string) wirefmt.Request {
	return wirefmt.Request{
		Method:  method,
		URL:     url,
		Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{ct}}},
		Body:    wirefmt.NewBody([]byte(body)),
	}
}

func TestHTTPKey_DeterministicAndCanonical(t *testing.T) {
	a, err := HTTPKey(httpReq("POST", "/v1/messages", "application/json", `{"b":2,"a":1}`), match.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Reordered JSON keys canonicalize to the same key.
	b, _ := HTTPKey(httpReq("POST", "/v1/messages", "application/json", `{"a":1,"b":2}`), match.Config{})
	if a != b {
		t.Fatalf("canonicalization failed: %q != %q", a, b)
	}
	// A different path yields a different key.
	c, _ := HTTPKey(httpReq("POST", "/v1/other", "application/json", `{"a":1,"b":2}`), match.Config{})
	if a == c {
		t.Fatal("different paths must not collide")
	}
}

func TestMCPKey_CallAndList(t *testing.T) {
	call := wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{"b":2,"a":1}`))}
	got := MCPKey(call, match.Config{})
	want := CallKey("add", []byte(`{"a":1,"b":2}`), match.Config{}) // canonical-equal args
	if got != want {
		t.Fatalf("tools/call key mismatch:\n got=%q\n want=%q", got, want)
	}
	if want == "" || got[:15] != "mcp:tools/call:" {
		t.Fatalf("unexpected call key format: %q", got)
	}
	list := wirefmt.Request{MCPMethod: "tools/list", Body: wirefmt.NewBody([]byte(`{}`))}
	if k := MCPKey(list, match.Config{}); k[:14] != "mcp:tools/list" {
		t.Fatalf("unexpected list key format: %q", k)
	}
}

func TestFile_RekeysHTTPAndMCP(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: httpReq("POST", "/v1/messages", "application/json", `{"a":1}`)},
		{Kind: "mcp", Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{"a":1}`))}},
	}}
	if err := File(f, match.Config{}); err != nil {
		t.Fatal(err)
	}
	for i, it := range f.Interactions {
		if it.Request.MatchKey == "" {
			t.Errorf("interaction %d (%s) not keyed", i, it.Kind)
		}
	}
	// Idempotent: a second pass yields identical keys.
	k0, k1 := f.Interactions[0].Request.MatchKey, f.Interactions[1].Request.MatchKey
	if err := File(f, match.Config{}); err != nil {
		t.Fatal(err)
	}
	if f.Interactions[0].Request.MatchKey != k0 || f.Interactions[1].Request.MatchKey != k1 {
		t.Fatal("rekey is not idempotent")
	}
}

func TestVolatilePathsAffectKey(t *testing.T) {
	req := httpReq("POST", "/v1/messages", "application/json", `{"prompt":"hi","ts":123}`)
	base, _ := HTTPKey(req, match.Config{})
	dropped, _ := HTTPKey(req, match.Config{VolatileJSONPaths: []string{"ts"}})
	if base == dropped {
		t.Fatal("dropping a volatile path should change the derived key")
	}
}

func TestBodyTransformAndDefaultMethod(t *testing.T) {
	// BodyTransform is applied before digesting on both the call and list paths.
	strip := func(path string, body []byte, ct string) []byte { return []byte(`{}`) }
	cfg := match.Config{BodyTransform: strip}
	if CallKey("add", []byte(`{"a":1}`), cfg) != CallKey("add", []byte(`{"a":999}`), cfg) {
		t.Error("BodyTransform should collapse differing call args to one key")
	}
	if ListKey([]byte(`{"cursor":"x"}`), cfg) != ListKey([]byte(`{"cursor":"y"}`), cfg) {
		t.Error("BodyTransform should collapse differing list params to one key")
	}
	// An unknown MCP method falls through to the generic "mcp:<method>" path.
	ping := wirefmt.Request{MCPMethod: "ping", Body: wirefmt.NewBody([]byte(`{}`))}
	if k := MCPKey(ping, match.Config{}); k[:9] != "mcp:ping:" {
		t.Fatalf("unexpected default-method key: %q", k)
	}
	if MCPKey(ping, cfg)[:9] != "mcp:ping:" {
		t.Error("default-method BodyTransform path should still produce an mcp:ping key")
	}
}

func TestHTTPKey_GzipMatchesIdentity(t *testing.T) {
	jsonBody := []byte(`{"a":1,"b":2}`)
	hdr := func(extra ...string) wirefmt.Headers {
		h := wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}}
		for i := 0; i+1 < len(extra); i += 2 {
			h = append(h, wirefmt.HeaderField{Name: extra[i], Values: []string{extra[i+1]}})
		}
		return h
	}
	identityKey, err := HTTPKey(wirefmt.Request{Method: "POST", URL: "/v1/messages", Headers: hdr(), Body: wirefmt.NewBody(jsonBody)}, match.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// gzip the same JSON.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(jsonBody)
	gw.Close()
	gz := buf.Bytes()

	gzipKey, err := HTTPKey(wirefmt.Request{Method: "POST", URL: "/v1/messages",
		Headers: hdr("Content-Encoding", "gzip"), Body: wirefmt.NewBody(gz)}, match.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if gzipKey != identityKey {
		t.Fatalf("gzip body keyed differently from identity:\n gzip=%q\n identity=%q", gzipKey, identityKey)
	}
	// The real invariant: stored-request keying agrees with LIVE keying.
	req, _ := http.NewRequest("POST", "http://x/v1/messages", bytes.NewReader(gz))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	in, _ := match.FromRequest(req, gz)
	liveKey, _ := match.Key(in, match.Config{})
	if gzipKey != liveKey {
		t.Fatalf("rekey gzip key %q != live key %q", gzipKey, liveKey)
	}
}
