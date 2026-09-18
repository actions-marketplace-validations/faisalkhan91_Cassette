package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestScenario(t *testing.T) {
	golden := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "the full answer text", FinishReason: "end_turn"}),
	}}
	variants, err := Scenario(golden, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]ScenarioVariant{}
	for _, v := range variants {
		names[v.Name] = v
		if v.Digest == "" {
			t.Fatalf("variant %s has no digest", v.Name)
		}
		// Each variant is a full, codec-valid cassette.
		if rep := CodecVerify(v.File); !rep.OK() {
			t.Fatalf("variant %s failed codec verify: %+v", v.Name, rep)
		}
	}
	for _, want := range []string{"finish-max_tokens", "finish-tool_use", "truncated", "empty-text", "malformed-args", "provider-error"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing variant %q (have %v)", want, keys(names))
		}
	}
	// The truncated variant differs semantically from the golden text.
	if got := names["truncated"].File.Interactions[0]; got == golden.Interactions[0] {
		t.Fatal("truncated variant should be a fresh interaction")
	}
	tr, _, _ := DecodeInteraction(names["empty-text"].File.Interactions[0])
	if tr.Text != "" {
		t.Fatalf("empty-text variant has text %q", tr.Text)
	}
}

func TestScenario_BadTurn(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1}
	if _, err := Scenario(f, 0); err == nil {
		t.Fatal("expected error for out-of-range turn")
	}
}

func keys(m map[string]ScenarioVariant) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
