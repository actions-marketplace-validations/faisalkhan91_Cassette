package analysis

import (
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// Divergence is the result of bisecting two recordings of the same scenario.
type Divergence struct {
	Turn    int      // first turn whose response transcript differs (-1 if none)
	DigestA string   // semantic digest of that turn on side A
	DigestB string   // ... and side B
	Axes    []string // request-input axes that differ at that turn (model/system/tools/messages/sampling/length)
}

// Diverged reports whether the two recordings differ.
func (d Divergence) Diverged() bool { return d.Turn >= 0 }

// Bisect aligns two recordings of the same scenario positionally by HTTP turn,
// finds the FIRST turn whose response transcript (semantic digest) differs, and
// attributes it to the request-input axes that changed at that turn — answering
// "what change made my agent start behaving differently" rather than just "it's
// different now". Alignment is positional; because each turn re-embeds prior
// turns in `messages`, a change at turn 0 typically also flips `messages` on
// every later turn, so callers should read the FIRST divergent turn as the cause.
func Bisect(a, b *wirefmt.File) Divergence {
	ta := httpTurns(a)
	tb := httpTurns(b)
	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}
	for i := 0; i < n; i++ {
		da := decodeResponse(ta[i])
		db := decodeResponse(tb[i])
		if da.Digest() != db.Digest() {
			return Divergence{
				Turn:    i,
				DigestA: da.Digest(),
				DigestB: db.Digest(),
				Axes:    AxisDiff(ta[i].Request.Body.Bytes(), tb[i].Request.Body.Bytes()),
			}
		}
	}
	if len(ta) != len(tb) {
		return Divergence{Turn: n, Axes: []string{"length"}}
	}
	return Divergence{Turn: -1}
}

// decodeResponse decodes a turn's response for divergence comparison (includes
// the synthetic unary transcript so different non-streaming answers differ).
func decodeResponse(it *wirefmt.Interaction) semequal.Transcript {
	tr, _, _ := DecodeInteraction(it)
	return tr
}
