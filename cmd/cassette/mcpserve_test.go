package main

import (
	"bytes"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
)

const mcpCassette = `schema_version: 1
interactions:
  - kind: mcp
    request:
      mcp_method: tools/call
      mcp_tool: add
      body: '{"a":7,"b":8}'
    response:
      body: '{"content":[{"type":"text","text":"sum=15"}]}'
`

func TestServeMCP(t *testing.T) {
	path := writeTemp(t, mcpCassette)
	c, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // notification: no response
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":7,"b":8}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":1}}}`, // miss → error
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := serveMCP(c, in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 { // initialize, hit, miss — notification produced nothing
		t.Fatalf("got %d response lines, want 3:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "serverInfo") || !strings.Contains(lines[0], `"id":1`) {
		t.Fatalf("initialize response wrong: %s", lines[0])
	}
	if !strings.Contains(lines[1], "sum=15") || !strings.Contains(lines[1], `"id":2`) {
		t.Fatalf("tools/call response wrong: %s", lines[1])
	}
	if !strings.Contains(lines[2], "error") || !strings.Contains(lines[2], `"id":3`) {
		t.Fatalf("expected error for unmatched call: %s", lines[2])
	}
}

func TestCLI_MCPServe_Usage(t *testing.T) {
	if code, _, _ := runArgs("mcp-serve"); code != exitUsage {
		t.Fatalf("mcp-serve no-arg exit=%d", code)
	}
	if code, _, _ := runArgs("mcp-serve", "/no/such/file.yaml"); code != exitFail {
		t.Fatalf("mcp-serve missing-file exit=%d", code)
	}
}
