package atrans

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func msg() anthropic.Message {
	return anthropic.Message{
		StopReason: anthropic.StopReason("tool_use"),
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello "},
			{Type: "text", Text: "world"},
			{Type: "tool_use", Name: "get_weather", Input: json.RawMessage(`{"b":2,"a":1}`)},
		},
	}
}

func TestFromMessage(t *testing.T) {
	tr := FromMessage(msg())
	if tr.Role != "assistant" {
		t.Fatalf("role = %q, want assistant", tr.Role)
	}
	if tr.Text != "Hello world" {
		t.Fatalf("text = %q", tr.Text)
	}
	if tr.FinishReason != "tool_use" {
		t.Fatalf("finish_reason = %q", tr.FinishReason)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool calls = %+v", tr.ToolCalls)
	}
	if tr.ToolCalls[0].Args != `{"a":1,"b":2}` {
		t.Fatalf("args = %q, want canonical (sorted)", tr.ToolCalls[0].Args)
	}
}

func TestFromMessage_DigestIgnoresVolatileID(t *testing.T) {
	a := msg()
	a.ID = "msg_AAA"
	b := msg()
	b.ID = "msg_BBB"
	if FromMessage(a).Digest() != FromMessage(b).Digest() {
		t.Fatal("digest must ignore the volatile message id")
	}
}
