package analysis

import (
	"fmt"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Stitch concatenates several cassettes into one ordered recording (argument
// order preserved, no dedup) — composing per-agent or per-tool-server captures
// into a single society replay. Match keys travel with each interaction.
func Stitch(files []*wirefmt.File) *wirefmt.File {
	out := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
	for _, f := range files {
		if f == nil {
			continue
		}
		if out.Expect == nil && f.Expect != nil {
			out.Expect = f.Expect
		}
		carryMatch(out, f)
		out.Interactions = append(out.Interactions, f.Interactions...)
	}
	return out
}

// carryMatch unions f's persisted match normalization (volatile JSON paths / header
// allowlist) into out. Composing or decomposing a cassette must preserve this block,
// or a recording keyed with volatile-path exclusion stops replaying (the live key is
// recomputed over the full body). Shared by Stitch, Split, and Merge.
func carryMatch(out, f *wirefmt.File) {
	if f.Match == nil {
		return
	}
	if out.Match == nil {
		out.Match = &wirefmt.MatchSpec{}
	}
	out.Match.VolatileJSONPaths = unionStrings(out.Match.VolatileJSONPaths, f.Match.VolatileJSONPaths)
	out.Match.HeaderAllowlist = unionStrings(out.Match.HeaderAllowlist, f.Match.HeaderAllowlist)
}

// Split decomposes a recording into groups keyed by an axis — "provider"
// (the wire dialect: anthropic / openai-chat / openai-responses / gemini / ollama,
// or "mcp") or "tool" (the first tool a turn calls, else "no-tool") — the inverse
// of Stitch. Each group preserves recorded order. Returns groupKey → cassette.
func Split(f *wirefmt.File, by string) (map[string]*wirefmt.File, error) {
	if by != "provider" && by != "tool" {
		return nil, fmt.Errorf("unknown --by %q (want provider|tool)", by)
	}
	groups := map[string]*wirefmt.File{}
	ensure := func(k string) *wirefmt.File {
		if groups[k] == nil {
			groups[k] = &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
			carryMatch(groups[k], f) // each split output keeps the source's match normalization
		}
		return groups[k]
	}
	for _, it := range f.Interactions {
		var key string
		switch by {
		case "provider":
			if it.Kind == "mcp" {
				key = "mcp"
			} else {
				key = ProviderName(wireenc.ProviderForURL(it.Request.URL))
			}
		case "tool":
			tr, _, _ := DecodeInteraction(it)
			if len(tr.ToolCalls) > 0 {
				key = tr.ToolCalls[0].Name
			} else {
				key = "no-tool"
			}
		}
		g := ensure(key)
		g.Interactions = append(g.Interactions, it)
	}
	return groups, nil
}
