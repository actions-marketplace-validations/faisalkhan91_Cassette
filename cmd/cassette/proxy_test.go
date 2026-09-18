package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/semequal"
)

func fakeUpstream(t *testing.T, text string, hits *int) *httptest.Server {
	t.Helper()
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: text, FinishReason: "end_turn"}, wireenc.Envelope{})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits++
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(sse)
	}))
}

const proxyReqBody = `{"model":"m","messages":[{"role":"user","content":"hello"}],"stream":true}`

func proxyPost(t *testing.T, serverURL string) string {
	t.Helper()
	resp, err := http.Post(serverURL+"/v1/messages", "application/json", strings.NewReader(proxyReqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	tr, _ := semequal.DecodeAnthropicSSE(b)
	return tr.Text
}

// record-mode proxy captures live traffic frame-exact; the result replays offline.
func TestProxy_RecordThenReplay(t *testing.T) {
	hits := 0
	upstream := fakeUpstream(t, "hi from upstream", &hits)
	defer upstream.Close()

	out := filepath.Join(t.TempDir(), "rec.yaml")
	handler, onShutdown, code := buildProxy(out, "record", upstream.URL, cassette.MatchConfig{}, nil, io.Discard, false)
	if code != exitOK || handler == nil || onShutdown == nil {
		t.Fatalf("buildProxy record: code=%d", code)
	}
	proxy := httptest.NewServer(handler)
	if got := proxyPost(t, proxy.URL); got != "hi from upstream" {
		t.Fatalf("proxied text=%q", got)
	}
	proxy.Close()
	if hits != 1 {
		t.Fatalf("upstream hits=%d, want 1", hits)
	}
	if err := onShutdown(); err != nil { // saves the cassette
		t.Fatal(err)
	}

	// Replay the recording offline with zero dials.
	handler2, _, code := buildProxy(out, "replay", "", cassette.MatchConfig{}, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("buildProxy replay: code=%d", code)
	}
	replay := httptest.NewServer(handler2)
	defer replay.Close()
	if got := proxyPost(t, replay.URL); got != "hi from upstream" {
		t.Fatalf("replayed text=%q", got)
	}
}

// branch-mode proxy replays the recorded prefix without touching the upstream.
func TestProxy_BranchReplaysPrefix(t *testing.T) {
	// Seed: record one interaction.
	hits := 0
	upstream := fakeUpstream(t, "seeded", &hits)
	defer upstream.Close()
	seed := filepath.Join(t.TempDir(), "seed.yaml")
	rh, save, _ := buildProxy(seed, "record", upstream.URL, cassette.MatchConfig{}, nil, io.Discard, false)
	rs := httptest.NewServer(rh)
	proxyPost(t, rs.URL)
	rs.Close()
	save()
	if hits != 1 {
		t.Fatalf("seed hits=%d", hits)
	}

	// Branch: the same request replays from the seed, no new upstream hit.
	bh, bsave, code := buildProxy(seed, "branch", upstream.URL, cassette.MatchConfig{}, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("buildProxy branch code=%d", code)
	}
	bs := httptest.NewServer(bh)
	if got := proxyPost(t, bs.URL); got != "seeded" {
		t.Fatalf("branch replay text=%q", got)
	}
	bs.Close()
	_ = bsave()
	if hits != 1 {
		t.Fatalf("branch dialed upstream on a matching request (hits=%d)", hits)
	}
}

func TestProxy_BuildErrors(t *testing.T) {
	if _, _, code := buildProxy("x.yaml", "bogus", "", cassette.MatchConfig{}, nil, io.Discard, false); code != exitUsage {
		t.Fatalf("bad mode code=%d", code)
	}
	if _, _, code := buildProxy("x.yaml", "record", "not-a-url", cassette.MatchConfig{}, nil, io.Discard, false); code != exitUsage {
		t.Fatalf("bad upstream code=%d", code)
	}
	if _, _, code := buildProxy("/no/such/dir-or-file", "replay", "", cassette.MatchConfig{}, nil, io.Discard, false); code != exitFail {
		t.Fatalf("missing replay source code=%d", code)
	}
}

func TestProxy_ArgValidation(t *testing.T) {
	if code, _, _ := runArgs("proxy", "x.yaml", "--mode", "record"); code != exitUsage {
		t.Fatalf("record without upstream exit=%d", code)
	}
	if code, _, _ := runArgs("proxy"); code != exitUsage {
		t.Fatalf("no-arg exit=%d", code)
	}
	// replay against a missing source returns through cmdProxy before binding.
	if code, _, _ := runArgs("proxy", "/no/such/file.yaml", "--mode", "replay"); code != exitFail {
		t.Fatalf("replay missing-source exit=%d", code)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1:8080", "localhost:9", "[::1]:8080", "127.0.0.1"} {
		if !isLoopbackAddr(a) {
			t.Errorf("%q should be loopback", a)
		}
	}
	for _, a := range []string{":8080", "0.0.0.0:8080", "192.168.1.5:8080", "example.com:443"} {
		if isLoopbackAddr(a) {
			t.Errorf("%q should NOT be loopback", a)
		}
	}
}

func TestForwardHandler_BadUpstream(t *testing.T) {
	if _, err := forwardHandler(http.DefaultClient, "not-a-url"); err == nil {
		t.Fatal("expected error for invalid upstream")
	}
}

// A dead upstream surfaces as a 502 from the proxy, not a panic.
func TestForwardHandler_UpstreamDown(t *testing.T) {
	h, err := forwardHandler(http.DefaultClient, "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(proxyReqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502", resp.StatusCode)
	}
}

// TestProxy_HonorsUpstreamBasePathAndHeaders proves the record proxy forwards to an
// upstream that lives under a base path (a corporate gateway like .../llm-proxy) and
// passes auth headers through — the real-world Claude Code / Codex → corporate LLM
// gateway shape. Regression guard for the dropped-base-path bug.
func TestProxy_HonorsUpstreamBasePathAndHeaders(t *testing.T) {
	var gotPath, gotAuth string
	sse := wireenc.Encode(wireenc.Anthropic, semequal.Transcript{Text: "ok", FinishReason: "end_turn"}, wireenc.Envelope{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(sse)
	}))
	defer upstream.Close()

	out := filepath.Join(t.TempDir(), "rec.yaml")
	// Upstream carries a base path, as a gateway does.
	handler, onShutdown, code := buildProxy(out, "record", upstream.URL+"/llm-proxy", cassette.MatchConfig{}, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("buildProxy: code=%d", code)
	}
	proxy := httptest.NewServer(handler)
	req, _ := http.NewRequest("POST", proxy.URL+"/v1/messages", strings.NewReader(proxyReqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "sk-ant-secret-forwarded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	proxy.Close()
	_ = onShutdown()

	if gotPath != "/llm-proxy/v1/messages" {
		t.Errorf("upstream saw path %q, want /llm-proxy/v1/messages (base path was dropped)", gotPath)
	}
	if gotAuth != "sk-ant-secret-forwarded" {
		t.Errorf("auth header not forwarded to upstream: %q", gotAuth)
	}
}

func TestSingleJoiningSlash(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "/v1/messages", "/v1/messages"}, // no base path (api.anthropic.com)
		{"/llm-proxy", "/v1/messages", "/llm-proxy/v1/messages"},
		{"/llm-proxy/", "/v1/messages", "/llm-proxy/v1/messages"},
		{"/p", "v1", "/p/v1"},
	}
	for _, c := range cases {
		if got := singleJoiningSlash(c.a, c.b); got != c.want {
			t.Errorf("singleJoiningSlash(%q,%q)=%q want %q", c.a, c.b, got, c.want)
		}
	}
}

