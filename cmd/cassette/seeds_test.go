package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func seedTurn(url, body string) *wirefmt.Interaction {
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: url, Body: wirefmt.NewBody([]byte(body))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte("data: {}\n\n"))}}
}

func seedFile(t *testing.T, its ...*wirefmt.Interaction) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := wirefmt.Save(p, &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: its}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_Seeds(t *testing.T) {
	mixed := seedFile(t,
		seedTurn("/v1/chat/completions", `{"seed":7}`),  // reproducible
		seedTurn("/v1/messages", `{"temperature":0.7}`)) // nondeterministic
	code, out, _ := runArgs("seeds", mixed)
	if code != exitOK { // no --strict → warn + exit 0
		t.Fatalf("seeds exit: %d", code)
	}
	for _, want := range []string{"reproducible", "nondeterministic", "1/2 turns reproducible", "note:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("seeds output missing %q:\n%s", want, out)
		}
	}
	// --strict fails on the nondeterministic turn.
	if code, _, _ := runArgs("seeds", mixed, "--strict"); code != exitFail {
		t.Fatalf("--strict should fail on nondeterministic: %d", code)
	}
	// All-reproducible passes even with --strict.
	allRepro := seedFile(t, seedTurn("/v1/chat/completions", `{"seed":1}`))
	if code, _, _ := runArgs("seeds", allRepro, "--strict"); code != exitOK {
		t.Fatalf("all-reproducible --strict should pass: %d", code)
	}
	// --json emits per-turn records.
	if _, jo, _ := runArgs("seeds", mixed, "--json"); !strings.Contains(jo, `"reproducible":true`) || !strings.Contains(jo, `"provider":"anthropic"`) {
		t.Fatalf("json output:\n%s", jo)
	}
	// MCP-only → no http turns.
	mcp := seedFile(t, &wirefmt.Interaction{Kind: "mcp", Response: wirefmt.Response{Body: wirefmt.NewBody([]byte("{}"))}})
	if code, o, _ := runArgs("seeds", mcp); code != exitOK || !strings.Contains(o, "no http turns") {
		t.Fatalf("mcp-only seeds: %d\n%s", code, o)
	}
	if code, _, _ := runArgs("seeds"); code != exitUsage {
		t.Fatalf("seeds no-path usage: %d", code)
	}
	if code, _, _ := runArgs("seeds", "/no/such.yaml"); code != exitFail {
		t.Fatalf("seeds bad path: %d", code)
	}
}
