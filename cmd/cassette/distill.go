package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"gopkg.in/yaml.v3"
)

type corpusEntry struct {
	Value          string `yaml:"value"`
	Cassette       string `yaml:"cassette"`
	Turn           int    `yaml:"turn"`
	Slot           string `yaml:"slot"`
	ExpectedDigest string `yaml:"expected_digest"`
}

// cmdDistill mines one recorded turn into a parameterized family of synthetic
// cassettes by templating a single response slot (the assistant text, or a
// tool's args) across a list of values and re-encoding via the keystone. It
// RECOMBINES recorded behavior; it cannot synthesize behavior the model never
// produced. Emits one cassette per value plus a self-checking corpus.yaml of
// expected semantic digests.
func cmdDistill(args []string, stdout, stderr io.Writer) int {
	inv, ok := parseOrUsage(newFlags().valFlag("turn", "slot", "values", "out").alias("o", "out"), args, distillUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if inv.nargs() > 1 {
		return exitUsage
	}
	in := inv.arg(0)
	slot := inv.str("slot")
	valuesPath := inv.str("values")
	outDir := inv.str("out")
	turn := -1
	if inv.has("turn") {
		turn = atoiOr(inv.str("turn"), -1)
	}
	if in == "" || slot == "" || valuesPath == "" || outDir == "" || turn < 0 {
		return distillUsage(stderr)
	}
	if slot != "text" && !strings.HasPrefix(slot, "tool.") {
		fmt.Fprintln(stderr, "cassette: --slot must be 'text' or 'tool.<i>.args'")
		return exitUsage
	}
	rawVals, err := os.ReadFile(valuesPath)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	var values []string
	for _, ln := range strings.Split(string(rawVals), "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		values = append(values, ln)
	}
	if len(values) == 0 {
		fmt.Fprintln(stderr, "cassette: no values to distill")
		return exitFail
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	st := newStyle(stderr)
	base := strings.TrimSuffix(filepath.Base(in), ".yaml")
	var corpus []corpusEntry
	for k, v := range values {
		// Fresh copy per value (re-load avoids shared-pointer mutation).
		f, err := wirefmt.Load(in)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if turn >= len(f.Interactions) {
			fmt.Fprintf(stderr, "cassette: turn %d out of range\n", turn)
			return exitFail
		}
		srcDigest := analysis.BehaviorDigest(f) // capture before the in-place edit
		it := f.Interactions[turn]
		tr, _, renderable := analysis.DecodeInteraction(it)
		if !renderable {
			fmt.Fprintf(stderr, "cassette: turn %d is not a streaming http turn\n", turn)
			return exitFail
		}
		edited, err := applyGraftEdits(tr, []string{slot + "=" + v}, nil)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		it.Response.Body = wirefmt.NewBody(wireenc.Encode(wireenc.ProviderForURL(it.Request.URL), edited, wireenc.Envelope{}))
		it.Response.Streaming = true
		it.Response.StreamTiming = nil
		f.Notice = fmt.Sprintf("distilled from %s (turn %d slot %q); synthetic response", filepath.Base(in), turn, slot)
		analysis.AppendProvenance(f, "distill", srcDigest, fmt.Sprintf("turn %d slot %s", turn, slot))

		raw, err := wirefmt.Marshal(f)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if hits := scrub.SecretScan(raw); len(hits) > 0 {
			fmt.Fprintf(stderr, "%s cassette: refusing to write — secret pattern(s) present: %v\n", st.cross(), hits)
			return exitFail
		}
		name := fmt.Sprintf("%s_%d.yaml", base, k)
		if err := wirefmt.Save(filepath.Join(outDir, name), f); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		corpus = append(corpus, corpusEntry{Value: v, Cassette: name, Turn: turn, Slot: slot, ExpectedDigest: edited.Digest()})
	}

	cb, _ := yaml.Marshal(struct {
		Corpus []corpusEntry `yaml:"corpus"`
	}{corpus})
	if err := os.WriteFile(filepath.Join(outDir, "corpus.yaml"), cb, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	so := newStyle(stdout)
	fmt.Fprintf(stdout, "%s distilled %d cassette(s) + corpus.yaml into %s\n", so.check(), len(corpus), outDir)
	return exitOK
}

func atoiOr(s string, def int) int {
	n := def
	if v, err := fmt.Sscanf(s, "%d", &n); v == 1 && err == nil {
		return n
	}
	return def
}

const distillUsageText = "usage: cassette distill <in.yaml> --turn N --slot text|tool.<i>.args --values vals.txt -o <dir>"

func distillUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, distillUsageText)
	return exitUsage
}
