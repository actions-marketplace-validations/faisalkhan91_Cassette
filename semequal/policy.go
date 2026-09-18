package semequal

import (
	"fmt"
	"sort"
	"testing"
)

// Policy is a per-field tolerance for comparing two transcripts, replacing the
// all-or-nothing digest equality with a declaration of which dimensions of a
// model turn are load-bearing. Tool calls (names + canonical args) and the
// finish reason must match exactly by default; prose Text may drift within a
// budget when a distance function is supplied.
type Policy struct {
	// TextDistance, if set, returns a distance in [0,1] between two texts. Text
	// passes when the distance is <= TextBudget. Default nil = exact match.
	TextDistance func(a, b string) float64
	TextBudget   float64
	// IgnoreFinishReason drops the finish-reason equality requirement.
	IgnoreFinishReason bool
	// IgnoreText drops the text requirement entirely (only tool calls matter).
	IgnoreText bool
}

// DriftReport is the structured result of a Policy comparison.
type DriftReport struct {
	OK       bool
	Fields   []string // offending field names
	TextDist float64  // computed text distance (when TextDistance is set)
}

func (r DriftReport) Error() string {
	if r.OK {
		return ""
	}
	return fmt.Sprintf("transcript drift on fields %v (text distance %.3f)", r.Fields, r.TextDist)
}

// Compare evaluates got against want under the policy.
func (p Policy) Compare(want, got Transcript) DriftReport {
	want, got = want.Normalize(), got.Normalize()
	var fields []string
	rep := DriftReport{}

	// Tool calls: exact (name + canonical args), order-insensitive (Normalize sorts).
	if !sameToolCalls(want.ToolCalls, got.ToolCalls) {
		fields = append(fields, "tool_calls")
	}
	if !p.IgnoreFinishReason && want.FinishReason != got.FinishReason {
		fields = append(fields, "finish_reason")
	}
	if !p.IgnoreText {
		if p.TextDistance != nil {
			rep.TextDist = p.TextDistance(want.Text, got.Text)
			if rep.TextDist > p.TextBudget {
				fields = append(fields, "text")
			}
		} else if want.Text != got.Text {
			fields = append(fields, "text")
		}
	}
	sort.Strings(fields)
	rep.Fields = fields
	rep.OK = len(fields) == 0
	return rep
}

func sameToolCalls(a, b []ToolCall) bool {
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

// AssertEqualUnder fails the test if got does not satisfy the policy vs want.
func AssertEqualUnder(t testing.TB, want, got Transcript, p Policy) {
	t.Helper()
	if rep := p.Compare(want, got); !rep.OK {
		t.Fatalf("%s\n--- want ---\n%s\n--- got ---\n%s", rep.Error(), want, got)
	}
}
