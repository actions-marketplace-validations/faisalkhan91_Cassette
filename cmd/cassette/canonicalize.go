package main

import (
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdCanonicalize re-emits every codec-preserving streaming response through the
// encoder with a zeroed envelope, so two recordings of the same behavior serialize
// byte-identically — killing re-record diff noise at the source. --check is a
// read-only gate (nonzero exit if the file is not already canonical); -o writes a
// copy instead of editing in place. Secrets are never written.
func cmdCanonicalize(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("check").valFlag("out").alias("o", "out"),
		args, canonicalizeUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return canonicalizeUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	n := analysis.CanonicalizeFile(f)
	st := newStyle(stdout)

	if in.boolv("check") {
		if n > 0 {
			fmt.Fprintf(stderr, "%s %s is not canonical (%d streaming turn(s) would change) — run `cassette canonicalize`\n",
				st.cross(), path, n)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s %s is canonical\n", st.check(), path)
		return exitOK
	}

	out := in.str("out")
	if out == "" {
		out = path
	}
	if code := saveScrubbed(out, f, stderr); code != exitOK {
		return code
	}
	if n == 0 {
		fmt.Fprintf(stdout, "%s %s already canonical\n", st.check(), out)
	} else {
		fmt.Fprintf(stdout, "%s canonicalized %d streaming turn(s) → %s\n", st.check(), n, out)
	}
	return exitOK
}

const canonicalizeUsageText = "usage: cassette canonicalize <cassette.yaml> [--check] [-o out.yaml]"

func canonicalizeUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, canonicalizeUsageText)
	return exitUsage
}
