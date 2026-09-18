package semequal

import (
	"fmt"
	"strings"
	"testing"
)

// anthropicTextStream builds a minimal Anthropic SSE stream whose only varying
// parts are the volatile message id and output token count.
func anthropicTextStream(id string, outTokens int, text string) []byte {
	return []byte(fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":%q,"role":"assistant","usage":{"input_tokens":3,"output_tokens":%d}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

event: message_stop
data: {"type":"message_stop"}

`, id, outTokens, text))
}

func TestDecodeAnthropic_Text(t *testing.T) {
	tr, err := DecodeAnthropicSSE(anthropicTextStream("msg_1", 5, "hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Role != "assistant" || tr.Text != "hello world" || tr.FinishReason != "end_turn" {
		t.Fatalf("unexpected transcript: %+v", tr)
	}
	if len(tr.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls: %+v", tr.ToolCalls)
	}
}

func TestSemequal_IgnoresVolatile(t *testing.T) {
	a, _ := DecodeAnthropicSSE(anthropicTextStream("msg_AAA", 7, "same text"))
	b, _ := DecodeAnthropicSSE(anthropicTextStream("msg_ZZZ", 999, "same text"))
	if a.Digest() != b.Digest() {
		t.Fatalf("digest must ignore volatile id/usage:\n a=%s\n b=%s", a, b)
	}
	if a.Digest() == "" {
		t.Fatal("digest must be non-empty")
	}
}

func TestDecodeAnthropic_ToolUse(t *testing.T) {
	stream := []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"b\":2,"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"a\":1}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

`)
	tr, err := DecodeAnthropicSSE(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected tool calls: %+v", tr.ToolCalls)
	}
	// Reassembled fragments, canonicalized (keys sorted).
	if tr.ToolCalls[0].Args != `{"a":1,"b":2}` {
		t.Fatalf("args = %q, want canonical", tr.ToolCalls[0].Args)
	}
	if tr.FinishReason != "tool_use" {
		t.Fatalf("finish = %q", tr.FinishReason)
	}
}

func TestDecodeAnthropic_StreamError(t *testing.T) {
	stream := []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial"}}

event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}

`)
	tr, err := DecodeAnthropicSSE(stream)
	if err == nil {
		t.Fatal("expected a stream error")
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.Text != "Partial" {
		t.Fatalf("partial text = %q", tr.Text)
	}
}

func TestDiffAndEqual(t *testing.T) {
	a := Transcript{Role: "assistant", Text: "hi", FinishReason: "end_turn"}
	b := Transcript{Role: "assistant", Text: "bye", FinishReason: "end_turn"}
	if Equal(a, b) {
		t.Fatal("different text must not be equal")
	}
	d := Diff(a, b)
	if !strings.Contains(d, "- ") || !strings.Contains(d, "+ ") {
		t.Fatalf("diff should show both sides:\n%s", d)
	}
	if Diff(a, a) != "" {
		t.Fatal("diff of equal transcripts must be empty")
	}
}

func TestNormalizeSortsToolCalls(t *testing.T) {
	tr := Transcript{ToolCalls: []ToolCall{{Name: "zebra"}, {Name: "alpha"}}}.Normalize()
	if tr.ToolCalls[0].Name != "alpha" || tr.ToolCalls[1].Name != "zebra" {
		t.Fatalf("tool calls not sorted: %+v", tr.ToolCalls)
	}
}

func TestCombinedDigestOrderMatters(t *testing.T) {
	x := Transcript{Text: "1"}
	y := Transcript{Text: "2"}
	if CombinedDigest([]Transcript{x, y}) == CombinedDigest([]Transcript{y, x}) {
		t.Fatal("combined digest should be order-sensitive across turns")
	}
}

func TestAssertSemanticEqual_Pass(t *testing.T) {
	a := Transcript{Text: "hello", FinishReason: "end_turn"}
	b := Transcript{Role: "assistant", Text: "hello", FinishReason: "end_turn"} // role defaulted
	AssertSemanticEqual(t, a, b)                                                // must not fail
}

func TestDecodeOpenAI_ToolUseAndVolatile(t *testing.T) {
	mk := func(id string) []byte {
		return []byte(`data: {"id":"` + id + `","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":"{\"b\":2,"}}]},"finish_reason":null}]}

data: {"id":"` + id + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a\":1}"}}]},"finish_reason":null}]}

data: {"id":"` + id + `","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
	}
	a, err := DecodeOpenAISSE(mk("chatcmpl_A"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ToolCalls) != 1 || a.ToolCalls[0].Name != "get_weather" || a.ToolCalls[0].Args != `{"a":1,"b":2}` {
		t.Fatalf("unexpected tool call: %+v", a.ToolCalls)
	}
	if a.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", a.FinishReason)
	}
	// Different volatile id ⇒ same digest.
	b, _ := DecodeOpenAISSE(mk("chatcmpl_DIFFERENT"))
	if a.Digest() != b.Digest() {
		t.Fatal("digest must ignore volatile id")
	}
}

func TestDecodeOpenAIResponses_TextToolVolatile(t *testing.T) {
	text, err := DecodeOpenAIResponsesSSE([]byte(`event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1"}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","output_index":0,"delta":"Hi "}

event: response.output_text.delta
data: {"type":"response.output_text.delta","output_index":0,"delta":"there"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_A","status":"completed"}}

`))
	if err != nil {
		t.Fatal(err)
	}
	if text.Text != "Hi there" || text.FinishReason != "completed" {
		t.Fatalf("text transcript: %+v", text)
	}

	tool := func(id string) Transcript {
		tr, _ := DecodeOpenAIResponsesSSE([]byte(`event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"` + id + `","name":"get_weather"}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"b\":2,"}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"a\":1}"}

event: response.completed
data: {"type":"response.completed","response":{"id":"` + id + `","status":"completed"}}

`))
		return tr
	}
	a := tool("resp_X")
	if len(a.ToolCalls) != 1 || a.ToolCalls[0].Name != "get_weather" || a.ToolCalls[0].Args != `{"a":1,"b":2}` {
		t.Fatalf("tool transcript: %+v", a.ToolCalls)
	}
	if a.Digest() != tool("resp_DIFFERENT").Digest() {
		t.Fatal("digest must ignore the volatile response id")
	}
}
