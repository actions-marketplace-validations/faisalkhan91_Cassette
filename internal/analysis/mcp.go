package analysis

import (
	"strings"

	"github.com/tidwall/gjson"
)

// Shared MCP response primitives, used across the MCP-aware analyses (mcp-diff,
// mcp-steps, the otel exporter, and decodeMCP) so a single command doesn't own a
// protocol helper the others depend on.

// mcpIsError reports whether a stored MCP response body represents an error — either
// a result-level "isError": true (tools/call) or a JSON-RPC envelope "error".
func mcpIsError(body []byte) bool {
	return gjson.GetBytes(body, "isError").Bool() || gjson.GetBytes(body, "error").Exists()
}

// mcpResultText flattens an MCP result's content[].text into a single string.
func mcpResultText(body []byte) string {
	var sb strings.Builder
	gjson.GetBytes(body, "content").ForEach(func(_, c gjson.Result) bool {
		if t := c.Get("text"); t.Exists() {
			sb.WriteString(t.String())
		}
		return true
	})
	return sb.String()
}
