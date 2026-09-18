package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdAudit emits a single deterministic, offline evidence bundle for a recording or
// corpus — the behavioral attestation manifest + coverage matrix + outbound PII
// findings + cross-turn taint flows — the artifact a SOC2 / EU AI Act / ISO 42001
// review asks for. With --fail-on it doubles as a CI governance gate.
func cmdAudit(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out", "fail-on").alias("o", "out"), args, auditUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" || in.nargs() > 1 {
		return auditUsage(stderr)
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

	bundle := analysis.BuildAudit(paths, files)
	out, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	out = append(out, '\n')

	st := newStyle(stdout)
	if dst := in.str("out"); dst != "" {
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			fmt.Fprintf(stderr, "cassette: write: %v\n", err)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s wrote %s — %d cassette(s), %d egress finding(s), %d taint flow(s)\n",
			st.check(), dst, bundle.Coverage.Cassettes, len(bundle.Egress), len(bundle.Taint))
	} else {
		stdout.Write(out)
	}

	// Governance gate: --fail-on lists egress data-class kinds and/or the keyword
	// "exfil"/"taint" (any model/tool-output value carried back outbound). Without
	// --fail-on, audit is pure evidence generation and always exits 0.
	failOn := in.flagSet("fail-on")
	if len(failOn) == 0 {
		return exitOK
	}
	var violations []string
	for _, e := range bundle.Egress {
		if failOn[e.Kind] {
			violations = append(violations, fmt.Sprintf("egress %s in turn %d (%s)", e.Kind, e.Turn, e.Redacted))
		}
	}
	if failOn["exfil"] || failOn["taint"] {
		for _, t := range bundle.Exfils() {
			violations = append(violations, fmt.Sprintf("exfil %s turn %d→%d (%s)", t.Kind, t.SourceTurn, t.SinkTurn, t.Redacted))
		}
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintf(stderr, "%s %s\n", st.cross(), v)
		}
		fmt.Fprintf(stderr, "%s %d policy violation(s)\n", st.cross(), len(violations))
		return exitFail
	}
	return exitOK
}

const auditUsageText = "usage: cassette audit <cassette.yaml|dir> [-o bundle.json] [--fail-on email,creditcard,exfil,...]"

func auditUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, auditUsageText)
	return exitUsage
}
