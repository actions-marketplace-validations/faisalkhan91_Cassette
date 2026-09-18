package cassette

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func recordOptions() Options {
	return Options{Mode: ModeRecord, Scrub: DefaultScrubConfig()}
}

// doPost issues a POST through c's client and returns the response body string.
func doPost(t *testing.T, c *Cassette, url, body string) (int, string) {
	t.Helper()
	resp, err := c.HTTPClient().Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

func TestRoundTrip_RecordRestoresBody(t *testing.T) {
	const reqBody = `{"model":"claude","max_tokens":7,"messages":[{"role":"user","content":"hello world"}]}`
	var got string
	var gotLen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		gotLen = r.ContentLength
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_1","content":"hi"}`))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "rt.yaml")
	c, err := Open(path, recordOptions())
	if err != nil {
		t.Fatal(err)
	}
	doPost(t, c, srv.URL+"/v1/messages", reqBody)

	if got != reqBody {
		t.Fatalf("server did not receive full body byte-for-byte:\n got=%q\n want=%q", got, reqBody)
	}
	if gotLen != int64(len(reqBody)) {
		t.Fatalf("server saw ContentLength=%d, want %d (body restore failed)", gotLen, len(reqBody))
	}
	if err := c.VerifyError(); err != nil {
		t.Fatalf("verify/save: %v", err)
	}
}

func TestReplay_BlocksNetwork(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	path := filepath.Join(t.TempDir(), "block.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	rec, _ := Open(path, recordOptions())
	doPost(t, rec, srv.URL+"/v1/messages", `{"a":1}`)
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close() // replay must be server-independent

	rp, err := Open(path, Options{Mode: ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	status, body := doPost(t, rp, "http://10.255.255.1:1/v1/messages", `{"a":1}`)
	if status != 200 || body != `{"ok":true}` {
		t.Fatalf("unexpected replay result: %d %q", status, body)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("expected 0 dials, got %d", rp.Dials())
	}
}

func TestReplay_UnrecordedRequestErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miss.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	rec, _ := Open(path, recordOptions())
	doPost(t, rec, srv.URL+"/v1/messages", `{"recorded":true}`)
	rec.VerifyError()
	srv.Close()

	rp, _ := Open(path, Options{Mode: ModeReplay})
	_, err := rp.HTTPClient().Post("http://x/v1/messages", "application/json", strings.NewReader(`{"NOT":"recorded"}`))
	if err == nil {
		t.Fatal("expected loud error on unrecorded request, got nil (silent passthrough?)")
	}
	if !strings.Contains(err.Error(), "no recorded interaction") {
		t.Fatalf("expected ErrNoMatch, got: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("a miss must not dial; got %d dials", rp.Dials())
	}
}

func TestReplay_DuplicateRequestsOrdered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dup.yaml")
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt64(&n, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"seq":%d}`, i)
	}))
	rec, _ := Open(path, recordOptions())
	doPost(t, rec, srv.URL+"/v1/messages", `{"same":"body"}`)
	doPost(t, rec, srv.URL+"/v1/messages", `{"same":"body"}`)
	rec.VerifyError()
	srv.Close()

	rp, _ := Open(path, Options{Mode: ModeReplay})
	_, b1 := doPost(t, rp, "http://x/v1/messages", `{"same":"body"}`)
	_, b2 := doPost(t, rp, "http://x/v1/messages", `{"same":"body"}`)
	if b1 != `{"seq":1}` || b2 != `{"seq":2}` {
		t.Fatalf("byte-identical requests must replay distinct responses in order: got %q then %q", b1, b2)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestReplay_Concurrent(t *testing.T) {
	const N = 16
	path := filepath.Join(t.TempDir(), "conc.yaml")
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt64(&n, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"seq":%d}`, i)
	}))
	rec, _ := Open(path, recordOptions())
	for i := 0; i < N; i++ {
		doPost(t, rec, srv.URL+"/v1/messages", `{"same":"body"}`) // identical key, N distinct responses
	}
	rec.VerifyError()
	srv.Close()

	rp, _ := Open(path, Options{Mode: ModeReplay})
	var wg sync.WaitGroup
	results := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, b := doPost(t, rp, "http://x/v1/messages", `{"same":"body"}`)
			results[i] = b
		}(i)
	}
	wg.Wait()

	seen := map[string]int{}
	for _, r := range results {
		seen[r]++
	}
	for i := 1; i <= N; i++ {
		key := fmt.Sprintf(`{"seq":%d}`, i)
		if seen[key] != 1 {
			t.Fatalf("response %q returned %d times, want exactly 1; full: %v", key, seen[key], seen)
		}
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("expected 0 dials, got %d", rp.Dials())
	}
}

func TestReplay_MatchesDespiteVolatileRequestFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "volatile.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	opts := recordOptions()
	opts.Match = MatchConfig{VolatileJSONPaths: []string{"metadata.ts"}}
	rec, _ := Open(path, opts)

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude","metadata":{"ts":"2026-01-01","u":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "sk-ant-AAA")
	req.Header.Set("X-Request-Id", "req-111")
	req.Header.Set("Idempotency-Key", "idem-111")
	resp, err := rec.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.VerifyError()
	srv.Close()

	rp, _ := Open(path, Options{Mode: ModeReplay, Match: MatchConfig{VolatileJSONPaths: []string{"metadata.ts"}}})
	// Different api-key, request-id, idempotency-key, timestamp, and reordered keys.
	req2, _ := http.NewRequest("POST", "http://x/v1/messages",
		strings.NewReader(`{"metadata":{"u":"a","ts":"2026-12-31"},"model":"claude"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("x-api-key", "sk-ant-ZZZ")
	req2.Header.Set("X-Request-Id", "req-999")
	req2.Header.Set("Idempotency-Key", "idem-999")
	resp2, err := rp.HTTPClient().Do(req2)
	if err != nil {
		t.Fatalf("volatile-only differences must still match: %v", err)
	}
	defer resp2.Body.Close()
	b, _ := io.ReadAll(resp2.Body)
	if string(b) != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", b)
	}
}

func TestExhaustPolicies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exhaust.yaml")
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt64(&n, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"seq":%d}`, i)
	}))
	rec, _ := Open(path, recordOptions())
	doPost(t, rec, srv.URL+"/v1/messages", `{"x":1}`)
	doPost(t, rec, srv.URL+"/v1/messages", `{"x":1}`)
	rec.VerifyError()
	srv.Close()

	t.Run("error", func(t *testing.T) {
		rp, _ := Open(path, Options{Mode: ModeReplay, OnExhausted: ExhaustError})
		doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
		doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
		_, err := rp.HTTPClient().Post("http://x/v1/messages", "application/json", strings.NewReader(`{"x":1}`))
		if err == nil {
			t.Fatal("expected exhaustion error")
		}
	})
	t.Run("repeat_last", func(t *testing.T) {
		rp, _ := Open(path, Options{Mode: ModeReplay, OnExhausted: ExhaustRepeatLast})
		doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
		doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
		_, b := doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
		if b != `{"seq":2}` {
			t.Fatalf("repeat_last should return last response, got %q", b)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		rp, _ := Open(path, Options{Mode: ModeReplay, OnExhausted: ExhaustCycle})
		var got []string
		for i := 0; i < 4; i++ {
			_, b := doPost(t, rp, "http://x/v1/messages", `{"x":1}`)
			got = append(got, b)
		}
		want := []string{`{"seq":1}`, `{"seq":2}`, `{"seq":1}`, `{"seq":2}`}
		if !equalSlices(got, want) {
			t.Fatalf("cycle order wrong: got %v want %v", got, want)
		}
	})
}

func equalSlices(a, b []string) bool {
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

func TestErrNoMatchWrapped(t *testing.T) {
	// Ensure ErrNoMatch is detectable via errors.Is through the http.Client error.
	path := filepath.Join(t.TempDir(), "empty.yaml")
	rec, _ := Open(path, recordOptions())
	rec.VerifyError() // writes an empty cassette

	rp, _ := Open(path, Options{Mode: ModeReplay})
	_, err := rp.replay(mustReq(t, `{"a":1}`), []byte(`{"a":1}`))
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("expected errors.Is(err, ErrNoMatch), got %v", err)
	}
}

