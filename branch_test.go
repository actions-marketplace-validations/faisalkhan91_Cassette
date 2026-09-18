package cassette_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// branchSeed builds a 1-turn seed cassette (the deterministic prefix) recorded
// against /v1/messages, with a persisted match key.
func branchSeed(t *testing.T) string {
	t.Helper()
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.HeadersFromHTTP(map[string][]string{"Content-Type": {"application/json"}}),
			Body:    wirefmt.NewBody([]byte(`{"model":"m","turn":1}`))},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"reply":"prefix turn 1"}`))},
	}}}
	p := filepath.Join(t.TempDir(), "seed.yaml")
	if err := wirefmt.Save(p, f); err != nil {
		t.Fatal(err)
	}
	return p // index() recomputes the match key on load when absent
}

func TestBranch_PrefixReplaysThenLiveSuffix(t *testing.T) {
	// A "live" provider for the divergent suffix.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if got := r.Header.Get("Authorization"); got != "Bearer LIVEKEY" {
			t.Errorf("auth not re-injected: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"reply":"live suffix"}`)
	}))
	defer srv.Close()

	seed := branchSeed(t)
	c, err := cassette.Open(seed, cassette.Options{
		Mode: cassette.ModeBranch,
		Live: cassette.LiveTransport(srv.URL, func(r *http.Request) { r.Header.Set("Authorization", "Bearer LIVEKEY") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := c.HTTPClient()

	// Turn 1: matches the prefix → served from the recording, NO live dial.
	resp, err := client.Post("http://placeholder.invalid/v1/messages", "application/json", strings.NewReader(`{"model":"m","turn":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "prefix turn 1") {
		t.Fatalf("prefix not replayed: %s", b)
	}
	if hits != 0 {
		t.Fatalf("prefix should not dial live, got %d hits", hits)
	}

	// Turn 2: diverges (new body) → goes LIVE, records, appends.
	resp2, err := client.Post("http://placeholder.invalid/v1/messages", "application/json", strings.NewReader(`{"model":"m","turn":2,"diverged":true}`))
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(b2), "live suffix") {
		t.Fatalf("divergent turn should be served live: %s", b2)
	}
	if hits != 1 {
		t.Fatalf("expected 1 live dial, got %d", hits)
	}

	// Verify saves the grown branch; branch mode tolerates dials + unconsumed.
	if err := c.VerifyError(); err != nil {
		t.Fatalf("branch VerifyError should not fail: %v", err)
	}

	// The saved branch = prefix + live suffix; reopen in replay and confirm both
	// turns now replay with ZERO dials (it became an ordinary cassette).
	rf, err := wirefmt.Load(seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.Interactions) != 2 {
		t.Fatalf("branch should have 2 interactions, got %d", len(rf.Interactions))
	}
	if rf.Interactions[1].Request.MatchKey == "" {
		t.Fatal("appended live interaction missing a persisted match key")
	}

	rc, err := cassette.Open(seed, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	rcl := rc.HTTPClient()
	for _, body := range []string{`{"model":"m","turn":1}`, `{"model":"m","turn":2,"diverged":true}`} {
		resp, err := rcl.Post("http://x.invalid/v1/messages", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("replay of branch failed: %v", err)
		}
		resp.Body.Close()
	}
	if err := rc.VerifyError(); err != nil {
		t.Fatalf("re-replay of branch should be clean: %v", err)
	}
	if rc.Dials() != 0 {
		t.Fatalf("re-replay should make zero dials, got %d", rc.Dials())
	}
}

func TestBranch_AuthScrubbedInSavedBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	seed := branchSeed(t)
	c, _ := cassette.Open(seed, cassette.Options{
		Mode: cassette.ModeBranch,
		Live: cassette.LiveTransport(srv.URL, func(r *http.Request) { r.Header.Set("Authorization", "Bearer sk-supersecret-0123456789") }),
	})
	resp, err := c.HTTPClient().Post("http://x.invalid/v1/messages", "application/json", strings.NewReader(`{"diverge":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := c.VerifyError(); err != nil {
		t.Fatal(err)
	}
	raw, _ := wirefmt.Load(seed)
	out, _ := wirefmt.Marshal(raw)
	if strings.Contains(string(out), "sk-supersecret") {
		t.Fatal("live auth leaked into the saved branch cassette")
	}
}
