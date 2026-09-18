package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdSchemaCheck validates that every emitted tool call's arguments conform to the
// JSON Schema the request advertised for that tool (HTTP tools[] and MCP
// tools/list) — proving the model honored its own tool contract, fully offline.
// Nonzero exit on any violation.
func cmdSchemaCheck(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json"), args, schemaCheckUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return schemaCheckUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	rep := analysis.SchemaCheck(f)

	if in.boolv("json") {
		b, _ := json.Marshal(rep)
		fmt.Fprintln(stdout, string(b))
		if !rep.OK() {
			return exitFail
		}
		return exitOK
	}

	st := newStyle(stdout)
	for _, v := range rep.Violations {
		fmt.Fprintf(stdout, "%s turn %d %s\n", st.cross(), v.Turn, st.bold(v.Tool))
		for _, p := range v.Problems {
			fmt.Fprintf(stdout, "    %s\n", p)
		}
	}
	if rep.OK() {
		fmt.Fprintf(stdout, "%s %d tool call(s) conform to their advertised schema; %d had no schema\n",
			st.check(), rep.Checked, rep.Unschemaed)
		return exitOK
	}
	fmt.Fprintf(stdout, "\n%s %d tool call(s) violate their advertised schema (of %d checked)\n",
		st.cross(), len(rep.Violations), rep.Checked)
	return exitFail
}

const schemaCheckUsageText = "usage: cassette schema-check <cassette.yaml> [--json]"

func schemaCheckUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, schemaCheckUsageText)
	return exitUsage
}
