package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func httpTurn(reqBody, respText string) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: respText, FinishReason: "end_turn"}, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
}

func TestTaint_DeterministicOrder(t *testing.T) {
	// Two distinct tokens that each cross from turn 0 to turn 1, so both surface as
	// flows. Under the old map-iteration the output order flipped randomly between
	// runs; it must now be stable (driven by the detector's scan order).
	body := `{"messages":[{"role":"user","content":"emails alice@example.com and carol@example.com"}]}`
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		httpTurn(body, "ok"),
		httpTurn(body, "ok"),
	}}
	first := Taint(f)
	if len(first) < 2 {
		t.Fatalf("expected >=2 cross-turn flows for a multi-token body, got %d: %+v", len(first), first)
	}
	want := make([]string, len(first))
	for i, fl := range first {
		want[i] = fl.Kind + "/" + fl.Redacted
	}
	for run := 0; run < 50; run++ {
		got := Taint(f)
		if len(got) != len(want) {
			t.Fatalf("run %d: flow count changed %d != %d", run, len(got), len(want))
		}
		for i, fl := range got {
			if k := fl.Kind + "/" + fl.Redacted; k != want[i] {
				t.Fatalf("run %d: order drift at %d: %q != %q", run, i, k, want[i])
			}
		}
	}
}

func TestTaint_CrossTurnFlows(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		// Turn 0: user pastes an email (user-input source); the tool result in the
		// response surfaces a DIFFERENT email (model-output source).
		httpTurn(`{"messages":[{"role":"user","content":"my email is alice@example.com"}]}`,
			"I looked it up; the owner is bob@vendor.example."),
		// Turn 1: the request history still carries alice@ (user-input flow) AND now
		// forwards bob@ outbound (model-output → exfil flow).
		httpTurn(`{"messages":[{"role":"user","content":"my email is alice@example.com"},{"role":"assistant","content":"owner bob@vendor.example"},{"role":"user","content":"email bob@vendor.example for me"}]}`,
			"Done."),
	}}

	flows := Taint(f)
	if len(flows) != 2 {
		t.Fatalf("got %d flows, want 2: %+v", len(flows), flows)
	}
	var sawUserInput, sawExfil bool
	for _, fl := range flows {
		if fl.Kind != "email" {
			t.Fatalf("unexpected class %q", fl.Kind)
		}
		if fl.SourceTurn != 0 || fl.SinkTurn != 1 {
			t.Fatalf("unexpected turns: %+v", fl)
		}
		switch fl.SourceKind {
		case "user-input":
			sawUserInput = true
		case "model-output":
			sawExfil = true
			if !fl.Exfil() {
				t.Fatal("model-output flow should report Exfil()")
			}
		}
	}
	if !sawUserInput || !sawExfil {
		t.Fatalf("missing a flow kind: userInput=%v exfil=%v", sawUserInput, sawExfil)
	}
}

func TestTaint_NoCrossTurn(t *testing.T) {
	// An email that appears only in a single turn's request does not cross turns.
	f := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		httpTurn(`{"messages":[{"role":"user","content":"alice@example.com"}]}`, "ok"),
	}}
	if flows := Taint(f); len(flows) != 0 {
		t.Fatalf("expected no cross-turn flows, got %+v", flows)
	}
}
