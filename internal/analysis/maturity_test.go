package analysis

import (
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// Direct unit tests for the engine functions previously exercised only through
// the cmd layer (DiffRecordings/diffTools/Diverged, JudgePrompt, Fuzz,
// RequestModel) — so the analysis package carries its own coverage.

func TestDiffRecordings_Direct(t *testing.T) {
	a := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "hello world", FinishReason: "end_turn"}),
		streamIt("/v1/chat/completions", semequal.Transcript{
			ToolCalls: []semequal.ToolCall{{Name: "search", Args: `{"q":"a"}`}}, FinishReason: "tool_calls"}),
	}}
	// b diverges on both turns: changed text, and a changed tool arg.
	b := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "goodbye world", FinishReason: "end_turn"}),
		streamIt("/v1/chat/completions", semequal.Transcript{
			ToolCalls: []semequal.ToolCall{{Name: "search", Args: `{"q":"b"}`}}, FinishReason: "tool_calls"}),
	}}

	d := DiffRecordings(a, b)
	if !d.Diverged() {
		t.Fatal("expected divergence")
	}
	if len(d.Turns) == 0 || !d.Turns[0].Root {
		t.Fatalf("first divergent turn should be Root: %+v", d.Turns)
	}
	// An identical pair does not diverge.
	if DiffRecordings(a, a).Diverged() {
		t.Fatal("identical recordings must not diverge")
	}
	// A length mismatch is surfaced via ExtraA/ExtraB.
	short := &wirefmt.File{SchemaVersion: 1, Interactions: a.Interactions[:1]}
	if d := DiffRecordings(a, short); d.ExtraA != 1 {
		t.Fatalf("ExtraA=%d, want 1", d.ExtraA)
	}
}

func TestJudgePrompt_Deterministic(t *testing.T) {
	turns := []semequal.Transcript{{Text: "the capital of France is Paris", FinishReason: "end_turn"}}
	a := JudgePrompt("Is the answer correct?", "claude-x", turns)
	b := JudgePrompt("Is the answer correct?", "claude-x", turns)
	if string(a) != string(b) {
		t.Fatal("JudgePrompt must be byte-deterministic")
	}
	if !strings.Contains(string(a), "Is the answer correct?") || !strings.Contains(string(a), "claude-x") {
		t.Fatalf("judge prompt missing rubric/model: %s", a)
	}
}

func TestRequestModel_Direct(t *testing.T) {
	it := &wirefmt.Interaction{Kind: "http", Request: wirefmt.Request{
		Body: wirefmt.NewBody([]byte(`{"model":"claude-opus-4-6","stream":true}`))}}
	if got := RequestModel(it); got != "claude-opus-4-6" {
		t.Fatalf("RequestModel=%q", got)
	}
	none := &wirefmt.Interaction{Kind: "http", Request: wirefmt.Request{Body: wirefmt.NewBody([]byte(`{}`))}}
	if got := RequestModel(none); got != "" {
		t.Fatalf("RequestModel on bodyless=%q, want empty", got)
	}
}

func TestFuzz_Direct(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "the quick brown fox", FinishReason: "end_turn"})}}
	want := CollectTurns(f)[0].Digest()
	variants, err := Fuzz(f, 0, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 8 {
		t.Fatalf("got %d variants, want 8", len(variants))
	}
	for i, vf := range variants {
		if got := CollectTurns(vf)[0].Digest(); got != want {
			t.Fatalf("variant %d digest drift", i)
		}
	}
	if _, err := Fuzz(f, 9, 3, 1); err == nil {
		t.Fatal("expected out-of-range turn error")
	}
}
