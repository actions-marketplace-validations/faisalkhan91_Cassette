package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdMCPDiff compares two MCP recordings' tool contracts and reports the changes,
// flagging breaking ones (removed tool, stricter input schema, success→error). With
// --fail-on-breaking it is a CI gate: record a golden MCP session, re-record against
// the updated server, and fail the build when the server breaks an advertised tool.
func cmdMCPDiff(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("fail-on-breaking", "json"), args, mcpDiffUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() != 2 {
		return mcpDiffUsage(stderr)
	}
	fa, ok := loadOrErr(in.arg(0), stderr)
	if !ok {
		return exitFail
	}
	fb, ok := loadOrErr(in.arg(1), stderr)
	if !ok {
		return exitFail
	}
	rep := analysis.MCPDiff(fa, fb)

	if in.boolv("json") {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
	} else {
		st := newStyle(stdout)
		if len(rep.Changes) == 0 {
			fmt.Fprintf(stdout, "%s no MCP tool-contract changes\n", st.check())
		}
		for _, c := range rep.Changes {
			mark := st.green("+ additive")
			if c.Breaking {
				mark = st.red("✗ breaking")
			}
			fmt.Fprintf(stdout, "  %s  %s %s — %s\n", mark, st.bold(c.Tool), c.Kind, c.Detail)
		}
		if rep.Breaking > 0 {
			fmt.Fprintf(stdout, "\n%s %d breaking change(s)\n", st.cross(), rep.Breaking)
		}
	}

	// The gate only fires under --fail-on-breaking; otherwise mcp-diff is informational.
	if in.boolv("fail-on-breaking") && rep.Breaking > 0 {
		return exitFail
	}
	return exitOK
}

const mcpDiffUsageText = "usage: cassette mcp-diff <old.yaml> <new.yaml> [--fail-on-breaking] [--json]"

func mcpDiffUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mcpDiffUsageText)
	return exitUsage
}
