package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestCLI_Lint(t *testing.T) {
	dir := t.TempDir()
	// A clean cassette.
	if err := wirefmt.Save(filepath.Join(dir, "clean.yaml"), &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{mergeStreamTurn(`{"m":1}`, "ok")}}); err != nil {
		t.Fatal(err)
	}
	// A cassette with a stale key (error).
	stale := mergeStreamTurn(`{"m":2}`, "x")
	stale.Request.MatchKey = "POST\n/v1/messages\njson:deadbeef"
	if err := wirefmt.Save(filepath.Join(dir, "stale.yaml"), &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{stale}}); err != nil {
		t.Fatal(err)
	}

	// Errors present → nonzero exit; output names the stale key.
	code, out, _ := runArgs("lint", dir)
	if code != exitFail {
		t.Fatalf("lint exit=%d, want fail\n%s", code, out)
	}
	if !strings.Contains(out, "stale_key") || !strings.Contains(out, "error(s)") {
		t.Fatalf("lint output:\n%s", out)
	}

	// A clean single file passes.
	if code, _, _ := runArgs("lint", filepath.Join(dir, "clean.yaml")); code != exitOK {
		t.Fatalf("lint clean exit=%d", code)
	}

	// --json emits findings; --strict makes a warning-only file fail.
	noFinish := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
		mergeStreamTurnNoFinish(`{"m":3}`)}}
	nf := filepath.Join(t.TempDir(), "nf.yaml")
	if err := wirefmt.Save(nf, noFinish); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runArgs("lint", nf); code != exitOK {
		t.Fatalf("lint warning-only (non-strict) exit=%d", code)
	}
	if code, out, _ := runArgs("lint", nf, "--strict", "--json"); code != exitFail || !strings.Contains(out, `"code":"no_finish"`) {
		t.Fatalf("lint --strict --json exit=%d\n%s", code, out)
	}

	// Usage error.
	if code, _, _ := runArgs("lint"); code != exitUsage {
		t.Fatalf("lint no-arg exit=%d", code)
	}
}

func mergeStreamTurnNoFinish(reqBody string) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: "hi"}, wireenc.Envelope{}) // no finish reason
	return &wirefmt.Interaction{Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			Body:    wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{Status: 200, Streaming: true,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
			Body:    wirefmt.NewBody(sse)}}
}
