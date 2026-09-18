package cassette

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func staleFile() *wirefmt.File {
	return &wirefmt.File{
		SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{{
			Kind: "http",
			Request: wirefmt.Request{
				Method:   "POST",
				URL:      "/v1/x",
				Headers:  wirefmt.HeadersFromHTTP(http.Header{"Content-Type": {"application/json"}}),
				Body:     wirefmt.NewBody([]byte(`{"a":1}`)),
				MatchKey: "STALE-WRONG-KEY", // simulates a hand edit that desynced the key
			},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"ok":true}`))},
		}},
	}
}

func TestRekeyFile_FixesStaleKey(t *testing.T) {
	// A stale persisted key makes the real request miss on replay.
	stalePath := filepath.Join(t.TempDir(), "stale.yaml")
	if err := wirefmt.Save(stalePath, staleFile()); err != nil {
		t.Fatal(err)
	}
	rpStale, err := Open(stalePath, Options{Mode: ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpStale.HTTPClient().Post("http://x/v1/x", "application/json", strings.NewReader(`{"a":1}`)); err == nil {
		t.Fatal("expected a miss with a stale MatchKey")
	}

	// RekeyPath (the public API) fixes the persisted key to match the stored request.
	fixedPath := filepath.Join(t.TempDir(), "fixed.yaml")
	if err := wirefmt.Save(fixedPath, staleFile()); err != nil {
		t.Fatal(err)
	}
	if err := RekeyPath(fixedPath, MatchConfig{}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := wirefmt.Load(fixedPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := rekey.HTTPKey(reloaded.Interactions[0].Request, match.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Interactions[0].Request.MatchKey != want {
		t.Fatalf("rekey did not recompute the key:\n got=%q\n want=%q", reloaded.Interactions[0].Request.MatchKey, want)
	}

	// After rekey, the same request replays.
	rp, err := Open(fixedPath, Options{Mode: ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	status, body := doPost(t, rp, "http://x/v1/x", `{"a":1}`)
	if status != 200 || body != `{"ok":true}` {
		t.Fatalf("replay after rekey failed: %d %q", status, body)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
