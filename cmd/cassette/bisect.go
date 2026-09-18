package main

import (
	"fmt"
	"io"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

func cmdBisect(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags(), args, "usage: cassette bisect <a.yaml> <b.yaml>", stderr)
	if !ok || in.nargs() != 2 {
		fmt.Fprintln(stderr, "usage: cassette bisect <a.yaml> <b.yaml>")
		return exitUsage
	}
	a, ok := loadOrErr(in.arg(0), stderr)
	if !ok {
		return exitFail
	}
	b, ok := loadOrErr(in.arg(1), stderr)
	if !ok {
		return exitFail
	}
	d := analysis.Bisect(a, b)
	st := newStyle(stdout)
	if !d.Diverged() {
		fmt.Fprintf(stdout, "%s no behavioral divergence\n", st.check())
		return exitOK
	}
	axes := strings.Join(d.Axes, ", ")
	if axes == "" {
		axes = "(response only; no request-input change)"
	}
	fmt.Fprintf(stdout, "%s diverged at turn %d — changed input axes: %s\n", st.yellow("⚠"), d.Turn, axes)
	return exitOK
}
