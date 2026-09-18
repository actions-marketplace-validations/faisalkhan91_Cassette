package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// compileScreenplayTo compiles a screenplay file into a cassette at dst (test helper).
func compileScreenplayTo(t *testing.T, screenplay, dst string) {
	t.Helper()
	raw, err := os.ReadFile(screenplay)
	if err != nil {
		t.Fatal(err)
	}
	var sp analysis.Screenplay
	if err := yaml.Unmarshal(raw, &sp); err != nil {
		t.Fatal(err)
	}
	f, err := analysis.Compile(sp)
	if err != nil {
		t.Fatalf("compile %s: %v", screenplay, err)
	}
	var stderr bytes.Buffer
	if code := saveScrubbed(dst, f, &stderr); code != exitOK {
		t.Fatalf("save %s: %s", dst, stderr.String())
	}
}

func TestDashboard(t *testing.T) {
	corpus := t.TempDir()
	compileScreenplayTo(t, "testdata/screenplay.yaml", filepath.Join(corpus, "http.yaml"))
	compileScreenplayTo(t, "testdata/mcp/calculator.screenplay.yaml", filepath.Join(corpus, "mcp.yaml"))

	out := filepath.Join(t.TempDir(), "report.html")
	var stdout, stderr bytes.Buffer
	if code := cmdDashboard([]string{corpus, "-o", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("dashboard failed: code=%d stderr=%s", code, stderr.String())
	}
	html, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)

	// Structural sections must be present.
	for _, want := range []string{
		"<title>cassette — corpus report</title>",
		"<h2>Coverage</h2>",
		"<h2>Cassettes</h2>",
		"<h2>Transcripts</h2>",
		"http.yaml", "mcp.yaml", // both cassettes listed
		"no external assets",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q", want)
		}
	}
	// Self-contained: no external resource references.
	for _, bad := range []string{"<script", "src=", "http://", "https://"} {
		if strings.Contains(got, bad) {
			t.Errorf("report references external/dynamic resource %q (must be self-contained)", bad)
		}
	}

	// Deterministic: a second render of the same corpus is byte-identical.
	out2 := filepath.Join(t.TempDir(), "report2.html")
	if code := cmdDashboard([]string{corpus, "-o", out2}, &bytes.Buffer{}, &stderr); code != exitOK {
		t.Fatalf("second dashboard failed: %s", stderr.String())
	}
	html2, _ := os.ReadFile(out2)
	if !bytes.Equal(html, html2) {
		t.Error("dashboard output is not byte-stable across runs for a fixed corpus")
	}
}
