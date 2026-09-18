package analysis

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func httpIt(url, reqBody, respBody string, status int, timing []int64) *wirefmt.Interaction {
	return &wirefmt.Interaction{
		Kind:    "http",
		Request: wirefmt.Request{Method: "POST", URL: url, MatchKey: "k:" + url, Body: wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{
			Status: status, Streaming: timing != nil, StreamTiming: timing,
			Body: wirefmt.NewBody([]byte(respBody)),
		},
	}
}

func TestMarshalOTLP_GenAIAttributes(t *testing.T) {
	f := fileOf(
		httpIt("/v1/messages", `{"model":"claude-3-5"}`, `{"usage":{"input_tokens":10,"output_tokens":5}}`, 200, []int64{20, 5, 5}),
		mcpCall("add", `{"a":1}`, `{"content":[{"type":"text","text":"3"}]}`),
	)
	raw, err := MarshalOTLP(f)
	if err != nil {
		t.Fatal(err)
	}
	// Must be valid JSON and stable (deterministic) across runs.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	again, _ := MarshalOTLP(f)
	if string(raw) != string(again) {
		t.Fatal("OTLP export is not deterministic")
	}

	s := string(raw)
	for _, want := range []string{
		`"gen_ai.provider.name"`, `"anthropic"`,
		`"gen_ai.request.model"`, `"claude-3-5"`,
		`"gen_ai.usage.input_tokens"`, `"10"`, // OTLP/JSON encodes ints as strings
		`"gen_ai.client.operation.time_to_first_chunk"`, // from StreamTiming[0]
		`"mcp.tool.name"`, `"add"`, `"mcp.method.name"`,
		`"resourceSpans"`, `"scopeSpans"`, `"spanId"`, `"traceId"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("OTLP output missing %s:\n%s", want, s)
		}
	}
}

// Spans chain along the trace: span N starts where N-1 ended (a waterfall), and a
// paced turn has a real, non-zero duration.
func TestMarshalOTLP_Waterfall(t *testing.T) {
	f := fileOf(
		httpIt("/v1/messages", `{"model":"m"}`, `{}`, 200, []int64{10, 5}),
		httpIt("/v1/messages", `{"model":"m"}`, `{}`, 200, []int64{20}),
	)
	raw, _ := MarshalOTLP(f)
	var d struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct{ StartTimeUnixNano, EndTimeUnixNano string } `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	json.Unmarshal(raw, &d)
	sp := d.ResourceSpans[0].ScopeSpans[0].Spans
	if len(sp) != 2 {
		t.Fatalf("want 2 spans, got %d", len(sp))
	}
	if sp[0].EndTimeUnixNano == "0" || sp[0].EndTimeUnixNano != sp[1].StartTimeUnixNano {
		t.Fatalf("spans should chain: span0.end=%s span1.start=%s", sp[0].EndTimeUnixNano, sp[1].StartTimeUnixNano)
	}
}

// Gemini carries the model in the URL path, not the body — otel must still emit it.
func TestMarshalOTLP_GeminiModelFromURL(t *testing.T) {
	f := fileOf(httpIt("/v1beta/models/gemini-2.0-flash:streamGenerateContent", `{"contents":[]}`, `{}`, 200, nil))
	raw, _ := MarshalOTLP(f)
	s := string(raw)
	if !strings.Contains(s, `"gemini"`) || !strings.Contains(s, "gemini-2.0-flash") {
		t.Fatalf("otel should derive the Gemini model from the URL path:\n%s", s)
	}
}

// A 4xx/error response maps to span status ERROR (code 2).
func TestMarshalOTLP_ErrorStatus(t *testing.T) {
	f := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"error":{"message":"rate limited"}}`, 429, nil))
	raw, _ := MarshalOTLP(f)
	if !strings.Contains(string(raw), `"code": 2`) {
		t.Fatalf("error response should map to status code 2:\n%s", raw)
	}
}