func mustReq(t *testing.T, body string) *http.Request {
	t.Helper()
	r, err := http.NewRequest("POST", "http://x/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}

// TestReplay_RequestBodySecretStillMatches is a regression test for the bug where
// scrubbing a secret in the REQUEST body changed the stored body and broke the
// recomputed match key on a FRESH replay process. The persisted match key makes
// replay matching independent of body scrubbing.
func TestReplay_RequestBodySecretStillMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bodysecret.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	rec, _ := Open(path, recordOptions()) // DefaultConfig scrubs sk-... in bodies
	doPost(t, rec, srv.URL+"/v1/messages", `{"api_key":"sk-ant-REALSECRET999","model":"claude"}`)
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	// Confirm the secret was actually scrubbed from the stored body.
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "sk-ant-REALSECRET999") {
		t.Fatalf("secret not scrubbed from stored cassette:\n%s", raw)
	}

	// Fresh replay process loads the SCRUBBED file; the original request must match.
	rp, err := Open(path, Options{Mode: ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	status, body := doPost(t, rp, "http://x/v1/messages", `{"api_key":"sk-ant-REALSECRET999","model":"claude"}`)
	if status != 200 || body != `{"ok":true}` {
		t.Fatalf("scrubbed-body request failed to replay: %d %q", status, body)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// TestReplay_GzipRequestBodyMatches is a regression test for the record/replay
// asymmetry on gzip-encoded request bodies.
func TestReplay_GzipRequestBodyMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gzipreq.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	plain := []byte(`{"model":"claude","max_tokens":5}`)
	gzBody := func() []byte {
		var b strings.Builder
		zw := gzip.NewWriter(stringWriter{&b})
		zw.Write(plain)
		zw.Close()
		return []byte(b.String())
	}

	rec, _ := Open(path, recordOptions())
	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", bytes.NewReader(gzBody()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := rec.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}

	// Fresh replay: a gzip-encoded request with the same logical body must match.
	rp, _ := Open(path, Options{Mode: ModeReplay})
	req2, _ := http.NewRequest("POST", "http://x/v1/messages", bytes.NewReader(gzBody()))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Content-Encoding", "gzip")
	resp2, err := rp.HTTPClient().Do(req2)
	if err != nil {
		t.Fatalf("gzip request replay failed: %v", err)
	}
	defer resp2.Body.Close()
	b, _ := io.ReadAll(resp2.Body)
	if string(b) != `{"ok":true}` {
		t.Fatalf("unexpected gzip replay body: %q", b)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

type stringWriter struct{ b *strings.Builder }

func (w stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// TestOpen_ScrubsByDefault is a regression test: Open() (not just the New test
// helper) must redact secrets by default, so a direct caller never writes a real
// API key to disk. Surfaced by a real proxy call leaking a key.
func TestOpen_ScrubsByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "default-scrub.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// No Scrub config set — must still scrub.
	c, err := Open(path, Options{Mode: ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "sk-ant-REALLOOKINGKEY123456")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := c.VerifyError(); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "sk-ant-REALLOOKINGKEY123456") {
		t.Fatalf("Open() did not scrub the api key by default:\n%s", raw)
	}
	if scrub.HasSecrets(raw) {
		t.Fatalf("Open() default scrub left secret patterns:\n%s", raw)
	}

	// DisableScrub must opt out (and is the ONLY way to do so).
	path2 := filepath.Join(t.TempDir(), "noscrub.yaml")
	c2, _ := Open(path2, Options{Mode: ModeRecord, DisableScrub: true})
	req2, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Api-Key", "sk-ant-PLAINTEXT")
	resp2, _ := c2.HTTPClient().Do(req2)
	io.ReadAll(resp2.Body)
	resp2.Body.Close()
	c2.VerifyError()
	raw2, _ := os.ReadFile(path2)
	if !strings.Contains(string(raw2), "sk-ant-PLAINTEXT") {
		t.Fatal("DisableScrub should have left the key unscrubbed")
	}
}

// TestOpen_PartialScrubMergesDefaults proves a partial ScrubConfig (here only
// BodyPatterns) keeps the default response stamps — setting one field must not
// silently drop the determinism/secret defaults.
func TestOpen_PartialScrubMergesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-scrub.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// "id" is a DEFAULT stamp; "note" carries a caller-specific secret.
		w.Write([]byte(`{"id":"msg_REAL123","created":1700000000,"note":"TOPSECRET"}`))
	}))
	defer srv.Close()

	// Caller sets ONLY BodyPatterns — RedactHeaders/ResponseStamps left nil.
	c, err := Open(path, Options{Mode: ModeRecord, Scrub: ScrubConfig{
		BodyPatterns: []*regexp.Regexp{regexp.MustCompile(`TOPSECRET`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	doPost(t, c, srv.URL+"/v1/messages", `{"model":"claude"}`)
	if err := c.VerifyError(); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, "TOPSECRET") {
		t.Errorf("caller BodyPatterns not applied:\n%s", s)
	}
	if strings.Contains(s, "msg_REAL123") || !strings.Contains(s, "[SCRUBBED]") {
		t.Errorf("default response stamp dropped by partial scrub config:\n%s", s)
	}
}

// TestMatcher_RecordReplayWithBodyTransform proves a custom BodyTransform flows
// from record (persisted key) to replay: a replayed request that differs only in
// a transformed-away field still matches the recording.
func TestMatcher_RecordReplayWithBodyTransform(t *testing.T) {
	strip := func(_ string, body []byte, _ string) []byte {
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return body
		}
		delete(m, "nonce")
		out, _ := json.Marshal(m)
		return out
	}
	path := filepath.Join(t.TempDir(), "transform.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	opts := recordOptions()
	opts.Match = MatchConfig{BodyTransform: strip}
	rec, _ := Open(path, opts)
	doPost(t, rec, srv.URL+"/v1/messages", `{"q":"hi","nonce":"RECORD"}`)
	if err := rec.VerifyError(); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	rp, _ := Open(path, Options{Mode: ModeReplay, Match: MatchConfig{BodyTransform: strip}})
	// Different nonce — must still match because the transform drops it.
	status, body := doPost(t, rp, "http://x/v1/messages", `{"q":"hi","nonce":"REPLAY-DIFFERENT"}`)
	if status != 200 || body != `{"ok":true}` {
		t.Fatalf("transform-normalized request failed to replay: %d %q", status, body)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// TestPublicMatchConfig_KeyFuncObservesInput exercises the public MatchConfig.KeyFunc
// closure end-to-end (record→replay), asserting the adapter maps internal match.Input
// to the public cassette.MatchInput correctly — the regression guard for the
// public-types refactor (the wrapper was previously built but never invoked in tests).
func TestPublicMatchConfig_KeyFuncObservesInput(t *testing.T) {
	var seen []MatchInput
	kf := func(in MatchInput) (string, error) {
		seen = append(seen, in)
		return in.Method + " " + in.Path, nil // deterministic, identical at record + replay
	}
	path := filepath.Join(t.TempDir(), "keyfunc.yaml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	opts := recordOptions()
	opts.Match = MatchConfig{KeyFunc: kf}
	rec, _ := Open(path, opts)
	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := rec.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.VerifyError()
	srv.Close()

	if len(seen) == 0 {
		t.Fatal("KeyFunc was never invoked at record time")
	}
	got := seen[len(seen)-1]
	if got.Method != "POST" || got.Path != "/v1/messages" {
		t.Errorf("KeyFunc saw Method=%q Path=%q, want POST /v1/messages", got.Method, got.Path)
	}
	if string(got.Body) != `{"model":"claude"}` {
		t.Errorf("KeyFunc saw Body=%q, want the request body", got.Body)
	}
	if got.ContentType != "application/json" {
		t.Errorf("KeyFunc saw ContentType=%q", got.ContentType)
	}

	// Replay with the same KeyFunc must match (same key derived from the same input).
	rp, _ := Open(path, Options{Mode: ModeReplay, Match: MatchConfig{KeyFunc: kf}})
	req2, _ := http.NewRequest("POST", "http://x/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := rp.HTTPClient().Do(req2)
	if err != nil {
		t.Fatalf("replay with public KeyFunc did not match: %v", err)
	}
	io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if rp.Dials() != 0 {
		t.Errorf("replay dialed the network %d time(s)", rp.Dials())
	}
}

// TestStreamCapture_TruncatedGzipDropsBody: when a gzip SSE capture is truncated
// (e.g. context-cancelled mid-stream), finalize must NOT store the compressed bytes
// (they'd replay as undecodable text once Content-Encoding is stripped) — it drops
// the body and records the reason. Success path stays byte-identical.
func TestStreamCapture_TruncatedGzipDropsBody(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("data: {\"x\":1}\n\n"))
	gw.Close()
	truncated := buf.Bytes()[:buf.Len()/2] // cut mid-member → gunzip fails

	c, err := Open(filepath.Join(t.TempDir(), "x.yaml"), Options{Mode: ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	idx := c.appendInteraction(&wirefmt.Interaction{Kind: "http", Response: wirefmt.Response{Streaming: true}})
	sc := c.newStreamCapture(io.NopCloser(bytes.NewReader(truncated)), idx, true)
	io.ReadAll(sc) // drive Read to EOF → finalize

	got := c.file.Interactions[idx].Response
	if len(got.Body.Bytes()) != 0 {
		t.Errorf("truncated gzip should drop the body, got %d bytes", len(got.Body.Bytes()))
	}
	if bytes.Equal(got.Body.Bytes(), truncated) {
		t.Error("stored the raw compressed bytes (would replay as garbage)")
	}
	if got.Error == "" {
		t.Error("expected a capture-error note on the interaction")
	}

	// Success path: a complete gzip member decodes and leaves Error empty.
	var ok bytes.Buffer
	gw2 := gzip.NewWriter(&ok)
	gw2.Write([]byte("data: {\"y\":2}\n\n"))
	gw2.Close()
	idx2 := c.appendInteraction(&wirefmt.Interaction{Kind: "http", Response: wirefmt.Response{Streaming: true}})
	io.ReadAll(c.newStreamCapture(io.NopCloser(bytes.NewReader(ok.Bytes())), idx2, true))
	if c.file.Interactions[idx2].Response.Error != "" {
		t.Errorf("complete gzip must not set an error: %q", c.file.Interactions[idx2].Response.Error)
	}
}
