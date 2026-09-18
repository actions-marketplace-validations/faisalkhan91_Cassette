package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func writeCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]*wirefmt.Interaction{
		"a.yaml": mergeStreamTurn(`{"m":1}`, "one"),
		"b.yaml": mergeStreamTurn(`{"m":1}`, "one"),  // duplicate behavior of a
		"c.yaml": mergeStreamTurnNoFinish(`{"m":2}`), // distinct (no finish)
	}
	for name, it := range files {
		if err := wirefmt.Save(filepath.Join(dir, name), &wirefmt.File{
			SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{it}}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCLI_Coverage(t *testing.T) {
	dir := writeCorpus(t)
	code, out, _ := runArgs("coverage", dir)
	if code != exitOK || !strings.Contains(out, "coverage") || !strings.Contains(out, "finish") {
		t.Fatalf("coverage exit=%d\n%s", code, out)
	}
	// --require a present cell passes; a missing one fails.
	if code, _, _ := runArgs("coverage", dir, "--require", "finish=end_turn"); code != exitOK {
		t.Fatalf("coverage require present exit=%d", code)
	}
	if code, out, _ := runArgs("coverage", dir, "--require", "tool=nonexistent"); code != exitFail || !strings.Contains(out, "missing required") {
		t.Fatalf("coverage require missing exit=%d\n%s", code, out)
	}
	// json format.
	if _, jout, _ := runArgs("coverage", dir, "--format", "json"); !strings.Contains(jout, `"turns":3`) {
		t.Fatalf("coverage json:\n%s", jout)
	}
}

func TestCLI_Shrink(t *testing.T) {
	dir := writeCorpus(t)
	out := filepath.Join(t.TempDir(), "kept")
	code, sout, _ := runArgs("shrink", dir, "-o", out)
	if code != exitOK || !strings.Contains(sout, "kept 2 of 3") {
		t.Fatalf("shrink exit=%d\n%s", code, sout)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept dir has %d files, want 2", len(entries))
	}

	// Without -o, lists the kept paths.
	if code, sout, _ := runArgs("shrink", dir); code != exitOK || !strings.Contains(sout, ".yaml") {
		t.Fatalf("shrink list exit=%d\n%s", code, sout)
	}
}
