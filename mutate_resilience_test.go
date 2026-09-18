package cassette_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/mutate"
)

// TestMutate_AgentResilience proves a mutated (truncated) stream is still
// consumed by the real provider SDK scanner without panicking, and surfaces as
// an incomplete result — the agent-resilience use case for the mutate corpus.
func TestMutate_AgentResilience(t *testing.T) {
	truncated := mutate.Apply(wirefix.AnthropicText, 7, mutate.TruncateAfterFrame(5))

	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.StreamHandler(truncated, fakeprovider.PerFrame),
	}))
	defer srv.Close()

	c, err := cassette.Open(filepath.Join(t.TempDir(), "m.yaml"), cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	msg, _, _ := driveStream(t, anthropicClient(c, srv.URL), "hi") // must not panic

	full := "Hello, world! 🌍"
	var got string
	for _, b := range msg.Content {
		if b.Type == "text" {
			got += b.Text
		}
	}
	if got == full {
		t.Fatalf("truncated stream should not yield the full text; got %q", got)
	}
}
