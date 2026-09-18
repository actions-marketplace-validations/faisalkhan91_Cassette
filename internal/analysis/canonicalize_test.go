package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestCanonicalizeFile(t *testing.T) {
	tr := semequal.Transcript{Text: "hi there", FinishReason: "end_turn"}
	// A non-canonical body: encoded with a custom (non-default) envelope, so its
	// volatile ids differ from the canonical zeroed-envelope form.
	noisy := wireenc.Encode(wireenc.Anthropic, tr, wireenc.Envelope{MessageID: "msg_live_42", ModelID: "claude-x", OutputTokens: 99})
	canonical := wireenc.Encode(wireenc.Anthropic, tr, wireenc.Envelope{})
	if string(noisy) == string(canonical) {
		t.Fatal("test setup: noisy form should differ from canonical")
	}

	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		{Kind: "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(noisy), StreamTiming: []int64{0, 5}}},
		// unary — must be left untouched
		{Kind: "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"id":"x"}`))}},
	}}

	before := CollectTurns(f) // semantic digests before
	n := CanonicalizeFile(f)
	if n != 1 {
		t.Fatalf("rewrote %d turns, want 1", n)
	}
	// Body is now the canonical form, timing dropped.
	if string(f.Interactions[0].Response.Body.Bytes()) != string(canonical) {
		t.Fatal("turn 0 not rewritten to canonical bytes")
	}
	if f.Interactions[0].Response.StreamTiming != nil {
		t.Fatal("StreamTiming should be dropped on a rewritten turn")
	}
	// Digest preserved.
	after := CollectTurns(f)
	if before[0].Digest() != after[0].Digest() {
		t.Fatalf("digest changed: %s -> %s", before[0].Digest(), after[0].Digest())
	}
	// Idempotent.
	if again := CanonicalizeFile(f); again != 0 {
		t.Fatalf("second pass rewrote %d turns, want 0", again)
	}
}
