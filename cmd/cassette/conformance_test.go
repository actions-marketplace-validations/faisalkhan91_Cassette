package main

import (
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/semequal"
)

func TestCLI_Conformance(t *testing.T) {
	// A keyless (hand-written) streaming cassette replays cleanly: the index
	// recomputes the key from the stored body.
	good := seedFile(t, codecStreamTurn("/v1/messages",
		semequal.Transcript{Text: "hi", FinishReason: "end_turn"}))

	code, out, _ := runArgs("conformance", good)
	if code != exitOK {
		t.Fatalf("conformance exit=%d\n%s", code, out)
	}
	for _, want := range []string{"POST /v1/messages", "replay cleanly", "0 dial"} {
		if !strings.Contains(out, want) {
			t.Fatalf("conformance output missing %q:\n%s", want, out)
		}
	}
	if _, jout, _ := runArgs("conformance", good, "--json"); !strings.Contains(jout, `"replay_ok":true`) {
		t.Fatalf("conformance --json missing replay_ok:\n%s", jout)
	}

	// A stored cassette carrying a stale (wrong) match key fails conformance.
	stale := codecStreamTurn("/v1/messages", semequal.Transcript{Text: "hi", FinishReason: "end_turn"})
	stale.Request.MatchKey = "POST\n/v1/messages\njson:deadbeef"
	bad := seedFile(t, stale)
	if code, out, _ := runArgs("conformance", bad); code != exitFail {
		t.Fatalf("conformance stale-key exit=%d (want %d)\n%s", code, exitFail, out)
	}

	// Usage error.
	if code, _, _ := runArgs("conformance"); code != exitUsage {
		t.Fatalf("conformance no-arg exit=%d", code)
	}
}
