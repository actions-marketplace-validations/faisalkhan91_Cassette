package cassette_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func intptr(n int) *int { return &n }

// TestFault_SyntheticRetryThenSuccess: a chaos overlay injects a 429 on attempt
// 1 WITHOUT consuming the recording; the SDK's retry then reaches the real 200,
// entirely offline (dials==0), and the cassette is never mutated.
func TestFault_SyntheticRetryThenSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.yaml")
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.JSONHandler(http.StatusOK, []byte(okMessageJSON), nil),
	}))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	recCl := retryClient(rec, srv.URL)
	if _, err := recCl.Messages.New(context.Background(), streamParams("hi")); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	rp, _ := cassette.Open(path, cassette.Options{
		Mode: cassette.ModeReplay,
		Faults: cassette.FaultSet{
			{Path: "/v1/messages", Attempt: 1, Status: 429, RetryAfter: "0", Body: rateLimitJSON},
		},
	})
	rpCl := anthropic.NewClient(
		option.WithHTTPClient(rp.HTTPClient()),
		option.WithBaseURL("http://replay.invalid"),
		option.WithAPIKey("x"),
		option.WithMaxRetries(2),
	)
	msg, err := rpCl.Messages.New(context.Background(), streamParams("hi"))
	if err != nil {
		t.Fatalf("replay through injected 429 should recover: %v", err)
	}
	if len(msg.Content) == 0 || msg.Content[0].Text != "ok" {
		t.Fatalf("final content != ok: %+v", msg.Content)
	}
	if rp.Dials() != 0 {
		t.Fatalf("dials = %d", rp.Dials())
	}
}

// TestFault_TruncateModifiesStreamButNotCassette: a modify fault truncates the
// served stream; the agent sees an incomplete result, yet the stored cassette
// bytes are unchanged.
func TestFault_TruncateModifiesStreamButNotCassette(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.yaml")
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.StreamHandler(wirefix.AnthropicText, fakeprovider.PerFrame),
	}))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if _, _, err := driveStream(t, anthropicClient(rec, srv.URL), "hi"); err != nil {
		t.Fatal(err)
	}
	rec.VerifyError()
	srv.Close()

	rp, _ := cassette.Open(path, cassette.Options{
		Mode:   cassette.ModeReplay,
		Faults: cassette.FaultSet{{Path: "/v1/messages", TruncateAfterFrame: intptr(4)}},
	})
	msg, _, _ := driveStream(t, anthropicClient(rp, "http://replay.invalid"), "hi")
	var got string
	for _, b := range msg.Content {
		if b.Type == "text" {
			got += b.Text
		}
	}
	if got == "Hello, world! 🌍" {
		t.Fatal("truncate fault should yield an incomplete stream")
	}

	// The pristine cassette is unchanged.
	f, _ := wirefmt.Load(path)
	if !bytes.Equal(f.Interactions[0].Response.Body.Bytes(), wirefix.AnthropicText) {
		t.Fatal("fault overlay must not mutate the recorded cassette")
	}
}

// TestFault_UnboundedSyntheticVerifyOK: an unbounded synthetic fault (no Attempt
// => every attempt) intentionally shadows the recording on every request, so the
// recording is never consumed — Verify must treat that as expected, not as a
// "never replayed" failure (the always-on-fault trap).
func TestFault_UnboundedSyntheticVerifyOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.yaml")
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.JSONHandler(http.StatusOK, []byte(okMessageJSON), nil),
	}))
	rec, _ := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	recCl := retryClient(rec, srv.URL)
	if _, err := recCl.Messages.New(context.Background(), streamParams("hi")); err != nil {
		t.Fatalf("record: %v", err)
	}
	rec.VerifyError()
	srv.Close()

	rp, _ := cassette.Open(path, cassette.Options{
		Mode:   cassette.ModeReplay,
		Faults: cassette.FaultSet{{Path: "/v1/messages", Status: 429, Body: rateLimitJSON}}, // Attempt omitted => every attempt
	})
	resp, err := rp.HTTPClient().Post("http://replay.invalid/v1/messages", "application/json", bytes.NewReader([]byte(`{"x":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 429 {
		t.Fatalf("expected synthetic 429, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("an unbounded synthetic fault must not make Verify fail: %v", err)
	}
}

func TestLoadFaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.chaos.yaml")
	os.WriteFile(p, []byte("faults:\n  - path: /v1/messages\n    attempt: 1\n    status: 429\n    retry_after: \"0\"\n  - path: /v1/messages\n    truncate_after_frame: 3\n"), 0o644)
	fs, err := cassette.LoadFaults(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 || fs[0].Status != 429 || fs[0].Attempt != 1 || fs[1].TruncateAfterFrame == nil || *fs[1].TruncateAfterFrame != 3 {
		t.Fatalf("unexpected parse: %+v", fs)
	}
}
