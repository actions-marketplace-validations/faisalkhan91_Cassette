package mutate_test

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/mutate"
	"github.com/faisalkhan91/cassette/semequal"
)

const fullText = "Hello, world! 🌍"

func TestApply_Deterministic(t *testing.T) {
	a := mutate.Apply(wirefix.AnthropicText, 42, mutate.ReorderContentDeltas(), mutate.DuplicateFrame(1))
	b := mutate.Apply(wirefix.AnthropicText, 42, mutate.ReorderContentDeltas(), mutate.DuplicateFrame(1))
	if !bytes.Equal(a, b) {
		t.Fatal("same seed + ops must be byte-deterministic")
	}
}

func TestTruncate_CutsStream(t *testing.T) {
	out := mutate.Apply(wirefix.AnthropicText, 1, mutate.TruncateAfterFrame(3))
	tr, _ := semequal.DecodeAnthropicSSE(out)
	if tr.Text == fullText {
		t.Fatal("truncation should drop the text deltas")
	}
	if len(out) >= len(wirefix.AnthropicText) {
		t.Fatal("truncated stream should be shorter")
	}
}

func TestDropTerminal_KeepsContent(t *testing.T) {
	out := mutate.Apply(wirefix.AnthropicText, 1, mutate.DropTerminalEvent())
	if bytes.Contains(out, []byte("message_stop")) {
		t.Fatal("terminal event should be dropped")
	}
	tr, _ := semequal.DecodeAnthropicSSE(out)
	if tr.Text != fullText {
		t.Fatalf("content before terminal should survive: %q", tr.Text)
	}
}

func TestInjectError_SurfacesError(t *testing.T) {
	out := mutate.Apply(wirefix.AnthropicText, 1, mutate.InjectError("Overloaded"))
	_, err := semequal.DecodeAnthropicSSE(out)
	if err == nil {
		t.Fatal("injected error event should surface as a decode error")
	}
}

func TestCorruptToolArgs_BreaksJSON(t *testing.T) {
	clean, _ := semequal.DecodeAnthropicSSE(wirefix.AnthropicToolUse)
	out := mutate.Apply(wirefix.AnthropicToolUse, 1, mutate.CorruptToolArgs())
	corrupt, _ := semequal.DecodeAnthropicSSE(out)
	if len(clean.ToolCalls) == 0 {
		t.Fatal("fixture should have a tool call")
	}
	if len(corrupt.ToolCalls) > 0 && corrupt.ToolCalls[0].Args == clean.ToolCalls[0].Args {
		t.Fatalf("corruption should change reassembled tool args (clean=%q)", clean.ToolCalls[0].Args)
	}
}

func TestReorder_ScramblesText(t *testing.T) {
	out := mutate.Apply(wirefix.AnthropicText, 1, mutate.ReorderContentDeltas())
	tr, _ := semequal.DecodeAnthropicSSE(out)
	if tr.Text == fullText {
		t.Fatal("reordering deltas should scramble assembled text")
	}
}

func TestDropMultibyte_ProducesInvalidUTF8(t *testing.T) {
	if !utf8.Valid(wirefix.AnthropicText) {
		t.Skip("fixture not utf8?")
	}
	out := mutate.Apply(wirefix.AnthropicText, 1, mutate.DropLastByteOfMultibyte())
	if utf8.Valid(out) {
		t.Fatal("dropping a multibyte tail byte should yield invalid UTF-8")
	}
}
