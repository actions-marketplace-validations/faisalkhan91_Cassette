package cassette

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/faisalkhan91/cassette/semequal"
)

// TimelineEntry is one recorded run's behavioral fingerprint in the ledger.
type TimelineEntry struct {
	Scenario string   `yaml:"scenario"`
	Combined string   `yaml:"combined_digest"`
	Turns    []string `yaml:"turns,omitempty"`
}

// Timeline is an append-only, git-diffable ledger of how a scenario's agent
// behavior evolved over time (per-turn semantic digests). `git log` over the
// ledger file shows the behavioral history across commits, model bumps, and
// prompt edits.
type Timeline struct {
	Entries []TimelineEntry `yaml:"entries"`
}

// LoadTimeline reads a ledger file (empty Timeline if it does not exist).
func LoadTimeline(path string) (*Timeline, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Timeline{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tl Timeline
	if err := yaml.Unmarshal(data, &tl); err != nil {
		return nil, err
	}
	return &tl, nil
}

// AppendTimeline records the run's transcript digests under scenario, content-
// addressably: if the most recent entry for that scenario already has the same
// combined digest, it is a no-op (returns added=false) so an unchanged re-record
// never churns the ledger. Otherwise it appends one entry and rewrites the file
// deterministically.
func AppendTimeline(path, scenario string, turns []semequal.Transcript) (bool, error) {
	tl, err := LoadTimeline(path)
	if err != nil {
		return false, err
	}
	combined := semequal.CombinedDigest(turns)
	for i := len(tl.Entries) - 1; i >= 0; i-- {
		if tl.Entries[i].Scenario == scenario {
			if tl.Entries[i].Combined == combined {
				return false, nil // unchanged behavior — no churn
			}
			break
		}
	}
	per := make([]string, len(turns))
	for i, tr := range turns {
		per[i] = tr.Digest()
	}
	tl.Entries = append(tl.Entries, TimelineEntry{Scenario: scenario, Combined: combined, Turns: per})

	out, err := yaml.Marshal(tl)
	if err != nil {
		return false, err
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
