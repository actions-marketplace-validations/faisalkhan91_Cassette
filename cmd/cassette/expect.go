package main

import (
	"fmt"
	"io"

	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdExpect manages the behavioral contract embedded in a cassette. With flags it
// writes the contract; with no flags it prints the current one; --clear removes
// it. The contract is preserved across re-record (see saveLocked), and
// `cassette assert <c>` with no invariant flags runs it.
func cmdExpect(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("no-tool").boolFlag("no-duplicate-tools", "finishes-clean", "clear"), args, expectUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path != "" && in.nargs() > 1 {
		return expectUsage(stderr)
	}
	noTools := in.list("no-tool")
	noDup := in.boolv("no-duplicate-tools")
	clean := in.boolv("finishes-clean")
	clear := in.boolv("clear")
	hasFlag := in.has("no-tool") || noDup || clean || clear
	if path == "" {
		return expectUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	st := newStyle(stdout)

	if !hasFlag {
		if f.Expect == nil {
			fmt.Fprintf(stdout, "%s no embedded contract\n", st.dim("expect:"))
			return exitOK
		}
		printExpect(stdout, st, f.Expect)
		return exitOK
	}

	if clear {
		f.Expect = nil
	} else {
		f.Expect = &wirefmt.Expect{NoTools: noTools, NoDuplicateTools: noDup, FinishesClean: clean}
	}
	raw, err := wirefmt.Marshal(f)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "%s cassette: refusing to write — secret pattern(s) present: %v\n", st.cross(), hits)
		return exitFail
	}
	if err := wirefmt.Save(path, f); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if clear {
		fmt.Fprintf(stdout, "%s cleared contract in %s\n", st.check(), path)
	} else {
		fmt.Fprintf(stdout, "%s wrote contract to %s\n", st.check(), path)
		printExpect(stdout, st, f.Expect)
	}
	return exitOK
}

func printExpect(w io.Writer, st style, e *wirefmt.Expect) {
	for _, n := range e.NoTools {
		fmt.Fprintf(w, "  no-tool: %s\n", n)
	}
	if e.NoDuplicateTools {
		fmt.Fprintln(w, "  no-duplicate-tools")
	}
	if e.FinishesClean {
		fmt.Fprintln(w, "  finishes-clean")
	}
}

const expectUsageText = "usage: cassette expect <cassette.yaml> [--no-tool NAME]... [--no-duplicate-tools] [--finishes-clean] [--clear]"

func expectUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, expectUsageText)
	return exitUsage
}
