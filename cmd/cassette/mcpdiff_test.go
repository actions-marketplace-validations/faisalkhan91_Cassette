package main

import (
	"strings"
	"testing"
)

const mcpGolden = `schema_version: 1
interactions:
  - kind: mcp
    request:
      mcp_method: tools/list
      body: '{}'
    response:
      body: '{"tools":[{"name":"add","inputSchema":{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}}]}'
`

// A removed tool is breaking: --fail-on-breaking must exit nonzero; without it,
// mcp-diff is informational (exit 0).
func TestCLI_MCPDiff_FailOnBreaking(t *testing.T) {
	old := writeTemp(t, mcpGolden)
	newer := writeTemp(t, `schema_version: 1
interactions:
  - kind: mcp
    request:
      mcp_method: tools/list
      body: '{}'
    response:
      body: '{"tools":[]}'
`)

	code, stdout, _ := runArgs("mcp-diff", old, newer)
	if code != exitOK {
		t.Fatalf("without --fail-on-breaking, mcp-diff is informational: code=%d", code)
	}
	if !strings.Contains(stdout, "breaking") || !strings.Contains(stdout, "tool_removed") {
		t.Fatalf("expected a breaking tool_removed in output:\n%s", stdout)
	}
	code, _, _ = runArgs("mcp-diff", old, newer, "--fail-on-breaking")
	if code != exitFail {
		t.Fatalf("--fail-on-breaking must exit nonzero on a breaking change: code=%d", code)
	}
}

// Identical contracts: clean, exit 0 even under the gate.
func TestCLI_MCPDiff_Clean(t *testing.T) {
	g := writeTemp(t, mcpGolden)
	code, stdout, _ := runArgs("mcp-diff", g, g, "--fail-on-breaking")
	if code != exitOK || !strings.Contains(stdout, "no MCP tool-contract changes") {
		t.Fatalf("identical recordings should pass clean: code=%d out=%s", code, stdout)
	}
}

func TestCLI_MCPDiff_Usage(t *testing.T) {
	if code, _, _ := runArgs("mcp-diff", "only-one.yaml"); code != exitUsage {
		t.Fatalf("mcp-diff with one arg: code=%d want exitUsage", code)
	}
}
