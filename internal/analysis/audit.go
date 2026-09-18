package analysis

import (
	"github.com/faisalkhan91/cassette/internal/egress"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// AuditBundle is a single, deterministic, offline evidence pack for a recording (or
// a whole corpus): the behavioral attestation manifest, the coverage matrix, the
// typed PII/data-class findings in outbound requests, and the cross-turn taint
// flows. It composes primitives cassette already has (attest, coverage,
// egress-audit, taint) into the one artifact a SOC2 / EU AI Act / ISO 42001 review
// asks for — generated from bytes you own, with no network and no timestamp (so it
// is byte-stable and committable; stamp the date via the filename).
type AuditBundle struct {
	Cassettes []string       `json:"cassettes"`
	Manifest  AttestManifest `json:"manifest"`
	Coverage  CoverageReport `json:"coverage"`
	Egress    []AuditEgress  `json:"egress"`
	Taint     []TaintFlow    `json:"taint"`
}

// AuditEgress is one typed sensitive-data finding in an outbound request, located by
// turn and redacted so the bundle is safe to commit.
type AuditEgress struct {
	Turn     int    `json:"turn"`
	Field    string `json:"field"` // where it was found: "url" | "header:<Name>" | "body"
	Kind     string `json:"kind"`
	Redacted string `json:"redacted"`
}

// BuildAudit composes the evidence bundle. paths and files are aligned (paths[i]
// describes files[i]); the manifest, egress scan, and taint trace run over the merged
// corpus while coverage is measured across the individual cassettes.
func BuildAudit(paths []string, files []*wirefmt.File) AuditBundle {
	merged, _ := Merge(files...)
	b := AuditBundle{
		Cassettes: paths,
		Manifest:  BuildAttestManifest(merged),
		Coverage:  Coverage(files),
		Taint:     Taint(merged),
	}
	dets := egress.Default()
	for i, it := range merged.Interactions {
		// Scan each outbound field SEPARATELY (not a \n-joined blob) so a pattern can't
		// straddle a synthetic boundary and the finding keeps its true origin.
		scan := func(field string, b2 []byte) {
			for _, fnd := range egress.Scan(b2, dets) {
				b.Egress = append(b.Egress, AuditEgress{Turn: i, Field: field, Kind: fnd.Kind, Redacted: fnd.Redacted})
			}
		}
		scan("url", []byte(it.Request.URL))
		for _, hf := range it.Request.Headers {
			for _, v := range hf.Values {
				scan("header:"+hf.Name, []byte(v))
			}
		}
		scan("body", it.Request.Body.Bytes())
	}
	// Stable JSON schema: empty findings serialize as [] (not null), so consumers can
	// rely on the array fields always being present.
	if b.Egress == nil {
		b.Egress = []AuditEgress{}
	}
	if b.Taint == nil {
		b.Taint = []TaintFlow{}
	}
	return b
}

// Exfils returns the taint flows that carry untrusted model/tool output back
// outbound — the highest-interest subset for a governance gate.
func (b AuditBundle) Exfils() []TaintFlow {
	var out []TaintFlow
	for _, t := range b.Taint {
		if t.Exfil() {
			out = append(out, t)
		}
	}
	return out
}
