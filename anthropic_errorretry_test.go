package cassette_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

const okMessageJSON = `{"id":"msg_01OK0001","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":2}}`

const rateLimitJSON = `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`

func retryClient(c *cassette.Cassette, baseURL string) anthropic.Client {
	return anthropic.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(2),
	)
}

func TestRecordReplay_ErrorThenRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry.yaml")

	// --- RECORD: 429 + Retry-After, then 200, recorded as an ordered pair ---
	seq := fakeprovider.Sequence(
		fakeprovider.RawHandler(http.StatusTooManyRequests,
			http.Header{"Content-Type": {"application/json"}, "Retry-After": {"0"}},
			[]byte(rateLimitJSON)),
		fakeprovider.JSONHandler(http.StatusOK, []byte(okMessageJSON), nil),
	)
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{"/v1/messages": seq}))

	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	recCl := retryClient(rec, srv.URL)
	msg, err := recCl.Messages.New(context.Background(), streamParams("hi"))
	if err != nil {
		t.Fatalf("record New (should succeed after retry): %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatalf("record save: %v", err)
	}
	srv.Close()

	// Two interactions recorded with the same key, in order: 429 then 200.
	f, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.Interactions); n != 2 {
		t.Fatalf("expected 2 recorded interactions (429,200), got %d", n)
	}
	if f.Interactions[0].Response.Status != 429 || f.Interactions[1].Response.Status != 200 {
		t.Fatalf("recorded statuses not ordered 429,200: %d,%d",
			f.Interactions[0].Response.Status, f.Interactions[1].Response.Status)
	}
	if len(msg.Content) == 0 || msg.Content[0].Text != "ok" {
		t.Fatalf("record final message text != ok: %+v", msg.Content)
	}

	// --- REPLAY: retries enabled, server down. The same retry path is reproduced
	// entirely offline: first 429, retry, then 200. ---
	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	rpCl := retryClient(rp, "http://replay.invalid")
	rmsg, err := rpCl.Messages.New(context.Background(), streamParams("hi"))
	if err != nil {
		t.Fatalf("replay New (should succeed after offline retry): %v", err)
	}
	if len(rmsg.Content) == 0 || rmsg.Content[0].Text != "ok" {
		t.Fatalf("replay final message text != ok: %+v", rmsg.Content)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("replay verify (both 429 and 200 must be consumed): %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("replay made %d dials, want 0", rp.Dials())
	}
}
