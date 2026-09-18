package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestPort_PreservesDigestAcrossDialects(t *testing.T) {
	// An authored Anthropic conversation (two turns, one with a tool call).
	src, err := Compile(Screenplay{Provider: "anthropic", Model: "claude-x", Turns: []ScreenplayTurn{
		{User: "weather in Paris?", ToolCalls: []ScreenplayTool{{Name: "get_weather", Args: `{"city":"Paris"}`}}, Finish: "tool_use"},
		{User: "thanks", Text: "You're welcome.", Finish: "end_turn"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := CollectTurns(src) // semantic transcripts of the source

	for _, to := range []struct {
		name string
		p    wireenc.Provider
		path string
	}{
		{"openai-chat", wireenc.OpenAIChat, "/v1/chat/completions"},
		{"openai-responses", wireenc.OpenAIResponses, "/v1/responses"},
	} {
		ported, n, err := Port(src, to.p)
		if err != nil {
			t.Fatalf("%s: %v", to.name, err)
		}
		if n != 2 {
			t.Fatalf("%s: ported %d turns, want 2", to.name, n)
		}
		got := CollectTurns(ported)
		if len(got) != len(want) {
			t.Fatalf("%s: %d turns, want %d", to.name, len(got), len(want))
		}
		for i := range want {
			if want[i].Digest() != got[i].Digest() {
				t.Fatalf("%s turn %d digest changed:\n%s", to.name, i, semequal.Diff(want[i], got[i]))
			}
		}
		// Requests are re-targeted to the new dialect's path and re-keyed.
		for i, it := range ported.Interactions {
			if it.Request.URL != to.path {
				t.Fatalf("%s turn %d url=%s want %s", to.name, i, it.Request.URL, to.path)
			}
			if it.Request.MatchKey == "" {
				t.Fatalf("%s turn %d missing key", to.name, i)
			}
		}
		// Round-trips cleanly under the target dialect.
		if rep := CodecVerify(ported); !rep.OK() {
			t.Fatalf("%s: ported file failed codec verify: %+v", to.name, rep)
		}
	}
}

func TestPort_CopiesUnary(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"id":"x"}`))}},
	}}
	ported, n, err := Port(f, wireenc.OpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unary should not be transpiled, ported=%d", n)
	}
	if ported.Interactions[0].Request.URL != "/v1/messages" {
		t.Fatal("unary turn should be copied unchanged")
	}
}

func TestPort_SameDialectIsNoop(t *testing.T) {
	src, _ := Compile(Screenplay{Provider: "anthropic", Turns: []ScreenplayTurn{{User: "hi", Text: "hello", Finish: "end_turn"}}})
	_, n, err := Port(src, wireenc.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("same-dialect port should transpile 0, got %d", n)
	}
}
