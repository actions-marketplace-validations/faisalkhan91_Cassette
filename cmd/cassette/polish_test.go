package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_StitchSplit(t *testing.T) {
	a := seedFile(t, mergeStreamTurn(`{"m":1}`, "one"))
	b := seedFile(t, mergeStreamTurn(`{"m":2}`, "two"))
	society := filepath.Join(t.TempDir(), "society.yaml")

	if code, sout, _ := runArgs("stitch", a, b, "-o", society); code != exitOK || !strings.Contains(sout, "2 interaction(s) from 2") {
		t.Fatalf("stitch exit=%d\n%s", code, sout)
	}
	if code, _, _ := runArgs("conformance", society); code != exitOK {
		t.Fatal("stitched cassette failed conformance")
	}

	outDir := filepath.Join(t.TempDir(), "parts")
	if code, sout, _ := runArgs("split", society, "--by", "provider", "-o", outDir); code != exitOK || !strings.Contains(sout, "by provider") {
		t.Fatalf("split exit=%d\n%s", code, sout)
	}
	if entries, _ := os.ReadDir(outDir); len(entries) == 0 {
		t.Fatal("split produced no files")
	}

	// Usage errors.
	if code, _, _ := runArgs("stitch"); code != exitUsage {
		t.Fatalf("stitch no-arg exit=%d", code)
	}
	if code, _, _ := runArgs("split", society, "--by", "bogus", "-o", outDir); code != exitUsage {
		t.Fatalf("split bad axis exit=%d", code)
	}
}

func TestCLI_Whatif(t *testing.T) {
	in := seedFile(t, mergeStreamTurn(`{"m":1}`, "original answer"))
	values := filepath.Join(t.TempDir(), "alts.txt")
	if err := os.WriteFile(values, []byte("first alternative\nsecond alternative\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "matrix.md")

	code, sout, serr := runArgs("whatif", in, "--turn", "0", "--slot", "text", "--values", values, "--report", report)
	if code != exitOK {
		t.Fatalf("whatif exit=%d\n%s\n%s", code, sout, serr)
	}
	if !strings.Contains(sout, "evaluated 2 alternative") || !strings.Contains(sout, "combined digest") {
		t.Fatalf("whatif stdout=%s", sout)
	}
	md, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "what-if matrix") || !strings.Contains(string(md), "first alternative") {
		t.Fatalf("report:\n%s", md)
	}

	// Usage / failure paths.
	if code, _, _ := runArgs("whatif", in, "--turn", "0"); code != exitUsage {
		t.Fatalf("whatif without --values exit=%d", code)
	}
	if code, _, _ := runArgs("whatif", in, "--turn", "9", "--values", values); code != exitFail {
		t.Fatalf("whatif out-of-range turn exit=%d", code)
	}
	// A bad graft slot (tool index out of range) is a usage error.
	if code, _, _ := runArgs("whatif", in, "--turn", "0", "--slot", "tool.9.args", "--values", values); code != exitUsage {
		t.Fatalf("whatif bad slot exit=%d", code)
	}
}

func TestCLI_StitchMissingFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.yaml")
	if code, _, _ := runArgs("stitch", "/no/such/file.yaml", "-o", out); code != exitFail {
		t.Fatalf("stitch missing-file exit=%d", code)
	}
}
