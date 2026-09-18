package wireenc

import (
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/semequal"
)

// Real-wire-shaped Gemini streamGenerateContent fixtures (text + tool-use),
// one in each framing the endpoint can return.

const geminiSSE = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Let me check"}]},"index":0}],"modelVersion":"gemini-1.5-pro","responseId":"r1"}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" the weather."},{"text":"(thinking)","thought":true}]},"index":0}],"usageMetadata":{"promptTokenCount":10}}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]},"finishReason":"STOP","index":0}],"usageMetadata":{"candidatesTokenCount":5}}

`

const geminiArray = `[{"candidates":[{"content":{"role":"model","parts":[{"text":"Let me check"}]},"index":0}]},
{"candidates":[{"content":{"role":"model","parts":[{"text":" the weather."},{"text":"(thinking)","thought":true}]},"index":0}]},
{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}]},"finishReason":"STOP","index":0}]}]`

func TestGemini_DecodeBothFramings(t *testing.T) {
	want := semequal.Transcript{Role: "assistant", Text: "Let me check the weather.",
		ToolCalls: []semequal.ToolCall{{Name: "get_weather", Args: `{"city":"Paris"}`}}, FinishReason: "STOP"}.Normalize()

	for _, tc := range []struct{ name, raw string }{{"sse", geminiSSE}, {"array", geminiArray}} {
		got, err := semequal.DecodeGeminiSSE([]byte(tc.raw))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !semequal.Equal(got, want) {
			t.Fatalf("%s decode mismatch:\n%s", tc.name, semequal.Diff(want, got))
		}
		if strings.Contains(got.Text, "thinking") {
			t.Fatalf("%s: thinking part should have been dropped: %q", tc.name, got.Text)
		}
	}
}

func TestGemini_RoundTrip(t *testing.T) {
	// decode→encode→decode is a stable, deterministic fixpoint for both framings.
	for _, raw := range []string{geminiSSE, geminiArray} {
		res, err := RoundTrip(Gemini, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Stable || !res.Deterministic {
			t.Fatalf("gemini round-trip stable=%v deterministic=%v", res.Stable, res.Deterministic)
		}
	}
}

func TestGemini_TruncatedArrayTolerated(t *testing.T) {
	// A stream cut mid-element drops only the incomplete trailing object.
	cut := geminiArray[:strings.Index(geminiArray, "functionCall")+20]
	got, err := semequal.DecodeGeminiSSE([]byte(cut))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Let me check the weather." {
		t.Fatalf("truncated decode lost completed text: %q", got.Text)
	}
}

func TestGemini_ProviderRouting(t *testing.T) {
	for _, u := range []string{
		"/v1beta/models/gemini-1.5-pro:streamGenerateContent",
		"/v1/projects/p/locations/l/publishers/google/models/gemini:generateContent",
	} {
		if ProviderForURL(u) != Gemini {
			t.Errorf("ProviderForURL(%q) did not route to Gemini", u)
		}
	}
	// Must not steal the OpenAI/Anthropic paths.
	if ProviderForURL("/v1/chat/completions") != OpenAIChat || ProviderForURL("/v1/messages") != Anthropic {
		t.Fatal("Gemini routing leaked into other dialects")
	}
}

// The OpenAI-compatible family (Azure, vLLM, Together, Groq, OpenRouter, Mistral,
// DeepSeek, …) is byte-identical to OpenAI Chat/Responses on the wire; because the
// stored URL is path-only, every such host routes to the OpenAI dialects on path
// alone. This is the explicit, tested guarantee.
func TestProviderForURL_OpenAICompatibleFamily(t *testing.T) {
	chat := []string{
		"/v1/chat/completions",                          // vLLM / Together / Groq / OpenRouter / Mistral / DeepSeek
		"/openai/deployments/my-gpt4o/chat/completions", // Azure OpenAI deployment path
		"/chat/completions",                             // base-path-less self-hosted
	}
	for _, u := range chat {
		if ProviderForURL(u) != OpenAIChat {
			t.Errorf("ProviderForURL(%q) != OpenAIChat", u)
		}
	}
	if ProviderForURL("/openai/deployments/my-gpt4o/responses") != OpenAIResponses {
		t.Error("Azure Responses deployment path did not route to OpenAIResponses")
	}
}

func TestOllama_RoundTripAndRouting(t *testing.T) {
	if ProviderForURL("/api/chat") != Ollama {
		t.Fatal("/api/chat did not route to Ollama")
	}
	tr := semequal.Transcript{Text: "hello there", ToolCalls: []semequal.ToolCall{
		{Name: "add", Args: `{"a":1,"b":2}`}}, FinishReason: "stop"}
	res, err := RoundTrip(Ollama, Encode(Ollama, tr, Envelope{}))
	if err != nil || !res.Stable || !res.Deterministic {
		t.Fatalf("ollama round-trip stable=%v det=%v err=%v", res.Stable, res.Deterministic, err)
	}
}
