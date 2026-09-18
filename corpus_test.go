package cassette_test

import (
	"path/filepath"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
)

// TestCorpus_Conformance gates EVERY committed cassette under testdata/cassettes/
// (the canonical known-good corpus) on a full replay self-check: each interaction's
// key re-derives, the request resolves to its recorded response, that response
// decodes to the same semantic transcript, and replay makes zero network dials.
//
// Previously the corpus was only exercised incidentally by whichever test happened
// to load a given fixture; this sweep makes the guarantee explicit, so a fixture
// added to the corpus that does not replay cleanly fails CI instead of rotting.
func TestCorpus_Conformance(t *testing.T) {
	paths, err := filepath.Glob("testdata/cassettes/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no cassettes under testdata/cassettes/ — corpus gate would be a no-op")
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			rep, err := cassette.Conformance(p)
			if err != nil {
				t.Fatalf("conformance: %v", err)
			}
			if !rep.OK() {
				for _, turn := range rep.Turns {
					if !turn.OK() {
						t.Errorf("[%d] %s %s: key_ok=%v replay_ok=%v — %s",
							turn.Index, turn.Method, turn.URL, turn.KeyOK, turn.ReplayOK, turn.Detail)
					}
				}
				t.Fatalf("%s: %d bad interaction(s), %d dial(s)", p, rep.Bad, rep.Dials)
			}
		})
	}
}
