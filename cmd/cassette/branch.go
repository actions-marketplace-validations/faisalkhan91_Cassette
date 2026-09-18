package main

import (
	"fmt"
	"io"
	"strconv"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// cmdBranch PREPARES a forward-branch seed cassette: it (optionally) grafts turn
// N's output, then truncates after N so the seed holds ONLY the deterministic
// prefix the agent replays before diverging. It does NOT re-run the agent —
// re-running forward is a library step: open the seed with Mode: ModeBranch +
// Options.Live and run your own agent harness, then Save() the grown branch.
func cmdBranch(args []string, stdout, stderr io.Writer) int {
	inv, ok := parseOrUsage(newFlags().valFlag("from", "graft", "out").alias("o", "out"), args, branchUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if inv.nargs() > 1 {
		return exitUsage
	}
	in := inv.arg(0)
	out := inv.str("out")
	sets := inv.list("graft")
	from := -1
	if inv.has("from") {
		n, err := strconv.Atoi(inv.str("from"))
		if err != nil {
			return branchUsage(stderr)
		}
		from = n
	}
	if in == "" || out == "" || from < 0 {
		return branchUsage(stderr)
	}
	f, ok := loadOrErr(in, stderr)
	if !ok {
		return exitFail
	}
	if from >= len(f.Interactions) {
		fmt.Fprintf(stderr, "cassette: --from %d out of range\n", from)
		return exitFail
	}
	it := f.Interactions[from]
	st := newStyle(stderr)

	if len(sets) > 0 {
		tr, _, renderable := analysis.DecodeInteraction(it)
		if !renderable {
			fmt.Fprintf(stderr, "cassette: turn %d is not a streaming http turn (cannot graft)\n", from)
			return exitFail
		}
		edited, err := applyGraftEdits(tr, sets, nil)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		it.Response.Body = wirefmt.NewBody(wireenc.Encode(wireenc.ProviderForURL(it.Request.URL), edited, wireenc.Envelope{}))
		it.Response.Streaming = true
		it.Response.StreamTiming = nil
	}

	// Seed = the deterministic prefix only; the agent diverges after turn N and
	// branch mode captures the live suffix from there.
	f.Interactions = f.Interactions[:from+1]
	f.Notice = fmt.Sprintf("branch seed: replay turns 0..%d then go live (open with Mode: ModeBranch)", from)

	raw, err := wirefmt.Marshal(f)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "%s cassette: refusing to write — secret pattern(s) present: %v\n", st.cross(), hits)
		return exitFail
	}
	if err := wirefmt.Save(out, f); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	so := newStyle(stdout)
	fmt.Fprintf(stdout, "%s branch seed %s (turns 0..%d)\n", so.check(), out, from)
	// Determinism banner for the divergence point's request.
	repro, reason := analysis.SamplingKnobs(it).Reproducible()
	if repro {
		fmt.Fprintf(stdout, "  determinism: pinned (%s) — the live re-run should reproduce\n", reason)
	} else {
		fmt.Fprintf(stdout, "  %s determinism: %s; this forward-branch is not reproducible, re-runs may differ\n", so.yellow("WARNING"), reason)
	}
	fmt.Fprintf(stdout, "  next: open %s with cassette.Options{Mode: cassette.ModeBranch, Live: cassette.LiveTransport(baseURL, auth)}, run your agent, then Save()\n", out)
	return exitOK
}

const branchUsageText = "usage: cassette branch <in.yaml> --from N [--graft key=value]... -o seed.yaml"

func branchUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, branchUsageText)
	return exitUsage
}
