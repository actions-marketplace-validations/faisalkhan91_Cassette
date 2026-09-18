package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdMCPSteps walks a recorded MCP session step by step — the JSON-RPC method
// sequence, the tools/list roster, and each tools/call's args → result/error — so you
// can read what an MCP server did at the protocol level. Read-only, offline.
func cmdMCPSteps(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json"), args, mcpStepsUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return mcpStepsUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	steps := analysis.MCPSteps(f)

	if in.boolv("json") {
		b, _ := json.Marshal(steps)
		fmt.Fprintln(stdout, string(b))
		return exitOK
	}

	st := newStyle(stdout)
	if len(steps) == 0 {
		fmt.Fprintf(stdout, "%s no MCP interactions in %s\n", st.dim("•"), path)
		return exitOK
	}
	for _, s := range steps {
		head := s.Method
		if s.Tool != "" {
			head += " " + st.bold(s.Tool)
		}
		marker := st.check()
		if s.IsError {
			marker = st.cross()
		}
		// Label by the turn index (s.Index) — the same value the --json output carries
		// and that `doc`/`conformance` use — so text and JSON agree.
		fmt.Fprintf(stdout, "%s turn %d  %s\n", marker, s.Index, head)
		if s.Params != "" {
			fmt.Fprintf(stdout, "       args:   %s\n", truncate(s.Params, 120))
		}
		if len(s.Tools) > 0 {
			fmt.Fprintf(stdout, "       tools:  %s\n", strings.Join(s.Tools, ", "))
		}
		if s.Result != "" {
			label := "result"
			if s.IsError {
				label = "error "
			}
			fmt.Fprintf(stdout, "       %s: %s\n", label, truncate(s.Result, 200))
		}
	}
	fmt.Fprintf(stdout, "\n%s %d MCP step(s)\n", st.check(), len(steps))
	return exitOK
}

const mcpStepsUsageText = "usage: cassette mcp-steps <session.yaml> [--json]"

func mcpStepsUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mcpStepsUsageText)
	return exitUsage
}
