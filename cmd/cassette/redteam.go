package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"gopkg.in/yaml.v3"
)

// cmdRefusal classifies each renderable turn of a recording as refused/complied/
// unknown — a pure, deterministic content classifier over the transcript.
func cmdRefusal(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags(), args, refusalUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() != 1 {
		fmt.Fprintln(stderr, refusalUsageText)
		return exitUsage
	}
	f, ok := loadOrErr(in.arg(0), stderr)
	if !ok {
		return exitFail
	}
	turns := analysis.CollectTurns(f)
	st := newStyle(stdout)
	for i, tr := range turns {
		v := analysis.ClassifyRefusal(tr)
		fmt.Fprintf(stdout, "  turn %d: %s\n", i, colorVerdict(st, v))
	}
	fmt.Fprintf(stdout, "%s final: %s\n", st.bold("="), colorVerdict(st, analysis.FinalVerdict(turns)))
	return exitOK
}

// redteamExpect is the manifest mapping cassette file → expected final verdict.
type redteamExpect struct {
	Expect map[string]string `yaml:"expect"`
}

// cmdRedteam classifies the final verdict of every cassette in a directory and,
// with --expect, diffs against expected verdicts — catching when a model upgrade
// silently flips refuse↔comply. HONEST: frozen replay re-classifies identically,
// so this is a re-record-then-diff gate, not a live probe.
func cmdRedteam(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("expect"), args, redteamUsageText, stderr)
	if !ok {
		return exitUsage
	}
	dir := "."
	if in.nargs() > 0 {
		dir = in.arg(in.nargs() - 1) // last positional wins, mirroring the prior loop
	}
	expectPath := in.str("expect")

	var expect redteamExpect
	if expectPath != "" {
		raw, err := os.ReadFile(expectPath)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if err := yaml.Unmarshal(raw, &expect); err != nil {
			fmt.Fprintf(stderr, "cassette: parse expect: %v\n", err)
			return exitFail
		}
	}

	files := walkYAML(dir)

	st := newStyle(stdout)
	failed := false
	count := 0
	for _, p := range files {
		f, err := wirefmt.Load(p)
		if err != nil {
			continue
		}
		count++
		v := string(analysis.FinalVerdict(analysis.CollectTurns(f)))
		base := filepath.Base(p)
		if expectPath != "" {
			want, ok := expect.Expect[base]
			if ok && want != v {
				fmt.Fprintf(stdout, "%s %s: expected %s, got %s\n", st.cross(), base, want, v)
				failed = true
				continue
			}
		}
		fmt.Fprintf(stdout, "  %s: %s\n", base, v)
	}
	if count == 0 {
		fmt.Fprintf(stdout, "%s no cassettes found under %s\n", st.yellow("note:"), dir)
	}
	if failed {
		return exitFail
	}
	return exitOK
}

func colorVerdict(st style, v analysis.RefusalVerdict) string {
	switch v {
	case analysis.Refused:
		return st.yellow(string(v))
	case analysis.Complied:
		return st.green(string(v))
	default:
		return st.dim(string(v))
	}
}

const refusalUsageText = "usage: cassette refusal <cassette.yaml>"

const redteamUsageText = "usage: cassette redteam [dir] [--expect manifest.yaml]"
