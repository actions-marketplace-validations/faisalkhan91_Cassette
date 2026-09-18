package analysis_test

import (
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func sseAnthropic(text string) []byte {
	return []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

`)
}

func TestDecodeInteraction_StreamingUnaryAndSkip(t *testing.T) {
	// Streaming http → renderable.
	stream := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sseAnthropic("hi"))}}
	if tr, unary, ok := analysis.DecodeInteraction(stream); !ok || unary || tr.Text != "hi" {
		t.Fatalf("streaming: tr=%+v unary=%v ok=%v", tr, unary, ok)
	}
	// Non-streaming JSON → unary synthetic, not renderable, distinct per body.
	u1 := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"a":"Paris"}`))}}
	u2 := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"a":"London"}`))}}
	t1, unary, ok := analysis.DecodeInteraction(u1)
	if !unary || ok {
		t.Fatalf("unary flags: unary=%v ok=%v", unary, ok)
	}
	t2, _, _ := analysis.DecodeInteraction(u2)
	if t1.Digest() == t2.Digest() {
		t.Fatal("different unary bodies must produce different digests")
	}
	// MCP tools/call → renderable (parity with HTTP): the tool call + result text.
	mcp := &wirefmt.Interaction{Kind: "mcp",
		Request:  wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{"a":1}`))},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"content":[{"type":"text","text":"3"}]}`))}}
	tr, unary, ok := analysis.DecodeInteraction(mcp)
	if !ok || unary {
		t.Fatalf("mcp should be renderable: ok=%v unary=%v", ok, unary)
	}
	if tr.Text != "3" || len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "add" {
		t.Fatalf("mcp transcript wrong: %+v", tr)
	}
}

func TestCollectTurns_SkipsUnaryIncludesMCP(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: wirefmt.Request{URL: "/v1/messages"}, Response: wirefmt.Response{Streaming: true, Body: wirefmt.NewBody(sseAnthropic("a"))}},
		{Kind: "http", Request: wirefmt.Request{URL: "/v1/messages"}, Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"x":1}`))}}, // unary, skipped
		{Kind: "mcp", Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "add", Body: wirefmt.NewBody([]byte(`{}`))},
			Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"content":[{"type":"text","text":"3"}]}`))}}, // MCP now included
	}}
	turns := analysis.CollectTurns(f)
	if len(turns) != 2 || turns[0].Text != "a" || turns[1].Text != "3" {
		t.Fatalf("CollectTurns = %+v", turns)
	}
}

func TestAxisDiff(t *testing.T) {
	a := []byte(`{"model":"claude-x","temperature":0.2,"messages":[1]}`)
	b := []byte(`{"model":"claude-y","temperature":0.9,"messages":[1]}`)
	got := analysis.AxisDiff(a, b)
	// model + sampling differ; messages identical. sampling collapses temperature.
	if len(got) != 2 || got[0] != "model" || got[1] != "sampling" {
		t.Fatalf("AxisDiff = %v", got)
	}
	if d := analysis.AxisDiff(a, a); len(d) != 0 {
		t.Fatalf("identical bodies should have no axis diff: %v", d)
	}
}
