package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestCLI_Fuzz(t *testing.T) {
	golden := seedFile(t, mergeStreamTurn(`{"m":1}`, "the quick brown fox"))
	out := filepath.Join(t.TempDir(), "variants")

	code, sout, serr := runArgs("fuzz", golden, "--turn", "0", "--reframings", "12", "--seed", "5", "-o", out)
	if code != exitOK {
		t.Fatalf("fuzz exit=%d\n%s\n%s", code, sout, serr)
	}
	if !strings.Contains(sout, "12 valid re-framing") {
		t.Fatalf("fuzz stdout=%s", sout)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 12 {
		t.Fatalf("wrote %d files, want 12", len(entries))
	}

	// Every variant replays AND decodes to the same transcript as the golden turn.
	gold, _ := wirefmt.Load(golden)
	want := analysis.CollectTurns(gold)[0].Digest()
	for _, e := range entries {
		p := filepath.Join(out, e.Name())
		if code, _, _ := runArgs("conformance", p); code != exitOK {
			t.Fatalf("variant %s failed conformance", e.Name())
		}
		f, _ := wirefmt.Load(p)
		if got := analysis.CollectTurns(f)[0].Digest(); got != want {
			t.Fatalf("variant %s digest drift", e.Name())
		}
	}

	// Usage errors.
	if code, _, _ := runArgs("fuzz", golden); code != exitUsage {
		t.Fatalf("fuzz without -o exit=%d", code)
	}
	if code, _, _ := runArgs("fuzz", golden, "--turn", "9", "-o", out); code != exitFail {
		t.Fatalf("fuzz bad turn exit=%d", code)
	}
}
