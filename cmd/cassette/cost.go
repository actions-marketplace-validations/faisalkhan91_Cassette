package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// price is per-million-token pricing for a model.
type price struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// cmdCost reports token usage (always) and cost (when a price table is given)
// over a recording, and fails CI when a budget is exceeded — a deterministic,
// offline, zero-network cost-regression gate. Usage is read byte-exact from the
// recording; turns with no usage data are reported as "unknown", never as zero.
func cmdCost(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("prices", "budget", "by"), args, costUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return costUsage(stderr)
	}
	pricesPath := in.str("prices")
	budget := in.str("budget")
	byModel := false
	if in.has("by") {
		if in.str("by") != "model" {
			fmt.Fprintln(stderr, "cassette: --by only supports 'model'")
			return exitUsage
		}
		byModel = true
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}

	prices := map[string]price{}
	if pricesPath != "" {
		raw, err := os.ReadFile(pricesPath)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		if err := json.Unmarshal(raw, &prices); err != nil {
			fmt.Fprintf(stderr, "cassette: parse prices: %v\n", err)
			return exitFail
		}
	}

	type agg struct {
		in, out, cacheR, cacheW int
		usd                     float64
		unknown                 int
	}
	total := agg{}
	perModel := map[string]*agg{}
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		u := analysis.DecodeUsage(it)
		model := analysis.RequestModel(it)
		m := perModel[model]
		if m == nil {
			m = &agg{}
			perModel[model] = m
		}
		if !u.Known {
			total.unknown++
			m.unknown++
			continue
		}
		var usd float64
		if p, ok := prices[model]; ok {
			usd = float64(u.InputTokens)/1e6*p.Input + float64(u.OutputTokens)/1e6*p.Output
		}
		total.in += u.InputTokens
		total.out += u.OutputTokens
		total.cacheR += u.CacheRead
		total.cacheW += u.CacheWrite
		total.usd += usd
		m.in += u.InputTokens
		m.out += u.OutputTokens
		m.usd += usd
	}

	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s %s\n", st.bold("cost"), path)
	fmt.Fprintf(stdout, "  tokens : %d in + %d out = %d total\n", total.in, total.out, total.in+total.out)
	if total.cacheR > 0 || total.cacheW > 0 {
		fmt.Fprintf(stdout, "  cache  : %d read, %d write\n", total.cacheR, total.cacheW)
	}
	if pricesPath != "" {
		fmt.Fprintf(stdout, "  cost   : $%.4f\n", total.usd)
	}
	if total.unknown > 0 {
		fmt.Fprintf(stdout, "  %s %d turn(s) had no usage data (counted as unknown, not zero)\n", st.yellow("note:"), total.unknown)
	}
	if byModel {
		var models []string
		for m := range perModel {
			models = append(models, m)
		}
		sort.Strings(models)
		fmt.Fprintf(stdout, "\n  by model:\n")
		for _, m := range models {
			a := perModel[m]
			name := m
			if name == "" {
				name = "(unknown model)"
			}
			line := fmt.Sprintf("    %-28s %d in + %d out", name, a.in, a.out)
			if pricesPath != "" {
				line += fmt.Sprintf("   $%.4f", a.usd)
			}
			fmt.Fprintln(stdout, line)
		}
	}

	// Budget gate.
	if budget != "" {
		kv := strings.SplitN(budget, "=", 2)
		if len(kv) != 2 {
			fmt.Fprintln(stderr, "cassette: --budget must be tokens=N or usd=X")
			return exitUsage
		}
		limit, err := strconv.ParseFloat(kv[1], 64)
		if err != nil {
			return costUsage(stderr)
		}
		switch kv[0] {
		case "tokens":
			if float64(total.in+total.out) > limit {
				fmt.Fprintf(stderr, "%s cassette: %d tokens exceeds budget %.0f\n", st.cross(), total.in+total.out, limit)
				return exitFail
			}
		case "usd":
			if pricesPath == "" {
				fmt.Fprintln(stderr, "cassette: usd budget requires --prices")
				return exitUsage
			}
			if total.usd > limit {
				fmt.Fprintf(stderr, "%s cassette: $%.4f exceeds budget $%.2f\n", st.cross(), total.usd, limit)
				return exitFail
			}
		default:
			fmt.Fprintln(stderr, "cassette: --budget must be tokens=N or usd=X")
			return exitUsage
		}
		fmt.Fprintf(stdout, "%s within budget\n", st.check())
	}
	return exitOK
}

const costUsageText = "usage: cassette cost <cassette.yaml> [--prices p.json] [--budget tokens=N|usd=X] [--by model]"

func costUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, costUsageText)
	return exitUsage
}
