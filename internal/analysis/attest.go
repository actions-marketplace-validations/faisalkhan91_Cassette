package analysis

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// AttestManifest binds a recording's BEHAVIOR (not its bytes) into a signable
// manifest: the combined + per-turn semantic digests, the wire-grammar shapes,
// the tool set, any embedded contract, and the derivation lineage (provenance).
// Two recordings with identical behavior but different volatile bytes produce the
// same semantic manifest, so a benign re-record verifies while a behavior change —
// or a rewritten lineage — fails. The provenance chain keys off behavioral digests,
// so it too is stable across benign re-records. CassetteSHA is the raw byte hash —
// advisory only (it changes on benign re-record).
type AttestManifest struct {
	CassetteSHA string               `json:"cassette_sha"`
	Combined    string               `json:"combined_digest"`
	TurnDigests []string             `json:"turn_digests"`
	WireShapes  []string             `json:"wire_shapes"`
	Tools       []string             `json:"tools"`
	Expect      *wirefmt.Expect      `json:"expect,omitempty"`
	Provenance  []wirefmt.Provenance `json:"provenance,omitempty"`
}

// BuildAttestManifest computes the manifest from a recording.
func BuildAttestManifest(f *wirefmt.File) AttestManifest {
	turns := CollectTurns(f)
	m := AttestManifest{
		Combined:   semequal.CombinedDigest(turns),
		Tools:      semequal.ToolNames(turns),
		Expect:     f.Expect,
		Provenance: f.Provenance,
	}
	for _, tr := range turns {
		m.TurnDigests = append(m.TurnDigests, tr.Digest())
	}
	for _, it := range f.Interactions {
		if it.Kind == "http" && it.Response.Streaming && it.Response.Body != nil {
			m.WireShapes = append(m.WireShapes, semequal.WireShape(it.Response.Body.Bytes()))
		}
	}
	if raw, err := wirefmt.Marshal(f); err == nil {
		sum := sha256.Sum256(raw)
		m.CassetteSHA = hex.EncodeToString(sum[:])
	}
	return m
}

// SemanticEqual reports whether two manifests describe the same BEHAVIOR and
// lineage, ignoring the advisory raw-byte hash. Provenance is compared so a signed
// attestation also binds the recording's declared derivation chain (a rewritten
// lineage fails verification); benign re-records keep it stable because the chain
// keys off behavioral digests.
func (m AttestManifest) SemanticEqual(o AttestManifest) bool {
	if m.Combined != o.Combined || !eqStrings(m.TurnDigests, o.TurnDigests) ||
		!eqStrings(m.WireShapes, o.WireShapes) || !eqStrings(m.Tools, o.Tools) {
		return false
	}
	return eqExpect(m.Expect, o.Expect) && eqProvenance(m.Provenance, o.Provenance)
}

func eqProvenance(a, b []wirefmt.Provenance) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqExpect(a, b *wirefmt.Expect) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.NoDuplicateTools == b.NoDuplicateTools && a.FinishesClean == b.FinishesClean && eqStrings(a.NoTools, b.NoTools)
}
