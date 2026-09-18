package cassette_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/otrans"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// recordTo records the given fixture (served at path) into a replay-ready cassette.
func recordTo(t *testing.T, cassettePath, route string, fixture []byte) {
	t.Helper()
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		route: fakeprovider.StreamHandler(fixture, fakeprovider.PerFrame),
	}))
	defer srv.Close()
	rec, err := cassette.Open(cassettePath, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	switch route {
	case "/v1/messages":
		if _, _, err := driveStream(t, anthropicClient(rec, srv.URL), "hi"); err != nil {
			t.Fatal(err)
		}
	case "/chat/completions":
		params := openai.ChatCompletionNewParams{
			Model:    openai.ChatModelGPT4o,
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		}
		if _, _, err := driveOpenAIStream(t, openaiClient(rec, srv.URL), params); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
}

func TestServe_AnthropicSDK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	recordTo(t, path, "/v1/messages", wirefix.AnthropicText)

	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(rp.Handler(cassette.ServeOptions{}))
	defer srv.Close()

	// A real Anthropic SDK client pointed at the cassette server.
	msg, _, err := driveStream(t, anthropicClient(rp, srv.URL), "hi")
	if err != nil {
		t.Fatalf("sdk via serve: %v", err)
	}
	want, _ := semequal.DecodeAnthropicSSE(wirefix.AnthropicText)
	if atrans.FromMessage(msg).Digest() != want.Digest() {
		t.Fatal("transcript via serve != fixture transcript")
	}
}

func TestServe_OpenAISDK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	recordTo(t, path, "/chat/completions", wirefix.OpenAIText)

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	srv := httptest.NewServer(rp.Handler(cassette.ServeOptions{}))
	defer srv.Close()

	client := openai.NewClient(
		option.WithHTTPClient(http.DefaultClient),
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("x"),
		option.WithMaxRetries(0),
	)
	acc := openai.ChatCompletionAccumulator{}
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	for stream.Next() {
		acc.AddChunk(stream.Current())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai sdk via serve: %v", err)
	}
	want, _ := semequal.DecodeOpenAISSE(wirefix.OpenAIText)
	if otrans.FromChatCompletion(acc.ChatCompletion).Digest() != want.Digest() {
		t.Fatal("openai transcript via serve != fixture transcript")
	}
}

func TestServe_RawByteIdentical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	recordTo(t, path, "/v1/messages", wirefix.AnthropicText)
	f, _ := wirefmt.Load(path)
	reqBody := f.Interactions[0].Request.Body.Bytes()

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	srv := httptest.NewServer(rp.Handler(cassette.ServeOptions{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, wirefix.AnthropicText) {
		t.Fatalf("served SSE bytes not identical to fixture:\n got=%q", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestServe_StrictMissAnd404(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	recordTo(t, path, "/v1/messages", wirefix.AnthropicText)
	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	srv := httptest.NewServer(rp.Handler(cassette.ServeOptions{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"unrecorded":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("miss should be 404, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("cassette_miss")) {
		t.Fatalf("expected miss JSON, got %q", b)
	}
}

func TestServe_Concurrent(t *testing.T) {
	// Record N identical requests with N distinct responses, then serve them; N
	// concurrent clients must each get a distinct recorded response exactly once.
	const N = 12
	path := filepath.Join(t.TempDir(), "c.yaml")
	var seq int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seq++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"n":` + itoa(seq) + `}`))
	}))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	for i := 0; i < N; i++ {
		resp, _ := rec.HTTPClient().Post(srv.URL+"/v1/x", "application/json", strings.NewReader(`{"same":1}`))
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	rec.VerifyError()
	srv.Close()

	rp, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	csrv := httptest.NewServer(rp.Handler(cassette.ServeOptions{}))
	defer csrv.Close()

	var wg sync.WaitGroup
	got := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := http.Post(csrv.URL+"/v1/x", "application/json", strings.NewReader(`{"same":1}`))
			if err != nil {
				return
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			got[i] = string(b)
		}(i)
	}
	wg.Wait()
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for i := 1; i <= N; i++ {
		if seen[`{"n":`+itoa(i)+`}`] != 1 {
			t.Fatalf("response n=%d returned %d times; full: %v", i, seen[`{"n":`+itoa(i)+`}`], seen)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
