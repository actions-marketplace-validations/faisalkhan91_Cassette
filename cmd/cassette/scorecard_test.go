package main

import "testing"

func TestExtractScore(t *testing.T) {
	cases := []struct {
		in    string
		want  float64
		found bool
	}{
		{"The answer is correct. Score: 87", 87, true},
		{"score=4.5 out of 5", 4.5, true},
		{"I rate this 92/100 overall", 92, true},
		{"9 out of 10 — solid", 9, true},
		{"PASS — looks good, no number here", 0, false},
	}
	for _, c := range cases {
		got, ok := extractScore(c.in)
		if ok != c.found || (ok && got != c.want) {
			t.Errorf("extractScore(%q)=(%g,%v) want (%g,%v)", c.in, got, ok, c.want, c.found)
		}
	}
}

func TestJudgePasses(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	// Scored gate.
	if !judgePasses("", f(80), judgeVerdict{Score: 87, HasScore: true}) {
		t.Error("87 >= 80 should pass")
	}
	if judgePasses("", f(90), judgeVerdict{Score: 87, HasScore: true}) {
		t.Error("87 < 90 should fail")
	}
	if judgePasses("", f(80), judgeVerdict{HasScore: false}) {
		t.Error("no score should fail a scored gate")
	}
	// Marker gate (no MinScore) is unchanged.
	if !judgePasses("PASS", nil, judgeVerdict{Text: "verdict: PASS"}) {
		t.Error("marker PASS should pass")
	}
	if judgePasses("PASS", nil, judgeVerdict{Text: "verdict: FAIL"}) {
		t.Error("marker absent should fail")
	}
}
