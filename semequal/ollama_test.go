package semequal

import "testing"

// Real-wire-shaped Ollama /api/chat NDJSON: per-line objects, content streamed
// across lines, a tool call, and a terminal done line with telemetry to drop.
const ollamaNDJSON = `{"model":"llama3.1","created_at":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":"Let me"},"done":false}
{"model":"llama3.1","created_at":"2024-01-01T00:00:01Z","message":{"role":"assistant","content":" check.","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":false}
{"model":"llama3.1","created_at":"2024-01-01T00:00:02Z","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","eval_count":42,"total_duration":123456}
`

func TestDecodeOllamaNDJSON(t *testing.T) {
	got, err := DecodeOllamaNDJSON([]byte(ollamaNDJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := Transcript{Role: "assistant", Text: "Let me check.",
		ToolCalls: []ToolCall{{Name: "get_weather", Args: `{"city":"Paris"}`}}, FinishReason: "stop"}.Normalize()
	if !Equal(got, want) {
		t.Fatalf("mismatch:\n%s", Diff(want, got))
	}
}

func TestDecodeOllamaNDJSON_Empty(t *testing.T) {
	got, err := DecodeOllamaNDJSON([]byte("\n  \n"))
	if err != nil || got.Text != "" || len(got.ToolCalls) != 0 {
		t.Fatalf("blank NDJSON should be empty: %+v (%v)", got, err)
	}
}
