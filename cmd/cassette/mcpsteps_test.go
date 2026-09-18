package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const mcpSession = `schema_version: 1
interactions:
  - kind: mcp
    request: { mcp_method: tools/list, body: '{}' }
    response:
      body: '{"tools":[{"name":"add"},{"name":"divide"}]}'
  - kind: mcp
    request: { mcp_method: tools/call, mcp_tool: add, body: '{"a":2,"b":3}' }
    response:
      body: '{"content":[{"type":"text","text":"5"}]}'
  - kind: mcp
    request: { mcp_method: tools/call, mcp_tool: divide, body: '{"a":1,"b":0}' }
    response:
      body: '{"isError":true,"content":[{"type":"text","text":"division by zero"}]}'
`

func TestCLI_MCPSteps_WalksSession(t *testing.T) {
	cass := writeTemp(t, mcpSession)

	code, stdout, stderr := runArgs("mcp-steps", cass)
	if code != exitOK {
		t.Fatalf("mcp-steps: code=%d stderr=%s", code, stderr)
	}
	for _, want := range []string{"tools/list", "add, divide", "tools/call", `args:`, "5", "division by zero"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("mcp-steps output missing %q:\n%s", want, stdout)
		}
	}

	// JSON: the divide step is flagged is_error.
	code, jout, _ := runArgs("mcp-steps", cass, "--json")
	if code != exitOK {
		t.Fatalf("mcp-steps --json: code=%d", code)
	}
	var steps []struct {
		Method  string `json:"method"`
		Tool    string `json:"tool"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(jout), &steps); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, jout)
	}
	if len(steps) != 3 || !steps[2].IsError || steps[2].Tool != "divide" {
		t.Fatalf("expected 3 steps with the divide step errored: %+v", steps)
	}
}

func TestCLI_MCPSteps_Usage(t *testing.T) {
	if code, _, _ := runArgs("mcp-steps"); code != exitUsage {
		t.Fatalf("mcp-steps no args: code=%d want exitUsage", code)
	}
}
