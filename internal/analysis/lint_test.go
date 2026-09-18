package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestLintFile_CleanAndDirty(t *testing.T) {
	// A clean authored cassette has no findings.
	clean, _ := Compile(Screenplay{Provider: "anthropic", Turns: []ScreenplayTurn{
		{User: "hi", Text: "hello", Finish: "end_turn"}}})
	if fs := LintFile("clean.yaml", []byte("schema_version: 1\n"), clean, 0); len(fs) != 0 {
		t.Fatalf("clean cassette has findings: %+v", fs)
	}

	// A turn missing a finish reason warns.
	noFinish := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		streamIt("/v1/messages", semequal.Transcript{Text: "hi"})}}
	fs := LintFile("nf.yaml", nil, noFinish, 0)
	if !hasCode(fs, "no_finish") {
		t.Fatalf("expected no_finish warning: %+v", fs)
	}

	// A stale match key is an error.
	stale := streamIt("/v1/messages", semequal.Transcript{Text: "x", FinishReason: "end_turn"})
	stale.Request.MatchKey = "POST\n/v1/messages\njson:deadbeef"
	fs = LintFile("stale.yaml", nil, &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{stale}}, 0)
	if !hasCode(fs, "stale_key") {
		t.Fatalf("expected stale_key error: %+v", fs)
	}

	// Oversized body warns under a tiny threshold.
	big := streamIt("/v1/messages", semequal.Transcript{Text: "some longer text here", FinishReason: "end_turn"})
	fs = LintFile("big.yaml", nil, &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{big}}, 8)
	if !hasCode(fs, "oversized") {
		t.Fatalf("expected oversized warning: %+v", fs)
	}

	// Secret bytes are an error; empty file warns.
	empty := &wirefmt.File{SchemaVersion: 1}
	fs = LintFile("empty.yaml", []byte("authorization: Bearer sk-abc123"), empty, 0)
	if !hasCode(fs, "empty") || !hasCode(fs, "secret") {
		t.Fatalf("expected empty warning + secret error: %+v", fs)
	}
	errs, warns := LintSummary(fs)
	if errs == 0 || warns == 0 {
		t.Fatalf("summary should count both: errs=%d warns=%d", errs, warns)
	}
}

func hasCode(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestLintFile_NoStaleKeyForScrubbedOrVolatileBody(t *testing.T) {
	// A scrubbed request body whose stored (pre-scrub) key no longer re-derives must
	// NOT be flagged stale (the stored key is the replay authority; rekey would
	// corrupt it). Same for a body field covered by the persisted volatile spec.
	f := &wirefmt.File{
		SchemaVersion: 1,
		Match:         &wirefmt.MatchSpec{VolatileJSONPaths: []string{"nonce"}},
		Interactions: []*wirefmt.Interaction{{
			Kind: "http",
			Request: wirefmt.Request{
				Method: "POST", URL: "/v1/messages",
				Headers:  wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
				Body:     wirefmt.NewBody([]byte(`{"model":"m","note":"REDACTED","nonce":"abc"}`)),
				MatchKey: "POST\n/v1/messages\njson:prescrub",
			},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte("data: {}\n\n"))},
		}},
	}
	for _, fnd := range LintFile("x.yaml", []byte("schema_version: 1\n"), f, 0) {
		if fnd.Code == "stale_key" {
			t.Fatalf("scrubbed/volatile body must not be flagged stale_key: %+v", fnd)
		}
	}
}
