package main

import (
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func taintTurn(reqBody, respText string) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: respText, FinishReason: "end_turn"}, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
}

func TestCLI_Taint(t *testing.T) {
	tainted := seedFile(t,
		taintTurn(`{"content":"my email is alice@example.com"}`, "noted"),
		taintTurn(`{"content":"alice@example.com again"}`, "ok"),
	)

	// Report mode: a flow is shown, exit 0 (no --fail-on).
	code, out, _ := runArgs("taint", tainted)
	if code != exitOK || !strings.Contains(out, "email") || !strings.Contains(out, "turn 0") {
		t.Fatalf("taint report exit=%d\n%s", code, out)
	}

	// --fail-on email turns it into a gate.
	if code, _, _ := runArgs("taint", tainted, "--fail-on", "email"); code != exitFail {
		t.Fatalf("taint --fail-on email exit=%d, want fail", code)
	}
	// A class that does not occur does not fail.
	if code, _, _ := runArgs("taint", tainted, "--fail-on", "ssn"); code != exitOK {
		t.Fatalf("taint --fail-on ssn exit=%d, want ok", code)
	}

	// --json carries the structured flows.
	if _, jout, _ := runArgs("taint", tainted, "--json"); !strings.Contains(jout, `"source_kind":"user-input"`) {
		t.Fatalf("taint --json:\n%s", jout)
	}

	// Clean cassette: no flow.
	clean := seedFile(t, taintTurn(`{"content":"hello"}`, "hi"))
	if code, out, _ := runArgs("taint", clean); code != exitOK || !strings.Contains(out, "no sensitive value") {
		t.Fatalf("taint clean exit=%d\n%s", code, out)
	}

	if code, _, _ := runArgs("taint"); code != exitUsage {
		t.Fatalf("taint no-arg exit=%d", code)
	}
}
