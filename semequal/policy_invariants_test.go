package semequal

import (
	"errors"
	"strings"
	"testing"
)

func TestPolicy_ExactByDefault(t *testing.T) {
	a := Transcript{Text: "hello", FinishReason: "stop", ToolCalls: []ToolCall{{Name: "t", Args: `{"a":1}`}}}
	if rep := (Policy{}).Compare(a, a); !rep.OK {
		t.Fatalf("identical should pass: %v", rep.Fields)
	}
	b := a
	b.Text = "HELLO"
	if rep := (Policy{}).Compare(a, b); rep.OK || rep.Fields[0] != "text" {
		t.Fatalf("text mismatch should flag text: %+v", rep)
	}
}

func TestPolicy_TextBudget(t *testing.T) {
	// A trivial char-diff-ratio distance.
	dist := func(x, y string) float64 {
		if x == y {
			return 0
		}
		d := len(x) - len(y)
		if d < 0 {
			d = -d
		}
		return float64(d) / float64(len(x)+1)
	}
	p := Policy{TextDistance: dist, TextBudget: 0.3}
	want := Transcript{Text: "the answer is 42", FinishReason: "stop"}
	got := Transcript{Text: "the answer is 42!", FinishReason: "stop"} // tiny drift
	if rep := p.Compare(want, got); !rep.OK {
		t.Fatalf("small drift within budget should pass: %+v", rep)
	}
	// But a vanished tool call still fails even with prose tolerance.
	want.ToolCalls = []ToolCall{{Name: "pay", Args: `{}`}}
	if rep := p.Compare(want, got); rep.OK {
		t.Fatal("a missing tool call must fail regardless of text budget")
	}
}

func TestPolicy_IgnoreFinishReason(t *testing.T) {
	a := Transcript{Text: "x", FinishReason: "stop"}
	b := Transcript{Text: "x", FinishReason: "length"}
	if (Policy{}).Compare(a, b).OK {
		t.Fatal("finish reason should matter by default")
	}
	if !(Policy{IgnoreFinishReason: true}).Compare(a, b).OK {
		t.Fatal("IgnoreFinishReason should drop the requirement")
	}
}

func conv() []Transcript {
	return []Transcript{
		{FinishReason: "tool_use", ToolCalls: []ToolCall{{Name: "get_price", Args: `{"x":1}`}}},
		{FinishReason: "tool_use", ToolCalls: []ToolCall{{Name: "check_inventory", Args: `{}`}}},
		{Text: "Here you go.", FinishReason: "end_turn"},
	}
}

func TestInvariants_Pass(t *testing.T) {
	errs := Check(conv(),
		NoToolCalled("delete_account"),
		NoDuplicateToolCall(),
		FinishesCleanly(),
		ToolCalledBefore("get_price", "check_inventory"),
	)
	if len(errs) != 0 {
		t.Fatalf("expected no violations, got %v", errs)
	}
}

func TestInvariants_Fail(t *testing.T) {
	if NoToolCalled("get_price")(conv()) == nil {
		t.Fatal("NoToolCalled should fire")
	}
	if ToolCalledBefore("check_inventory", "get_price")(conv()) == nil {
		t.Fatal("wrong ordering should fire")
	}
	// dangling tool call at the end
	dangling := []Transcript{{FinishReason: "tool_use", ToolCalls: []ToolCall{{Name: "t"}}}}
	if FinishesCleanly()(dangling) == nil {
		t.Fatal("dangling final tool call should fail FinishesCleanly")
	}
	// duplicate
	dup := []Transcript{
		{ToolCalls: []ToolCall{{Name: "t", Args: "{}"}}},
		{ToolCalls: []ToolCall{{Name: "t", Args: "{}"}}, FinishReason: "end_turn"},
	}
	if NoDuplicateToolCall()(dup) == nil {
		t.Fatal("duplicate tool call should fire")
	}
}

func TestInvariants_AllToolArgsValid(t *testing.T) {
	mustBeObject := func(_, args string) error {
		if !strings.HasPrefix(args, "{") {
			return errors.New("not a JSON object")
		}
		return nil
	}
	if AllToolArgsValid(mustBeObject)(conv()) != nil {
		t.Fatal("valid args should pass")
	}
	bad := []Transcript{{ToolCalls: []ToolCall{{Name: "t", Args: "not json"}}}}
	if AllToolArgsValid(mustBeObject)(bad) == nil {
		t.Fatal("invalid args should fail")
	}
}

func TestToolNames(t *testing.T) {
	got := ToolNames(conv())
	if len(got) != 2 || got[0] != "check_inventory" || got[1] != "get_price" {
		t.Fatalf("ToolNames = %v", got)
	}
}
