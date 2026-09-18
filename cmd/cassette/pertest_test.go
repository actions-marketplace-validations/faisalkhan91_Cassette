package main

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

// perTestHandler routes by header to an isolated per-test cassette; an unknown test
// name is a miss (recorded by the CI gate).
func TestPerTestHandler(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"login":    `{"model":"m","messages":[{"role":"user","content":"log in"}],"stream":true}`,
		"checkout": `{"model":"m","messages":[{"role":"user","content":"check out"}],"stream":true}`,
	} {
		f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{mergeStreamTurn(body, name+"-reply")}}
		if err := wirefmt.Save(filepath.Join(dir, name+".yaml"), f); err != nil {
			t.Fatal(err)
		}
	}

	rec := newMissRecorder()
	h, err := perTestHandler(dir, "X-Cassette-Test", cassette.MatchConfig{}, cassette.ServeOptions{OnMiss: recordingOnMiss(rec, io.Discard)}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	post := func(test, body string) (int, string) {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if test != "" {
			req.Header.Set("X-Cassette-Test", test)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// The login request routed to the login cassette resolves; the SAME login
	// request routed to checkout's cassette does NOT (isolation).
	loginBody := `{"model":"m","messages":[{"role":"user","content":"log in"}],"stream":true}`
	if code, _ := post("login", loginBody); code != http.StatusOK {
		t.Fatalf("login→login status=%d, want 200", code)
	}
	if code, _ := post("checkout", loginBody); code != http.StatusNotFound {
		t.Fatalf("login req routed to checkout should miss, got %d", code)
	}
	// An unknown test name is a miss.
	if code, _ := post("nope", loginBody); code != http.StatusNotFound {
		t.Fatalf("unknown test status=%d, want 404", code)
	}
	if rec.empty() {
		t.Fatal("misses should have been recorded for the CI gate")
	}
}

func TestServe_RouteHeaderRequiresDir(t *testing.T) {
	f := writeTemp(t, validCassette)
	if code, _, _ := runArgs("serve", f, "--route-header", "X-Cassette-Test", "--addr", "127.0.0.1:0"); code != exitUsage {
		t.Fatalf("route-header on a file should be a usage error, got %d", code)
	}
}
