package main

import (
	"fmt"
	"io"
	"os"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdOTel exports a recording (or a directory, merged) as OpenTelemetry GenAI spans
// in OTLP/JSON — a bridge INTO Langfuse / Phoenix / SigNoz / any OTLP backend rather
// than a competing trace UI. Deterministic (digest-derived IDs, timing-derived
// durations); read-only, no network.
func cmdOTel(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, otelUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" || in.nargs() > 1 {
		return otelUsage(stderr)
	}
	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	files, err := loadAll(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	merged, _ := analysis.Merge(files...)
	out, err := analysis.MarshalOTLP(merged)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	out = append(out, '\n')

	if dst := in.str("out"); dst != "" {
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			fmt.Fprintf(stderr, "cassette: write: %v\n", err)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s wrote %s\n", newStyle(stdout).check(), dst)
		return exitOK
	}
	stdout.Write(out)
	return exitOK
}

const otelUsageText = "usage: cassette otel <cassette.yaml|dir> [-o spans.json]"

func otelUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, otelUsageText)
	return exitUsage
}
