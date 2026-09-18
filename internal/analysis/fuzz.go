package analysis

import (
	"fmt"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Fuzz generates n valid re-framings of a recording's turn: byte-different
// streams (split text deltas, varied envelope, shuffled tool order) that all
// decode to the SAME transcript. Each is returned as a full cassette (the target
// turn replaced) so it replays. It asserts every variant round-trips to the
// original transcript — the property a consumer's parser must also satisfy.
// Deterministic for a given seed.
func Fuzz(f *wirefmt.File, turn, n int, seed int64) ([]*wirefmt.File, error) {
	idx, ok := nthHTTPIndex(f, turn)
	if !ok {
		return nil, fmt.Errorf("turn %d is out of range (recording has %d http turn(s))", turn, countHTTP(f))
	}
	it := f.Interactions[idx]
	base, unary, renderable := DecodeInteraction(it)
	if unary || !renderable {
		return nil, fmt.Errorf("turn %d is not a decodable streaming turn", turn)
	}
	p := wireenc.ProviderForURL(it.Request.URL)
	want := base.Digest()

	var out []*wirefmt.File
	for i, s := range wireenc.Reframe(p, base, seed, n) {
		got, err := wireenc.Decode(p, s)
		if err != nil {
			return nil, fmt.Errorf("reframing %d failed to decode: %w", i, err)
		}
		if got.Digest() != want {
			return nil, fmt.Errorf("reframing %d did not round-trip to the original transcript", i)
		}
		out = append(out, cloneWithResponse(f, idx, streamResponse(s)))
	}
	return out, nil
}
