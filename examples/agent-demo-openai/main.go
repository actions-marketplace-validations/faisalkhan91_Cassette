// Command agent-demo-openai is the OpenAI counterpart of the north-star demo:
// the SAME agent code runs once "live" (HTTP to an in-process fake provider
// replaying a REAL-shaped OpenAI Chat Completions stream, recording a cassette)
// and once in "replay" with network egress made impossible. Both must produce a
// SEMANTICALLY IDENTICAL transcript (proven by semequal's digest) with PROVABLY
// ZERO outbound dials. A second live "control" run sees a DIFFERENT volatile
// completion id yet the same digest.
//
//	go run ./examples/agent-demo-openai                # assert committed expected.sha
//	go run ./examples/agent-demo-openai -write-golden   # (re)write expected.sha
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/otrans"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/semequal"
)

func main() {
	update := flag.Bool("write-golden", false, "write expected.sha instead of asserting it")
	flag.Parse()
	if err := run(os.Stdout, *update); err != nil {
		fmt.Fprintln(os.Stderr, "demo FAILED:", err)
		os.Exit(1)
	}
}

type leg struct {
	transcript semequal.Transcript
	id         string
}

func run(out io.Writer, update bool) error {
	tmp, err := os.MkdirTemp("", "cassette-openai-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "openai.yaml")

	var counter int64
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/chat/completions": volatileChatHandler(&counter),
	}))

	// LIVE: record.
	recCas, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		return err
	}
	live, err := turn(recCas, srv.URL)
	if err != nil {
		return fmt.Errorf("live leg: %w", err)
	}
	if err := recCas.VerifyError(); err != nil {
		return fmt.Errorf("record/save: %w", err)
	}

	// CONTROL: different volatile id, same transcript.
	ctlCas, err := cassette.Open(filepath.Join(tmp, "ctl.yaml"), cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		return err
	}
	ctl, err := turn(ctlCas, srv.URL)
	if err != nil {
		return fmt.Errorf("control leg: %w", err)
	}
	if live.transcript.Digest() != ctl.transcript.Digest() {
		return fmt.Errorf("volatile fields leaked into transcript")
	}
	if live.id == ctl.id {
		return fmt.Errorf("fake did not vary volatile id (%q == %q)", live.id, ctl.id)
	}
	srv.Close()

	// REPLAY: network egress impossible.
	rpCas, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		return err
	}
	replay, err := turn(rpCas, "http://replay.invalid")
	if err != nil {
		return fmt.Errorf("replay leg: %w", err)
	}
	if err := rpCas.VerifyError(); err != nil {
		return fmt.Errorf("replay verify: %w", err)
	}
	if rpCas.Dials() != 0 {
		return fmt.Errorf("replay made %d dials; want 0", rpCas.Dials())
	}
	if live.transcript.Digest() != replay.transcript.Digest() {
		return fmt.Errorf("transcripts differ:\n%s", semequal.Diff(live.transcript, replay.transcript))
	}

	digest := live.transcript.Digest()
	goldenPath := goldenFile()
	if update {
		if err := os.WriteFile(goldenPath, []byte(digest+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote golden %s = %s\n", goldenPath, digest)
	} else {
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			return fmt.Errorf("read golden (run with -write-golden first): %w", err)
		}
		if got := string(bytes.TrimSpace(want)); got != digest {
			return fmt.Errorf("transcript digest %s != committed golden %s", digest, got)
		}
	}

	fmt.Fprintln(out, "north-star demo (OpenAI tool call): PASS")
	for _, tc := range live.transcript.ToolCalls {
		fmt.Fprintf(out, "  tool call    : %s(%s)\n", tc.Name, tc.Args)
	}
	fmt.Fprintf(out, "  finish       : %s\n", live.transcript.FinishReason)
	fmt.Fprintf(out, "  live id      : %s\n", live.id)
	fmt.Fprintf(out, "  control id   : %s (different volatile value, ignored)\n", ctl.id)
	fmt.Fprintf(out, "  digest       : %s\n", digest)
	fmt.Fprintf(out, "  outbound dials: %d  (network egress was impossible)\n", rpCas.Dials())
	return nil
}

// turn runs one streaming OpenAI tool-calling turn through the cassette client.
func turn(c *cassette.Cassette, baseURL string) (leg, error) {
	client := openai.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("demo-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("What's the weather in Paris?")},
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:       "get_weather",
				Parameters: shared.FunctionParameters{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
			}),
		},
	})
	defer stream.Close()
	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		acc.AddChunk(stream.Current())
	}
	if err := stream.Err(); err != nil {
		return leg{}, err
	}
	return leg{transcript: otrans.FromChatCompletion(acc.ChatCompletion), id: acc.ID}, nil
}

var idRe = regexp.MustCompile(`chatcmpl_[A-Za-z]+`)

func volatileChatHandler(counter *int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(counter, 1)
		fixture := idRe.ReplaceAll(wirefix.OpenAIToolUse, []byte("chatcmpl_RUN"+strconv.FormatInt(n, 10)))
		fakeprovider.StreamHandler(fixture, fakeprovider.PerFrame)(w, r)
	}
}

func goldenFile() string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "expected.sha"
	}
	return filepath.Join(filepath.Dir(self), "expected.sha")
}
