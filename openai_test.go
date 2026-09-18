package cassette_test

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/otrans"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// openCassetteStatuses returns the ordered response statuses of the http
// interactions in a recorded cassette.
func openCassetteStatuses(t *testing.T, path string) ([]int, error) {
	t.Helper()
	f, err := wirefmt.Load(path)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, it := range f.Interactions {
		if it.Kind == "http" {
			out = append(out, it.Response.Status)
		}
	}
	return out, nil
}

func openaiClient(c *cassette.Cassette, baseURL string) openai.Client {
	return openai.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
}

func chatServer(fixture []byte, chunk fakeprovider.Chunker) *http.ServeMux {
	return fakeprovider.Mux(map[string]http.Handler{
		"/chat/completions": fakeprovider.StreamHandler(fixture, chunk),
	})
}

// driveOpenAIStream consumes an OpenAI streaming chat completion, accumulating
// the final completion and recording how many content/tool-arg deltas arrived
// (proving incremental, in-order delivery rather than one buffered blob).
func driveOpenAIStream(t *testing.T, client openai.Client, params openai.ChatCompletionNewParams) (openai.ChatCompletion, int, error) {
	t.Helper()
	stream := client.Chat.Completions.NewStreaming(context.Background(), params)
	defer stream.Close()
	acc := openai.ChatCompletionAccumulator{}
	deltas := 0
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) > 0 {
			d := chunk.Choices[0].Delta
			if d.Content != "" || len(d.ToolCalls) > 0 {
				deltas++
			}
		}
	}
	return acc.ChatCompletion, deltas, stream.Err()
}

func TestOpenAI_RecordReplay_Streaming_SSE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai_stream.yaml")
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Say hello")},
	}

	// RECORD against the fake provider.
	srv := fakeprovider.NewServer(chatServer(wirefix.OpenAIText, fakeprovider.PerFrame))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	liveCC, liveDeltas, err := driveOpenAIStream(t, openaiClient(rec, srv.URL), params)
	if err != nil {
		t.Fatalf("live stream: %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	if liveDeltas < 2 {
		t.Fatalf("expected multiple incremental deltas, got %d", liveDeltas)
	}
	if got := liveCC.Choices[0].Message.Content; got != "Hello, world!" {
		t.Fatalf("assembled content = %q, want %q", got, "Hello, world!")
	}
	// WIRE equivalence: the recorded streaming body is byte-identical to the fixture.
	f, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var captured []byte
	for _, it := range f.Interactions {
		if it.Kind == "http" && it.Response.Streaming {
			captured = it.Response.Body.Bytes()
		}
	}
	if !bytes.Equal(captured, wirefix.OpenAIText) {
		t.Fatalf("recorded OpenAI SSE is not byte-identical to the fixture:\n got=%q", captured)
	}

	// REPLAY with server down, network blocked.
	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	replayCC, _, err := driveOpenAIStream(t, openaiClient(rp, "http://replay.invalid"), params)
	if err != nil {
		t.Fatalf("replay stream: %v", err)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatal(err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("replay dials = %d, want 0", rp.Dials())
	}
	if otrans.FromChatCompletion(liveCC).Digest() != otrans.FromChatCompletion(replayCC).Digest() {
		t.Fatal("semantic transcript differs live vs replay")
	}
	// semequal byte decoder matches the SDK accumulation.
	byteT, err := semequal.DecodeOpenAISSE(wirefix.OpenAIText)
	if err != nil {
		t.Fatal(err)
	}
	if byteT.Digest() != otrans.FromChatCompletion(liveCC).Digest() {
		t.Fatalf("byte decoder != SDK:\n%s", semequal.Diff(otrans.FromChatCompletion(liveCC), byteT))
	}
}

func TestOpenAI_RecordReplay_ToolCall_Fragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai_tool.yaml")
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Weather in Paris?")},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:       "get_weather",
				Parameters: shared.FunctionParameters{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
			}),
		},
	}

	srv := fakeprovider.NewServer(chatServer(wirefix.OpenAIToolUse, fakeprovider.Adversarial))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	liveCC, _, err := driveOpenAIStream(t, openaiClient(rec, srv.URL), params)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	tc := liveCC.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].Function.Name != "get_weather" {
		t.Fatalf("unexpected tool calls: %+v", tc)
	}
	if tc[0].Function.Arguments != `{"location":"Paris"}` {
		t.Fatalf("reassembled arguments = %q, want %q", tc[0].Function.Arguments, `{"location":"Paris"}`)
	}

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	replayCC, _, err := driveOpenAIStream(t, openaiClient(rp, "http://replay.invalid"), params)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("dials = %d", rp.Dials())
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatal(err)
	}
	if otrans.FromChatCompletion(liveCC).Digest() != otrans.FromChatCompletion(replayCC).Digest() {
		t.Fatal("tool-call transcript differs live vs replay")
	}
}

const openaiOKJSON = `{"id":"chatcmpl_ok","object":"chat.completion","created":1700000000,"model":"gpt-4o-2024-08-06","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`

const openaiRateLimitJSON = `{"error":{"message":"Rate limit reached","type":"rate_limit_error","code":"rate_limit_exceeded"}}`

func TestOpenAI_RecordReplay_ErrorThenRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openai_retry.yaml")
	seq := fakeprovider.Sequence(
		fakeprovider.RawHandler(http.StatusTooManyRequests,
			http.Header{"Content-Type": {"application/json"}, "Retry-After": {"0"}},
			[]byte(openaiRateLimitJSON)),
		fakeprovider.JSONHandler(http.StatusOK, []byte(openaiOKJSON), nil),
	)
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{"/chat/completions": seq}))

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	recCl := openai.NewClient(
		option.WithHTTPClient(rec.HTTPClient()),
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(2),
	)
	cc, err := recCl.Chat.Completions.New(context.Background(), params)
	if err != nil {
		t.Fatalf("record New (should succeed after retry): %v", err)
	}
	if cc.Choices[0].Message.Content != "ok" {
		t.Fatalf("record content = %q", cc.Choices[0].Message.Content)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	f, _ := openCassetteStatuses(t, path)
	if len(f) != 2 || f[0] != 429 || f[1] != 200 {
		t.Fatalf("expected recorded statuses [429,200], got %v", f)
	}

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	rpCl := openai.NewClient(
		option.WithHTTPClient(rp.HTTPClient()),
		option.WithBaseURL("http://replay.invalid"),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(2),
	)
	rcc, err := rpCl.Chat.Completions.New(context.Background(), params)
	if err != nil {
		t.Fatalf("replay New (offline retry): %v", err)
	}
	if rcc.Choices[0].Message.Content != "ok" {
		t.Fatalf("replay content = %q", rcc.Choices[0].Message.Content)
	}
	if rp.Dials() != 0 {
		t.Fatalf("dials = %d", rp.Dials())
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify (both 429 and 200 consumed): %v", err)
	}
}
