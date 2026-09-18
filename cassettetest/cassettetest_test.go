package cassettetest

import (
	"testing"

	"github.com/faisalkhan91/cassette"
)

func TestResolveMode_Precedence(t *testing.T) {
	t.Setenv("CASSETTE_MODE", "record")
	if got := resolveMode(); got != cassette.ModeRecord {
		t.Fatalf("env=record -> %v, want record", got)
	}
	t.Setenv("CASSETTE_MODE", "replay")
	if got := resolveMode(); got != cassette.ModeReplay {
		t.Fatalf("env=replay -> %v, want replay", got)
	}
	t.Setenv("CASSETTE_MODE", "auto")
	if got := resolveMode(); got != cassette.ModeAuto {
		t.Fatalf("env=auto -> %v, want auto", got)
	}
	// No env: defaults to replay (the -update flag defaults false).
	t.Setenv("CASSETTE_MODE", "")
	if got := resolveMode(); got != cassette.ModeReplay {
		t.Fatalf("no env, no -update -> %v, want replay", got)
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("Test/Foo Bar"); got != "Test_Foo_Bar" {
		t.Fatalf("sanitize = %q", got)
	}
}
