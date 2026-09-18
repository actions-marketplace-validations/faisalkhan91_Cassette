package analysis

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// MCPStep is one protocol-level exchange in a recorded MCP session — the JSON-RPC
// method, the tool + arguments (for tools/call), the advertised roster (for
// tools/list), and the flattened result text or error. It is the protocol lens on a
// session (what the server did, step by step), distinct from the LLM-transcript lens
// that `doc`/`explain` render.
type MCPStep struct {
	Index   int      `json:"index"`
	Method  string   `json:"method"`
	Tool    string   `json:"tool,omitempty"`
	Params  string   `json:"params,omitempty"`
	Result  string   `json:"result,omitempty"`
	IsError bool     `json:"is_error,omitempty"`
	Tools   []string `json:"tools,omitempty"`
}

// MCPSteps walks a recording's MCP interactions in order, returning a step per
// JSON-RPC exchange. Non-MCP interactions are skipped. Pure and offline.
func MCPSteps(f *wirefmt.File) []MCPStep {
	steps := []MCPStep{} // empty serializes as [] not null
	for i, it := range f.Interactions {
		if it.Kind != "mcp" {
			continue
		}
		s := MCPStep{Index: i, Method: it.Request.MCPMethod, Tool: it.Request.MCPTool}
		if p := strings.TrimSpace(string(it.Request.Body.Bytes())); p != "" && p != "{}" {
			s.Params = p
		}
		body := it.Response.Body.Bytes()
		s.IsError = mcpIsError(body)
		if it.Request.MCPMethod == "tools/list" {
			gjson.GetBytes(body, "tools").ForEach(func(_, t gjson.Result) bool {
				if n := t.Get("name").String(); n != "" {
					s.Tools = append(s.Tools, n)
				}
				return true
			})
		} else {
			s.Result = mcpResultText(body)
		}
		steps = append(steps, s)
	}
	return steps
}
