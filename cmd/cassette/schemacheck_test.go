package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// mcpSchemaCorpus builds a tools/list advertising an "add" schema plus one
// tools/call whose args either conform or not.
func mcpSchemaCorpus(callArgs string) *wirefmt.File {
	list := &wirefmt.Interaction{Kind: "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/list"},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"tools":[{"name":"add","inputSchema":{"type":"object","required":["a","b"],"properties":{"a":{"type":"integer"},"b":{"type":"integer"}}}}]}`))}}
	call := &wirefmt.Interaction{Kind: "mcp",
		Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(callArgs))}}
	return &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{list, call}}
}

func TestCmdSchemaCheck(t *testing.T) {
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.yaml")
	badPath := filepath.Join(dir, "bad.yaml")
	if err := wirefmt.Save(okPath, mcpSchemaCorpus(`{"a":1,"b":2}`)); err != nil {
		t.Fatal(err)
	}
	if err := wirefmt.Save(badPath, mcpSchemaCorpus(`{"a":1}`)); err != nil { // missing required "b"
		t.Fatal(err)
	}

	// Conforming corpus → exit OK, "conform" in text output.
	var out, errb bytes.Buffer
	if code := cmdSchemaCheck([]string{okPath}, &out, &errb); code != exitOK {
		t.Fatalf("conforming: code=%d stderr=%s", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("conform")) {
		t.Errorf("expected a conformance line; got:\n%s", out.String())
	}

	// Conforming corpus, --json → exit OK, parseable JSON with checked>=1.
	out.Reset()
	if code := cmdSchemaCheck([]string{okPath, "--json"}, &out, &errb); code != exitOK {
		t.Fatalf("conforming --json: code=%d", code)
	}
	var rep struct {
		Checked    int   `json:"Checked"`
		Violations []any `json:"Violations"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", err, out.String())
	}
	if rep.Checked < 1 {
		t.Errorf("expected checked>=1, got %d", rep.Checked)
	}

	// Violating corpus → exit fail, names the offending tool.
	out.Reset()
	if code := cmdSchemaCheck([]string{badPath}, &out, &errb); code != exitFail {
		t.Fatalf("violating: code=%d want exitFail; out=%s", code, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("add")) {
		t.Errorf("expected the offending tool 'add' in output; got:\n%s", out.String())
	}

	// Violating corpus, --json → exit fail.
	out.Reset()
	if code := cmdSchemaCheck([]string{badPath, "--json"}, &out, &errb); code != exitFail {
		t.Fatalf("violating --json: code=%d want exitFail", code)
	}

	// Usage + load errors.
	if code := cmdSchemaCheck(nil, &out, &errb); code != exitUsage {
		t.Errorf("no args: code=%d want exitUsage", code)
	}
	if code := cmdSchemaCheck([]string{"a", "b"}, &out, &errb); code != exitUsage {
		t.Errorf("too many args: code=%d want exitUsage", code)
	}
	if code := cmdSchemaCheck([]string{filepath.Join(dir, "nope.yaml")}, &out, &errb); code != exitFail {
		t.Errorf("missing file: code=%d want exitFail", code)
	}
}