// TestProxy_VolatilePathsMatch proves --volatile lets a recorded session replay
// even when the client injects a per-run volatile field (the real Claude Code /
// Codex case: metadata.user_id, an env-stamped system prompt). Record and replay
// both drop the volatile path before keying, so a differing value still matches.
func TestProxy_VolatilePathsMatch(t *testing.T) {
	upstream := fakeUpstream(t, "hi", nil)
	defer upstream.Close()
	out := filepath.Join(t.TempDir(), "v.yaml")
	vol := cassette.MatchConfig{VolatileJSONPaths: []string{"metadata.nonce"}}

	rh, save, code := buildProxy(out, "record", upstream.URL, vol, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("record build: %d", code)
	}
	rec := httptest.NewServer(rh)
	body := func(nonce string) string {
		return `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"metadata":{"nonce":"` + nonce + `"}}`
	}
	post := func(srv, b string) int {
		resp, err := http.Post(srv+"/v1/messages", "application/json", strings.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	post(rec.URL, body("AAA")) // record with one nonce
	rec.Close()
	_ = save()

	rph, _, code := buildProxy(out, "replay", "", vol, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("replay build: %d", code)
	}
	replay := httptest.NewServer(rph)
	defer replay.Close()
	if st := post(replay.URL, body("ZZZ")); st != 200 { // different nonce → still matches
		t.Fatalf("replay with a different volatile nonce got %d, want 200 (volatile path not dropped)", st)
	}
}

// TestProxy_RealWorldVolatileProfiles is the committed, synthetic regression for
// the docs/recipes/realworld.md setup: it reproduces the request SHAPES of Claude
// Code (/v1/messages with a volatile metadata.user_id + env-stamped system) and
// Codex (/v1/responses with volatile client_metadata + prompt_cache_key), and
// proves that with the documented --volatile paths a session records under one set
// of volatile values and replays under different ones — fully offline. No real
// capture; pure synthetic bodies.
func TestProxy_RealWorldVolatileProfiles(t *testing.T) {
	profiles := []struct {
		name, path, volatile string
		bodyA, bodyB         string
	}{
		{
			name: "claude-code", path: "/llm-proxy/v1/messages", volatile: "metadata.user_id,system",
			bodyA: `{"model":"claude","system":[{"type":"text","text":"env A: 2026-06-29"}],"messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"sess-AAAA"},"stream":true}`,
			bodyB: `{"model":"claude","system":[{"type":"text","text":"env B: 2026-06-30"}],"messages":[{"role":"user","content":"hi"}],"metadata":{"user_id":"sess-BBBB"},"stream":true}`,
		},
		{
			name: "codex", path: "/llm-proxy/v1/responses", volatile: "client_metadata,prompt_cache_key",
			bodyA: `{"model":"gpt-5","input":[{"role":"user","content":"hi"}],"client_metadata":{"session_id":"AAAA","turn_id":"a1"},"prompt_cache_key":"AAAA","stream":true}`,
			bodyB: `{"model":"gpt-5","input":[{"role":"user","content":"hi"}],"client_metadata":{"session_id":"BBBB","turn_id":"b2"},"prompt_cache_key":"BBBB","stream":true}`,
		},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			upstream := fakeUpstream(t, "hello from cassette", nil)
			defer upstream.Close()
			out := filepath.Join(t.TempDir(), "s.yaml")
			vol := cassette.MatchConfig{VolatileJSONPaths: strings.Split(p.volatile, ",")}

			rh, save, code := buildProxy(out, "record", upstream.URL, vol, nil, io.Discard, false)
			if code != exitOK {
				t.Fatalf("record build %d", code)
			}
			rec := httptest.NewServer(rh)
			post := func(srv, body string) int {
				resp, err := http.Post(srv+p.path, "application/json", strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				io.ReadAll(resp.Body)
				resp.Body.Close()
				return resp.StatusCode
			}
			post(rec.URL, p.bodyA) // record with volatile set A
			rec.Close()
			_ = save()

			rph, _, code := buildProxy(out, "replay", "", vol, nil, io.Discard, false)
			if code != exitOK {
				t.Fatalf("replay build %d", code)
			}
			replay := httptest.NewServer(rph)
			defer replay.Close()
			if st := post(replay.URL, p.bodyB); st != 200 { // volatile set B → still matches
				t.Fatalf("%s: replay with fresh volatile values got %d, want 200", p.name, st)
			}
		})
	}
}

// TestProxy_PersistedVolatileSelfDescribing: a cassette recorded with --volatile
// persists its match normalization, so replay reproduces the keys WITHOUT the
// caller re-supplying --volatile.
func TestProxy_PersistedVolatileSelfDescribing(t *testing.T) {
	upstream := fakeUpstream(t, "ok", nil)
	defer upstream.Close()
	out := filepath.Join(t.TempDir(), "v.yaml")
	body := func(n string) string {
		return `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"metadata":{"nonce":"` + n + `"}}`
	}
	post := func(url, b string) int {
		resp, err := http.Post(url+"/v1/messages", "application/json", strings.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	rh, save, _ := buildProxy(out, "record", upstream.URL, cassette.MatchConfig{VolatileJSONPaths: []string{"metadata.nonce"}}, nil, io.Discard, false)
	rec := httptest.NewServer(rh)
	post(rec.URL, body("AAA"))
	rec.Close()
	_ = save()

	// Replay with NO match config — the persisted match block must drive it.
	rph, _, code := buildProxy(out, "replay", "", cassette.MatchConfig{}, nil, io.Discard, false)
	if code != exitOK {
		t.Fatalf("replay build %d", code)
	}
	replay := httptest.NewServer(rph)
	defer replay.Close()
	if st := post(replay.URL, body("ZZZ")); st != 200 {
		t.Fatal("self-describing replay failed: persisted --volatile not applied on reopen")
	}
}
