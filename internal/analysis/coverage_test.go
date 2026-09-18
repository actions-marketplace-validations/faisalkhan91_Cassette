package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestCoverage(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "hi", FinishReason: "end_turn"}),
		streamIt("/v1/chat/completions", semequal.Transcript{
			ToolCalls: []semequal.ToolCall{{Name: "get_weather", Args: `{}`}}, FinishReason: "tool_calls"}),
		// unary
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"id":"x"}`))}},
	}}
	rep := Coverage([]*wirefmt.File{f})
	if rep.Turns != 3 || rep.Streaming != 2 || rep.Unary != 1 {
		t.Fatalf("counts wrong: %+v", rep)
	}
	if rep.Tools["get_weather"] != 1 {
		t.Fatalf("tools: %+v", rep.Tools)
	}
	if !rep.Has("finish", "end_turn") || !rep.Has("tool", "get_weather") {
		t.Fatalf("Has wrong: %+v", rep)
	}
	if rep.Has("finish", "max_tokens") {
		t.Fatal("should not have unseen finish reason")
	}
	if rep.Providers["anthropic"] == 0 || rep.Providers["openai-chat"] == 0 {
		t.Fatalf("providers: %+v", rep.Providers)
	}
}

func TestShrink_SetCover(t *testing.T) {
	// a and b are byte-identical behavior; c is distinct. Minimal cover = {a or b, c} = 2.
	a := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "one", FinishReason: "end_turn"})}}
	b := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "one", FinishReason: "end_turn"})}}
	c := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/chat/completions", semequal.Transcript{
			ToolCalls: []semequal.ToolCall{{Name: "f", Args: `{}`}}, FinishReason: "tool_calls"})}}

	kept := Shrink([]string{"a", "b", "c"}, []*wirefmt.File{a, b, c})
	if len(kept) != 2 {
		t.Fatalf("kept %v, want 2 (one of a/b, plus c)", kept)
	}
	// c must be kept (only source of the tool + openai provider signals).
	hasC := false
	for _, k := range kept {
		if k == "c" {
			hasC = true
		}
	}
	if !hasC {
		t.Fatalf("c must be kept: %v", kept)
	}
}
