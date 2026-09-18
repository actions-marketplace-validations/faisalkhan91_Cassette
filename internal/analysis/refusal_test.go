package analysis_test

import (
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestClassifyRefusal(t *testing.T) {
	cases := []struct {
		tr   semequal.Transcript
		want analysis.RefusalVerdict
	}{
		{semequal.Transcript{Text: "I can't help with that."}, analysis.Refused},
		{semequal.Transcript{Text: "I'm unable to assist."}, analysis.Refused},
		{semequal.Transcript{Text: "Sure, here is the answer."}, analysis.Complied},
		{semequal.Transcript{ToolCalls: []semequal.ToolCall{{Name: "do"}}}, analysis.Complied},
		{semequal.Transcript{Text: ""}, analysis.Unknown},
	}
	for _, c := range cases {
		if got := analysis.ClassifyRefusal(c.tr); got != c.want {
			t.Errorf("ClassifyRefusal(%q) = %s, want %s", c.tr.Text, got, c.want)
		}
	}
}

func TestFinalVerdict(t *testing.T) {
	if analysis.FinalVerdict(nil) != analysis.Unknown {
		t.Fatal("empty → unknown")
	}
	turns := []semequal.Transcript{
		{Text: "working on it"},
		{Text: "I cannot comply."},
	}
	if analysis.FinalVerdict(turns) != analysis.Refused {
		t.Fatal("final turn refusal should win")
	}
}
