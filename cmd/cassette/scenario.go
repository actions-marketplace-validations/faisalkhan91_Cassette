package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdScenario derives a matrix of valid edge/error cassettes from one golden turn
// (each finish reason, truncated/empty text, malformed-but-legal tool args, an
// injected provider error) and writes them — plus a self-checking corpus.yaml of
// expected digests — into an output directory. One golden run → a behavior matrix,
// offline.
func cmdScenario(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("turn", "out").alias("o", "out"), args, scenarioUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	out := in.str("out")
	if path == "" || out == "" || in.nargs() > 1 {
		return scenarioUsage(stderr)
	}
	turn := 0
	if in.has("turn") {
		n, err := strconv.Atoi(in.str("turn"))
		if err != nil || n < 0 {
			fmt.Fprintf(stderr, "cassette: invalid --turn %q\n", in.str("turn"))
			return exitUsage
		}
		turn = n
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}
	variants, err := analysis.Scenario(f, turn)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	type entry struct {
		Name   string `yaml:"name"`
		File   string `yaml:"file"`
		Digest string `yaml:"digest"`
	}
	corpus := struct {
		Base     string  `yaml:"base"`
		Turn     int     `yaml:"turn"`
		Variants []entry `yaml:"variants"`
	}{Base: filepath.Base(path), Turn: turn}

	for _, v := range variants {
		name := stem + "." + v.Name + ".yaml"
		dst := filepath.Join(out, name)
		// Variants only swap one response; secret-scan + write through the scrubber.
		if code := saveScrubbed(dst, v.File, stderr); code != exitOK {
			return code
		}
		corpus.Variants = append(corpus.Variants, entry{Name: v.Name, File: name, Digest: v.Digest})
	}
	data, err := yaml.Marshal(corpus)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if err := os.WriteFile(filepath.Join(out, "corpus.yaml"), data, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s wrote %d scenario variant(s) + corpus.yaml → %s\n", st.check(), len(variants), out)
	return exitOK
}

const scenarioUsageText = "usage: cassette scenario <golden.yaml> [--turn N] -o <dir>"

func scenarioUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, scenarioUsageText)
	return exitUsage
}
