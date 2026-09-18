package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdGraft rewrites ONE recorded turn's semantic output (text / finish / tool
// calls), re-encodes it to a valid wire stream, and writes a NEW cassette. It is
// a single-turn counterfactual, NOT a re-simulation: replay matches on the
// request, so editing a response does not make the agent re-derive later turns.
// Graft warns when a downstream request still embeds the pre-edit output, and
// --truncate-after can drop the now-inconsistent tail.
func cmdGraft(args []string, stdout, stderr io.Writer) int {
	inv, ok := parseOrUsage(newFlags().valFlag("turn", "set", "drop-tool", "truncate-after", "out").alias("o", "out"), args, graftUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if inv.nargs() > 1 {
		return exitUsage
	}
	in := inv.arg(0)
	out := inv.str("out")
	sets := inv.list("set")
	turn := -1
	if inv.has("turn") {
		n, err := strconv.Atoi(inv.str("turn"))
		if err != nil {
			return graftUsage(stderr)
		}
		turn = n
	}
	truncateAfter := -1
	if inv.has("truncate-after") {
		n, err := strconv.Atoi(inv.str("truncate-after"))
		if err != nil {
			return graftUsage(stderr)
		}
		truncateAfter = n
	}
	var dropTools []int
	for _, s := range inv.list("drop-tool") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return graftUsage(stderr)
		}
		dropTools = append(dropTools, n)
	}
	if in == "" || out == "" || turn < 0 {
		return graftUsage(stderr)
	}
	f, ok := loadOrErr(in, stderr)
	if !ok {
		return exitFail
	}
	srcDigest := analysis.BehaviorDigest(f) // capture BEFORE the in-place edit
	if turn >= len(f.Interactions) {
		fmt.Fprintf(stderr, "cassette: turn %d out of range\n", turn)
		return exitFail
	}
	it := f.Interactions[turn]
	tr, _, renderable := analysis.DecodeInteraction(it)
	if !renderable {
		fmt.Fprintf(stderr, "cassette: turn %d is not a streaming http turn (cannot graft)\n", turn)
		return exitFail
	}
	preText := tr.Text
	preArgs := make([]string, len(tr.ToolCalls))
	for i, tc := range tr.ToolCalls {
		preArgs[i] = tc.Args
	}

	edited, err := applyGraftEdits(tr, sets, dropTools)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	// Re-encode and splice back; drop StreamTiming (no longer real frames).
	body := wireenc.Encode(wireenc.ProviderForURL(it.Request.URL), edited, wireenc.Envelope{})
	it.Response.Body = wirefmt.NewBody(body)
	it.Response.Streaming = true
	it.Response.StreamTiming = nil
	if f.Notice == "" {
		f.Notice = fmt.Sprintf("turn %d grafted (synthetic response); not a re-simulated agent run", turn)
	}

	st := newStyle(stderr)
	// Warn about downstream turns whose request embeds the PRE-edit output.
	var stale []int
	for i := turn + 1; i < len(f.Interactions); i++ {
		rb := string(f.Interactions[i].Request.Body.Bytes())
		hit := preText != "" && strings.Contains(rb, preText)
		for _, pa := range preArgs {
			if pa != "" && strings.Contains(rb, pa) {
				hit = true
			}
		}
		if hit {
			stale = append(stale, i)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(stderr, "%s downstream turns %v still embed the pre-edit output and are now counterfactual.\n", st.yellow("warning:"), stale)
		fmt.Fprintf(stderr, "         they replay their ORIGINAL responses; use --truncate-after %d for a clean single-turn counterfactual.\n", turn)
	}
	if truncateAfter >= 0 && truncateAfter+1 < len(f.Interactions) {
		f.Interactions = f.Interactions[:truncateAfter+1]
	}

	analysis.AppendProvenance(f, "graft", srcDigest, fmt.Sprintf("turn %d", turn))
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
	fmt.Fprintf(stdout, "%s grafted turn %d → %s\n", so.check(), turn, out)
	return exitOK
}

// applyGraftEdits applies --set / --drop-tool edits to a transcript.
func applyGraftEdits(tr semequal.Transcript, sets []string, dropTools []int) (semequal.Transcript, error) {
	for _, s := range sets {
		kv := strings.SplitN(s, "=", 2)
		if len(kv) != 2 {
			return tr, fmt.Errorf("bad --set %q (want key=value)", s)
		}
		key, val := kv[0], kv[1]
		switch {
		case key == "text":
			tr.Text = val
		case key == "append":
			tr.Text += val
		case key == "finish":
			tr.FinishReason = val
		case strings.HasPrefix(key, "tool."):
			parts := strings.Split(key, ".")
			if len(parts) != 3 {
				return tr, fmt.Errorf("bad tool key %q (want tool.<i>.name|args)", key)
			}
			idx, err := strconv.Atoi(parts[1])
			if err != nil || idx < 0 || idx >= len(tr.ToolCalls) {
				return tr, fmt.Errorf("tool index %q out of range", parts[1])
			}
			switch parts[2] {
			case "name":
				tr.ToolCalls[idx].Name = val
			case "args":
				if c, ok := canon.Canonicalize([]byte(val)); ok {
					val = string(c)
				} else {
					return tr, fmt.Errorf("tool.%d.args is not valid JSON", idx)
				}
				tr.ToolCalls[idx].Args = val
			default:
				return tr, fmt.Errorf("unknown tool field %q", parts[2])
			}
		default:
			return tr, fmt.Errorf("unknown --set key %q", key)
		}
	}
	if len(dropTools) > 0 {
		drop := map[int]bool{}
		for _, d := range dropTools {
			drop[d] = true
		}
		var kept []semequal.ToolCall
		for i, tc := range tr.ToolCalls {
			if !drop[i] {
				kept = append(kept, tc)
			}
		}
		tr.ToolCalls = kept
	}
	return tr, nil
}

const graftUsageText = "usage: cassette graft <in.yaml> --turn N [--set text=…|finish=…|tool.<i>.name=…|tool.<i>.args=JSON]... [--drop-tool i]... [--truncate-after M] -o out.yaml"

func graftUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, graftUsageText)
	return exitUsage
}
