package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdTaint traces sensitive values that cross turns — data that first appeared in
// an earlier turn (as user input or as model/tool output) and is carried outbound
// in a later request. It is a report by default; --fail-on <classes> makes it a
// gate. Masked samples only; no raw secrets are printed.
func cmdTaint(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json").valFlag("fail-on"), args, taintUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return taintUsage(stderr)
	}
	failOn := in.flagSet("fail-on")
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	flows := analysis.Taint(f)

	if in.boolv("json") {
		b, _ := json.Marshal(flows)
		fmt.Fprintln(stdout, string(b))
	} else {
		st := newStyle(stdout)
		if len(flows) == 0 {
			fmt.Fprintf(stdout, "%s no sensitive value crosses turns\n", st.check())
			return exitOK
		}
		for _, fl := range flows {
			src := fl.SourceKind
			if fl.Exfil() {
				src = st.yellow(src + " (untrusted)")
			}
			fmt.Fprintf(stdout, "%s %s: turn %d (%s) → turn %d · %d outbound turn(s) · %s\n",
				st.yellow("→"), fl.Kind, fl.SourceTurn, src, fl.SinkTurn, fl.OutboundTurns, st.dim(fl.Redacted))
		}
	}

	for _, fl := range flows {
		if failOn[fl.Kind] {
			return exitFail
		}
	}
	return exitOK
}

const taintUsageText = "usage: cassette taint <cassette.yaml> [--fail-on email,creditcard,...] [--json]"

func taintUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, taintUsageText)
	return exitUsage
}
