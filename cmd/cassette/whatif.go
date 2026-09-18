package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdWhatif grafts a turn across a list of alternative values and reports, for
// each, the resulting combined semantic digest and which downstream turns become
// counterfactual (their request still embeds the pre-edit output) — a one-shot
// what-if matrix instead of N manual graft+inspect cycles.
func cmdWhatif(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("turn", "slot", "values", "report"), args, whatifUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	valuesFile := in.str("values")
	if path == "" || valuesFile == "" || in.nargs() > 1 {
		return whatifUsage(stderr)
	}
	turn := atoiOr(in.str("turn"), 0)
	slot := in.str("slot")
	if slot == "" {
		slot = "text"
	}
	raw, ok := readOrErr(valuesFile, stderr)
	if !ok {
		return exitFail
	}
	var alts []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimRight(line, "\r"); strings.TrimSpace(line) != "" {
			alts = append(alts, line)
		}
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	if turn < 0 || turn >= len(f.Interactions) {
		fmt.Fprintf(stderr, "cassette: turn %d out of range\n", turn)
		return exitFail
	}
	it := f.Interactions[turn]
	base, _, renderable := analysis.DecodeInteraction(it)
	if !renderable {
		fmt.Fprintf(stderr, "cassette: turn %d is not a streaming http turn\n", turn)
		return exitFail
	}
	preText := base.Text
	baseDigest := semequal.CombinedDigest(analysis.CollectTurns(f))
	p := wireenc.ProviderForURL(it.Request.URL)

	type row struct {
		alt, digest string
		changed     bool
		stale       int
	}
	var rows []row
	for _, alt := range alts {
		edited, err := applyGraftEdits(base, []string{slot + "=" + alt}, nil)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitUsage
		}
		vf := whatifClone(f, turn, wireenc.Encode(p, edited, wireenc.Envelope{}))
		d := semequal.CombinedDigest(analysis.CollectTurns(vf))
		rows = append(rows, row{alt: alt, digest: d, changed: d != baseDigest, stale: countDownstreamEmbeds(f, turn, preText)})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "| alternative | combined digest | changed | stale downstream |\n")
	fmt.Fprintf(&b, "|-------------|-----------------|---------|------------------|\n")
	for _, r := range rows {
		mark := "no"
		if r.changed {
			mark = "yes"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d |\n", truncate(r.alt, 40), r.digest[:12], mark, r.stale)
	}
	table := b.String()

	if rep := in.str("report"); rep != "" {
		header := fmt.Sprintf("# what-if matrix — turn %d, slot %s\n\nbase digest: `%s`\n\n", turn, slot, baseDigest[:12])
		if err := os.WriteFile(rep, []byte(header+table), 0o644); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
	}
	fmt.Fprint(stdout, table)
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s evaluated %d alternative(s) for turn %d\n", st.check(), len(rows), turn)
	return exitOK
}

// whatifClone returns a copy of f with turn idx's response replaced by a streaming
// body, without mutating the original (later turns share their pointers, unchanged).
func whatifClone(f *wirefmt.File, idx int, body []byte) *wirefmt.File {
	out := &wirefmt.File{SchemaVersion: f.SchemaVersion, Notice: f.Notice, Expect: f.Expect}
	out.Interactions = make([]*wirefmt.Interaction, len(f.Interactions))
	copy(out.Interactions, f.Interactions)
	orig := f.Interactions[idx]
	out.Interactions[idx] = &wirefmt.Interaction{Kind: orig.Kind, Request: orig.Request,
		Response: wirefmt.Response{Status: 200, Streaming: true,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
			Body:    wirefmt.NewBody(body)}}
	return out
}

// countDownstreamEmbeds counts turns after idx whose request still embeds the
// pre-edit output text (they would replay their original, now-counterfactual,
// responses).
func countDownstreamEmbeds(f *wirefmt.File, idx int, preText string) int {
	if preText == "" {
		return 0
	}
	n := 0
	for i := idx + 1; i < len(f.Interactions); i++ {
		if strings.Contains(string(f.Interactions[i].Request.Body.Bytes()), preText) {
			n++
		}
	}
	return n
}

const whatifUsageText = "usage: cassette whatif <in.yaml> --turn N --slot text --values alts.txt [--report matrix.md]"

func whatifUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, whatifUsageText)
	return exitUsage
}
