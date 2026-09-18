package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Scenario(t *testing.T) {
	golden := seedFile(t, mergeStreamTurn(`{"m":1}`, "the answer is forty two"))
	out := filepath.Join(t.TempDir(), "scenarios")

	code, sout, serr := runArgs("scenario", golden, "--turn", "0", "-o", out)
	if code != exitOK {
		t.Fatalf("scenario exit=%d\n%s\n%s", code, sout, serr)
	}
	if !strings.Contains(sout, "variant(s) + corpus.yaml") {
		t.Fatalf("scenario stdout=%s", sout)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var haveCorpus, haveProviderErr bool
	for _, e := range entries {
		if e.Name() == "corpus.yaml" {
			haveCorpus = true
		}
		if strings.Contains(e.Name(), "provider-error") {
			haveProviderErr = true
		}
	}
	if !haveCorpus || !haveProviderErr {
		t.Fatalf("missing outputs: corpus=%v providerErr=%v (entries=%v)", haveCorpus, haveProviderErr, entries)
	}

	// Every generated variant must itself replay (conformance).
	for _, e := range entries {
		if e.Name() == "corpus.yaml" {
			continue
		}
		if code, _, _ := runArgs("conformance", filepath.Join(out, e.Name())); code != exitOK {
			t.Fatalf("variant %s failed conformance", e.Name())
		}
	}

	// Usage errors.
	if code, _, _ := runArgs("scenario", golden); code != exitUsage {
		t.Fatalf("scenario without -o exit=%d", code)
	}
	if code, _, _ := runArgs("scenario", golden, "--turn", "9", "-o", out); code != exitFail {
		t.Fatalf("scenario bad turn exit=%d", code)
	}
}
