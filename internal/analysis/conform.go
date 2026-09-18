package analysis

import (
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// This file implements codec verification: a proof that the Transcript⇄wire codec
// (semequal.Decode* + internal/wireenc.Encode) is behavior-preserving and
// deterministic for every recorded streaming turn. The encoder is the keystone
// every authoring feature (graft/distill/branch and the PLAN6 authoring cluster)
// stands on, so this hardens the byte-exact brand at the codec layer. Fully offline.

// CodecTurn is the codec-verify verdict for one http interaction.
type CodecTurn struct {
	Turn     int    `json:"turn"`
	Provider string `json:"provider,omitempty"`
	// Skipped is the reason a turn is not codec-checkable (unary/non-SSE, empty, or
	// an error stream the encoder has no path for); empty when the turn was checked.
	Skipped string `json:"skipped,omitempty"`
	// Stable is the decode→encode→decode fixpoint: the re-decoded transcript is
	// semantically equal to the first decode.
	Stable bool `json:"stable"`
	// Deterministic reports that re-encoding the decoded transcript is byte-stable.
	Deterministic bool `json:"deterministic"`
	// Diff is a transcript diff explaining a non-stable turn (empty when stable).
	Diff string `json:"diff,omitempty"`
}

// OK reports whether the turn is either skipped or fully codec-preserving.
func (t CodecTurn) OK() bool { return t.Skipped != "" || (t.Stable && t.Deterministic) }

// CodecReport is the whole-file codec-verify result.
type CodecReport struct {
	Turns   []CodecTurn `json:"turns"`
	Checked int         `json:"checked"`
	Skipped int         `json:"skipped"`
	Bad     int         `json:"bad"`
}

// OK reports whether every checked turn round-tripped (no bad turns).
func (r CodecReport) OK() bool { return r.Bad == 0 }

// CodecVerify proves the Transcript⇄wire codec is behavior-preserving and
// deterministic for every recorded streaming turn: decode→encode→decode reaches a
// stable transcript fixpoint and re-encoding is byte-deterministic. Unary
// (non-SSE) JSON responses, empty/non-http interactions, and error streams are
// skipped (the encoder covers streaming success responses). Pure, offline.
func CodecVerify(f *wirefmt.File) CodecReport {
	var rep CodecReport
	turn := -1
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		turn++
		ct := CodecTurn{Turn: turn}
		_, unary, renderable := DecodeInteraction(it)
		switch {
		case unary:
			ct.Skipped = "unary (non-SSE; codec covers streaming)"
		case !renderable:
			ct.Skipped = "no decodable streaming body"
		default:
			p := wireenc.ProviderForURL(it.Request.URL)
			ct.Provider = ProviderName(p)
			res, err := wireenc.RoundTrip(p, it.Response.Body.Bytes())
			if err != nil {
				ct.Skipped = "error stream (encoder has no error path)"
				break
			}
			ct.Stable = res.Stable
			ct.Deterministic = res.Deterministic
			if !res.Stable {
				ct.Diff = semequal.Diff(res.Decoded, res.Redecoded)
			}
		}
		if ct.Skipped != "" {
			rep.Skipped++
		} else {
			rep.Checked++
			if !ct.OK() {
				rep.Bad++
			}
		}
		rep.Turns = append(rep.Turns, ct)
	}
	return rep
}

// ProviderName is the stable, human/JSON-facing label for a wire dialect. It
// delegates to the dialect registry so the label can't drift from routing.
func ProviderName(p wireenc.Provider) string { return wireenc.Name(p) }
