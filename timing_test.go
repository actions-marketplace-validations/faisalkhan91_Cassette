package cassette

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// frameReader yields at most one SSE frame per Read, so capture sees frames
// arrive one at a time.
type frameReader struct {
	frames [][]byte
	i      int
}

func (f *frameReader) Read(p []byte) (int, error) {
	if f.i >= len(f.frames) {
		return 0, io.EOF
	}
	n := copy(p, f.frames[f.i])
	if n < len(f.frames[f.i]) {
		f.frames[f.i] = f.frames[f.i][n:]
		return n, nil
	}
	f.i++
	return n, nil
}
func (f *frameReader) Close() error { return nil }

func TestStreamTiming_CaptureRecordsDeltas(t *testing.T) {
	body := []byte("event: a\ndata: 1\n\nevent: b\ndata: 2\n\nevent: c\ndata: 3\n\n")

	c, err := Open(filepath.Join(t.TempDir(), "t.yaml"), Options{Mode: ModeRecord, CaptureTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic clock: advances 10ms on every call.
	var ticks int64
	c.clock = func() time.Time { ticks++; return time.Unix(0, ticks*10*int64(time.Millisecond)) }

	c.file.Interactions = append(c.file.Interactions, &wirefmt.Interaction{
		Kind: "http", Response: wirefmt.Response{Streaming: true},
	})
	sc := c.newStreamCapture(&frameReader{frames: splitSSEFrames(body)}, 0, false)

	buf := make([]byte, 4096)
	for {
		if _, err := sc.Read(buf); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	_ = sc.Close()

	timing := c.file.Interactions[0].Response.StreamTiming
	if len(timing) != 3 {
		t.Fatalf("expected 3 per-frame deltas, got %v", timing)
	}
	for i, d := range timing {
		if d <= 0 {
			t.Fatalf("delta[%d] = %d, want > 0 (timing: %v)", i, d, timing)
		}
	}
}

func TestStreamTiming_PacedReplaySleeps(t *testing.T) {
	frames := splitSSEFrames([]byte("a\n\nb\n\nc\n\n"))
	delays := []int64{30, 10, 20}
	var slept []int64
	pr := &pacedReader{
		ctx:    context.Background(),
		sleep:  func(_ context.Context, d time.Duration) error { slept = append(slept, d.Milliseconds()); return nil },
		frames: frames,
		delays: delays,
	}
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a\n\nb\n\nc\n\n" {
		t.Fatalf("paced output altered bytes: %q", out)
	}
	if len(slept) != 3 || slept[0] != 30 || slept[1] != 10 || slept[2] != 20 {
		t.Fatalf("sleeps = %v, want [30 10 20]", slept)
	}
}

func TestStreamTiming_ContextCancelMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pr := &pacedReader{
		ctx:    ctx,
		sleep:  sleepCtx, // real context-aware sleep
		frames: splitSSEFrames([]byte("a\n\nb\n\n")),
		delays: []int64{60000, 60000}, // would block ~2min if not cancelled
	}
	cancel() // cancel before reading
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(pr)
		done <- err
	}()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paced reader did not honor context cancellation promptly")
	}
}

func TestStreamTiming_DefaultByteStable(t *testing.T) {
	// Recording the same SSE stream with timing OFF (default) must not add a
	// stream_timing field, and the saved bytes must be byte-stable across records.
	record := func(capture bool) []byte {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, freq := range []string{"event: a\ndata: 1\n\n", "event: b\ndata: 2\n\n"} {
				w.Write([]byte(freq))
				if fl != nil {
					fl.Flush()
				}
			}
		}))
		defer srv.Close()
		path := filepath.Join(t.TempDir(), "s.yaml")
		c, _ := Open(path, Options{Mode: ModeRecord, CaptureTiming: capture})
		resp, err := c.HTTPClient().Get(srv.URL + "/v1/messages")
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if err := c.VerifyError(); err != nil {
			t.Fatal(err)
		}
		data, _ := wirefmt.Load(path)
		raw, _ := wirefmt.Marshal(data)
		return raw
	}

	off1 := record(false)
	off2 := record(false)
	if string(off1) != string(off2) {
		t.Fatal("recording with timing OFF is not byte-stable")
	}
	if strings.Contains(string(off1), "stream_timing") {
		t.Fatalf("timing OFF must not write stream_timing:\n%s", off1)
	}
	on := record(true)
	if !strings.Contains(string(on), "stream_timing") {
		t.Fatalf("timing ON must write stream_timing:\n%s", on)
	}
}
