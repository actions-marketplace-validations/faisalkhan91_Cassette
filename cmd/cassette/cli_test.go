package main

import (
	"bytes"
	"testing"
)

func TestFlagSet_Parse(t *testing.T) {
	fs := newFlags().boolFlag("strict", "json").valFlag("addr", "out", "no-tool").alias("o", "out")
	in, err := fs.parse([]string{"file.yaml", "--addr", ":8080", "--strict", "-o", "x", "--no-tool=a", "--no-tool", "b", "pos2"})
	if err != nil {
		t.Fatal(err)
	}
	if in.arg(0) != "file.yaml" || in.arg(1) != "pos2" || in.nargs() != 2 {
		t.Fatalf("positionals: %v", in.args())
	}
	if in.str("addr") != ":8080" || in.str("out") != "x" || !in.boolv("strict") || in.boolv("json") {
		t.Fatalf("flags: addr=%q out=%q strict=%v json=%v", in.str("addr"), in.str("out"), in.boolv("strict"), in.boolv("json"))
	}
	if got := in.list("no-tool"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("repeatable: %v", got)
	}
}

func TestFlagSet_Errors(t *testing.T) {
	fs := newFlags().boolFlag("strict").valFlag("addr")
	for _, args := range [][]string{
		{"--bogus"},    // unknown long
		{"-z"},         // unknown short
		{"--addr"},     // missing value
		{"--strict=1"}, // bool with value
	} {
		if _, err := fs.parse(args); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
	// "--" stops flag parsing.
	in, err := fs.parse([]string{"--", "--addr", "x"})
	if err != nil || in.nargs() != 2 {
		t.Fatalf("-- terminator: %v %v", in, err)
	}
}

func TestParseOrUsage(t *testing.T) {
	fs := newFlags().boolFlag("x")
	var errb bytes.Buffer
	if _, ok := parseOrUsage(fs, []string{"--nope"}, "usage: cassette demo", &errb); ok {
		t.Fatal("expected failure")
	}
	if !bytes.Contains(errb.Bytes(), []byte("usage: cassette demo")) {
		t.Fatalf("usage not printed: %s", errb.String())
	}
}
