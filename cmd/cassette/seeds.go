package main

import (
	"encoding/json"
	"fmt"
	"io"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdSeeds reports the sampling-determinism knobs per turn and classifies each
// reproducible vs nondeterministic — so a user knows whether a forward-branch
// re-run will reproduce. It reports intent (knobs set), not realized determinism.
func cmdSeeds(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("strict", "json"), args, seedsUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path != "" && in.nargs() > 1 {
		return exitUsage
	}
	strict := in.boolv("strict")
	asJSON := in.boolv("json")
	if path == "" {
		return seedsUsage(stderr)
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}

	st := newStyle(stdout)
	total, repro, nondeterministic := 0, 0, 0
	turn := -1
	if !asJSON {
		fmt.Fprintf(stdout, "%-4s %-17s %-12s %-7s %-7s %s\n", "turn", "provider", "seed", "temp", "top_p", "verdict")
	}
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		turn++
		total++
		k := analysis.SamplingKnobs(it)
		ok, reason := k.Reproducible()
		if ok {
			repro++
		} else {
			nondeterministic++
		}
		if asJSON {
			b, _ := json.Marshal(map[string]any{
				"turn": turn, "provider": k.Provider, "seed_set": k.SeedSet, "seed": k.Seed,
				"seed_supported": k.SeedSupported, "temperature_set": k.TempSet, "temperature": k.Temp,
				"top_p_set": k.TopPSet, "top_p": k.TopP, "reproducible": ok, "reason": reason})
			fmt.Fprintln(stdout, string(b))
			continue
		}
		verdict := st.yellow("nondeterministic")
		if ok {
			verdict = st.green("reproducible")
		}
		fmt.Fprintf(stdout, "%-4d %-17s %-12s %-7s %-7s %s — %s\n",
			turn, k.Provider, seedCell(k), floatCell(k.TempSet, k.Temp), floatCell(k.TopPSet, k.TopP), verdict, reason)
	}

	if !asJSON {
		if total == 0 {
			fmt.Fprintln(stdout, "no http turns to classify")
			return exitOK
		}
		fmt.Fprintf(stdout, "\n%d/%d turns reproducible\n", repro, total)
		fmt.Fprintln(stdout, st.dim("note: reproducible == sampling pinned; providers do not guarantee bitwise-identical output across model/system_fingerprint versions."))
	}
	if nondeterministic > 0 && strict {
		return exitFail
	}
	return exitOK
}

func seedCell(k analysis.Knobs) string {
	if !k.SeedSupported {
		return "n/a"
	}
	if k.SeedSet {
		return fmt.Sprintf("%d", k.Seed)
	}
	return "unset"
}

func floatCell(set bool, v float64) string {
	if !set {
		return "unset"
	}
	return fmt.Sprintf("%g", v)
}

const seedsUsageText = "usage: cassette seeds <cassette.yaml> [--strict] [--json]"

func seedsUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, seedsUsageText)
	return exitUsage
}
