package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	cassette "github.com/faisalkhan91/cassette"
)

// cmdMCPServe replays a recorded MCP session as a real stdio JSON-RPC server, so
// any MCP client can talk to the recording with zero subprocess launch and zero
// network — the MCP analogue of `serve`. It reuses the in-process MCP replay
// (c.MCP(nil)), so matching is identical to record/replay.
func cmdMCPServe(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags(), args, mcpServeUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return mcpServeUsage(stderr)
	}
	c, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if err := serveMCP(c, os.Stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	return exitOK
}

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// serveMCP runs a newline-delimited JSON-RPC loop (MCP stdio framing) over in/out,
// answering each request from the recording. initialize and ping are synthesized;
// tools/list and tools/call replay recorded results; notifications (no id) are
// silently accepted.
func serveMCP(c *cassette.Cassette, in io.Reader, out io.Writer) error {
	caller := c.MCP(nil)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	ctx := context.Background()

	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req mcpRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // not a JSON-RPC frame
		}
		if len(req.ID) == 0 {
			continue // a notification — no response
		}
		result, rpcErr := dispatchMCP(ctx, caller, req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = map[string]any{"code": -32603, "message": rpcErr.Error()}
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func dispatchMCP(ctx context.Context, caller cassette.MCPCaller, req mcpRPCRequest) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "cassette-mcp-serve", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		var p mcp.ListToolsParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		return caller.ListTools(ctx, &p)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		return caller.CallTool(ctx, &mcp.CallToolParams{Name: p.Name, Arguments: p.Arguments})
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

const mcpServeUsageText = "usage: cassette mcp-serve <cassette.yaml>"

func mcpServeUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mcpServeUsageText)
	return exitUsage
}
