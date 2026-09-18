package main

import (
	"encoding/json"
	"fmt"
	"io"

	cassette "github.com/faisalkhan91/cassette"
)

// cmdConformance proves a cassette actually replays: for every interaction it
// checks the persisted match key re-derives from the stored request and that the
// request resolves through the in-process replay matcher to a response decoding to
// the stored semantic transcript — all with zero outbound dials. A proactive
// complement to `doctor` (which diagnoses a miss after the fact).
func cmdConformance(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json"), args, conformanceUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return conformanceUsage(stderr)
	}
	// Load once for a clean error message on a malformed file.
	if _, ok := loadOrErr(path, stderr); !ok {
		return exitFail
	}
	rep, err := cassette.Conformance(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	if in.boolv("json") {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
		if !rep.OK() {
			return exitFail
		}
		return exitOK
	}

	st := newStyle(stdout)
	for _, t := range rep.Turns {
		label := t.Method + " " + t.URL
		if t.Kind == "mcp" {
			label = "mcp " + t.Tool
		}
		mark := st.check()
		if !t.OK() {
			mark = st.cross()
		}
		detail := ""
		if t.Detail != "" {
			detail = " — " + t.Detail
		}
		fmt.Fprintf(stdout, "%s [%d] %s%s\n", mark, t.Index, label, st.dim(detail))
	}
	if rep.OK() {
		fmt.Fprintf(stdout, "\n%s %d interaction(s) replay cleanly; %d dial(s)\n",
			st.check(), len(rep.Turns), rep.Dials)
		return exitOK
	}
	fmt.Fprintf(stdout, "\n%s %d interaction(s) failed conformance; %d dial(s)\n",
		st.cross(), rep.Bad, rep.Dials)
	return exitFail
}

const conformanceUsageText = "usage: cassette conformance <cassette.yaml> [--json]"

func conformanceUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, conformanceUsageText)
	return exitUsage
}
