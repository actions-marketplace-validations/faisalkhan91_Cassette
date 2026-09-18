package analysis

import (
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// ToolChange is one tool-call difference at a turn.
type ToolChange struct {
	Kind  string // "added" | "removed" | "args"
	Name  string
	ArgsA string // present for removed / args
	ArgsB string // present for added / args
}

// TurnDiff captures the SIGNAL difference at one turn between two recordings —
// what behavior changed, with volatile id/usage/SSE-boundary churn already
// normalized away by the semantic decode.
type TurnDiff struct {
	Turn        int
	Root        bool // the first divergent turn (the likely cause; later turns are cascade)
	TextChanged bool
	TextA       string
	TextB       string
	Tools       []ToolChange
	FinishA     string // non-empty only when finish reason flipped
	FinishB     string
	Axes        []string // request-input axes that changed at this turn
}

// RecordingDiff is the accumulated semantic diff of two recordings.
type RecordingDiff struct {
	Turns  []TurnDiff
	ExtraA int // turns present only in A
	ExtraB int // turns present only in B
}

// Diverged reports whether the recordings differ at all.
func (d RecordingDiff) Diverged() bool {
	return len(d.Turns) > 0 || d.ExtraA != 0 || d.ExtraB != 0
}

// DiffRecordings aligns two recordings positionally by HTTP turn and accumulates
// EVERY divergent turn (unlike Bisect, which stops at the first). The first
// divergence is marked Root; because each turn re-embeds prior output in
// `messages`, later turns are typically cascade effects of the root.
func DiffRecordings(a, b *wirefmt.File) RecordingDiff {
	ta, tb := httpTurns(a), httpTurns(b)
	n := len(ta)
	if len(tb) < n {
		n = len(tb)
	}
	var out RecordingDiff
	first := true
	for i := 0; i < n; i++ {
		da := decodeResponse(ta[i])
		db := decodeResponse(tb[i])
		if da.Digest() == db.Digest() {
			continue
		}
		td := TurnDiff{Turn: i}
		if first {
			td.Root = true
			first = false
		}
		if da.Text != db.Text {
			td.TextChanged = true
			td.TextA, td.TextB = da.Text, db.Text
		}
		td.Tools = diffTools(da.ToolCalls, db.ToolCalls)
		if da.FinishReason != db.FinishReason {
			td.FinishA, td.FinishB = da.FinishReason, db.FinishReason
		}
		td.Axes = AxisDiff(ta[i].Request.Body.Bytes(), tb[i].Request.Body.Bytes())
		out.Turns = append(out.Turns, td)
	}
	out.ExtraA = len(ta) - n
	out.ExtraB = len(tb) - n
	return out
}

// diffTools compares two tool-call lists, matching by name and position-within-
// name, classifying each difference as added/removed/args-changed.
func diffTools(a, b []semequal.ToolCall) []ToolChange {
	byName := map[string][2][]string{} // name -> [argsA, argsB]
	order := []string{}
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	for _, tc := range a {
		add(tc.Name)
		e := byName[tc.Name]
		e[0] = append(e[0], tc.Args)
		byName[tc.Name] = e
	}
	for _, tc := range b {
		add(tc.Name)
		e := byName[tc.Name]
		e[1] = append(e[1], tc.Args)
		byName[tc.Name] = e
	}
	var out []ToolChange
	for _, name := range order {
		la, lb := byName[name][0], byName[name][1]
		n := len(la)
		if len(lb) < n {
			n = len(lb)
		}
		for i := 0; i < n; i++ {
			if la[i] != lb[i] {
				out = append(out, ToolChange{Kind: "args", Name: name, ArgsA: la[i], ArgsB: lb[i]})
			}
		}
		for i := n; i < len(la); i++ {
			out = append(out, ToolChange{Kind: "removed", Name: name, ArgsA: la[i]})
		}
		for i := n; i < len(lb); i++ {
			out = append(out, ToolChange{Kind: "added", Name: name, ArgsB: lb[i]})
		}
	}
	return out
}
