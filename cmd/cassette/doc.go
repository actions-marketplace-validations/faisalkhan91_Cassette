package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// renderDoc renders a cassette as a human-readable Markdown transcript,
// regenerated from the fixture so it can never drift from the recorded behavior.
func renderDoc(path string, f *wirefmt.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Cassette: %s\n\n", filepath.Base(path))
	if f.Notice != "" {
		fmt.Fprintf(&b, "> %s\n\n", f.Notice)
	}
	fmt.Fprintf(&b, "%d interaction(s).\n", len(f.Interactions))
	for i, it := range f.Interactions {
		switch it.Kind {
		case "mcp":
			tool := it.Request.MCPTool
			if tool != "" {
				tool = " `" + tool + "`"
			}
			fmt.Fprintf(&b, "\n## %d. MCP %s%s\n\n", i, it.Request.MCPMethod, tool)
			if body := it.Response.Body.Bytes(); len(body) > 0 {
				fmt.Fprintf(&b, "result: `%s`\n", truncate(string(body), 200))
			}
		case "http":
			fmt.Fprintf(&b, "\n## %d. %s %s → %d\n\n", i, it.Request.Method, it.Request.URL, it.Response.Status)
			tr, ok := decodeTranscript(it)
			if !ok {
				continue
			}
			if tr.Text != "" {
				for _, line := range strings.Split(strings.TrimRight(tr.Text, "\n"), "\n") {
					fmt.Fprintf(&b, "> %s\n", line)
				}
			}
			for _, tc := range tr.ToolCalls {
				fmt.Fprintf(&b, "- 🔧 `%s(%s)`\n", tc.Name, tc.Args)
			}
			if tr.FinishReason != "" {
				fmt.Fprintf(&b, "\n_finish: %s_\n", tr.FinishReason)
			}
		}
	}
	return b.String()
}

const docUsageText = "usage: cassette doc <cassette.yaml> [--check doc.md]"

func docUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, docUsageText)
	return exitUsage
}

func cmdDoc(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("check"), args, docUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	check := in.str("check")
	if path != "" && in.nargs() > 1 {
		return exitUsage
	}
	if path == "" {
		return docUsage(stderr)
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	md := renderDoc(path, f)
	if check != "" {
		want, err := os.ReadFile(check)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if string(want) != md {
			st := newStyle(stderr)
			fmt.Fprintf(stderr, "%s cassette: %s is stale; regenerate with `cassette doc %s`\n", st.cross(), check, path)
			return exitFail
		}
		st := newStyle(stdout)
		fmt.Fprintf(stdout, "%s %s is up to date\n", st.check(), check)
		return exitOK
	}
	fmt.Fprint(stdout, md)
	return exitOK
}
