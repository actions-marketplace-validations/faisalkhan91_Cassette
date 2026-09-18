package analysis_test

import (
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func httpResp(url string, streaming bool, body string) *wirefmt.Interaction {
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: url, Body: wirefmt.NewBody([]byte(`{"model":"m"}`))},
		Response: wirefmt.Response{Status: 200, Streaming: streaming, Body: wirefmt.NewBody([]byte(body))}}
}

func TestDecodeUsage_Providers(t *testing.T) {
	anthropic := httpResp("/v1/messages", true,
		`event: message_start
data: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":12,"output_tokens":1,"cache_read_input_tokens":4}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}

`)
	if u := analysis.DecodeUsage(anthropic); !u.Known || u.InputTokens != 12 || u.OutputTokens != 9 || u.CacheRead != 4 {
		t.Fatalf("anthropic usage = %+v", u)
	}

	openai := httpResp("/v1/chat/completions", true,
		`data: {"choices":[{"delta":{"content":"hi"}}]}

data: {"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":7}}

data: [DONE]

`)
	if u := analysis.DecodeUsage(openai); !u.Known || u.InputTokens != 20 || u.OutputTokens != 7 {
		t.Fatalf("openai usage = %+v", u)
	}

	responses := httpResp("/v1/responses", true,
		`event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":3}}}

`)
	if u := analysis.DecodeUsage(responses); !u.Known || u.InputTokens != 5 || u.OutputTokens != 3 {
		t.Fatalf("responses usage = %+v", u)
	}

	// Gemini streams usageMetadata (the live API returns it on every recording).
	gemini := httpResp("/v1beta/models/gemini-1.5-pro:streamGenerateContent", true,
		`data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":700,"candidatesTokenCount":88,"cachedContentTokenCount":40}}

`)
	if u := analysis.DecodeUsage(gemini); !u.Known || u.InputTokens != 700 || u.OutputTokens != 88 || u.CacheRead != 40 {
		t.Fatalf("gemini usage = %+v", u)
	}

	// Ollama native NDJSON reports prompt_eval_count/eval_count on the final line
	// (no SSE "data:" prefix).
	ollama := httpResp("/api/chat", true,
		`{"message":{"role":"assistant","content":"hi"},"done":false}
{"done":true,"done_reason":"stop","prompt_eval_count":150,"eval_count":42}
`)
	if u := analysis.DecodeUsage(ollama); !u.Known || u.InputTokens != 150 || u.OutputTokens != 42 {
		t.Fatalf("ollama usage = %+v", u)
	}

	// Unary (non-streaming) JSON body.
	unary := httpResp("/v1/messages", false, `{"usage":{"input_tokens":2,"output_tokens":8}}`)
	if u := analysis.DecodeUsage(unary); !u.Known || u.Total() != 10 {
		t.Fatalf("unary usage = %+v", u)
	}

	// No usage data → unknown, never silent zero.
	none := httpResp("/v1/chat/completions", true, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	if u := analysis.DecodeUsage(none); u.Known {
		t.Fatalf("expected unknown usage, got %+v", u)
	}
}
