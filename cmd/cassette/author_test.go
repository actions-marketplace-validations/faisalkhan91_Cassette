package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestCLI_Author_Errors(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "o.yaml")

	// Malformed YAML → parse error, nonzero exit.
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("provider: anthropic\nturns: [oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, serr := runArgs("author", bad, "-o", out); code != exitFail || !strings.Contains(serr, "parse screenplay") {
		t.Fatalf("malformed yaml: code=%d serr=%s", code, serr)
	}

	// Valid YAML but unknown provider → Compile error, nonzero exit.
	badProv := filepath.Join(dir, "prov.yaml")
	if err := os.WriteFile(badProv, []byte("provider: nope\nturns:\n  - user: hi\n    text: ok\n    finish: end_turn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runArgs("author", badProv, "-o", out); code != exitFail {
		t.Fatalf("unknown provider: code=%d want exitFail", code)
	}
}

func TestCLI_Author(t *testing.T) {
	out := filepath.Join(t.TempDir(), "fixture.yaml")
	code, sout, serr := runArgs("author", "testdata/screenplay.yaml", "-o", out)
	if code != exitOK {
		t.Fatalf("author exit=%d\nstdout=%s\nstderr=%s", code, sout, serr)
	}
	if !strings.Contains(sout, "authored 2 turn(s)") || !strings.Contains(sout, "0 dials") {
		t.Fatalf("author stdout=%s", sout)
	}

	// The authored cassette must replay (conformance) and codec-verify cleanly.
	if code, _, _ := runArgs("conformance", out); code != exitOK {
		t.Fatalf("authored cassette failed conformance")
	}
	if code, _, _ := runArgs("codec", "verify", out); code != exitOK {
		t.Fatalf("authored cassette failed codec verify")
	}

	// And it decodes to the intended transcripts.
	f, err := wirefmt.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	turns := analysis.CollectTurns(f)
	if len(turns) != 2 || len(turns[0].ToolCalls) != 1 || turns[0].ToolCalls[0].Name != "get_weather" {
		t.Fatalf("authored turns wrong: %+v", turns)
	}

	// Usage errors.
	if code, _, _ := runArgs("author", "testdata/screenplay.yaml"); code != exitUsage {
		t.Fatalf("author without -o exit=%d", code)
	}
	if code, _, _ := runArgs("author"); code != exitUsage {
		t.Fatalf("author no-arg exit=%d", code)
	}
	if code, _, _ := runArgs("author", "/no/such/file.yaml", "-o", out); code != exitFail {
		t.Fatalf("author missing-input exit=%d", code)
	}
}
