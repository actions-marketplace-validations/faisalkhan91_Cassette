package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// StampProvenance carries the source chain forward and appends a new link, so a
// port→graft pipeline accumulates lineage.
func TestStampProvenance_Accumulates(t *testing.T) {
	src := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	d0 := BehaviorDigest(src)

	step1 := fileOf(httpIt("/v1/chat/completions", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	StampProvenance(step1, src, "port", "→ openai-chat")
	if len(step1.Provenance) != 1 || step1.Provenance[0].Op != "port" || step1.Provenance[0].From != d0 {
		t.Fatalf("first link wrong: %+v", step1.Provenance)
	}

	step2 := fileOf(httpIt("/v1/chat/completions", `{"model":"m2"}`, `{"content":[]}`, 200, nil))
	StampProvenance(step2, step1, "graft", "turn 0")
	if len(step2.Provenance) != 2 {
		t.Fatalf("chain did not accumulate: %+v", step2.Provenance)
	}
	if step2.Provenance[0].Op != "port" || step2.Provenance[1].Op != "graft" {
		t.Fatalf("chain order wrong: %+v", step2.Provenance)
	}
	if step2.Provenance[1].From != BehaviorDigest(step1) {
		t.Fatal("second link should point at step1's behavior digest")
	}
	// Source must not be mutated by stamping a derivative.
	var _ = wirefmt.Provenance{}
	if len(src.Provenance) != 0 {
		t.Fatal("stamping a derivative mutated the source's provenance")
	}
}

// The attestation manifest binds the lineage: a signed cassette also asserts how it
// was derived, so a rewritten chain fails verification — while identical behavior +
// lineage (a benign re-derive) still verifies.
func TestAttestManifest_BindsProvenance(t *testing.T) {
	base := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	derived := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	StampProvenance(derived, base, "port", "→ openai-chat")

	m := BuildAttestManifest(derived)
	if len(m.Provenance) != 1 || m.Provenance[0].Op != "port" {
		t.Fatalf("manifest should embed lineage: %+v", m.Provenance)
	}
	// Same behavior but no lineage must NOT be semantically equal.
	if BuildAttestManifest(base).SemanticEqual(m) {
		t.Fatal("a manifest with lineage must not equal one without")
	}
	// Identical behavior + lineage (benign re-derive) verifies.
	derived2 := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	StampProvenance(derived2, base, "port", "→ openai-chat")
	if !BuildAttestManifest(derived2).SemanticEqual(m) {
		t.Fatal("identical behavior + lineage should verify")
	}
	// A rewritten lineage fails.
	derived3 := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	StampProvenance(derived3, base, "graft", "turn 0")
	if BuildAttestManifest(derived3).SemanticEqual(m) {
		t.Fatal("a rewritten lineage must fail verification")
	}
}
