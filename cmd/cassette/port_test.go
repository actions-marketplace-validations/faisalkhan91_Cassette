package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestCLI_Port(t *testing.T) {
	// Author an anthropic cassette, then port it to openai-chat.
	anthro := filepath.Join(t.TempDir(), "anthro.yaml")
	if code, _, _ := runArgs("author", "testdata/screenplay.yaml", "-o", anthro); code != exitOK {
		t.Fatal("author failed")
	}
	want := decodeDigests(t, anthro)

	out := filepath.Join(t.TempDir(), "openai.yaml")
	code, sout, serr := runArgs("port", anthro, "--to", "openai-chat", "-o", out)
	if code != exitOK {
		t.Fatalf("port exit=%d\n%s\n%s", code, sout, serr)
	}
	if !strings.Contains(sout, "ported 2 turn(s) to openai-chat") {
		t.Fatalf("port stdout=%s", sout)
	}

	// The ported cassette replays and preserves the per-turn semantic digests.
	if code, _, _ := runArgs("conformance", out); code != exitOK {
		t.Fatal("ported cassette failed conformance")
	}
	got := decodeDigests(t, out)
	if len(got) != len(want) {
		t.Fatalf("turn count changed: %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("turn %d digest changed across port", i)
		}
	}

	// Porting to the same dialect transpiles nothing.
	same := filepath.Join(t.TempDir(), "same.yaml")
	if code, sout, _ := runArgs("port", anthro, "--to", "anthropic", "-o", same); code != exitOK || !strings.Contains(sout, "nothing transpiled") {
		t.Fatalf("port same-dialect exit=%d\n%s", code, sout)
	}

	// Usage errors.
	if code, _, _ := runArgs("port", anthro); code != exitUsage {
		t.Fatalf("port without --to exit=%d", code)
	}
	if code, _, _ := runArgs("port", anthro, "--to", "bogus"); code != exitUsage {
		t.Fatalf("port bad provider exit=%d", code)
	}
}

func decodeDigests(t *testing.T, path string) []string {
	t.Helper()
	f, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tr := range analysis.CollectTurns(f) {
		out = append(out, tr.Digest())
	}
	return out
}
