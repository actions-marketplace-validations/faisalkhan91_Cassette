package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdLint is the one-pass corpus health gate: it runs every per-cassette check
// (secret scan, stale match keys, oversized bodies, undecodable/finish-less
// streams, empty files) across a file or directory and emits findings, exiting
// nonzero on any error (or, with --strict, any warning). The CI front door for a
// fixture library.
func cmdLint(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json", "strict").valFlag("max-bytes"), args, lintUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" || in.nargs() > 1 {
		return lintUsage(stderr)
	}
	maxBody := analysis.DefaultMaxBodyBytes
	if in.has("max-bytes") {
		n, err := strconv.Atoi(in.str("max-bytes"))
		if err != nil || n <= 0 {
			fmt.Fprintf(stderr, "cassette: invalid --max-bytes %q\n", in.str("max-bytes"))
			return exitUsage
		}
		maxBody = n
	}

	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	var findings []analysis.Finding
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		f, err := wirefmt.Unmarshal(raw)
		if err != nil {
			findings = append(findings, analysis.Finding{File: p, Index: -1, Severity: "error", Code: "malformed", Message: err.Error()})
			continue
		}
		findings = append(findings, analysis.LintFile(p, raw, f, maxBody)...)
	}

	errs, warns := analysis.LintSummary(findings)
	if in.boolv("json") {
		b, _ := json.Marshal(findings)
		fmt.Fprintln(stdout, string(b))
	} else {
		st := newStyle(stdout)
		for _, fi := range findings {
			loc := fi.File
			if fi.Index >= 0 {
				loc += " [" + strconv.Itoa(fi.Index) + "]"
			}
			tag := st.yellow("warn")
			if fi.Severity == "error" {
				tag = st.red("error")
			}
			fmt.Fprintf(stdout, "%s %s: %s — %s\n", tag, loc, fi.Code, fi.Message)
		}
		mark := st.check()
		if errs > 0 {
			mark = st.cross()
		}
		fmt.Fprintf(stdout, "%s %d cassette(s) · %d error(s), %d warning(s)\n", mark, len(paths), errs, warns)
	}

	if errs > 0 || (in.boolv("strict") && warns > 0) {
		return exitFail
	}
	return exitOK
}

// lintTargets resolves a file or directory to a sorted list of cassette paths
// (recursing into subdirectories).
func lintTargets(src string) ([]string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{src}, nil
	}
	var paths []string
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml":
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .yaml cassettes under %s", src)
	}
	return paths, nil
}

const lintUsageText = "usage: cassette lint <cassette.yaml|dir> [--json] [--strict] [--max-bytes N]"

func lintUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, lintUsageText)
	return exitUsage
}
