package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdShrink computes the minimal subset of a corpus that preserves every distinct
// behavioral signal (per-turn semantic digests + tool/finish/provider/refusal
// coverage) and, with -o, copies the kept cassettes into that directory. Turns
// "we have 4000 cassettes" into "these N cover every behavior".
func cmdShrink(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, shrinkUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" {
		src = "."
	}
	if in.nargs() > 1 {
		return shrinkUsage(stderr)
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
	kept := analysis.Shrink(paths, files)
	st := newStyle(stdout)

	out := in.str("out")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		for _, p := range kept {
			data, err := os.ReadFile(p)
			if err != nil {
				fmt.Fprintf(stderr, "cassette: %v\n", err)
				return exitFail
			}
			if err := os.WriteFile(filepath.Join(out, filepath.Base(p)), data, 0o644); err != nil {
				fmt.Fprintf(stderr, "cassette: %v\n", err)
				return exitFail
			}
		}
	} else {
		for _, p := range kept {
			fmt.Fprintln(stdout, p)
		}
	}
	dest := out
	if dest == "" {
		dest = "(listed above)"
	}
	fmt.Fprintf(stdout, "%s kept %d of %d cassette(s) — same behavioral coverage → %s\n",
		st.check(), len(kept), len(paths), dest)
	return exitOK
}

const shrinkUsageText = "usage: cassette shrink [dir] [-o kept/]"

func shrinkUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, shrinkUsageText)
	return exitUsage
}
