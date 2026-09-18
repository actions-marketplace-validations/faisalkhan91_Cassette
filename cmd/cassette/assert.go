package main

import (
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func cmdAssert(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("finishes-clean", "no-duplicate-tools").valFlag("no-tool"), args, assertUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return assertUsage(stderr)
	}
	noTools := in.list("no-tool")
	finishesClean := in.boolv("finishes-clean")
	noDup := in.boolv("no-duplicate-tools")
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	// With no invariant flags, fall back to the contract embedded in the cassette
	// (`cassette expect`), so `cassette assert <c>` runs zero-arg in CI.
	if len(noTools) == 0 && !noDup && !finishesClean && f.Expect != nil {
		noTools = f.Expect.NoTools
		noDup = f.Expect.NoDuplicateTools
		finishesClean = f.Expect.FinishesClean
	}
	turns := analysis.CollectTurns(f)
	var invs []semequal.Invariant
	for _, n := range noTools {
		invs = append(invs, semequal.NoToolCalled(n))
	}
	if noDup {
		invs = append(invs, semequal.NoDuplicateToolCall())
	}
	if finishesClean {
		invs = append(invs, semequal.FinishesCleanly())
	}
	st := newStyle(stderr)
	errs := semequal.Check(turns, invs...)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(stderr, "%s %v\n", st.cross(), e)
		}
		return exitFail
	}
	so := newStyle(stdout)
	fmt.Fprintf(stdout, "%s %s — %d turn(s) satisfy %d invariant(s)\n", so.check(), path, len(turns), len(invs))
	return exitOK
}

const assertUsageText = "usage: cassette assert <cassette.yaml> [--no-tool NAME]... [--no-duplicate-tools] [--finishes-clean]"

func assertUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, assertUsageText)
	return exitUsage
}
