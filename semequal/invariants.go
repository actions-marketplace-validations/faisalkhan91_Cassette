package semequal

import (
	"fmt"
	"sort"
	"testing"
)

// Invariant is a behavioral assertion over an ordered multi-turn conversation
// (the assistant turns, in encounter order). It returns a non-nil error naming
// the violation, or nil if the conversation satisfies it. Invariants turn a
// cassette from a byte oracle into a behavioral spec checker.
//
// Note: invariants read the conversation in TURN order (which is encounter
// order). Within a single turn, tool calls are compared as a set.
type Invariant func(turns []Transcript) error

// Check runs every invariant and returns all violations.
func Check(turns []Transcript, invs ...Invariant) []error {
	var errs []error
	for _, inv := range invs {
		if inv == nil {
			continue
		}
		if err := inv(turns); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// AssertInvariants fails the test on any violation.
func AssertInvariants(t testing.TB, turns []Transcript, invs ...Invariant) {
	t.Helper()
	for _, err := range Check(turns, invs...) {
		t.Errorf("invariant violated: %v", err)
	}
}

// NoToolCalled asserts a tool with the given name is never called.
func NoToolCalled(name string) Invariant {
	return func(turns []Transcript) error {
		for i, tr := range turns {
			for _, tc := range tr.ToolCalls {
				if tc.Name == name {
					return fmt.Errorf("forbidden tool %q called in turn %d", name, i)
				}
			}
		}
		return nil
	}
}

// NoDuplicateToolCall asserts no (name, args) pair is called more than once.
func NoDuplicateToolCall() Invariant {
	return func(turns []Transcript) error {
		seen := map[string]int{}
		for i, tr := range turns {
			for _, tc := range tr.ToolCalls {
				k := tc.Name + "\x00" + tc.Args
				if prev, ok := seen[k]; ok {
					return fmt.Errorf("duplicate tool call %s(%s) in turns %d and %d", tc.Name, tc.Args, prev, i)
				}
				seen[k] = i
			}
		}
		return nil
	}
}

// FinishesCleanly asserts the final turn ends with a terminal finish reason and
// no dangling tool call (i.e. the agent didn't stop mid-tool-use).
func FinishesCleanly() Invariant {
	terminal := map[string]bool{"end_turn": true, "stop": true, "completed": true, "stop_sequence": true, "": false}
	return func(turns []Transcript) error {
		if len(turns) == 0 {
			return fmt.Errorf("no turns")
		}
		last := turns[len(turns)-1]
		if !terminal[last.FinishReason] {
			return fmt.Errorf("final turn finished with %q, not a terminal reason", last.FinishReason)
		}
		if len(last.ToolCalls) > 0 {
			return fmt.Errorf("final turn has %d dangling tool call(s)", len(last.ToolCalls))
		}
		return nil
	}
}

// ToolCalledBefore asserts the first turn that calls tool a is no later than the
// first turn that calls tool b (cross-turn ordering).
func ToolCalledBefore(a, b string) Invariant {
	return func(turns []Transcript) error {
		first := func(name string) int {
			for i, tr := range turns {
				for _, tc := range tr.ToolCalls {
					if tc.Name == name {
						return i
					}
				}
			}
			return -1
		}
		ia, ib := first(a), first(b)
		if ia < 0 {
			return fmt.Errorf("tool %q was never called", a)
		}
		if ib >= 0 && ia > ib {
			return fmt.Errorf("tool %q (turn %d) must be called before %q (turn %d)", a, ia, b, ib)
		}
		return nil
	}
}

// AllToolArgsValid asserts every tool call's canonical arguments satisfy validate
// (e.g. a JSON-schema check supplied by the caller — schema validation that needs
// provider SDK types lives in the adapter layer, not here).
func AllToolArgsValid(validate func(name, args string) error) Invariant {
	return func(turns []Transcript) error {
		for i, tr := range turns {
			for _, tc := range tr.ToolCalls {
				if err := validate(tc.Name, tc.Args); err != nil {
					return fmt.Errorf("turn %d tool %q args invalid: %w", i, tc.Name, err)
				}
			}
		}
		return nil
	}
}

// ToolNames returns the distinct tool names called across the conversation
// (sorted) — a convenience for tests and docs.
func ToolNames(turns []Transcript) []string {
	set := map[string]bool{}
	for _, tr := range turns {
		for _, tc := range tr.ToolCalls {
			set[tc.Name] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
