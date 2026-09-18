package analysis

import (
	"testing"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func mergeTurn(url, reqBody string, tr semequal.Transcript) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.ProviderForURL(url), tr, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: url, Body: wirefmt.NewBody([]byte(reqBody))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
}

func TestMerge_DedupAndConflict(t *testing.T) {
	a := mergeTurn("/v1/messages", `{"m":1}`, semequal.Transcript{Text: "one", FinishReason: "end_turn"})
	bSame := mergeTurn("/v1/messages", `{"m":1}`, semequal.Transcript{Text: "one", FinishReason: "end_turn"})          // exact dup of a
	c := mergeTurn("/v1/messages", `{"m":2}`, semequal.Transcript{Text: "two", FinishReason: "end_turn"})              // distinct key
	conflict := mergeTurn("/v1/messages", `{"m":1}`, semequal.Transcript{Text: "DIFFERENT", FinishReason: "end_turn"}) // same key as a, diff response

	f1 := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{a, c}}
	f2 := &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{bSame, conflict}}

	merged, rep := Merge(f1, f2)
	if rep.Deduped != 1 {
		t.Fatalf("deduped=%d, want 1", rep.Deduped)
	}
	// a, c, conflict survive (bSame is the exact dup).
	if rep.Total != 3 || len(merged.Interactions) != 3 {
		t.Fatalf("total=%d, want 3", rep.Total)
	}
	if len(rep.Conflicts) != 1 {
		t.Fatalf("conflicts=%v, want 1", rep.Conflicts)
	}
}

func TestMerge_PreservesOrder(t *testing.T) {
	prefix := mergeTurn("/v1/messages", `{"login":1}`, semequal.Transcript{Text: "ok", FinishReason: "end_turn"})
	tail := mergeTurn("/v1/messages", `{"checkout":1}`, semequal.Transcript{Text: "done", FinishReason: "end_turn"})
	merged, _ := Merge(
		&wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{prefix}},
		&wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{tail}},
	)
	if len(merged.Interactions) != 2 {
		t.Fatalf("want 2 interactions, got %d", len(merged.Interactions))
	}
	turns := CollectTurns(merged)
	if turns[0].Text != "ok" || turns[1].Text != "done" {
		t.Fatalf("order not preserved: %+v", turns)
	}
}
