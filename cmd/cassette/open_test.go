package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// open renders the offline HTML report and prints the transcript; --no-open keeps it
// headless. The HTML is self-contained (no external asset refs).
func TestCLI_Open_RendersAndPrints(t *testing.T) {
	cass := writeTemp(t, validCassette)
	out := filepath.Join(t.TempDir(), "report.html")

	code, stdout, stderr := runArgs("open", cass, "-o", out, "--no-open")
	if code != exitOK {
		t.Fatalf("open: code=%d stderr=%s", code, stderr)
	}
	html, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, "<html") || strings.Contains(s, "http://") && strings.Contains(s, "<script src=") {
		t.Fatalf("report is not self-contained HTML:\n%.200s", s)
	}
	if !strings.Contains(stdout, "report:") || !strings.Contains(stdout, out) {
		t.Fatalf("stdout missing report path: %s", stdout)
	}
}

func TestCLI_Open_Usage(t *testing.T) {
	if code, _, _ := runArgs("open"); code != exitUsage {
		t.Fatalf("open with no args: code=%d want exitUsage", code)
	}
}
