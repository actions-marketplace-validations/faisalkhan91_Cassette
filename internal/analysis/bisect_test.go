package analysis_test

import (
	"path/filepath"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func anthroText(letter string) []byte {
	return []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + letter + `"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

`)
}

func turn(model, text string) *wirefmt.Interaction {
	return &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.HeadersFromHTTP(map[string][]string{"Content-Type": {"application/json"}}),
			Body:    wirefmt.NewBody([]byte(`{"model":"` + model + `","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(anthroText(text))},
	}
}

func saveFile(t *testing.T, name string, its ...*wirefmt.Interaction) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := wirefmt.Save(p, &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: its}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBisect_LocalizesTurnAndAxis(t *testing.T) {
	a, _ := wirefmt.Load(saveFile(t, "a.yaml", turn("claude-x", "A"), turn("claude-x", "A")))
	// Turn 0 identical; turn 1 differs in response AND the model axis.
	b, _ := wirefmt.Load(saveFile(t, "b.yaml", turn("claude-x", "A"), turn("claude-y", "B")))

	d := analysis.Bisect(a, b)
	if !d.Diverged() || d.Turn != 1 {
		t.Fatalf("expected divergence at turn 1, got %+v", d)
	}
	found := false
	for _, ax := range d.Axes {
		if ax == "model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected model axis attribution, got %v", d.Axes)
	}
}

// unaryTurn is a non-streaming JSON response carrying a distinct answer.
func unaryTurn(answer string) *wirefmt.Interaction {
	return &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.HeadersFromHTTP(map[string][]string{"Content-Type": {"application/json"}}),
			Body:    wirefmt.NewBody([]byte(`{"model":"claude-x","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: false,
			Body: wirefmt.NewBody([]byte(`{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"` + answer + `"}]}`))},
	}
}

// Regression: unary (non-streaming) responses must be compared on their body,
// not collapsed to an identical empty transcript that masks real divergence.
func TestBisect_UnaryResponsesDiverge(t *testing.T) {
	a, _ := wirefmt.Load(saveFile(t, "a.yaml", unaryTurn("Paris")))
	b, _ := wirefmt.Load(saveFile(t, "b.yaml", unaryTurn("London")))
	d := analysis.Bisect(a, b)
	if !d.Diverged() || d.Turn != 0 {
		t.Fatalf("two different unary answers must diverge at turn 0, got %+v", d)
	}
	// Identical unary bodies must NOT diverge.
	c, _ := wirefmt.Load(saveFile(t, "c.yaml", unaryTurn("Paris")))
	if analysis.Bisect(a, c).Diverged() {
		t.Fatal("identical unary recordings should not diverge")
	}
}

func TestBisect_Identical(t *testing.T) {
	a, _ := wirefmt.Load(saveFile(t, "a.yaml", turn("m", "A"), turn("m", "B")))
	b, _ := wirefmt.Load(saveFile(t, "b.yaml", turn("m", "A"), turn("m", "B")))
	if analysis.Bisect(a, b).Diverged() {
		t.Fatal("identical recordings should not diverge")
	}
}
