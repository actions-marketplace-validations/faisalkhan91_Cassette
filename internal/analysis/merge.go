package analysis

import (
	"sort"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// MergeReport summarizes a Merge.
type MergeReport struct {
	Total     int      // interactions in the merged file
	Deduped   int      // exact-duplicate interactions dropped
	Conflicts []string // logical keys seen with two or more DIFFERENT responses
}

// Merge unions the interactions of several cassettes into one, in argument order,
// dropping exact duplicates (same request key + identical request and response
// bytes) and detecting conflicts (the same request key mapping to two different
// responses). Distinct behavior is never lost — only byte-identical copies are
// collapsed — so passing a shared-prefix cassette first and a per-test tail second
// yields a layered fixture deterministically. The first non-nil Expect contract is
// carried onto the result.
func Merge(files ...*wirefmt.File) (*wirefmt.File, MergeReport) {
	out := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
	var rep MergeReport
	seenExact := map[string]bool{}   // logicalKey+reqSum+respSum
	keyResp := map[string]string{}   // logicalKey -> first response sum
	conflicting := map[string]bool{} // logicalKey with >1 distinct response

	for _, f := range files {
		if f == nil {
			continue
		}
		if out.Expect == nil && f.Expect != nil {
			out.Expect = f.Expect
		}
		// Preserve match normalization so a merged directory of --volatile cassettes
		// stays self-describing (union the volatile paths / header allowlist).
		carryMatch(out, f)
		for _, it := range f.Interactions {
			key := logicalKey(it)
			reqSum := canon.SumHex(it.Request.Body.Bytes())
			respSum := canon.SumHex(it.Response.Body.Bytes())
			identity := key + "\x1f" + reqSum + "\x1f" + respSum
			if seenExact[identity] {
				rep.Deduped++
				continue
			}
			seenExact[identity] = true
			if prev, ok := keyResp[key]; ok {
				if prev != respSum {
					conflicting[key] = true
				}
			} else {
				keyResp[key] = respSum
			}
			out.Interactions = append(out.Interactions, it)
		}
	}
	rep.Total = len(out.Interactions)
	for k := range conflicting {
		rep.Conflicts = append(rep.Conflicts, k)
	}
	sort.Strings(rep.Conflicts)
	return out, rep
}

// logicalKey is the request match key for dedup/conflict purposes: the persisted
// MatchKey when present, else a best-effort recomputation.
func logicalKey(it *wirefmt.Interaction) string {
	if it.Request.MatchKey != "" {
		return it.Request.MatchKey
	}
	switch it.Kind {
	case "http":
		k, err := match.Key(match.Input{
			Method:      it.Request.Method,
			Path:        it.Request.URL,
			Body:        it.Request.Body.Bytes(),
			ContentType: it.Request.Headers.Get("Content-Type"),
		}, match.Config{})
		if err == nil {
			return k
		}
		return "http:" + it.Request.Method + ":" + it.Request.URL + ":" + canon.SumHex(it.Request.Body.Bytes())
	case "mcp":
		return "mcp:" + it.Request.MCPMethod + ":" + it.Request.MCPTool + ":" + canon.SumHex(it.Request.Body.Bytes())
	default:
		return it.Kind + ":" + canon.SumHex(it.Request.Body.Bytes())
	}
}

// unionStrings appends b's items not already in a, preserving order; deterministic.
func unionStrings(a, b []string) []string {
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			a = append(a, s)
		}
	}
	return a
}
