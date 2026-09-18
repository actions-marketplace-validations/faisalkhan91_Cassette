package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/semequal"
)

// TestMigrate_PreservesDigests migrates an authored anthropic corpus to the
// openai-chat dialect and asserts the command succeeds and every migrated cassette
// still decodes to the same per-turn behavior.
func TestMigrate_PreservesDigests(t *testing.T) {
	corpus := t.TempDir()
	compileScreenplayTo(t, "testdata/screenplay.yaml", filepath.Join(corpus, "helper.yaml"))

	outDir := filepath.Join(t.TempDir(), "migrated")
	var stdout, stderr bytes.Buffer
	if code := cmdMigrate([]string{corpus, "--to", "openai-chat", "-o", outDir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("migrate failed: code=%d\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "digests preserved") {
		t.Errorf("expected per-file 'digests preserved' report; got:\n%s", stdout.String())
	}

	// Source and migrated corpus must carry identical per-turn digests.
	before := loadTurns(t, filepath.Join(corpus, "helper.yaml"))
	after := loadTurns(t, filepath.Join(outDir, "helper.yaml"))
	if d := digestDrift(before, after); d != "" {
		t.Fatalf("migrated corpus drifted: %s", d)
	}
}

// TestMigrate_DriftFailsLoudly verifies the safety net: when a migrated turn's
// behavior differs, the drift detector reports it (the command then exits nonzero).
func TestMigrate_DriftFailsLoudly(t *testing.T) {
	a := mkTranscript("hello")
	b := mkTranscript("hello")
	if d := digestDrift([]semequal.Transcript{a}, []semequal.Transcript{b}); d != "" {
		t.Fatalf("identical transcripts should not drift, got %q", d)
	}
	c := mkTranscript("HELLO — different")
	if d := digestDrift([]semequal.Transcript{a}, []semequal.Transcript{c}); d == "" {
		t.Fatal("changed transcript must be reported as drift")
	}
	if d := digestDrift([]semequal.Transcript{a}, []semequal.Transcript{a, b}); d == "" {
		t.Fatal("turn-count change must be reported as drift")
	}
}

func mkTranscript(text string) semequal.Transcript {
	return semequal.Transcript{Role: "assistant", Text: text}
}

func loadTurns(t *testing.T, path string) []semequal.Transcript {
	t.Helper()
	f, ok := loadOrErr(path, &bytes.Buffer{})
	if !ok {
		t.Fatalf("load %s", path)
	}
	return analysis.CollectTurns(f)
}

func TestMigrate_CommandErrors(t *testing.T) {
	dir := t.TempDir()
	compileScreenplayTo(t, "testdata/screenplay.yaml", filepath.Join(dir, "a.yaml"))
	var out, errb bytes.Buffer

	// Invalid --to dialect → usage error.
	if code := cmdMigrate([]string{dir, "--to", "bogus", "-o", filepath.Join(t.TempDir(), "x")}, &out, &errb); code != exitUsage {
		t.Errorf("invalid --to: code=%d want exitUsage", code)
	}
	// Missing required flags → usage.
	if code := cmdMigrate([]string{dir}, &out, &errb); code != exitUsage {
		t.Errorf("no --to/-o: code=%d want exitUsage", code)
	}
	// Nonexistent source → fail.
	if code := cmdMigrate([]string{filepath.Join(dir, "nope"), "--to", "openai-chat", "-o", filepath.Join(t.TempDir(), "o")}, &out, &errb); code != exitFail {
		t.Errorf("missing source: code=%d want exitFail", code)
	}
}
