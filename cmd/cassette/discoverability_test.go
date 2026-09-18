package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Every command must be assigned to a help group, and every group used must be in
// groupOrder — so `cassette --help` can never silently drop or misplace a command.
func TestCommandGroups_Complete(t *testing.T) {
	valid := map[string]bool{}
	for _, g := range groupOrder {
		valid[g] = true
	}
	for _, c := range commands {
		g, ok := commandGroup[c.Name]
		if !ok {
			t.Errorf("command %q has no help group (add it to commandGroup)", c.Name)
			continue
		}
		if !valid[g] {
			t.Errorf("command %q is in group %q which is not in groupOrder", c.Name, g)
		}
	}
	// And no group name maps a command that doesn't exist.
	for name := range commandGroup {
		if commandByName[name] == nil {
			t.Errorf("commandGroup has %q but there is no such command", name)
		}
	}
}

func TestUsage_Grouped(t *testing.T) {
	_, out, _ := runArgs("--help")
	for _, g := range []string{"Record & serve", "Author & transform", "Safety & scrubbing"} {
		if !strings.Contains(out, g) {
			t.Errorf("grouped help missing section %q", g)
		}
	}
}

// The curated first-run tier must appear above the full list and point newcomers
// at the entry-point commands.
func TestUsage_StartHere(t *testing.T) {
	_, out, _ := runArgs("--help")
	start := strings.Index(out, "Start here")
	if start < 0 {
		t.Fatal("help is missing the 'Start here' tier")
	}
	groups := strings.Index(out, "Usage:")
	for _, c := range []string{"up", "open", "proxy", "serve", "verify", "conformance"} {
		if !strings.Contains(out[start:groups], c) {
			t.Errorf("'Start here' tier omits %q", c)
		}
	}
}

func TestSuggestCommand(t *testing.T) {
	cases := map[string]string{
		"infsect": "inspect",
		"sereve":  "serve",
		"verfy":   "verify",
	}
	for in, want := range cases {
		if got := suggestCommand(in); got != want {
			t.Errorf("suggestCommand(%q)=%q, want %q", in, got, want)
		}
	}
	// A wildly different token gets no suggestion.
	if got := suggestCommand("zzzzzzzzzz"); got != "" {
		t.Errorf("suggestCommand(garbage)=%q, want empty", got)
	}
	// And the unknown-command path prints the suggestion.
	if _, _, errOut := runArgs("infsect"); !strings.Contains(errOut, `Did you mean "cassette inspect"`) {
		t.Errorf("unknown-command output lacks suggestion: %s", errOut)
	}
}

func TestCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		code, out, _ := runArgs("completion", shell)
		if code != exitOK || !strings.Contains(out, "inspect") || !strings.Contains(out, "cassette") {
			t.Fatalf("completion %s: code=%d out=%s", shell, code, out[:min(120, len(out))])
		}
	}
	if code, _, _ := runArgs("completion", "tcsh"); code != exitUsage {
		t.Fatalf("completion bad-shell exit=%d", code)
	}
	if code, _, _ := runArgs("completion"); code != exitUsage {
		t.Fatalf("completion no-arg exit=%d", code)
	}
}

// All color must flow through ui.go's style; no command file may emit a raw ANSI
// escape. (Guards the single-source-of-color invariant.)
func TestNoRawANSI(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == "ui.go" {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		for _, esc := range []string{`\x1b`, `\033`, `\e[`} {
			if bytes.Contains(b, []byte(esc)) {
				t.Errorf("%s contains a raw ANSI escape (%s); route color through ui.go's style", n, esc)
			}
		}
	}
}

func TestAsciiOnly(t *testing.T) {
	t.Setenv("CASSETTE_ASCII", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	if asciiOnly() {
		t.Fatal("UTF-8 locale should not force ASCII")
	}
	t.Setenv("LANG", "C")
	if !asciiOnly() {
		t.Fatal("non-UTF-8 locale should force ASCII")
	}
	t.Setenv("CASSETTE_ASCII", "1")
	t.Setenv("LANG", "en_US.UTF-8")
	if !asciiOnly() {
		t.Fatal("CASSETTE_ASCII must force ASCII regardless of locale")
	}
}
