package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	cassette "github.com/faisalkhan91/cassette"
	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdAuthor compiles a terse screenplay (YAML) into a fully valid, replayable
// cassette via the wireenc encoder — no API key, no network, deterministic bytes.
// It verifies the result replays (conformance, zero dials) before writing, and
// never writes secrets.
func cmdAuthor(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("out").alias("o", "out"), args, authorUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	out := in.str("out")
	if path == "" || out == "" || in.nargs() > 1 {
		return authorUsage(stderr)
	}
	raw, ok := readOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	var sp analysis.Screenplay
	if err := yaml.Unmarshal(raw, &sp); err != nil {
		fmt.Fprintf(stderr, "cassette: parse screenplay: %v\n", err)
		return exitFail
	}
	f, err := analysis.Compile(sp)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	// Write to a temp file beside the target, prove it replays, then move it into
	// place — so a cassette that fails conformance never lands on disk.
	dir := filepath.Dir(out)
	tmpf, err := os.CreateTemp(dir, ".author-*.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	tmp := tmpf.Name()
	tmpf.Close()
	defer os.Remove(tmp)

	if code := saveScrubbed(tmp, f, stderr); code != exitOK {
		return code
	}
	rep, err := cassette.Conformance(tmp)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: verify authored cassette: %v\n", err)
		return exitFail
	}
	if !rep.OK() {
		fmt.Fprintf(stderr, "cassette: authored cassette failed conformance (%d interaction(s), %d dial(s)) — this is a bug, please report\n", rep.Bad, rep.Dials)
		return exitFail
	}
	if err := os.Rename(tmp, out); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}

	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s authored %d turn(s) → %s (replays offline, 0 dials)\n", st.check(), len(f.Interactions), out)
	return exitOK
}

const authorUsageText = "usage: cassette author <screenplay.yaml> -o <cassette.yaml>"

func authorUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, authorUsageText)
	return exitUsage
}
