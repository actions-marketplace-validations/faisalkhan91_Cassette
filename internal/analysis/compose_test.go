package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// Stitch and Split must preserve the recording-level match normalization
// (VolatileJSONPaths), or a volatile-keyed recording stops replaying afterward.
func TestStitchAndSplit_PreserveMatch(t *testing.T) {
	src := &wirefmt.File{
		SchemaVersion: 1,
		Match:         &wirefmt.MatchSpec{VolatileJSONPaths: []string{"client_metadata", "prompt_cache_key"}},
		Interactions:  []*wirefmt.Interaction{streamIt("/v1/messages", semequal.Transcript{Text: "x", FinishReason: "end_turn"})},
	}
	st := Stitch([]*wirefmt.File{src})
	if st.Match == nil || len(st.Match.VolatileJSONPaths) != 2 {
		t.Fatalf("Stitch dropped the match block: %+v", st.Match)
	}
	groups, err := Split(src, "provider")
	if err != nil {
		t.Fatal(err)
	}
	for k, g := range groups {
		if g.Match == nil || len(g.Match.VolatileJSONPaths) != 2 {
			t.Fatalf("Split group %q dropped the match block: %+v", k, g.Match)
		}
	}
}

func TestStitchAndSplit_RoundTrip(t *testing.T) {
	a := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "anthropic", FinishReason: "end_turn"})}}
	b := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/chat/completions", semequal.Transcript{
			ToolCalls: []semequal.ToolCall{{Name: "search", Args: `{}`}}, FinishReason: "tool_calls"})}}

	society := Stitch([]*wirefmt.File{a, b})
	if len(society.Interactions) != 2 {
		t.Fatalf("stitched %d, want 2", len(society.Interactions))
	}

	byProvider, err := Split(society, "provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(byProvider) != 2 || byProvider["anthropic"] == nil || byProvider["openai-chat"] == nil {
		t.Fatalf("split by provider: %v", keysOf(byProvider))
	}

	byTool, err := Split(society, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if byTool["search"] == nil || byTool["no-tool"] == nil {
		t.Fatalf("split by tool: %v", keysOf(byTool))
	}

	if _, err := Split(society, "bogus"); err == nil {
		t.Fatal("expected error for unknown axis")
	}
}

func keysOf(m map[string]*wirefmt.File) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
