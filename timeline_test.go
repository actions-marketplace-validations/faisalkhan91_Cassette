package cassette_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/semequal"
)

func TestTimeline_AppendDedupAndChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.yaml")
	run1 := []semequal.Transcript{{Text: "hi", FinishReason: "end_turn"}}

	added, err := cassette.AppendTimeline(path, "greet", run1)
	if err != nil || !added {
		t.Fatalf("first append should add: added=%v err=%v", added, err)
	}
	before, _ := os.ReadFile(path)

	// Same behavior again → no-op, byte-identical ledger.
	added, _ = cassette.AppendTimeline(path, "greet", run1)
	if added {
		t.Fatal("unchanged behavior must not append")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("ledger must be byte-stable on a no-op")
	}

	// Changed behavior → exactly one new entry.
	run2 := []semequal.Transcript{{Text: "hello there", FinishReason: "end_turn"}}
	added, _ = cassette.AppendTimeline(path, "greet", run2)
	if !added {
		t.Fatal("changed behavior should append")
	}
	tl, _ := cassette.LoadTimeline(path)
	if len(tl.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(tl.Entries))
	}
	if tl.Entries[0].Combined == tl.Entries[1].Combined {
		t.Fatal("the two entries should differ")
	}
}
