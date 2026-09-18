package cassette_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// An embedded Expect contract must survive `go test -update` (re-record), so a
// recording keeps its CI guardrail.
func TestExpect_SurvivesReRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	// Seed a cassette that already carries a contract.
	seed := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion,
		Expect: &wirefmt.Expect{NoTools: []string{"delete_account"}, FinishesClean: true}}
	if err := wirefmt.Save(path, seed); err != nil {
		t.Fatal(err)
	}

	// Re-record over it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.HTTPClient().Post(srv.URL+"/v1/x", "application/json", strings.NewReader(`{"q":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := c.VerifyError(); err != nil {
		t.Fatal(err)
	}

	got, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Expect == nil || len(got.Expect.NoTools) != 1 || got.Expect.NoTools[0] != "delete_account" || !got.Expect.FinishesClean {
		t.Fatalf("contract did not survive re-record: %+v", got.Expect)
	}
	if len(got.Interactions) != 1 {
		t.Fatalf("expected the new interaction recorded, got %d", len(got.Interactions))
	}
}
