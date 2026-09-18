package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdExplain is a per-turn microscope: it shows everything the engine knows
// about one recorded interaction — the request→match-key derivation, the
// canonicalized request body, the decoded semantic transcript, and the response
// wire-grammar (and optionally the raw SSE frames). Pure read.
func cmdExplain(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("turn").boolFlag("raw"), args, explainUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path != "" && in.nargs() > 1 {
		return exitUsage
	}
	turn := 0
	if in.has("turn") {
		n, err := strconv.Atoi(in.str("turn"))
		if err != nil {
			return explainUsage(stderr)
		}
		turn = n
	}
	raw := in.boolv("raw")
	if path == "" {
		return explainUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	if turn < 0 || turn >= len(f.Interactions) {
		fmt.Fprintf(stderr, "cassette: turn %d out of range (0..%d)\n", turn, len(f.Interactions)-1)
		return exitFail
	}

	it := f.Interactions[turn]
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s turn %d of %d  ·  kind=%s\n", st.bold("explain"), turn, len(f.Interactions), it.Kind)

	if it.Kind == "mcp" {
		fmt.Fprintf(stdout, "  method : %s\n", it.Request.MCPMethod)
		if it.Request.MCPTool != "" {
			fmt.Fprintf(stdout, "  tool   : %s\n", it.Request.MCPTool)
		}
		if body := it.Response.Body.Bytes(); len(body) > 0 {
			fmt.Fprintf(stdout, "  result : %s\n", truncate(string(body), 400))
		}
		return exitOK
	}

	fmt.Fprintf(stdout, "  request: %s %s\n", it.Request.Method, it.Request.URL)
	fmt.Fprintf(stdout, "  status : %s   streaming=%v\n", st.statusColor(it.Response.Status), it.Response.Streaming)
	if it.Request.MatchKey != "" {
		fmt.Fprintf(stdout, "\n%s\n", st.bold("match key"))
		for _, line := range strings.Split(strings.TrimRight(it.Request.MatchKey, "\n"), "\n") {
			fmt.Fprintf(stdout, "  %s\n", st.dim(line))
		}
	}
	if rb := it.Request.Body.Bytes(); len(rb) > 0 {
		fmt.Fprintf(stdout, "\n%s (the bytes that derive the key)\n", st.bold("canonicalized request body"))
		if c, ok := canon.Canonicalize(rb); ok {
			fmt.Fprintf(stdout, "  %s\n", truncate(string(c), 600))
		} else {
			fmt.Fprintf(stdout, "  %s\n", truncate(string(rb), 600))
		}
	}

	tr, unary, renderable := analysis.DecodeInteraction(it)
	fmt.Fprintf(stdout, "\n%s\n", st.bold("decoded transcript"))
	switch {
	case renderable:
		if tr.Text != "" {
			fmt.Fprintf(stdout, "  text   : %s\n", truncate(tr.Text, 400))
		}
		for _, tc := range tr.ToolCalls {
			fmt.Fprintf(stdout, "  tool   : %s(%s)\n", st.cyan(tc.Name), truncate(tc.Args, 200))
		}
		if tr.FinishReason != "" {
			fmt.Fprintf(stdout, "  finish : %s\n", tr.FinishReason)
		}
	case unary:
		fmt.Fprintf(stdout, "  %s (non-streaming JSON; compared by body digest)\n", st.dim(tr.Text))
	default:
		fmt.Fprintf(stdout, "  %s\n", st.dim("(no decodable transcript)"))
	}

	if it.Response.Streaming {
		if rb := it.Response.Body.Bytes(); len(rb) > 0 {
			fmt.Fprintf(stdout, "\n%s\n  %s\n", st.bold("wire shape"), strings.Join(semequal.WireEvents(rb), " · "))
			if raw {
				fmt.Fprintf(stdout, "\n%s\n%s\n", st.bold("raw frames"), string(rb))
			}
		}
	}
	return exitOK
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

const explainUsageText = "usage: cassette explain <cassette.yaml> [--turn N] [--raw]"

func explainUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, explainUsageText)
	return exitUsage
}
