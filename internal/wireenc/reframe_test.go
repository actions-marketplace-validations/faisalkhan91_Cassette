package wireenc

import (
	"bytes"
	"testing"

	"github.com/faisalkhan91/cassette/semequal"
)

func TestReframe_AllDecodeToSameTranscript(t *testing.T) {
	tr := semequal.Transcript{Text: "The quick brown fox jumps over the lazy dog.",
		ToolCalls:    []semequal.ToolCall{{Name: "a", Args: `{"x":1}`}, {Name: "b", Args: `{"y":2}`}},
		FinishReason: "tool_use"}
	want := tr.Normalize().Digest()

	for _, p := range providers {
		streams := Reframe(p.p, tr, 7, 30)
		if len(streams) != 30 {
			t.Fatalf("%s: got %d streams", p.name, len(streams))
		}
		distinct := map[string]bool{}
		for i, s := range streams {
			got, err := Decode(p.p, s)
			if err != nil {
				t.Fatalf("%s #%d decode: %v", p.name, i, err)
			}
			if got.Digest() != want {
				t.Fatalf("%s #%d digest drift:\n%s", p.name, i, semequal.Diff(tr.Normalize(), got))
			}
			distinct[string(s)] = true
		}
		// Anthropic reframings vary the bytes (split deltas / envelope); other
		// dialects at least vary the envelope.
		if p.p == Anthropic && len(distinct) < 5 {
			t.Fatalf("%s: expected varied framings, got %d distinct", p.name, len(distinct))
		}
	}
}

func TestReframe_EdgeTranscripts(t *testing.T) {
	// Empty text + no tools, and single-rune text, must still round-trip.
	for _, tr := range []semequal.Transcript{
		{FinishReason: "end_turn"},
		{Text: "x", FinishReason: "end_turn"},
	} {
		for _, s := range Reframe(Anthropic, tr, 1, 5) {
			got, err := Decode(Anthropic, s)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Digest() != tr.Normalize().Digest() {
				t.Fatalf("edge transcript drift for %+v", tr)
			}
		}
	}
}

func TestReframe_DeterministicPerSeed(t *testing.T) {
	tr := semequal.Transcript{Text: "hello world", FinishReason: "end_turn"}
	a := Reframe(Anthropic, tr, 42, 10)
	b := Reframe(Anthropic, tr, 42, 10)
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("reframing %d not reproducible for the same seed", i)
		}
	}
	// A different seed should change the framing.
	c := Reframe(Anthropic, tr, 43, 10)
	same := true
	for i := range a {
		if !bytes.Equal(a[i], c[i]) {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different seeds produced identical framings")
	}
}
