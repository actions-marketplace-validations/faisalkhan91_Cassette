package otrans

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestFromChatCompletion(t *testing.T) {
	const data = `{"id":"chatcmpl_x","choices":[{"index":0,"message":{"role":"assistant","content":"hi",
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"b\":2,\"a\":1}"}}]},
		"finish_reason":"tool_calls"}]}`
	var cc openai.ChatCompletion
	if err := json.Unmarshal([]byte(data), &cc); err != nil {
		t.Fatal(err)
	}
	tr := FromChatCompletion(cc)
	if tr.Text != "hi" || tr.FinishReason != "tool_calls" {
		t.Fatalf("unexpected transcript: %+v", tr)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "get_weather" || tr.ToolCalls[0].Args != `{"a":1,"b":2}` {
		t.Fatalf("tool call wrong (args must be canonicalized): %+v", tr.ToolCalls)
	}
	// Volatile id must not affect the digest.
	const data2 = `{"id":"chatcmpl_DIFFERENT","choices":[{"index":0,"message":{"role":"assistant","content":"hi",
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"b\":2,\"a\":1}"}}]},
		"finish_reason":"tool_calls"}]}`
	var cc2 openai.ChatCompletion
	if err := json.Unmarshal([]byte(data2), &cc2); err != nil {
		t.Fatal(err)
	}
	if FromChatCompletion(cc).Digest() != FromChatCompletion(cc2).Digest() {
		t.Fatal("digest must ignore the volatile completion id")
	}
}

func TestFromResponse(t *testing.T) {
	const data = `{"id":"resp_x","status":"completed","output":[
		{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
		{"type":"function_call","id":"f1","call_id":"c1","name":"get_weather","arguments":"{\"x\":1}"}]}`
	var r responses.Response
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatal(err)
	}
	tr := FromResponse(r)
	if tr.Text != "hello" || tr.FinishReason != "completed" {
		t.Fatalf("unexpected transcript: %+v", tr)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "get_weather" || tr.ToolCalls[0].Args != `{"x":1}` {
		t.Fatalf("tool call wrong: %+v", tr.ToolCalls)
	}
}
