package analysis

import (
	"bytes"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// CanonicalizeFile rewrites every codec-preserving streaming response in f to its
// canonical wire form: decoded to a semequal.Transcript and re-encoded through
// wireenc with a zeroed Envelope, so the volatile fields (ids, usage, model echo)
// and the incidental SSE chunk boundaries collapse to a fixed shape. Two
// recordings of the same behavior therefore serialize to byte-identical bytes, so
// `git diff` on a re-record shows only what the model actually did differently.
//
// It preserves each turn's semantic digest (decode(canonical) == decode(original)
// for the turns it rewrites) and leaves untouched any turn the codec does not
// round-trip — unary (non-SSE) responses, error streams, and any non-preserving
// turn. Per-frame StreamTiming is dropped only on a turn it actually REWRITES (the
// frame indices no longer line up after re-encoding); a turn already in canonical
// form is left exactly as recorded, so a timing-faithful recording (captured with
// `--pace`) survives canonicalize and its `--check` gate untouched. It is
// idempotent: running it twice changes nothing the second time. Returns the number
// of interactions rewritten.
func CanonicalizeFile(f *wirefmt.File) int {
	n := 0
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		_, unary, renderable := DecodeInteraction(it)
		if unary || !renderable {
			continue
		}
		p := wireenc.ProviderForURL(it.Request.URL)
		raw := it.Response.Body.Bytes()
		res, err := wireenc.RoundTrip(p, raw)
		if err != nil || !res.Stable {
			continue // error stream or non-preserving: leave exactly as recorded
		}
		canonical := wireenc.Encode(p, res.Decoded, wireenc.Envelope{})
		if bytes.Equal(canonical, raw) {
			continue // already canonical — leave the turn (and any captured timing) intact
		}
		it.Response.Body = wirefmt.NewBody(canonical)
		it.Response.StreamTiming = nil // frames rewritten: per-frame timing no longer maps
		n++
	}
	return n
}
