package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// cmdInit scaffolds an offline-replay setup in a project: a cassettes directory, a
// .gitignore entry for the CI miss-manifest, and a printed copy-paste snippet for
// pointing the project's provider base URL at `cassette proxy`. It is idempotent —
// safe to re-run — and writes nothing but the directory and the gitignore line.
func cmdInit(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("stack"), args, initUsageText, stderr)
	if !ok {
		return exitUsage
	}
	root := in.arg(0)
	if root == "" {
		root = "."
	}
	if in.nargs() > 1 {
		return initUsage(stderr)
	}

	cassettes := filepath.Join(root, "testdata", "cassettes")
	if err := os.MkdirAll(cassettes, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	added, err := ensureGitignore(filepath.Join(root, ".gitignore"), "cassette-misses.json")
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s scaffolded offline replay in %s\n", st.check(), root)
	fmt.Fprintf(stdout, "  %s %s\n", st.dim("dir   "), cassettes)
	if added {
		fmt.Fprintf(stdout, "  %s .gitignore += cassette-misses.json\n", st.dim("ignore"))
	} else {
		fmt.Fprintf(stdout, "  %s .gitignore already ignores cassette-misses.json\n", st.dim("ignore"))
	}

	stack := in.str("stack")
	if stack == "" {
		stack = detectStack(root)
	}
	fmt.Fprintln(stdout, "\nNext: record once, then replay offline. Point your provider base URL at the proxy:")
	fmt.Fprint(stdout, initSnippet(stack))
	return exitOK
}

// ensureGitignore appends line to the gitignore at path (creating it if absent),
// unless it is already present. Returns whether it added the line.
func ensureGitignore(path, line string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, l := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(l) == line {
			return false, nil
		}
	}
	body := string(existing)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += line + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// detectStack guesses the project's language from marker files; "" if unknown.
func detectStack(root string) string {
	// Ordered (not a map) so a polyglot repo resolves to a stable, prioritized stack.
	for _, m := range []struct{ marker, stack string }{
		{"promptfooconfig.yaml", "promptfoo"},
		{"promptfooconfig.js", "promptfoo"},
		{"go.mod", "go"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"package.json", "node"},
	} {
		if _, err := os.Stat(filepath.Join(root, m.marker)); err == nil {
			return m.stack
		}
	}
	return ""
}

// initSnippet returns a stack-tailored base-url snippet. The proxy records on first
// run (--mode record --upstream <real API>) and replays offline thereafter.
func initSnippet(stack string) string {
	const common = "  # record once (hits the real API), then replay offline forever:\n" +
		"  cassette proxy ./testdata/cassettes --mode record --upstream https://api.openai.com --addr :8080\n" +
		"  cassette proxy ./testdata/cassettes --mode replay --addr :8080   # CI: no key, no network\n\n"
	switch stack {
	case "python":
		return common + "  # point your SDK at the proxy (pytest/conftest.py recipe: docs/recipes/):\n" +
			"  export OPENAI_BASE_URL=http://localhost:8080/v1\n"
	case "node":
		return common + "  # point your SDK at the proxy:\n" +
			"  process.env.OPENAI_BASE_URL = 'http://localhost:8080/v1'\n"
	case "go":
		return common + "  # or skip the proxy entirely in Go — wrap the client with cassettetest.New(t, \"\").\n"
	case "promptfoo":
		// promptfoo speaks OpenAI; point its provider at the cassette replay endpoint
		// so the eval runs deterministic and offline (see docs/recipes/promptfoo.md).
		return common + "  # then run promptfoo against the offline replay endpoint:\n" +
			"  #   providers:\n" +
			"  #     - id: openai:chat:gpt-4o-mini\n" +
			"  #       config: { apiBaseUrl: http://localhost:8080/v1, apiKey: replay }\n"
	default:
		return common + "  # set your provider's *_BASE_URL to http://localhost:8080 (see docs/recipes/).\n"
	}
}

const initUsageText = "usage: cassette init [dir] [--stack python|node|go|promptfoo]"

func initUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, initUsageText)
	return exitUsage
}
