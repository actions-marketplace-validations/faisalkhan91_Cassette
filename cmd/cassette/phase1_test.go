package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func toolUseFixture(t *testing.T) string {
	t.Helper()
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Body: wirefmt.NewBody([]byte(`{"model":"claude","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)},
	}}}
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := wirefmt.Save(p, f); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runArgs("rekey", p); code != exitOK { // populate match keys like a real recording
		t.Fatal("rekey fixture failed")
	}
	return p
}

func TestCLI_Explain(t *testing.T) {
	p := toolUseFixture(t)
	code, out, _ := runArgs("explain", p, "--turn", "0")
	if code != exitOK {
		t.Fatalf("explain: %d", code)
	}
	for _, want := range []string{"match key", "POST /v1/messages", "canonicalized request body", "get_weather", "wire shape"} {
		if !strings.Contains(out, want) {
			t.Fatalf("explain output missing %q:\n%s", want, out)
		}
	}
	// --raw dumps frames.
	if _, rawOut, _ := runArgs("explain", p, "--turn", "0", "--raw"); !strings.Contains(rawOut, "raw frames") {
		t.Fatalf("--raw should dump frames:\n%s", rawOut)
	}
	if code, _, _ := runArgs("explain", p, "--turn", "9"); code != exitFail {
		t.Fatalf("out-of-range turn should fail: %d", code)
	}
	if code, _, _ := runArgs("explain"); code != exitUsage {
		t.Fatalf("no path should be usage: %d", code)
	}
	// An MCP turn renders its method/tool/result.
	mp := filepath.Join(t.TempDir(), "m.yaml")
	mf := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind:     "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/call", MCPTool: "adder"},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"sum":3}`))},
	}}}
	if err := wirefmt.Save(mp, mf); err != nil {
		t.Fatal(err)
	}
	if _, mo, _ := runArgs("explain", mp, "--turn", "0"); !strings.Contains(mo, "adder") || !strings.Contains(mo, "sum") {
		t.Fatalf("mcp explain missing method/result:\n%s", mo)
	}
}

func TestCLI_Expect(t *testing.T) {
	p := toolUseFixture(t)
	// No contract yet → assert with no flags is a trivial pass.
	if code, _, _ := runArgs("expect", p); code != exitOK {
		t.Fatalf("expect show (empty): %d", code)
	}
	// Set a contract that the recording VIOLATES (it calls get_weather).
	if code, _, _ := runArgs("expect", p, "--no-tool", "get_weather"); code != exitOK {
		t.Fatalf("expect set: %d", code)
	}
	f, err := wirefmt.Load(p)
	if err != nil || f.Expect == nil || len(f.Expect.NoTools) != 1 || f.Expect.NoTools[0] != "get_weather" {
		t.Fatalf("contract not persisted: %+v err=%v", f.Expect, err)
	}
	// Showing the contract (no flags) prints it.
	if _, show, _ := runArgs("expect", p); !strings.Contains(show, "get_weather") {
		t.Fatalf("expect show should print contract:\n%s", show)
	}
	// assert with no flags now runs the embedded contract → fails (get_weather present).
	if code, _, _ := runArgs("assert", p); code != exitFail {
		t.Fatalf("embedded contract should fail: %d", code)
	}
	// Clear it → assert passes again.
	if code, _, _ := runArgs("expect", p, "--clear"); code != exitOK {
		t.Fatalf("expect clear: %d", code)
	}
	if f, _ := wirefmt.Load(p); f.Expect != nil {
		t.Fatal("contract not cleared")
	}
	if code, _, _ := runArgs("assert", p); code != exitOK {
		t.Fatalf("assert after clear should pass: %d", code)
	}
	// All-flag contract exercises every printExpect branch.
	if code, o, _ := runArgs("expect", p, "--no-duplicate-tools", "--finishes-clean"); code != exitOK ||
		!strings.Contains(o, "no-duplicate-tools") || !strings.Contains(o, "finishes-clean") {
		t.Fatalf("expect all-flags: %d\n%s", code, o)
	}
	if code, _, _ := runArgs("expect"); code != exitUsage {
		t.Fatalf("expect no-path usage: %d", code)
	}
	// Refuse to write when the cassette carries a secret.
	sec := filepath.Join(t.TempDir(), "s.yaml")
	os.WriteFile(sec, []byte("schema_version: 1\ninteractions:\n  - kind: http\n    request: {method: POST, url: /v1/x, body: 'authorization Bearer sk-secretkey1234567890'}\n    response: {status: 200, body: '{}'}\n"), 0o644)
	if code, _, errOut := runArgs("expect", sec, "--finishes-clean"); code != exitFail || !strings.Contains(errOut, "refusing to write") {
		t.Fatalf("expect should refuse secrets: %d\n%s", code, errOut)
	}
}

func TestCLI_Orphans(t *testing.T) {
	dir := t.TempDir()
	cdir := filepath.Join(dir, "testdata", "cassettes")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A test file referencing New(t,"foo") and an implicit New(t,"") in TestBar.
	testGo := `package x
import "testing"
func TestBar(t *testing.T) { c := New(t, ""); _ = c }
func TestFoo(t *testing.T) { c := New(t, "foo"); _ = c }
func TestMiss(t *testing.T) { c := New(t, "ghost"); _ = c }
`
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(testGo), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo", "TestBar", "deadweight"} {
		if err := os.WriteFile(filepath.Join(cdir, name+".yaml"), []byte("schema_version: 1\ninteractions: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := runArgs("orphans", dir)
	if code != exitFail { // deadweight is an orphan + ghost is missing
		t.Fatalf("orphans should flag issues: %d\n%s", code, out)
	}
	if !strings.Contains(out, "deadweight") {
		t.Fatalf("expected deadweight flagged orphan:\n%s", out)
	}
	if !strings.Contains(out, "ghost") {
		t.Fatalf("expected ghost flagged missing:\n%s", out)
	}
	if strings.Contains(out, "foo.yaml") || strings.Contains(out, "TestBar.yaml") {
		t.Fatalf("referenced fixtures should not be flagged:\n%s", out)
	}

	// Clean dir (every fixture referenced, none missing) → exit 0.
	clean := t.TempDir()
	cc := filepath.Join(clean, "testdata", "cassettes")
	os.MkdirAll(cc, 0o755)
	os.WriteFile(filepath.Join(clean, "y_test.go"), []byte("package y\nimport \"testing\"\nfunc TestA(t *testing.T){ New(t,\"a\") }\n"), 0o644)
	os.WriteFile(filepath.Join(cc, "a.yaml"), []byte("schema_version: 1\ninteractions: []\n"), 0o644)
	if code, o, _ := runArgs("orphans", clean); code != exitOK || !strings.Contains(o, "no orphans") {
		t.Fatalf("clean dir should pass: %d\n%s", code, o)
	}
	if code, _, _ := runArgs("orphans", "--bad"); code != exitUsage {
		t.Fatalf("bad flag should be usage: %d", code)
	}
}
