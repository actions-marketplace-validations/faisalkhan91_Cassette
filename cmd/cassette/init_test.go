package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_ScaffoldsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	// Seed an existing .gitignore + a go.mod so stack detection has something to find.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := cmdInit([]string{root}, &stdout, &stderr); code != exitOK {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr.String())
	}
	// Cassettes dir created.
	if fi, err := os.Stat(filepath.Join(root, "testdata", "cassettes")); err != nil || !fi.IsDir() {
		t.Fatalf("testdata/cassettes not created: %v", err)
	}
	// gitignore preserved its existing line and gained the miss-manifest line.
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(gi), "/bin") || !strings.Contains(string(gi), "cassette-misses.json") {
		t.Fatalf("gitignore wrong:\n%s", gi)
	}
	// Go stack detected → its snippet mentions cassettetest.New.
	if !strings.Contains(stdout.String(), "cassettetest.New") {
		t.Errorf("expected go-stack snippet; got:\n%s", stdout.String())
	}

	// Idempotent: a second run must not duplicate the gitignore line.
	var out2 bytes.Buffer
	if code := cmdInit([]string{root}, &out2, &stderr); code != exitOK {
		t.Fatalf("second init failed: %s", stderr.String())
	}
	gi2, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if n := strings.Count(string(gi2), "cassette-misses.json"); n != 1 {
		t.Fatalf("gitignore line duplicated %d times", n)
	}
	if !strings.Contains(out2.String(), "already ignores") {
		t.Errorf("second run should report the line already present; got:\n%s", out2.String())
	}
}

func TestInit_StackSnippets(t *testing.T) {
	for _, tc := range []struct{ stack, want string }{
		{"python", "OPENAI_BASE_URL"},
		{"node", "process.env"},
		{"go", "cassettetest.New"},
		{"promptfoo", "apiBaseUrl"},
		{"", "*_BASE_URL"},
	} {
		if got := initSnippet(tc.stack); !strings.Contains(got, tc.want) {
			t.Errorf("stack %q snippet missing %q:\n%s", tc.stack, tc.want, got)
		}
	}
}
