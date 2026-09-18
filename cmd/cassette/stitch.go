package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdStitch concatenates several cassettes into one ordered "society" recording.
func cmdStitch(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, stitchUsageText, stderr)
	if !ok {
		return exitUsage
	}
	out := in.str("out")
	if out == "" || in.nargs() == 0 {
		return stitchUsage(stderr)
	}
	var files []*wirefmt.File
	for _, p := range in.args() {
		f, ok := loadOrErr(p, stderr)
		if !ok {
			return exitFail
		}
		files = append(files, f)
	}
	st := analysis.Stitch(files)
	if code := saveScrubbed(out, st, stderr); code != exitOK {
		return code
	}
	sty := newStyle(stdout)
	fmt.Fprintf(stdout, "%s stitched %d interaction(s) from %d cassette(s) → %s\n",
		sty.check(), len(st.Interactions), len(files), out)
	return exitOK
}

const stitchUsageText = "usage: cassette stitch <a.yaml> <b.yaml> [more...] -o <society.yaml>"

func stitchUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, stitchUsageText)
	return exitUsage
}

// cmdSplit decomposes a recording into per-provider or per-tool cassettes.
func cmdSplit(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("by", "out").alias("o", "out"), args, splitUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	out := in.str("out")
	if path == "" || out == "" || in.nargs() > 1 {
		return splitUsage(stderr)
	}
	by := in.str("by")
	if by == "" {
		by = "provider"
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	groups, err := analysis.Split(f, by)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitUsage
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dst := filepath.Join(out, safeFileName(k)+".yaml")
		if code := saveScrubbed(dst, groups[k], stderr); code != exitOK {
			return code
		}
	}
	sty := newStyle(stdout)
	fmt.Fprintf(stdout, "%s split into %d file(s) by %s → %s\n", sty.check(), len(groups), by, out)
	return exitOK
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFileName(s string) string {
	out := unsafeFileChars.ReplaceAllString(s, "_")
	if out == "" {
		return "group"
	}
	return out
}

const splitUsageText = "usage: cassette split <cassette.yaml> --by provider|tool -o <dir>"

func splitUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, splitUsageText)
	return exitUsage
}
