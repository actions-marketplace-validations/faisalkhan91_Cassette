package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func mergeStreamTurn(reqBody, text string) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: text, FinishReason: "end_turn"}, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			Body:    wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{Status: 200, Streaming: true,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
			Body:    wirefmt.NewBody(sse)}}
}

func TestCLI_Merge(t *testing.T) {
	a := seedFile(t, mergeStreamTurn(`{"m":1}`, "one"))
	b := seedFile(t, mergeStreamTurn(`{"m":2}`, "two"))
	out := filepath.Join(t.TempDir(), "merged.yaml")

	code, sout, _ := runArgs("merge", a, b, "-o", out)
	if code != exitOK || !strings.Contains(sout, "merged 2 interaction") {
		t.Fatalf("merge exit=%d\n%s", code, sout)
	}
	if code, _, _ := runArgs("conformance", out); code != exitOK {
		t.Fatal("merged cassette failed conformance")
	}

	// Usage error.
	if code, _, _ := runArgs("merge", a); code != exitUsage {
		t.Fatalf("merge without -o exit=%d", code)
	}

	// --strict fails when two cassettes give different responses for the same request.
	c1 := seedFile(t, mergeStreamTurn(`{"m":9}`, "one"))
	c2 := seedFile(t, mergeStreamTurn(`{"m":9}`, "two"))
	conflictOut := filepath.Join(t.TempDir(), "conflict.yaml")
	if code, sout, _ := runArgs("merge", c1, c2, "-o", conflictOut, "--strict"); code != exitFail || !strings.Contains(sout, "conflicting") {
		t.Fatalf("merge --strict on conflict exit=%d\n%s", code, sout)
	}
}

// serve <dir> serves every cassette in a directory as one endpoint.
func TestCLI_ServeDirectory(t *testing.T) {
	dir := t.TempDir()
	for i, body := range []string{`{"m":1}`, `{"m":2}`} {
		f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
			mergeStreamTurn(body, []string{"alpha", "beta"}[i])}}
		if err := wirefmt.Save(filepath.Join(dir, []string{"a.yaml", "b.yaml"}[i]), f); err != nil {
			t.Fatal(err)
		}
	}
	c, err := openForReplay(dir, cassette.MatchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(c.Handler(cassette.ServeOptions{}))
	defer srv.Close()

	// Both recorded requests resolve against the merged directory endpoint.
	for _, body := range []string{`{"m":1}`, `{"m":2}`} {
		resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d for %s", resp.StatusCode, body)
		}
		resp.Body.Close()
	}
}
