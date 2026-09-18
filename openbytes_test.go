package cassette_test

import (
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestOpenBytes_ReplaysFromMemory(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Body: wirefmt.NewBody([]byte(`{"q":1}`))},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"ok":true}`))},
	}}}
	raw, err := wirefmt.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}

	c, err := cassette.OpenBytes("packed", raw, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode() != cassette.ModeReplay {
		t.Fatalf("mode = %v", c.Mode())
	}
	if c.Path() != "packed" {
		t.Fatalf("path = %q", c.Path())
	}
	// Replays the recorded interaction with zero network.
	resp, err := c.HTTPClient().Post("http://x.invalid/v1/messages", "application/json", strings.NewReader(`{"q":1}`))
	if err == nil {
		resp.Body.Close()
	} else {
		t.Fatalf("replay: %v", err)
	}
	if err := c.VerifyError(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Dials() != 0 {
		t.Fatalf("dials = %d", c.Dials())
	}

	// Garbage bytes are rejected, not panicked on.
	if _, err := cassette.OpenBytes("bad", []byte("schema_version: 2\n}{"), cassette.Options{}); err == nil {
		t.Fatal("expected parse error on garbage")
	}
}
