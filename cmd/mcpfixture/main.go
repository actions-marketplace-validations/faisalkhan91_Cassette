// Command mcpfixture is a tiny in-repo MCP server used only by cassette's MCP
// self-test. It speaks JSON-RPC over stdio (no network) and exposes a single
// "add" tool. On startup it optionally touches the file named by
// CASSETTE_MCP_MARKER, so the replay test can prove the binary was NEVER exec'd
// (the marker is absent in replay).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type addArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addResult struct {
	Sum int `json:"sum"`
}

func addHandler(_ context.Context, _ *mcp.CallToolRequest, in addArgs) (*mcp.CallToolResult, addResult, error) {
	sum := in.A + in.B
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("sum=%d", sum)}},
	}, addResult{Sum: sum}, nil
}

// buildServer constructs the fixture MCP server with its single "add" tool.
func buildServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "mcpfixture", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "add",
		Description: "Add two integers and return the sum.",
	}, addHandler)
	return server
}

func main() {
	if marker := os.Getenv("CASSETTE_MCP_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("exec'd"), 0o644)
	}
	if err := buildServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "mcpfixture:", err)
		os.Exit(1)
	}
}
