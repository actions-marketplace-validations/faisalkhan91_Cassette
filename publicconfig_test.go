package cassette_test

import (
	"path/filepath"
	"regexp"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
)

// TestPublicConfigTypes_Constructible is the regression guard for the API-leak fix:
// an EXTERNAL package (note _test package) must be able to name and construct the
// match/scrub config types and pass them through Options. If these types ever
// revert to internal/* aliases, this file stops compiling.
func TestPublicConfigTypes_Constructible(t *testing.T) {
	opts := cassette.Options{
		Mode: cassette.ModeReplay,
		Match: cassette.MatchConfig{
			HeaderAllowlist:   []string{"X-Tenant"},
			VolatileJSONPaths: []string{"metadata.ts"},
			PathTransform:     func(method, path string) string { return path },
			BodyTransform:     func(path string, body []byte, ct string) []byte { return body },
			KeyFunc: func(in cassette.MatchInput) (string, error) {
				return in.Method + " " + in.Path, nil
			},
		},
		Scrub: cassette.ScrubConfig{
			RedactHeaders:  []string{"Authorization"},
			BodyPatterns:   []*regexp.Regexp{regexp.MustCompile(`sk-\w+`)},
			ResponseStamps: map[string]any{"id": "[X]"},
		},
	}
	_ = opts
	// Ready-made bases are public too.
	_ = cassette.AzureMatchConfig()
	_ = cassette.DefaultScrubConfig()
	// RekeyPath is the public, externally-callable rekey entry point — all-public
	// types, no internal/* leak. Calling a missing file returns an error (not a
	// panic), which proves the surface is actually reachable by an external caller.
	if err := cassette.RekeyPath(filepath.Join(t.TempDir(), "nope.yaml"), cassette.MatchConfig{}); err == nil {
		t.Error("RekeyPath on a missing file should return an error")
	}
}
