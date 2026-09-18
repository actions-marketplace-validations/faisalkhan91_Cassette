package analysis

import (
	"encoding/json"
	"github.com/faisalkhan91/cassette/internal/canon"
	"strings"
	"testing"
)

func TestCompileMCP(t *testing.T) {
	sp := Screenplay{
		// No provider needed: a pure-MCP screenplay.
		Turns: []ScreenplayTurn{
			{Kind: "mcp", Method: "tools/list", ToolCalls: []ScreenplayTool{
				{Name: "add", Args: `{"type":"object"}`},
			}},
			{Kind: "mcp", Method: "tools/call", Tool: "add", Args: `{"a":2,"b":3}`, Text: "5"},
			{Kind: "mcp", Method: "tools/call", Tool: "divide", Args: `{"a":1,"b":0}`, ToolError: "division by zero"},
			{Kind: "mcp", Tool: "noop", Result: `{"content":[{"type":"text","text":"ok"}]}`}, // default method, explicit result
		},
	}
	f, err := Compile(sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Interactions) != 4 {
		t.Fatalf("got %d interactions, want 4", len(f.Interactions))
	}
	// tools/call keys mirror mcpKeyFromStored: tool name + canonical-args digest.
	call := f.Interactions[1]
	if call.Request.MCPMethod != "tools/call" || call.Request.MCPTool != "add" {
		t.Fatalf("bad tools/call request: %+v", call.Request)
	}
	if !strings.HasPrefix(call.Request.MatchKey, "mcp:tools/call:add:") {
		t.Errorf("tools/call key = %q, want mcp:tools/call:add:<digest>", call.Request.MatchKey)
	}
	// Canonical args ⇒ key is independent of key order / whitespace.
	if got := "mcp:tools/call:add:" + canon.Digest([]byte(`{"b":3,"a":2}`)); got != call.Request.MatchKey {
		t.Errorf("key not canonical: %q vs %q", got, call.Request.MatchKey)
	}
	// The error turn synthesizes isError.
	var res map[string]any
	if err := json.Unmarshal(f.Interactions[2].Response.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["isError"] != true {
		t.Errorf("tool_error turn missing isError: %v", res)
	}
	// tools/list key uses the params digest.
	if !strings.HasPrefix(f.Interactions[0].Request.MatchKey, "mcp:tools/list:") {
		t.Errorf("tools/list key = %q", f.Interactions[0].Request.MatchKey)
	}
}

func TestCompileMCPErrors(t *testing.T) {
	cases := []struct {
		name string
		turn ScreenplayTurn
	}{
		{"call without tool", ScreenplayTurn{Kind: "mcp", Method: "tools/call"}},
		{"bad args json", ScreenplayTurn{Kind: "mcp", Tool: "x", Args: `{bad`}},
		{"bad result json", ScreenplayTurn{Kind: "mcp", Tool: "x", Result: `{bad`}},
		{"unknown method", ScreenplayTurn{Kind: "mcp", Method: "tools/frobnicate", Tool: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile(Screenplay{Turns: []ScreenplayTurn{tc.turn}}); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}
