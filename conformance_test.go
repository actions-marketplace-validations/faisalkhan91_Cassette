package cassette

import (
	"path/filepath"
	"testing"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func conformStreamTurn(url, reqBody string, tr semequal.Transcript) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.ProviderForURL(url), tr, wireenc.Envelope{})
	it := &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{
			Method:  "POST",
			URL:     url,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			Body:    wirefmt.NewBody([]byte(reqBody)),
		},
		Response: wirefmt.Response{
			Status:    200,
			Headers:   wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
			Streaming: true,
			Body:      wirefmt.NewBody(sse),
		},
	}
	k, _ := rekey.HTTPKey(it.Request, match.Config{})
	it.Request.MatchKey = k
	return it
}

func conformMCPTurn(body string) *wirefmt.Interaction {
	it := &wirefmt.Interaction{
		Kind:    "mcp",
		Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(body))},
	}
	it.Request.MatchKey = rekey.MCPKey(it.Request, match.Config{})
	return it
}

func writeConformanceCassette(t *testing.T, its ...*wirefmt.Interaction) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := wirefmt.Save(p, &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: its}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConformance_OK(t *testing.T) {
	p := writeConformanceCassette(t,
		conformStreamTurn("/v1/messages", `{"model":"claude","stream":true}`,
			semequal.Transcript{Text: "hello", FinishReason: "end_turn"}),
		conformMCPTurn(`{"a":1}`),
	)
	rep, err := Conformance(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("expected OK, got %+v", rep)
	}
	if rep.Dials != 0 {
		t.Fatalf("dials=%d, want 0", rep.Dials)
	}
	if !rep.Turns[0].KeyOK || !rep.Turns[0].ReplayOK {
		t.Fatalf("http turn failed: %+v", rep.Turns[0])
	}
	if !rep.Turns[1].KeyOK {
		t.Fatalf("mcp turn failed: %+v", rep.Turns[1])
	}
}

func TestConformance_StaleKey(t *testing.T) {
	it := conformStreamTurn("/v1/messages", `{"model":"claude","stream":true}`,
		semequal.Transcript{Text: "hello", FinishReason: "end_turn"})
	// Hand-edit the request body without rekeying: the persisted key is now stale.
	it.Request.Body = wirefmt.NewBody([]byte(`{"model":"claude","stream":true,"edited":true}`))
	p := writeConformanceCassette(t, it)

	rep, err := Conformance(p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Bad == 0 {
		t.Fatalf("expected failure on stale key, got %+v", rep)
	}
	if rep.Turns[0].KeyOK {
		t.Fatal("expected KeyOK=false on stale key")
	}
	if rep.Turns[0].ReplayOK {
		t.Fatal("expected ReplayOK=false: derived key no longer finds the stale-keyed entry")
	}
}

func TestConformance_MCPStaleKey(t *testing.T) {
	it := conformMCPTurn(`{"a":1}`)
	it.Request.MatchKey = "mcp:tools/call:add:deadbeef" // wrong
	p := writeConformanceCassette(t, it)
	rep, err := Conformance(p)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || rep.Turns[0].KeyOK {
		t.Fatalf("expected mcp stale-key failure, got %+v", rep)
	}
}

func TestConformance_UnknownKind(t *testing.T) {
	// A non-http/non-mcp interaction is skipped (counts as passing).
	p := writeConformanceCassette(t, &wirefmt.Interaction{
		Kind:    "other",
		Request: wirefmt.Request{Method: "GET", URL: "/x"},
	})
	rep, err := Conformance(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || !rep.Turns[0].OK() {
		t.Fatalf("unknown kind should be skipped/OK, got %+v", rep)
	}
}

func TestConformance_HandWrittenNoKey(t *testing.T) {
	// A hand-written cassette with no persisted key: index recomputes from the body,
	// so it still replays cleanly.
	it := conformStreamTurn("/v1/messages", `{"model":"claude","stream":true}`,
		semequal.Transcript{Text: "hi", FinishReason: "end_turn"})
	it.Request.MatchKey = ""
	p := writeConformanceCassette(t, it)
	rep, err := Conformance(p)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("expected OK for keyless hand-written cassette, got %+v", rep)
	}
}

// TestConformance_TrustsScrubbedBodyKey: when scrub altered a key-relevant request
// body, the stored (pre-scrub) key is not re-derivable from the stored body. Real
// recordings hit this. Conformance must trust the stored key (it's how live replay
// matches) — not false-fail, and never advise rekey (which would corrupt it).
func TestConformance_TrustsScrubbedBodyKey(t *testing.T) {
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: "ok", FinishReason: "end_turn"}, wireenc.Envelope{})
	it := &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{
			Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			// Body carries a scrub sentinel (as a real scrubbed recording would), and a
			// stored key that does NOT re-derive from this (post-scrub) body.
			Body:     wirefmt.NewBody([]byte(`{"model":"m","note":"REDACTED","messages":[{"role":"user","content":"hi"}]}`)),
			MatchKey: "POST\n/v1/messages\njson:prescrubdigest",
		},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)},
	}
	path := writeConformanceCassette(t, it)
	rep, err := Conformance(path)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || rep.Bad != 0 {
		t.Fatalf("scrubbed-body cassette should pass conformance (replay works via the stored key); bad=%d", rep.Bad)
	}
}
