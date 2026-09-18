package main

import (
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdPort transpiles a recording's wire dialect: it re-encodes each streaming turn
// into the target dialect and re-shapes the request (path + body) so a client built
// for that provider can replay the SAME recorded behavior offline. The per-turn
// semantic digest is preserved. It is NOT a claim of model equivalence across
// providers — only a re-targeting of the wire dialect.
func cmdPort(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("to", "out").alias("o", "out"), args, portUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 || in.str("to") == "" {
		return portUsage(stderr)
	}
	to, _, err := analysis.ProviderRouting(in.str("to"))
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitUsage
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	ported, n, err := analysis.Port(f, to)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	analysis.StampProvenance(ported, f, "port", "→ "+in.str("to"))
	out := in.str("out")
	if out == "" {
		out = path
	}
	if code := saveScrubbed(out, ported, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s ported %d turn(s) to %s → %s\n", st.check(), n, in.str("to"), out)
	if n == 0 {
		fmt.Fprintln(stdout, st.dim("note: nothing transpiled (already the target dialect, or only unary/error turns)"))
	}
	return exitOK
}

const portUsageText = "usage: cassette port <cassette.yaml> --to anthropic|openai-chat|openai-responses|gemini|ollama [-o out.yaml]"

func portUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, portUsageText)
	return exitUsage
}
