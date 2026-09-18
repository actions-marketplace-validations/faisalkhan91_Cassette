package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdFuzz generates a deterministic battery of VALID re-framings of one turn
// (split text deltas, varied envelope, shuffled tool order) that all decode to the
// same transcript, and writes them as replayable cassettes — a property-test
// corpus for the consumer's own SSE/agent parser. The inverse of mutate.
func cmdFuzz(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("turn", "reframings", "seed", "out").alias("o", "out"),
		args, fuzzUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	out := in.str("out")
	if path == "" || out == "" || in.nargs() > 1 {
		return fuzzUsage(stderr)
	}
	turn, n, seed := 0, 20, int64(1)
	for _, fl := range []struct {
		name string
		dst  *int
	}{{"turn", &turn}, {"reframings", &n}} {
		if in.has(fl.name) {
			v, err := strconv.Atoi(in.str(fl.name))
			if err != nil {
				fmt.Fprintf(stderr, "cassette: --%s must be an integer (got %q)\n", fl.name, in.str(fl.name))
				return exitUsage
			}
			*fl.dst = v
		}
	}
	if in.has("seed") {
		v, err := strconv.ParseInt(in.str("seed"), 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: --seed must be an integer (got %q)\n", in.str("seed"))
			return exitUsage
		}
		seed = v
	}
	if turn < 0 || n <= 0 {
		fmt.Fprintln(stderr, "cassette: --turn must be >=0 and --reframings >0")
		return exitUsage
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	variants, err := analysis.Fuzz(f, turn, n, seed)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for i, vf := range variants {
		dst := filepath.Join(out, fmt.Sprintf("%s.reframe-%03d.yaml", stem, i))
		if code := saveScrubbed(dst, vf, stderr); code != exitOK {
			return code
		}
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s wrote %d valid re-framing(s) (seed %d) → %s — all decode to the same transcript\n",
		st.check(), len(variants), seed, out)
	return exitOK
}

const fuzzUsageText = "usage: cassette fuzz <cassette.yaml> [--turn N] [--reframings 20] [--seed 1] -o <dir>"

func fuzzUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, fuzzUsageText)
	return exitUsage
}
