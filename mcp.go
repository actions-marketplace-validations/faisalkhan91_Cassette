package cassette

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// MCPCaller is the MCP client-call boundary cassette wraps. *mcp.ClientSession
// satisfies it. Recording delegates to a real session and records params/result
// JSON; replay serves recorded results keyed by tool name + canonicalized
// arguments, touching NO transport (the child server is never launched).
type MCPCaller interface {
	ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// MCP wraps real for record/replay. In replay, real may be nil — it is never
// used (proving the server process is never exec'd).
func (c *Cassette) MCP(real MCPCaller) MCPCaller {
	return &mcpWrapper{c: c, real: real}
}

type mcpWrapper struct {
	c    *Cassette
	real MCPCaller
}

func (w *mcpWrapper) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	argsJSON := marshalArgs(params.Arguments)
	key := rekey.CallKey(params.Name, argsJSON, w.c.match)

	if w.c.mode == ModeRecord {
		res, err := w.real.CallTool(ctx, params)
		if err != nil {
			return nil, err
		}
		body, mErr := json.Marshal(res)
		if mErr != nil {
			return nil, fmt.Errorf("cassette: marshal CallToolResult: %w", mErr)
		}
		w.record("tools/call", params.Name, argsJSON, body, key)
		return res, nil
	}

	body, err := w.replay(ctx, key)
	if err != nil {
		return nil, err
	}
	var out mcp.CallToolResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("cassette: unmarshal recorded CallToolResult: %w", err)
	}
	return &out, nil
}

func (w *mcpWrapper) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	paramsJSON := marshalArgs(params)
	key := rekey.ListKey(paramsJSON, w.c.match)

	if w.c.mode == ModeRecord {
		res, err := w.real.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		body, mErr := json.Marshal(res)
		if mErr != nil {
			return nil, fmt.Errorf("cassette: marshal ListToolsResult: %w", mErr)
		}
		w.record("tools/list", "", paramsJSON, body, key)
		return res, nil
	}

	body, err := w.replay(ctx, key)
	if err != nil {
		return nil, err
	}
	var out mcp.ListToolsResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("cassette: unmarshal recorded ListToolsResult: %w", err)
	}
	return &out, nil
}

func (w *mcpWrapper) record(method, tool string, reqBody, respBody []byte, key string) {
	it := &wirefmt.Interaction{
		Kind: "mcp",
		Request: wirefmt.Request{
			MCPMethod: method,
			MCPTool:   tool,
			Body:      wirefmt.NewBody(reqBody),
			MatchKey:  key, // persisted so arg scrubbing can't break replay matching
		},
		Response: wirefmt.Response{
			Body: wirefmt.NewBody(respBody),
		},
	}
	if len(reqBody) > 0 {
		it.Request.BodySHA256 = canon.Digest(reqBody)
	}
	w.c.appendInteraction(it)
}

func (w *mcpWrapper) replay(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.c.mu.Lock()
	idx, ok := w.c.nextIndexLocked(key)
	if ok {
		w.c.consumed[idx] = true
	}
	w.c.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: mcp %s", ErrNoMatch, key)
	}
	return w.c.file.Interactions[idx].Response.Body.Bytes(), nil
}

// MCP key derivation lives in internal/rekey (shared with HTTP key derivation and
// the rekey/conformance paths); the recorder above calls rekey.CallKey/ListKey.

func marshalArgs(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}
