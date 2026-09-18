package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	cassette "github.com/faisalkhan91/cassette"
	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// TestMCPConformanceCorpus compiles the committed MCP screenplay corpus, proves it
// passes key-integrity conformance, replays it through mcp-serve (tools/list, a
// successful tools/call, and a tool-error), and decodes each interaction — the
// authored→replay loop for MCP, fully offline.
func TestMCPConformanceCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/mcp/calculator.screenplay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var sp analysis.Screenplay
	if err := yaml.Unmarshal(raw, &sp); err != nil {
		t.Fatalf("parse screenplay: %v", err)
	}
	f, err := analysis.Compile(sp)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(f.Interactions) != 3 {
		t.Fatalf("compiled %d interactions, want 3", len(f.Interactions))
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "calculator.yaml")
	var stderr bytes.Buffer
	if code := saveScrubbed(path, f, &stderr); code != exitOK {
		t.Fatalf("saveScrubbed: %d %s", code, stderr.String())
	}

	// Key-integrity conformance: every authored MCP key must re-derive from its body.
	rep, err := cassette.Conformance(path)
	if err != nil {
		t.Fatalf("conformance: %v", err)
	}
	if !rep.OK() || rep.Bad != 0 {
		t.Fatalf("authored MCP corpus failed conformance: bad=%d dials=%d", rep.Bad, rep.Dials)
	}

	// Replay through mcp-serve with the same calls a client would make.
	c, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	reqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"divide","arguments":{"a":1,"b":0}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := serveMCP(c, strings.NewReader(reqs), &out); err != nil {
		t.Fatalf("serveMCP: %v", err)
	}
	got := out.String()
	for _, want := range []string{`"name":"add"`, `"name":"divide"`, `"text":"5"`, `division by zero`, `"isError":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("replay output missing %q; got:\n%s", want, got)
		}
	}

	// Every interaction decodes to a renderable transcript via the analysis router.
	for i, it := range f.Interactions {
		if _, _, renderable := analysis.DecodeInteraction(it); !renderable {
			t.Errorf("interaction %d (%s) did not decode to a renderable transcript", i, it.Request.MCPMethod)
		}
	}
}
