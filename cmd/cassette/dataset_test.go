package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func datasetDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cdir := filepath.Join(dir, "testdata", "cassettes")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A weather tool call with a system prompt + user text + offered tool.
	body := []byte(`{"model":"claude-opus-4-6","system":"be terse","tools":[{"name":"get_weather"}],"messages":[{"role":"user","content":"weather in Paris?"}]}`)
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody(body)},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)}},
	}}
	if err := wirefmt.Save(filepath.Join(cdir, "w.yaml"), f); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCLI_Dataset(t *testing.T) {
	dir := datasetDir(t)

	// jsonl (default).
	code, out, _ := runArgs("dataset", dir)
	if code != exitOK || !strings.Contains(out, `"model":"claude-opus-4-6"`) || !strings.Contains(out, "get_weather") {
		t.Fatalf("dataset jsonl: %d\n%s", code, out)
	}
	if !strings.Contains(out, `"user_text":"weather in Paris?"`) {
		t.Fatalf("user_text missing:\n%s", out)
	}
	// table + csv formats.
	if _, o, _ := runArgs("dataset", dir, "--format", "table"); !strings.Contains(o, "rows)") {
		t.Fatalf("table format:\n%s", o)
	}
	if _, o, _ := runArgs("dataset", dir, "--format", "csv"); !strings.Contains(o, "cassette,index,model") {
		t.Fatalf("csv header missing:\n%s", o)
	}
	// --where filters.
	if _, o, _ := runArgs("dataset", dir, "--where", "model=nope"); strings.Contains(o, "get_weather") {
		t.Fatalf("where should filter everything out:\n%s", o)
	}
	if _, o, _ := runArgs("dataset", dir, "--where", "tool=get_weather"); !strings.Contains(o, "get_weather") {
		t.Fatalf("where tool should match:\n%s", o)
	}
	// bad format / bad where.
	if code, _, _ := runArgs("dataset", dir, "--format", "xml"); code != exitUsage {
		t.Fatalf("bad format: %d", code)
	}
	if code, _, _ := runArgs("dataset", dir, "--where", "noeq"); code != exitUsage {
		t.Fatalf("bad where: %d", code)
	}
}

func TestCLI_Gitconfig(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	code, out, _ := runArgs("gitconfig", "--install", dir)
	if code != exitOK || !strings.Contains(out, "textconv") {
		t.Fatalf("gitconfig install: %d\n%s", code, out)
	}
	ga, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if !strings.Contains(string(ga), "diff=cassette") {
		t.Fatalf(".gitattributes not written: %q", ga)
	}
	// Idempotent: second install doesn't duplicate the line.
	runArgs("gitconfig", "--install", dir)
	ga2, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if strings.Count(string(ga2), "diff=cassette") != 1 {
		t.Fatalf("install not idempotent: %q", ga2)
	}
	// Non-repo dir → fail.
	if code, _, _ := runArgs("gitconfig", "--install", t.TempDir()); code != exitFail {
		t.Fatalf("non-repo should fail: %d", code)
	}
	// No --install → usage.
	if code, _, _ := runArgs("gitconfig"); code != exitUsage {
		t.Fatalf("no --install usage: %d", code)
	}
}
