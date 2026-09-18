package analysis

import (
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// CoverageReport is a behavioral coverage matrix over a set of cassettes: which
// tools, finish reasons, providers, and refusal verdicts the corpus actually
// exercises, plus stream/unary counts. Measured on the normalized Transcript axis
// (behavior), which only cassette extracts.
type CoverageReport struct {
	Cassettes int            `json:"cassettes"`
	Turns     int            `json:"turns"`
	Tools     map[string]int `json:"tools"`
	Finish    map[string]int `json:"finish"`
	Providers map[string]int `json:"providers"`
	Refusal   map[string]int `json:"refusal"`
	Streaming int            `json:"streaming"`
	Unary     int            `json:"unary"`
}

// Coverage builds the coverage matrix for a set of cassettes.
func Coverage(files []*wirefmt.File) CoverageReport {
	rep := CoverageReport{
		Cassettes: len(files),
		Tools:     map[string]int{}, Finish: map[string]int{},
		Providers: map[string]int{}, Refusal: map[string]int{},
	}
	for _, f := range files {
		for _, it := range f.Interactions {
			if it.Kind != "http" {
				continue
			}
			tr, unary, renderable := DecodeInteraction(it)
			if !unary && !renderable {
				continue
			}
			rep.Turns++
			rep.Providers[ProviderName(wireenc.ProviderForURL(it.Request.URL))]++
			if unary {
				rep.Unary++
				continue
			}
			rep.Streaming++
			for _, tc := range tr.ToolCalls {
				rep.Tools[tc.Name]++
			}
			if tr.FinishReason != "" {
				rep.Finish[tr.FinishReason]++
			}
			rep.Refusal[string(ClassifyRefusal(tr))]++
		}
	}
	return rep
}

// Has reports whether an axis ("tool"|"finish"|"provider"|"refusal") contains a
// value in the coverage.
func (r CoverageReport) Has(axis, value string) bool {
	switch axis {
	case "tool", "tools":
		return r.Tools[value] > 0
	case "finish":
		return r.Finish[value] > 0
	case "provider":
		return r.Providers[value] > 0
	case "refusal":
		return r.Refusal[value] > 0
	default:
		return false
	}
}

// signalsOf returns the behavioral signals a single cassette contributes: per-turn
// semantic digests plus tool/finish/provider/refusal coverage cells. The union
// across a corpus is what shrink must preserve.
func signalsOf(f *wirefmt.File) map[string]bool {
	sig := map[string]bool{}
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		tr, unary, renderable := DecodeInteraction(it)
		if !unary && !renderable {
			continue
		}
		sig["provider:"+ProviderName(wireenc.ProviderForURL(it.Request.URL))] = true
		sig["digest:"+tr.Digest()] = true
		if unary {
			continue
		}
		for _, tc := range tr.ToolCalls {
			sig["tool:"+tc.Name] = true
		}
		if tr.FinishReason != "" {
			sig["finish:"+tr.FinishReason] = true
		}
		sig["refusal:"+string(ClassifyRefusal(tr))] = true
	}
	return sig
}

// Shrink computes a minimal subset of the named cassettes whose union of
// behavioral signals (per-turn semantic digests + tool/finish/provider/refusal
// coverage) equals the whole corpus's — a greedy set cover. names[i] labels
// files[i]; the returned kept names preserve input order. Deterministic.
func Shrink(names []string, files []*wirefmt.File) (kept []string) {
	type item struct {
		name string
		sig  map[string]bool
	}
	items := make([]item, len(files))
	universe := map[string]bool{}
	for i, f := range files {
		s := signalsOf(f)
		items[i] = item{name: names[i], sig: s}
		for k := range s {
			universe[k] = true
		}
	}
	covered := map[string]bool{}
	used := make([]bool, len(items))
	keptIdx := map[int]bool{}
	for len(covered) < len(universe) {
		best, bestGain := -1, 0
		for i, it := range items {
			if used[i] {
				continue
			}
			gain := 0
			for k := range it.sig {
				if !covered[k] {
					gain++
				}
			}
			if gain > bestGain {
				best, bestGain = i, gain
			}
		}
		if best < 0 {
			break // remaining files add nothing
		}
		used[best] = true
		keptIdx[best] = true
		for k := range items[best].sig {
			covered[k] = true
		}
	}
	// Preserve input order in the result.
	for i := range items {
		if keptIdx[i] {
			kept = append(kept, items[i].name)
		}
	}
	return kept
}
