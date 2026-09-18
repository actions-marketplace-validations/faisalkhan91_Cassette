package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdCodec is the codec-introspection command. Today it has one subcommand,
// `verify`, which proves the Transcript⇄wire codec round-trips every recorded
// streaming turn (decode→encode→decode is a stable fixpoint and encoding is
// deterministic) — hardening the keystone every authoring feature stands on.
func cmdCodec(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "verify" {
		return codecUsage(stderr)
	}
	in, ok := parseOrUsage(newFlags().boolFlag("json"), args[1:], codecUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return codecUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	rep := analysis.CodecVerify(f)

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
		switch {
		case t.Skipped != "":
			fmt.Fprintf(stdout, "%s turn %d %s — %s\n", st.dim("·"), t.Turn, t.Provider, st.dim(t.Skipped))
		case t.OK():
			fmt.Fprintf(stdout, "%s turn %d %s — stable, deterministic\n", st.check(), t.Turn, t.Provider)
		default:
			fmt.Fprintf(stdout, "%s turn %d %s — %s\n", st.cross(), t.Turn, t.Provider, codecFail(t))
			if t.Diff != "" {
				fmt.Fprint(stdout, indentLines(t.Diff, "    "))
			}
		}
	}
	if rep.OK() {
		fmt.Fprintf(stdout, "\n%s codec round-trips %d streaming turn(s); %d skipped\n",
			st.check(), rep.Checked, rep.Skipped)
		return exitOK
	}
	fmt.Fprintf(stdout, "\n%s %d/%d streaming turn(s) are not codec-preserving\n",
		st.cross(), rep.Bad, rep.Checked)
	return exitFail
}

func codecFail(t analysis.CodecTurn) string {
	switch {
	case !t.Stable && !t.Deterministic:
		return "not stable and not deterministic"
	case !t.Stable:
		return "not a round-trip fixpoint"
	default:
		return "encoding not deterministic"
	}
}

// indentLines prefixes every non-empty line of s with pad.
func indentLines(s, pad string) string {
	var b []byte
	for _, line := range splitLines(s) {
		if line == "" {
			b = append(b, '\n')
			continue
		}
		b = append(b, pad...)
		b = append(b, line...)
		b = append(b, '\n')
	}
	return string(b)
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

const codecUsageText = "usage: cassette codec verify <cassette.yaml> [--json]"

func codecUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, codecUsageText)
	return exitUsage
}
