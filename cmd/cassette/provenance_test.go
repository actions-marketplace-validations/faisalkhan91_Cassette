package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// port stamps a provenance link; provenance reads it back and names the source.
func TestCLI_Provenance_PortChain(t *testing.T) {
	src := writeTemp(t, validCassette)
	ported := filepath.Join(t.TempDir(), "ported.yaml")

	if code, _, errb := runArgs("port", src, "--to", "openai-chat", "-o", ported); code != exitOK {
		t.Fatalf("port: code=%d %s", code, errb)
	}
	// The derived file records its lineage.
	f, err := wirefmt.Load(ported)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Provenance) != 1 || f.Provenance[0].Op != "port" || f.Provenance[0].From == "" {
		t.Fatalf("expected one port provenance link with a source digest: %+v", f.Provenance)
	}

	code, stdout, _ := runArgs("provenance", ported)
	if code != exitOK {
		t.Fatalf("provenance: code=%d", code)
	}
	if !strings.Contains(stdout, "port") || !strings.Contains(stdout, "lineage") {
		t.Fatalf("provenance output missing the port lineage:\n%s", stdout)
	}
}

// An original recording has no lineage (but still reports a behavior digest).
func TestCLI_Provenance_Original(t *testing.T) {
	src := writeTemp(t, validCassette)
	code, stdout, _ := runArgs("provenance", src)
	if code != exitOK {
		t.Fatalf("provenance: code=%d", code)
	}
	if !strings.Contains(stdout, "behavior digest") || !strings.Contains(stdout, "no recorded lineage") {
		t.Fatalf("expected no-lineage output for an original recording:\n%s", stdout)
	}
}

// A multi-step pipeline accumulates lineage links in order (the chain carries the
// source's prior provenance forward on each derivation).
func TestCLI_Provenance_PipelineAccumulates(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, validCassette)
	step1 := filepath.Join(dir, "chat.yaml")
	step2 := filepath.Join(dir, "responses.yaml")

	if code, _, e := runArgs("port", src, "--to", "openai-chat", "-o", step1); code != exitOK {
		t.Fatalf("port 1: %d %s", code, e)
	}
	if code, _, e := runArgs("port", step1, "--to", "openai-responses", "-o", step2); code != exitOK {
		t.Fatalf("port 2: %d %s", code, e)
	}
	f, err := wirefmt.Load(step2)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Provenance) != 2 || f.Provenance[0].Op != "port" || f.Provenance[1].Op != "port" {
		t.Fatalf("expected two accumulated port links: %+v", f.Provenance)
	}
}

func TestCLI_Provenance_Usage(t *testing.T) {
	if code, _, _ := runArgs("provenance"); code != exitUsage {
		t.Fatalf("provenance no args: code=%d want exitUsage", code)
	}
}
