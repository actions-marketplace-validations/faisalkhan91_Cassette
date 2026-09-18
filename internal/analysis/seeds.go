package analysis

import (
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Knobs are the sampling-determinism parameters read from a recorded request.
type Knobs struct {
	Provider string // wireenc dialect name: anthropic | openai-chat | openai-responses | gemini | ollama
	SeedSet  bool
	Seed     int64
	TempSet  bool
	Temp     float64
	TopPSet  bool
	TopP     float64
	// SeedSupported reports whether the provider exposes a seed parameter at all
	// (only OpenAI Chat Completions does). When false, an absent seed is "n/a",
	// not "you forgot one".
	SeedSupported bool
}

// SamplingKnobs extracts the sampling knobs from an interaction's request body.
func SamplingKnobs(it *wirefmt.Interaction) Knobs {
	var k Knobs
	// Use the canonical dialect registry so a Gemini/Ollama turn is labelled
	// correctly (it used to fall through to "anthropic") and seed support stays
	// in one place.
	p := wireenc.ProviderForURL(it.Request.URL)
	k.Provider = ProviderName(p)
	k.SeedSupported = wireenc.SeedSupported(p)
	if it.Request.Body == nil {
		return k
	}
	body := it.Request.Body.Bytes()
	if v := gjson.GetBytes(body, "seed"); v.Exists() {
		k.SeedSet = true
		k.Seed = v.Int()
	}
	if v := gjson.GetBytes(body, "temperature"); v.Exists() {
		k.TempSet = true
		k.Temp = v.Float()
	}
	if v := gjson.GetBytes(body, "top_p"); v.Exists() {
		k.TopPSet = true
		k.TopP = v.Float()
	}
	return k
}

// Reproducible reports whether the turn's sampling is pinned (seed set, or greedy
// temperature==0) and a human rationale. top_p alone never qualifies: it only
// truncates the nucleus, it does not pin the draw. NOTE: even a pinned turn is
// best-effort — providers do not guarantee bitwise-identical output across model
// or system-fingerprint versions.
func (k Knobs) Reproducible() (bool, string) {
	switch {
	case k.SeedSet && k.SeedSupported:
		return true, fmt.Sprintf("seed=%d", k.Seed)
	case k.TempSet && k.Temp == 0:
		return true, "temperature=0 (greedy)"
	case k.SeedSet && !k.SeedSupported:
		return false, fmt.Sprintf("seed=%d ignored (%s has no seed param), no temperature=0", k.Seed, k.Provider)
	case k.TempSet:
		return false, fmt.Sprintf("temperature=%g, no seed", k.Temp)
	default:
		return false, "temperature unset → provider default (likely >0), no seed"
	}
}
