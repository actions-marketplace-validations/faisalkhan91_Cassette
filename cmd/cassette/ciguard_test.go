package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
)

func TestMissRecorder(t *testing.T) {
	r := newMissRecorder()
	if !r.empty() {
		t.Fatal("fresh recorder should be empty")
	}
	r.record("POST", "/v1/messages")
	r.record("POST", "/v1/messages") // dedup, count++
	r.record("GET", "/v1/models")
	m := r.manifest()
	if len(m) != 2 {
		t.Fatalf("manifest has %d entries, want 2", len(m))
	}
	// Deterministic order: GET before POST.
	if m[0].Method != "GET" || m[1].Method != "POST" || m[1].Count != 2 {
		t.Fatalf("manifest order/count wrong: %+v", m)
	}
}

func TestMissRecorder_WriteManifest(t *testing.T) {
	r := newMissRecorder()
	r.record("POST", "/v1/messages")
	p := filepath.Join(t.TempDir(), "misses.json")
	if err := r.writeManifest(p); err != nil {
		t.Fatal(err)
	}
	var mm missManifest
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &mm); err != nil {
		t.Fatal(err)
	}
	if mm.Version != missManifestVersion || len(mm.Misses) != 1 || mm.Misses[0].Path != "/v1/messages" {
		t.Fatalf("manifest content wrong: %+v", mm)
	}
}

func TestFinalizeMisses(t *testing.T) {
	// nil/empty recorder → OK, no file.
	if code := finalizeMisses(nil, "/tmp/none.json", io.Discard, io.Discard); code != exitOK {
		t.Fatalf("nil recorder code=%d", code)
	}
	// With misses → fail + manifest written.
	r := newMissRecorder()
	r.record("POST", "/v1/messages")
	p := filepath.Join(t.TempDir(), "m.json")
	if code := finalizeMisses(r, p, io.Discard, io.Discard); code != exitFail {
		t.Fatalf("with-miss code=%d, want fail", code)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
}

// End-to-end: a replay proxy with the CI gate records an uncovered request and the
// finalize step fails; a covered request leaves the gate green.
func TestCIGuard_ProxyReplayGate(t *testing.T) {
	// Author a 1-turn cassette so a known request matches.
	cas := seedFile(t, mergeStreamTurn(`{"model":"m","messages":[{"role":"user","content":"hello"}],"stream":true}`, "hi"))

	rec := newMissRecorder()
	h, _, code := buildProxy(cas, "replay", "", cassette.MatchConfig{}, rec, io.Discard, false)
	if code != exitOK {
		t.Fatalf("buildProxy replay code=%d", code)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// A request that does NOT match the recording → recorded as a miss, 404.
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"DIFFERENT"}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("miss status=%d, want 404", resp.StatusCode)
	}
	if rec.empty() {
		t.Fatal("miss was not recorded")
	}
	if code := finalizeMisses(rec, filepath.Join(t.TempDir(), "m.json"), io.Discard, io.Discard); code != exitFail {
		t.Fatalf("gate should fail after a miss, code=%d", code)
	}
}
