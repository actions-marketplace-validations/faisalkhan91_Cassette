// Package cassettetest is the test-only ergonomic surface for cassette: the
// `New(t, name)` helper, mode resolution from CASSETTE_MODE/-update, and the
// t-taking assertions (Verify, AssertNoCanary).
//
// It lives in its own package so the core library (package cassette) never
// imports "testing" or registers a global "flag" — importing the library into a
// production binary must not pull in the test framework or pollute the process
// flag set. Consumers import this package only from their _test.go files:
//
//	func TestAgent(t *testing.T) {
//	    c := cassettetest.New(t, "")        // testdata/cassettes/TestAgent.yaml
//	    client := myClient(c.HTTPClient())  // wrap, then run the agent
//	}                                       // t.Cleanup verifies on exit
package cassettetest

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette"
)

// update records or refreshes cassettes when set: `go test -run X -update`.
// Plain `go test` replays. The flag is registered here (a test-support package),
// never in the core library, so it cannot leak onto an importing binary's flag set.
var update = flag.Bool("update", false, "record/refresh cassettes instead of replaying")

// DefaultDir is where New stores cassettes (the Go testdata convention).
const DefaultDir = "testdata/cassettes"

// New loads (or, in record mode, prepares) the cassette named name under
// testdata/cassettes/. If name is empty it is derived from t.Name(). The mode is
// resolved by precedence: CASSETTE_MODE env > -update flag > replay (default).
// A t.Cleanup is registered to Verify the cassette at test end.
func New(t testing.TB, name string) *cassette.Cassette {
	return NewWithOptions(t, name, cassette.Options{})
}

// NewWithOptions is New with caller-supplied Options. A left-unset Mode
// (ModeUnset, the zero value) is resolved from the env/flag; an explicitly set
// Mode wins (explicit API > env > flag).
func NewWithOptions(t testing.TB, name string, opts cassette.Options) *cassette.Cassette {
	t.Helper()
	if name == "" {
		name = sanitize(t.Name())
	}
	path := filepath.Join(DefaultDir, name+".yaml")

	if opts.Mode == cassette.ModeUnset { // caller did not choose: resolve from env/flag
		opts.Mode = resolveMode()
	}
	// Scrubbing defaults are installed by Open (secrets-by-default).

	if opts.Mode == cassette.ModeRecord {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("cassette: mkdir: %v", err)
		}
	}

	c, err := cassette.Open(path, opts)
	if err != nil {
		t.Fatalf("cassette: open %s: %v", path, err)
	}
	t.Cleanup(func() { Verify(t, c) })
	return c
}

// resolveMode applies env > flag > default(replay).
func resolveMode() cassette.Mode {
	switch strings.ToLower(os.Getenv("CASSETTE_MODE")) {
	case "record":
		return cassette.ModeRecord
	case "replay":
		return cassette.ModeReplay
	case "auto":
		return cassette.ModeAuto
	}
	if update != nil && *update {
		return cassette.ModeRecord
	}
	return cassette.ModeReplay
}

// Verify asserts (in replay) that every recorded interaction was consumed and
// that zero network dials occurred; (in record) it saves the cassette. It fails
// the test on error. Registered automatically by New via t.Cleanup.
func Verify(t testing.TB, c *cassette.Cassette) {
	t.Helper()
	if err := c.VerifyError(); err != nil {
		t.Fatalf("cassette verify (mode=%s): %v", c.Mode(), err)
	}
}

// AssertNoCanary fails the test if any canary token appears in a recorded
// outbound request.
func AssertNoCanary(t testing.TB, c *cassette.Cassette, canaries ...string) {
	t.Helper()
	for can, idxs := range c.CanaryHits(canaries...) {
		t.Errorf("canary %q leaked into outbound request(s) %v", can, idxs)
	}
}

var nonNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func sanitize(name string) string {
	return nonNameChars.ReplaceAllString(name, "_")
}
