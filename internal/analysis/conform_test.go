package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func streamIt(url string, tr semequal.Transcript) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.ProviderForURL(url), tr, wireenc.Envelope{})
	return &wirefmt.Interaction{
		Kind:     "http",
		Request:  wirefmt.Request{Method: "POST", URL: url, Body: wirefmt.NewBody([]byte(`{"stream":true}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)},
	}
}

func TestCodecVerify(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "hi", FinishReason: "end_turn"}),
		streamIt("/v1/chat/completions", semequal.Transcript{Text: "yo",
			ToolCalls: []semequal.ToolCall{{Name: "f", Args: `{"x":1}`}}, FinishReason: "tool_calls"}),
		streamIt("/v1/responses", semequal.Transcript{Text: "ok", FinishReason: "completed"}),
		// unary (non-SSE) — skipped
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"id":"x"}`))}},
		// error stream — skipped (encoder has no error path)
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(
				[]byte("event: error\ndata: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"x\"}}\n\n"))}},
		// non-http — ignored entirely
		{Kind: "mcp", Request: wirefmt.Request{MCPTool: "add"}},
	}}

	rep := CodecVerify(f)
	if !rep.OK() {
		t.Fatalf("expected OK, got bad=%d: %+v", rep.Bad, rep.Turns)
	}
	if rep.Checked != 3 {
		t.Fatalf("checked=%d, want 3", rep.Checked)
	}
	if rep.Skipped != 2 {
		t.Fatalf("skipped=%d, want 2", rep.Skipped)
	}
	// Providers are labeled per dialect.
	if rep.Turns[0].Provider != "anthropic" || rep.Turns[1].Provider != "openai-chat" || rep.Turns[2].Provider != "openai-responses" {
		t.Fatalf("provider labels wrong: %+v", rep.Turns)
	}
	for _, ct := range rep.Turns[:3] {
		if !ct.Stable || !ct.Deterministic {
			t.Fatalf("turn %d not codec-preserving: %+v", ct.Turn, ct)
		}
	}
}
