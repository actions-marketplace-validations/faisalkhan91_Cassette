package analysis

import (
	"github.com/faisalkhan91/cassette/internal/egress"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// TaintFlow is one sensitive value that crosses turns: it originated in an earlier
// turn (as user input or as model/tool output) and is carried OUTBOUND in a later
// request. egress-audit finds typed PII per request; taint follows the FLOW.
type TaintFlow struct {
	Kind          string `json:"kind"`           // data class (email, creditcard, ...)
	Redacted      string `json:"redacted"`       // masked sample, safe to print/commit
	SourceTurn    int    `json:"source_turn"`    // where the value first appeared
	SourceKind    string `json:"source_kind"`    // "user-input" (a request) | "model-output" (a response/tool result)
	SinkTurn      int    `json:"sink_turn"`      // latest outbound request still carrying it
	OutboundTurns int    `json:"outbound_turns"` // number of outbound requests carrying it
}

// Exfil reports whether the source is untrusted model/tool output that is later
// sent outbound — the highest-interest case (potential exfiltration / injection
// relay), versus a user-input value merely echoed through conversation history.
func (t TaintFlow) Exfil() bool { return t.SourceKind == "model-output" }

// Taint traces sensitive values across the ordered conversation, returning the
// flows where a value first seen in an earlier turn is carried outbound in a later
// request. Detection uses the egress detector bank; correlation uses the
// detectors' deterministic masked form, so raw secrets never leave the function.
// Pure and offline.
func Taint(f *wirefmt.File) []TaintFlow {
	dets := egress.Default()
	type rec struct {
		kind, redacted     string
		sourceTurn         int
		sourceKind         string
		sinkTurn, outbound int
	}
	m := map[string]*rec{}
	var order []string
	// Correlate by the non-reversible fingerprint of the raw value (not the lossy
	// mask, which can collide distinct secrets sharing first/last chars).
	id := func(fn egress.Finding) string { return fn.Kind + "\x1f" + fn.Fingerprint }
	// scan returns one Finding per distinct token in a body, in the detector bank's
	// stable scan order — iterate this (NOT a map) so `order`, and thus the output,
	// is deterministic when a body carries two or more new tokens.
	scan := func(b []byte) []egress.Finding {
		var out []egress.Finding
		seen := map[string]bool{}
		for _, fn := range egress.Scan(b, dets) {
			k := id(fn)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, fn)
		}
		return out
	}

	turn := -1
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		turn++
		// Outbound request: a sink. A token already sourced in an earlier turn is a
		// cross-turn flow; an unseen token originates here as user input.
		for _, fn := range scan(it.Request.Body.Bytes()) {
			k := id(fn)
			r := m[k]
			if r == nil {
				m[k] = &rec{kind: fn.Kind, redacted: fn.Redacted, sourceTurn: turn, sourceKind: "user-input", sinkTurn: turn, outbound: 1}
				order = append(order, k)
				continue
			}
			if r.sourceTurn < turn {
				if turn > r.sinkTurn {
					r.sinkTurn = turn
				}
				r.outbound++
			}
		}
		// Response: a potential SOURCE of untrusted (model/tool) content, not a sink.
		for _, fn := range scan(it.Response.Body.Bytes()) {
			k := id(fn)
			if m[k] == nil {
				m[k] = &rec{kind: fn.Kind, redacted: fn.Redacted, sourceTurn: turn, sourceKind: "model-output", sinkTurn: -1}
				order = append(order, k)
			}
		}
	}

	var out []TaintFlow
	for _, k := range order {
		r := m[k]
		if r.sinkTurn > r.sourceTurn {
			out = append(out, TaintFlow{
				Kind: r.kind, Redacted: r.redacted,
				SourceTurn: r.sourceTurn, SourceKind: r.sourceKind,
				SinkTurn: r.sinkTurn, OutboundTurns: r.outbound,
			})
		}
	}
	return out
}
