package semequal

import (
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefix"
)

func TestWireShape_StableAndGrammarSensitive(t *testing.T) {
	base := WireShape(wirefix.AnthropicText)
	if base == "" || !strings.Contains(base, "message_start") {
		t.Fatalf("unexpected shape: %q", base)
	}
	// Changing a volatile data value must NOT change the shape.
	mutated := strings.Replace(string(wirefix.AnthropicText), "msg_01TEXTLIVE0001", "msg_DIFFERENT_VOLATILE", 1)
	if WireShape([]byte(mutated)) != base {
		t.Fatal("volatile data change must not alter the wire shape")
	}
	// Reordering events MUST change the shape.
	frames := strings.SplitAfter(string(wirefix.AnthropicText), "\n\n")
	if len(frames) >= 3 {
		frames[1], frames[2] = frames[2], frames[1]
		if WireShape([]byte(strings.Join(frames, ""))) == base {
			t.Fatal("reordering events must change the wire shape")
		}
	}
}

func TestWireShape_OpenAI(t *testing.T) {
	s := WireShape(wirefix.OpenAIText)
	if !strings.Contains(s, "[DONE]") {
		t.Fatalf("OpenAI shape should include terminal [DONE]: %q", s)
	}
	if WireShapeDigest(wirefix.OpenAIText) == "" {
		t.Fatal("digest empty")
	}
}

// Chat Completions chunks carry no top-level "type"; the shape must still
// distinguish the role/content/finish grammar instead of collapsing to "data".
func TestWireShape_OpenAIChatGrammar(t *testing.T) {
	s := WireShape(wirefix.OpenAIText)
	for _, want := range []string{"delta.role", "delta.content", "delta.finish:stop"} {
		if !strings.Contains(s, want) {
			t.Fatalf("chat shape %q should distinguish %q", s, want)
		}
	}
	if strings.Contains(s, "data,data") {
		t.Fatalf("chat chunks must not all collapse to \"data\": %q", s)
	}
	// A volatile content VALUE change must not alter the grammar shape.
	mutated := strings.Replace(string(wirefix.OpenAIText), `"Hello"`, `"Bonjour"`, 1)
	if WireShape([]byte(mutated)) != s {
		t.Fatal("changing delta content text must not change the wire shape")
	}
}
