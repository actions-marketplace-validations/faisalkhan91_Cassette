package cassettetest_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/cassettetest"
)

// Records through New (mode resolved from the env), then replays the same
// fixture offline — exercising New/NewWithOptions/Verify/AssertNoCanary and the
// auto-registered cleanup verify. t.Chdir keeps the testdata/ dir inside a temp
// directory so the run leaves nothing behind.
func TestNew_RecordThenReplay(t *testing.T) {
	t.Chdir(t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	hit := func(c *cassette.Cassette) {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", http.NoBody)
		resp, err := c.HTTPClient().Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// Record.
	t.Setenv("CASSETTE_MODE", "record")
	rec := cassettetest.New(t, "roundtrip")
	hit(rec)
	cassettetest.Verify(t, rec) // saves
	cassettetest.AssertNoCanary(t, rec, "never-present")

	// Replay: zero dials, every interaction consumed (cleanup verify asserts it).
	t.Setenv("CASSETTE_MODE", "replay")
	rp := cassettetest.NewWithOptions(t, "roundtrip", cassette.Options{})
	hit(rp)
	if got := rp.Dials(); got != 0 {
		t.Fatalf("replay made %d dials, want 0", got)
	}
}

// An explicitly set Mode must win over the environment (the ModeUnset distinction:
// a real ModeRecord is no longer mistaken for "unset" and overridden by env).
func TestNewWithOptions_ExplicitModeWinsOverEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CASSETTE_MODE", "replay") // env says replay…
	c := cassettetest.NewWithOptions(t, "explicit", cassette.Options{Mode: cassette.ModeRecord})
	if c.Mode() != cassette.ModeRecord { // …but the explicit ModeRecord wins
		t.Fatalf("explicit ModeRecord overridden by env: got %s", c.Mode())
	}
}
