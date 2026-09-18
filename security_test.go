package cassette

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// A secret that survives scrubbing (here, in a header the scrubber's allowlist
// doesn't cover) must block the write — the saveLocked backstop.
func TestSave_RefusesSurvivingSecret(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	c, err := Open(p, Options{Mode: ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	c.file.Interactions = append(c.file.Interactions, &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			// Not in the redact allowlist, so scrubbing leaves it — the scan must catch it.
			Headers: wirefmt.Headers{{Name: "X-Custom-Trace", Values: []string{"ghp_0123456789012345678901234567890123456789"}}},
			Body:    wirefmt.NewBody([]byte(`{"model":"m"}`))},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"ok":true}`))},
	})
	if err := c.VerifyError(); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected the save to refuse a surviving secret, got %v", err)
	}
}

// A body-embedded credential is redacted by scrubbing, so the save succeeds.
func TestSave_ScrubbedBodySecretOK(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	c, err := Open(p, Options{Mode: ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	c.file.Interactions = append(c.file.Interactions, &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Body: wirefmt.NewBody([]byte(`{"key":"AKIAIOSFODNN7EXAMPLE","model":"m"}`))},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"ok":true}`))},
	})
	if err := c.VerifyError(); err != nil {
		t.Fatalf("body secret should be scrubbed and the save should succeed, got %v", err)
	}
	f, err := wirefmt.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(f.Interactions[0].Request.Body.Bytes()), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key should have been redacted from the saved body")
	}
}
