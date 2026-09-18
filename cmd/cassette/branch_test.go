package main

import (
	"path/filepath"
	"strings"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestCmdBranch_PreparesSeed(t *testing.T) {
	in := saveCass(t, "in.yaml",
		anthroTurn("m", "first", "end_turn", "", ""),
		anthroTurn("m", "second", "end_turn", "", ""),
		anthroTurn("m", "third", "end_turn", "", ""))
	out := filepath.Join(t.TempDir(), "seed.yaml")
	code, stdout, _ := runArgs("branch", in, "--from", "1", "--graft", "text=edited turn one", "-o", out)
	if code != exitOK {
		t.Fatalf("branch: %d", code)
	}
	if !strings.Contains(stdout, "determinism") || !strings.Contains(stdout, "ModeBranch") {
		t.Fatalf("branch banner/guidance missing:\n%s", stdout)
	}
	f, _ := wirefmt.Load(out)
	if len(f.Interactions) != 2 { // truncated after --from 1
		t.Fatalf("seed should have 2 interactions, got %d", len(f.Interactions))
	}
	if !strings.Contains(f.Notice, "branch seed") {
		t.Fatalf("notice missing: %q", f.Notice)
	}
	tr, _, _ := analysis.DecodeInteraction(f.Interactions[1])
	if tr.Text != "edited turn one" {
		t.Fatalf("graft not applied to seed: %q", tr.Text)
	}
}

func TestCmdBranch_Errors(t *testing.T) {
	in := saveCass(t, "in.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	out := filepath.Join(t.TempDir(), "s.yaml")
	if code, _, _ := runArgs("branch", in, "--from", "0"); code != exitUsage {
		t.Fatalf("missing -o usage: %d", code)
	}
	if code, _, _ := runArgs("branch", "--from", "0", "-o", out); code != exitUsage {
		t.Fatalf("missing input usage: %d", code)
	}
	if code, _, _ := runArgs("branch", in, "--from", "9", "-o", out); code != exitFail {
		t.Fatalf("out-of-range --from: %d", code)
	}
	if code, _, _ := runArgs("branch", "/no/such.yaml", "--from", "0", "-o", out); code != exitFail {
		t.Fatalf("bad input: %d", code)
	}
}
