package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func noisyTurn() *wirefmt.Interaction {
	tr := semequal.Transcript{Text: "hello", FinishReason: "end_turn"}
	noisy := wireenc.Encode(wireenc.Anthropic, tr, wireenc.Envelope{MessageID: "msg_live_7", ModelID: "claude-x"})
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"stream":true}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(noisy)}}
}

func TestCLI_Canonicalize(t *testing.T) {
	src := seedFile(t, noisyTurn())

	// --check fails on a non-canonical file.
	if code, _, errOut := runArgs("canonicalize", src, "--check"); code != exitFail {
		t.Fatalf("check on non-canonical exit=%d\n%s", code, errOut)
	}

	// Write a canonicalized copy to -o.
	out := filepath.Join(t.TempDir(), "canon.yaml")
	code, sout, _ := runArgs("canonicalize", src, "-o", out)
	if code != exitOK || !strings.Contains(sout, "canonicalized 1") {
		t.Fatalf("canonicalize -o exit=%d\n%s", code, sout)
	}

	// The output is now canonical (idempotent), and re-canonicalizing is a no-op.
	if code, _, _ := runArgs("canonicalize", out, "--check"); code != exitOK {
		t.Fatalf("check on canonical output exit=%d", code)
	}
	if code, sout, _ := runArgs("canonicalize", out); code != exitOK || !strings.Contains(sout, "already canonical") {
		t.Fatalf("re-canonicalize exit=%d\n%s", code, sout)
	}

	// Usage error.
	if code, _, _ := runArgs("canonicalize"); code != exitUsage {
		t.Fatalf("canonicalize no-arg exit=%d", code)
	}
}
