// Command cassette inspects, scrubs, and verifies cassette files.
//
// Usage:
//
//	cassette inspect <cassette.yaml>   # pretty-print; exit 0 if valid, nonzero if malformed
//	cassette scrub   <cassette.yaml>   # redact secrets in place (idempotent)
//	cassette verify  <cassette.yaml>   # exit 0 iff well-formed AND no secret patterns
//
// Exit codes: 0 success; 1 failure (malformed / secrets found); 2 usage error.
package main

import (
	"fmt"
	"io"
	"os"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func main() {
	// If this binary was produced by `cassette pack`, it carries an embedded
	// recording in its own image — serve it and ignore subcommands.
	if payload, ok, err := readTrailer(); ok || err != nil {
		os.Exit(runPacked(payload, err, os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	args = stripNoColor(args)
	if len(args) < 1 {
		usage(stderr)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]

	// Top-level help: `cassette help <cmd>` shows that command's detailed help;
	// bare `cassette help` / `-h` / `--help` shows the command list.
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		if len(rest) > 0 && printCommandHelp(rest[0], stdout) {
			return exitOK
		}
		usage(stdout)
		return exitOK
	}
	// Per-command help: `cassette <cmd> --help` / `-h`.
	if wantsHelp(rest) && printCommandHelp(cmd, stdout) {
		return exitOK
	}

	if cmd == "--version" {
		cmd = "version"
	}
	c := commandByName[cmd]
	if c == nil {
		fmt.Fprintf(stderr, "cassette: unknown command %q\n", cmd)
		if sugg := suggestCommand(cmd); sugg != "" {
			fmt.Fprintf(stderr, "Did you mean \"cassette %s\"?\n", sugg)
		}
		usage(stderr)
		return exitUsage
	}
	return c.Run(rest, stdout, stderr)
}

// startHere is the curated first-run path shown above the full command list, so a
// newcomer sees the five commands that matter before the other ~50. It is a guide,
// not a group: these commands still appear in their real sections below.
var startHere = []struct{ cmds, blurb string }{
	{"up", "point your provider here: record once, then replay offline (start here)"},
	{"open", "see a recording: offline dashboard + transcript"},
	{"proxy / serve", "language-agnostic record/replay endpoints (advanced)"},
	{"verify / conformance", "prove it is secret-free (verify) and replayable (conformance)"},
}

func usage(w io.Writer) {
	st := newStyle(w)
	fmt.Fprint(w, "cassette — record/replay VCR for LLM (HTTP/SSE) and MCP tool calls\n\n")
	fmt.Fprintf(w, "%s\n", st.bold("Start here"))
	for _, s := range startHere {
		fmt.Fprintf(w, "  cassette %-21s %s\n", s.cmds, s.blurb)
	}
	fmt.Fprint(w, "\nUsage:\n")
	for _, group := range groupOrder {
		first := true
		for _, c := range commands { // registry order within each group
			if commandGroup[c.Name] != group {
				continue
			}
			if first {
				fmt.Fprintf(w, "\n%s\n", st.bold(group))
				first = false
			}
			fmt.Fprintf(w, "  cassette %-13s %s\n", c.Name, c.summary())
		}
	}
	fmt.Fprint(w, "\nRun \"cassette <command> --help\" (or \"cassette help <command>\") for details on a command.\n"+
		"Exit codes: 0 ok, 1 failure, 2 usage error.\n")
}

// suggestCommand returns the closest command name to an unknown input within a
// small edit distance, or "" if nothing is close enough (avoids wild guesses).
func suggestCommand(input string) string {
	best, bestDist := "", 1<<30
	for _, c := range commands {
		d := levenshtein(input, c.Name)
		if d < bestDist {
			best, bestDist = c.Name, d
		}
	}
	// Only suggest when the typo is plausibly that command (≤ a third of its length).
	if bestDist <= 2 || bestDist*3 <= len(best) {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// stripNoColor consumes the global --no-color flag from anywhere in args (setting
// the package-level switch ui.go honors) and returns the remaining args. Keeping
// it global means every command gets --no-color without threading a flag through.
func stripNoColor(args []string) []string {
	forceNoColor = false
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--no-color" {
			forceNoColor = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// oneArg extracts a single positional path, tolerating a leading --update flag
// (accepted for workflow consistency).
func oneArg(args []string) (string, bool) {
	var path string
	seen := false
	for _, a := range args {
		switch a {
		case "--update", "-update":
			continue
		default:
			if seen {
				return "", false
			}
			path = a
			seen = true
		}
	}
	return path, seen
}

// saveScrubbed re-scrubs f, refuses if any secret pattern survives, and writes
// atomically. Shared by the editing subcommands.
func saveScrubbed(path string, f *wirefmt.File, stderr io.Writer) int {
	scrub.DefaultConfig().File(f)
	data, err := wirefmt.Marshal(f)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: marshal: %v\n", err)
		return exitFail
	}
	if hits := scrub.SecretScan(data); len(hits) > 0 {
		fmt.Fprintf(stderr, "cassette: refusing to write — secret pattern(s) present: %v\n", hits)
		return exitFail
	}
	if err := wirefmt.Save(path, f); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}
	return exitOK
}

// parseEditArgs extracts a single positional cassette path plus one required
// value flag (--name VALUE or --name=VALUE), in any order, via the shared flag
// parser. Returns ok=false if the path or flag value is missing, an unknown flag
// is present, or there are extra positionals.
func parseEditArgs(args []string, flagName string) (path, val string, ok bool) {
	in, err := newFlags().valFlag(flagName).parse(args)
	if err != nil || in.nargs() != 1 || in.str(flagName) == "" {
		return "", "", false
	}
	return in.arg(0), in.str(flagName), true
}

// decodeTranscript decodes a streaming http interaction's response into a
// semequal.Transcript, dispatching the provider decoder by URL path.
func decodeTranscript(it *wirefmt.Interaction) (semequal.Transcript, bool) {
	tr, _, renderable := analysis.DecodeInteraction(it)
	return tr, renderable
}

// readOrErr reads a file's raw bytes, printing a CLI error and returning ok=false
// on failure — the raw-bytes sibling of loadOrErr for commands that read specs,
// keys, or price tables rather than cassettes.
func readOrErr(path string, stderr io.Writer) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, false
	}
	return data, true
}

// loadOrErr loads a cassette, printing a CLI error and returning ok=false on
// failure — the shared front door for the read-only subcommands.
func loadOrErr(path string, stderr io.Writer) (*wirefmt.File, bool) {
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, false
	}
	return f, true
}
