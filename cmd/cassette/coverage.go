package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdCoverage reports the behavioral coverage matrix of a corpus (tools called,
// finish reasons, providers, refusal verdicts) and, with --require, gates CI on
// the presence of named cells (e.g. --require tool=delete_account,finish=end_turn).
func cmdCoverage(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("require", "format"), args, coverageUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" {
		src = "."
	}
	if in.nargs() > 1 {
		return coverageUsage(stderr)
	}
	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	files, err := loadAll(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	rep := analysis.Coverage(files)

	if in.str("format") == "json" {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
	} else {
		st := newStyle(stdout)
		fmt.Fprintf(stdout, "%s %d cassette(s) · %d turn(s) · %d streaming, %d unary\n",
			st.bold("coverage"), rep.Cassettes, rep.Turns, rep.Streaming, rep.Unary)
		printAxis(stdout, st, "providers", rep.Providers)
		printAxis(stdout, st, "tools", rep.Tools)
		printAxis(stdout, st, "finish", rep.Finish)
		printAxis(stdout, st, "refusal", rep.Refusal)
	}

	// --require gate.
	var missing []string
	for _, req := range strings.Split(in.str("require"), ",") {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		axis, val, ok := strings.Cut(req, "=")
		if !ok || !rep.Has(axis, val) {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		st := newStyle(stdout)
		fmt.Fprintf(stdout, "%s missing required coverage: %s\n", st.cross(), strings.Join(missing, ", "))
		return exitFail
	}
	return exitOK
}

func printAxis(w io.Writer, st style, label string, m map[string]int) {
	if len(m) == 0 {
		fmt.Fprintf(w, "  %-10s %s\n", label, st.dim("(none)"))
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, m[k]))
	}
	fmt.Fprintf(w, "  %-10s %s\n", label, strings.Join(parts, "  "))
}

// loadAll parses every path into a *wirefmt.File.
func loadAll(paths []string) ([]*wirefmt.File, error) {
	out := make([]*wirefmt.File, 0, len(paths))
	for _, p := range paths {
		f, err := wirefmt.Load(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, f)
	}
	return out, nil
}

const coverageUsageText = "usage: cassette coverage [dir] [--require tool=NAME,finish=X,...] [--format table|json]"

func coverageUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, coverageUsageText)
	return exitUsage
}
