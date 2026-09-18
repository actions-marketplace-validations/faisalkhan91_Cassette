package cassette_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
)

// anthropicClient builds an Anthropic client whose HTTP transport is the
// cassette (record or replay). MaxRetries(0) keeps the recorded cursor in sync.
func anthropicClient(c *cassette.Cassette, baseURL string) anthropic.Client {
	return anthropic.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
}

func streamParams(prompt string) anthropic.MessageNewParams {
	return anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_6,
		MaxTokens: 1024,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	}
}

// driveStream consumes a streaming message via the SDK, accumulating the final
// message and recording the ordered sequence of decoded event types (proving
// incremental, in-order delivery rather than a single buffered blob).
func driveStream(t *testing.T, client anthropic.Client, prompt string) (anthropic.Message, []string, error) {
	t.Helper()
	stream := client.Messages.NewStreaming(context.Background(), streamParams(prompt))
	defer stream.Close()
	msg := anthropic.Message{}
	var order []string
	for stream.Next() {
		ev := stream.Current()
		order = append(order, ev.Type)
		if err := msg.Accumulate(ev); err != nil {
			t.Fatalf("accumulate: %v", err)
		}
	}
	return msg, order, stream.Err()
}

// rawReplay issues a raw POST through the cassette client and returns the
// response bytes verbatim — used to assert byte-identical (WIRE) replay.
func rawReplay(t *testing.T, c *cassette.Cassette, baseURL, path, body string) (int, []byte) {
	t.Helper()
	resp, err := c.HTTPClient().Post(baseURL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("raw replay POST: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("raw replay read: %v", err)
	}
	return resp.StatusCode, b
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
