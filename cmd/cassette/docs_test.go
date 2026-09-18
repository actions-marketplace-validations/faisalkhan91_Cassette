package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// docs/cli.md must document every registered command, so the reference can never
// silently drift from the binary (the "33 vs 50 commands" bug, prevented).
func TestDocsCLI_CoversEveryCommand(t *testing.T) {
	data, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, c := range commands {
		if !strings.Contains(doc, "cassette "+c.Name) {
			t.Errorf("docs/cli.md does not document command %q (add a `cassette %s …` line)", c.Name, c.Name)
		}
	}
}

var docFlagRe = regexp.MustCompile(`--[a-z][a-z0-9-]+`)

// TestDocsCLI_FlagsMatchRegistry guards against docs/cli.md documenting flags a
// command does not actually accept (the verify-attest `--att`/`--pub` fabrication).
// Every long flag shown on a command's cli.md reference line must also appear in
// that command's registry Help synopsis (the source of truth). Names-only coverage
// (above) let the fabrication through; this checks the flag tokens too.
func TestDocsCLI_FlagsMatchRegistry(t *testing.T) {
	data, err := os.ReadFile("../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for _, c := range commands {
		prefix := "cassette " + c.Name + " "
		for _, raw := range lines {
			s := strings.TrimSpace(raw)
			if !strings.HasPrefix(s, prefix) {
				continue
			}
			code := s // drop the trailing "# comment" so flags in prose don't count
			if i := strings.Index(code, "#"); i >= 0 {
				code = code[:i]
			}
			for _, fl := range docFlagRe.FindAllString(code, -1) {
				if !strings.Contains(c.Help, fl) {
					t.Errorf("docs/cli.md documents flag %s for %q, but it is absent from the command's registry Help — fabricated or renamed flag?", fl, c.Name)
				}
			}
		}
	}
}
