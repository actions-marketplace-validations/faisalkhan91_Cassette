package semequal

import (
	"strings"
	"testing"
)

const gemSSE = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Paris is"}]},"index":0}],"modelVersion":"gemini-1.5-pro"}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" the capital."},{"text":"(thinking)","thought":true}]},"index":0}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"q":"paris"}}}]},"finishReason":"STOP","index":0}]}

`

const gemArray = `[{"candidates":[{"content":{"role":"model","parts":[{"text":"Paris is"}]}}]},
{"candidates":[{"content":{"role":"model","parts":[{"text":" the capital."}]}}]},
{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"q":"paris"}}}],"role":"model"},"finishReason":"STOP"}]}]`

func TestDecodeGeminiSSE_BothFramings(t *testing.T) {
	want := Transcript{Role: "assistant", Text: "Paris is the capital.",
		ToolCalls: []ToolCall{{Name: "lookup", Args: `{"q":"paris"}`}}, FinishReason: "STOP"}.Normalize()
	for _, tc := range []struct{ name, raw string }{{"sse", gemSSE}, {"array", gemArray}} {
		got, err := DecodeGeminiSSE([]byte(tc.raw))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !Equal(got, want) {
			t.Fatalf("%s mismatch:\n%s", tc.name, Diff(want, got))
		}
		if strings.Contains(got.Text, "thinking") {
			t.Fatalf("%s: thinking part not dropped", tc.name)
		}
	}
}

func TestDecodeGeminiSSE_Empty(t *testing.T) {
	got, err := DecodeGeminiSSE(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" || len(got.ToolCalls) != 0 {
		t.Fatalf("empty input should decode to empty transcript: %+v", got)
	}
}

func TestSplitTopLevelJSONObjects(t *testing.T) {
	// Strings containing braces/escapes must not confuse the splitter, and a
	// truncated trailing object is dropped.
	in := `[{"a":"}{"},{"b":"x\"y"},{"c":`
	got := splitTopLevelJSONObjects([]byte(in))
	if len(got) != 2 {
		t.Fatalf("got %d objects, want 2: %q", len(got), got)
	}
	if string(got[0]) != `{"a":"}{"}` || string(got[1]) != `{"b":"x\"y"}` {
		t.Fatalf("split wrong: %q", got)
	}
}
