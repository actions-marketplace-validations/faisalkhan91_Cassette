package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cassette "github.com/faisalkhan91/cassette"
)

func upPost(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The headline behavior: an empty dir records (forwarding to the upstream), and a
// re-run replays the same request OFFLINE — the upstream is never dialed again.
func TestUp_RecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	var hits int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","content":[{"type":"text","text":"hi"}]}`)
	}))
	defer upstream.Close()
	body := `{"model":"claude","messages":[{"role":"user","content":"hi"}]}`

	// Record leg.
	h, onShutdown, recording, recPath, code := buildUp(dir, upstream.URL, cassette.MatchConfig{}, false, io.Discard)
	if code != exitOK || !recording || recPath == "" {
		t.Fatalf("buildUp record: code=%d recording=%v path=%q", code, recording, recPath)
	}
	recSrv := httptest.NewServer(h)
	if st, _ := upPost(t, recSrv.URL+"/v1/messages", body); st != 200 {
		t.Fatalf("record POST status=%d", st)
	}
	recSrv.Close()
	if err := onShutdown(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("upstream hits after record = %d, want 1", got)
	}

	// Replay leg: same dir now has recording.yaml -> replay, zero upstream contact.
	h2, _, recording2, _, code2 := buildUp(dir, upstream.URL, cassette.MatchConfig{}, false, io.Discard)
	if code2 != exitOK || recording2 {
		t.Fatalf("buildUp replay: code=%d recording=%v", code2, recording2)
	}
	rpSrv := httptest.NewServer(h2)
	defer rpSrv.Close()
	st, got := upPost(t, rpSrv.URL+"/v1/messages", body)
	if st != 200 || !strings.Contains(got, "hi") {
		t.Fatalf("replay status=%d body=%q", st, got)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("replay dialed the upstream (hits=%d) — replay must be zero-egress", got)
	}
}

// A replay miss is diagnosed: 404 with an additive "hint" field, not a bare error.
func TestUp_DoctorOnMiss(t *testing.T) {
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	// Record one turn.
	h, onShutdown, _, _, _ := buildUp(dir, upstream.URL, cassette.MatchConfig{}, false, io.Discard)
	recSrv := httptest.NewServer(h)
	upPost(t, recSrv.URL+"/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"one"}]}`)
	recSrv.Close()
	onShutdown()

	// Replay with a DIFFERENT body -> miss -> 404 + hint.
	h2, _, _, _, _ := buildUp(dir, upstream.URL, cassette.MatchConfig{}, false, io.Discard)
	rpSrv := httptest.NewServer(h2)
	defer rpSrv.Close()
	st, got := upPost(t, rpSrv.URL+"/v1/messages", `{"model":"claude","messages":[{"role":"user","content":"DIFFERENT"}]}`)
	if st != http.StatusNotFound {
		t.Fatalf("miss status=%d, want 404", st)
	}
	if !strings.Contains(got, `"hint"`) || !strings.Contains(got, "no recorded interaction matches") {
		t.Fatalf("miss body missing diagnosis hint: %s", got)
	}
}

// up --pace is the timing-faithful mode: the record leg captures per-frame
// inter-arrival deltas (stream_timing), which a plain record omits.
func TestUp_PaceCapturesTiming(t *testing.T) {
	sse := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, f := range []string{"event: a\ndata: 1\n\n", "event: b\ndata: 2\n\n"} {
			io.WriteString(w, f)
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(12 * time.Millisecond)
		}
	}
	body := `{"model":"claude","messages":[{"role":"user","content":"hi"}]}`

	record := func(t *testing.T, pace bool) string {
		dir := t.TempDir()
		up := httptest.NewServer(http.HandlerFunc(sse))
		defer up.Close()
		h, onShutdown, _, recPath, code := buildUp(dir, up.URL, cassette.MatchConfig{}, pace, io.Discard)
		if code != exitOK {
			t.Fatalf("buildUp code=%d", code)
		}
		srv := httptest.NewServer(h)
		upPost(t, srv.URL+"/v1/messages", body)
		srv.Close()
		if err := onShutdown(); err != nil {
			t.Fatalf("save: %v", err)
		}
		raw, _ := os.ReadFile(recPath)
		return string(raw)
	}

	if got := record(t, true); !strings.Contains(got, "stream_timing") {
		t.Errorf("up --pace did not capture stream_timing:\n%s", got)
	}
	if got := record(t, false); strings.Contains(got, "stream_timing") {
		t.Errorf("plain up captured stream_timing (should be off by default):\n%s", got)
	}
}

func TestResolveUpstream(t *testing.T) {
	// Explicit upstream always wins.
	if u, ok := resolveUpstream("https://example.test", "/anything"); !ok || u.Host != "example.test" {
		t.Fatalf("explicit upstream not honored: %v %v", u, ok)
	}
	// Auto-detect from a known provider path.
	if u, ok := resolveUpstream("", "/v1/messages"); !ok || u.Host != "api.anthropic.com" {
		t.Fatalf("auto-detect anthropic failed: %v %v", u, ok)
	}
	// Ambiguous OpenAI-compatible path -> no guess.
	if _, ok := resolveUpstream("", "/v1/chat/completions"); ok {
		t.Fatal("ambiguous /chat/completions must not auto-resolve an upstream")
	}
}
