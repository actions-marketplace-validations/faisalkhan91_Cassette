package analysis

import (
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// BehaviorDigest is the combined semantic digest of a recording — the same value
// the attestation manifest binds. It is stable across benign re-records (volatile
// bytes ignored), so it is the right identity for a provenance link.
func BehaviorDigest(f *wirefmt.File) string {
	return semequal.CombinedDigest(CollectTurns(f))
}

// StampProvenance records that out was derived from source via op. It carries
// source's existing lineage forward and appends a new link pointing at source's
// behavior digest, so the chain accumulates across a port→graft→distill pipeline.
// Deterministic and secret-free; call it on the OUTPUT file before saving.
func StampProvenance(out, source *wirefmt.File, op, note string) {
	chain := make([]wirefmt.Provenance, 0, len(source.Provenance)+1)
	chain = append(chain, source.Provenance...)
	out.Provenance = chain
	AppendProvenance(out, op, BehaviorDigest(source), note)
}

// AppendProvenance appends a lineage link with an explicit source digest. Use it for
// an IN-PLACE transform (e.g. graft) where the source digest must be captured BEFORE
// the file is mutated; the file's existing chain is preserved.
func AppendProvenance(out *wirefmt.File, op, from, note string) {
	out.Provenance = append(out.Provenance, wirefmt.Provenance{Op: op, From: from, Note: note})
}
