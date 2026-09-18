package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdProvenance prints a derived cassette's lineage chain — the operations that
// produced it and the behavioral digest of each source — so you can prove what a
// recording was derived from. Read-only, offline.
func cmdProvenance(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("json"), args, provenanceUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return provenanceUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}

	if in.boolv("json") {
		prov := f.Provenance
		if prov == nil {
			prov = []wirefmt.Provenance{} // serialize as [] not null
		}
		b, _ := json.Marshal(struct {
			Digest     string               `json:"behavior_digest"`
			Provenance []wirefmt.Provenance `json:"provenance"`
		}{analysis.BehaviorDigest(f), prov})
		fmt.Fprintln(stdout, string(b))
		return exitOK
	}

	st := newStyle(stdout)
	fmt.Fprintf(stdout, "behavior digest: %s…\n", short(analysis.BehaviorDigest(f)))
	if len(f.Provenance) == 0 {
		fmt.Fprintf(stdout, "%s no recorded lineage — this is an original recording (or pre-dates provenance)\n", st.dim("•"))
		return exitOK
	}
	// Print newest-derivation last: source → … → this file.
	fmt.Fprintf(stdout, "lineage (oldest first):\n")
	for i, p := range f.Provenance {
		note := ""
		if p.Note != "" {
			note = " " + st.dim("("+p.Note+")")
		}
		fmt.Fprintf(stdout, "  %d. %s from %s…%s\n", i+1, st.bold(p.Op), short(p.From), note)
	}
	return exitOK
}

const provenanceUsageText = "usage: cassette provenance <cassette.yaml> [--json]"

func provenanceUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, provenanceUsageText)
	return exitUsage
}
