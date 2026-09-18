package analysis

import (
	"testing"
)

func TestCompile(t *testing.T) {
	sp := Screenplay{
		Provider: "anthropic",
		Model:    "claude-opus-4-6",
		Turns: []ScreenplayTurn{
			{User: "weather?", ToolCalls: []ScreenplayTool{{Name: "get_weather", Args: `{"city":"Paris"}`}}, Finish: "tool_use"},
			{User: "thanks", Text: "You're welcome.", Finish: "end_turn"},
		},
	}
	f, err := Compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Interactions) != 2 {
		t.Fatalf("got %d interactions, want 2", len(f.Interactions))
	}
	// Every interaction is a keyed, streaming http turn whose response decodes to
	// the authored transcript.
	turns := CollectTurns(f)
	if len(turns) != 2 {
		t.Fatalf("decoded %d turns, want 2", len(turns))
	}
	if len(turns[0].ToolCalls) != 1 || turns[0].ToolCalls[0].Name != "get_weather" {
		t.Fatalf("turn 0 tool calls wrong: %+v", turns[0])
	}
	if turns[1].Text != "You're welcome." || turns[1].FinishReason != "end_turn" {
		t.Fatalf("turn 1 wrong: %+v", turns[1])
	}
	for i, it := range f.Interactions {
		if it.Request.MatchKey == "" {
			t.Fatalf("turn %d missing match key", i)
		}
		if !it.Response.Streaming {
			t.Fatalf("turn %d not streaming", i)
		}
	}
	// The whole authored file passes codec verification.
	if rep := CodecVerify(f); !rep.OK() || rep.Checked != 2 {
		t.Fatalf("authored file failed codec verify: %+v", rep)
	}
}

func TestCompile_RequestOverride(t *testing.T) {
	sp := Screenplay{Provider: "openai-chat", Turns: []ScreenplayTurn{
		{User: "hi", Text: "hello", Finish: "stop", Request: `{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`},
	}}
	f, err := Compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Interactions[0].Request.Body.Bytes()); got != `{"model":"gpt","messages":[{"role":"user","content":"hi"}]}` {
		t.Fatalf("request override not used: %s", got)
	}
	if f.Interactions[0].Request.URL != "/v1/chat/completions" {
		t.Fatalf("wrong url: %s", f.Interactions[0].Request.URL)
	}
}

func TestCompile_Errors(t *testing.T) {
	if _, err := Compile(Screenplay{Provider: "bogus"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if _, err := Compile(Screenplay{Provider: "anthropic"}); err == nil {
		t.Fatal("expected error for no turns")
	}
	if _, err := Compile(Screenplay{Turns: []ScreenplayTurn{{User: "x", Request: "{bad json"}}}); err == nil {
		t.Fatal("expected error for invalid request override")
	}
}

func TestCompile_StampsUsage(t *testing.T) {
	// Authored usage flows into the response's usage block so an authored corpus can
	// exercise cost/dashboard/budget tooling.
	f, err := Compile(Screenplay{
		Provider: "anthropic",
		Model:    "claude-opus-4-6",
		Turns: []ScreenplayTurn{
			{User: "hi", Text: "Hello.", Finish: "end_turn", InputTokens: 1200, OutputTokens: 350},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	u := DecodeUsage(f.Interactions[0])
	if u.InputTokens != 1200 || u.OutputTokens != 350 {
		t.Fatalf("authored usage = %d in / %d out, want 1200/350", u.InputTokens, u.OutputTokens)
	}
	// Default (unstamped) stays zero.
	f2, _ := Compile(Screenplay{Provider: "anthropic", Model: "m",
		Turns: []ScreenplayTurn{{User: "hi", Text: "ok", Finish: "end_turn"}}})
	if u2 := DecodeUsage(f2.Interactions[0]); u2.InputTokens != 0 || u2.OutputTokens != 0 {
		t.Fatalf("unstamped usage = %+v, want zero", u2)
	}
}

func TestCompile_StampsUsage_OpenAI(t *testing.T) {
	// The OpenAI Chat + Responses encoders now emit a usage carrier when tokens are
	// supplied, so authored OpenAI corpora exercise cost/dashboard like Anthropic.
	for _, provider := range []string{"openai-chat", "openai-responses"} {
		f, err := Compile(Screenplay{
			Provider: provider, Model: "gpt-4o",
			Turns: []ScreenplayTurn{{User: "hi", Text: "ok", Finish: "stop", InputTokens: 2010, OutputTokens: 410}},
		})
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if u := DecodeUsage(f.Interactions[0]); !u.Known || u.InputTokens != 2010 || u.OutputTokens != 410 {
			t.Fatalf("%s authored usage = %+v, want 2010/410 known", provider, u)
		}
		// Unstamped stays unknown (no usage frame emitted → byte-identical default).
		f0, _ := Compile(Screenplay{Provider: provider, Model: "gpt-4o",
			Turns: []ScreenplayTurn{{User: "hi", Text: "ok", Finish: "stop"}}})
		if u := DecodeUsage(f0.Interactions[0]); u.Known {
			t.Fatalf("%s unstamped usage should be unknown, got %+v", provider, u)
		}
	}
}
