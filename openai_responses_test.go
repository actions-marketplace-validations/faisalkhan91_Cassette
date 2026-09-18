package cassette_test

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/otrans"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func responsesServer(fixture []byte, chunk fakeprovider.Chunker) *http.ServeMux {
	return fakeprovider.Mux(map[string]http.Handler{
		"/responses": fakeprovider.StreamHandler(fixture, chunk),
	})
}

func driveResponses(t *testing.T, c *cassette.Cassette, baseURL, input string) (responses.Response, int, error) {
	t.Helper()
	client := openai.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel(openai.ChatModelGPT4o),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
	})
	defer stream.Close()
	var final responses.Response
	deltas := 0
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta", "response.function_call_arguments.delta":
			deltas++
		case "response.completed":
			final = ev.Response
		}
	}
	return final, deltas, stream.Err()
}

func TestOpenAIResponses_RecordReplay_Streaming_SSE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resp_text.yaml")

	srv := fakeprovider.NewServer(responsesServer(wirefix.OpenAIResponsesText, fakeprovider.PerFrame))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	live, deltas, err := driveResponses(t, rec, srv.URL, "Say hello")
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	if deltas < 2 {
		t.Fatalf("expected incremental deltas, got %d", deltas)
	}
	lt := otrans.FromResponse(live)
	if lt.Text != "Hello, world!" {
		t.Fatalf("assembled text = %q", lt.Text)
	}
	// WIRE equivalence: recorded body is byte-identical to the fixture.
	f, _ := wirefmt.Load(path)
	var captured []byte
	for _, it := range f.Interactions {
		if it.Response.Streaming {
			captured = it.Response.Body.Bytes()
		}
	}
	if !bytes.Equal(captured, wirefix.OpenAIResponsesText) {
		t.Fatalf("recorded Responses SSE not byte-identical:\n%q", captured)
	}

	// REPLAY, network blocked.
	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	replay, _, err := driveResponses(t, rp, "http://replay.invalid", "Say hello")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatal(err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("dials = %d", rp.Dials())
	}
	if lt.Digest() != otrans.FromResponse(replay).Digest() {
		t.Fatal("semantic transcript differs live vs replay")
	}
	// Byte decoder matches the SDK-built Response.
	byteT, err := semequal.DecodeOpenAIResponsesSSE(wirefix.OpenAIResponsesText)
	if err != nil {
		t.Fatal(err)
	}
	if byteT.Digest() != lt.Digest() {
		t.Fatalf("byte decoder != SDK:\n%s", semequal.Diff(lt, byteT))
	}
}

func TestOpenAIResponses_RecordReplay_ToolCall_Fragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resp_tool.yaml")

	srv := fakeprovider.NewServer(responsesServer(wirefix.OpenAIResponsesToolUse, fakeprovider.Adversarial))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	live, _, err := driveResponses(t, rec, srv.URL, "Weather in Paris?")
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	lt := otrans.FromResponse(live)
	if len(lt.ToolCalls) != 1 || lt.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected tool calls: %+v", lt.ToolCalls)
	}
	if lt.ToolCalls[0].Args != `{"location":"Paris"}` {
		t.Fatalf("reassembled args = %q", lt.ToolCalls[0].Args)
	}

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	replay, _, err := driveResponses(t, rp, "http://replay.invalid", "Weather in Paris?")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("dials = %d", rp.Dials())
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatal(err)
	}
	if lt.Digest() != otrans.FromResponse(replay).Digest() {
		t.Fatal("tool-call transcript differs live vs replay")
	}
	// Byte decoder cross-check.
	byteT, _ := semequal.DecodeOpenAIResponsesSSE(wirefix.OpenAIResponsesToolUse)
	if byteT.Digest() != lt.Digest() {
		t.Fatalf("byte decoder != SDK:\n%s", semequal.Diff(lt, byteT))
	}
}
