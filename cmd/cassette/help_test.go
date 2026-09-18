package main

import (
	"strings"
	"testing"
)

func TestHelp_PerCommand(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"serve", "--help"}, "--pace"},
		{[]string{"serve", "-h"}, "offline LLM endpoint"},
		{[]string{"pack", "--help"}, "self-contained"},
		{[]string{"help", "bisect"}, "first turn"},
		{[]string{"mutate", "--help"}, "drop-terminal"},
		{[]string{"doc", "--help"}, "--check"},
	}
	for _, tc := range cases {
		code, out, _ := runArgs(tc.args...)
		if code != exitOK {
			t.Errorf("%v: exit %d, want 0", tc.args, code)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v: help missing %q\n%s", tc.args, tc.want, out)
		}
		if !strings.Contains(out, "Usage:") {
			t.Errorf("%v: help missing Usage section", tc.args)
		}
	}
}

func TestHelp_TopLevel(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		code, out, _ := runArgs(args...)
		if code != exitOK || !strings.Contains(out, "Usage:") || !strings.Contains(out, "cassette pack") {
			t.Fatalf("%v: top-level help wrong: code=%d\n%s", args, code, out)
		}
	}
	// `help <unknown>` falls back to the top-level usage (still exit 0).
	if code, out, _ := runArgs("help", "frobnicate"); code != exitOK || !strings.Contains(out, "Usage:") {
		t.Fatalf("help unknown: code=%d\n%s", code, out)
	}
}

// The registry is the single source of truth: every command must have a Run, a
// non-empty Help whose title line yields a summary, and a unique name. (Dispatch,
// usage, and help all derive from it, so they can no longer drift apart.)
func TestRegistry_WellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if c.Name == "" || c.Run == nil {
			t.Errorf("command %q: missing Name or Run", c.Name)
		}
		if seen[c.Name] {
			t.Errorf("duplicate command name %q", c.Name)
		}
		seen[c.Name] = true
		if c.summary() == "" {
			t.Errorf("command %q: Help has no '— summary' title line", c.Name)
		}
		if !strings.HasPrefix(c.Help, "cassette "+c.Name+" ") {
			t.Errorf("command %q: Help title should start with 'cassette %s '", c.Name, c.Name)
		}
	}
	// Every command's summary appears in the generated top-level usage.
	_, top, _ := runArgs("--help")
	for _, c := range commands {
		if !strings.Contains(top, "cassette "+c.Name) {
			t.Errorf("top-level usage missing %q", c.Name)
		}
	}
}
