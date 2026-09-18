package cassette

import (
	"path/filepath"
	"testing"
)

func TestOpenAuto_RecordsThenReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.yaml")

	// First open with no file present: resolves to record.
	c1, err := Open(path, Options{Mode: ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	if c1.Mode() != ModeRecord {
		t.Fatalf("auto with missing file -> %v, want record", c1.Mode())
	}
	if err := c1.Save(); err != nil {
		t.Fatal(err)
	}

	// Second open with file present: resolves to replay.
	c2, err := Open(path, Options{Mode: ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	if c2.Mode() != ModeReplay {
		t.Fatalf("auto with existing file -> %v, want replay", c2.Mode())
	}
}

func TestModeString(t *testing.T) {
	cases := map[Mode]string{ModeReplay: "replay", ModeRecord: "record", ModeAuto: "auto"}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Fatalf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}
