package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdMerge unions several cassettes (or directories of cassettes) into one,
// dropping exact duplicates and reporting conflicts (same request, different
// response). Argument order is preserved, so a shared-prefix cassette first plus
// per-test tails after yields a deterministic layered fixture.
func cmdMerge(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("strict").valFlag("out").alias("o", "out"), args, mergeUsageText, stderr)
	if !ok {
		return exitUsage
	}
	out := in.str("out")
	if out == "" || in.nargs() == 0 {
		return mergeUsage(stderr)
	}
	var files []*wirefmt.File
	for _, src := range in.args() {
		fs, err := loadCassettes(src)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		files = append(files, fs...)
	}
	merged, rep := analysis.Merge(files...)
	if code := saveScrubbed(out, merged, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s merged %d interaction(s) (deduped %d) → %s\n", st.check(), rep.Total, rep.Deduped, out)
	if len(rep.Conflicts) > 0 {
		fmt.Fprintf(stdout, "%s %d conflicting key(s) (same request, different response)\n", st.yellow("!"), len(rep.Conflicts))
		if in.boolv("strict") {
			return exitFail
		}
	}
	return exitOK
}

// loadCassettes loads one cassette file, or every *.yaml/*.yml cassette in a
// directory (sorted, non-recursive), so a directory is one logical fixture.
func loadCassettes(src string) ([]*wirefmt.File, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		f, err := wirefmt.Load(src)
		if err != nil {
			return nil, err
		}
		return []*wirefmt.File{f}, nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".yaml", ".yml":
			paths = append(paths, filepath.Join(src, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .yaml cassettes in directory %s", src)
	}
	var out []*wirefmt.File
	for _, p := range paths {
		f, err := wirefmt.Load(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, f)
	}
	return out, nil
}

const mergeUsageText = "usage: cassette merge <a.yaml|dir> [more...] -o <out.yaml> [--strict]"

func mergeUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mergeUsageText)
	return exitUsage
}
