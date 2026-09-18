package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cmdGitconfig installs a local git textconv diff driver so `git diff`, `git log
// -p`, and `git show` render cassettes as the human `cassette doc` transcript
// instead of raw YAML SSE. LOCAL review only — GitHub does not run textconv
// server-side, and each reviewer must run --install and have `cassette` on PATH.
func cmdGitconfig(args []string, stdout, stderr io.Writer) int {
	install := false
	dir := "."
	for _, a := range args {
		switch {
		case a == "--install":
			install = true
		case strings.HasPrefix(a, "-"):
			return gitconfigUsage(stderr)
		default:
			dir = a
		}
	}
	if !install {
		return gitconfigUsage(stderr)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, "cassette: %s is not a git repository: %s\n", dir, strings.TrimSpace(string(out)))
		return exitFail
	}
	if err := exec.Command("git", "-C", dir, "config", "diff.cassette.textconv", "cassette doc").Run(); err != nil {
		fmt.Fprintf(stderr, "cassette: git config: %v\n", err)
		return exitFail
	}
	gaPath := filepath.Join(dir, ".gitattributes")
	line := "testdata/cassettes/*.yaml diff=cassette"
	existing, _ := os.ReadFile(gaPath)
	if !strings.Contains(string(existing), line) {
		body := string(existing)
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += line + "\n"
		if err := os.WriteFile(gaPath, []byte(body), 0o644); err != nil {
			fmt.Fprintf(stderr, "cassette: write .gitattributes: %v\n", err)
			return exitFail
		}
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s installed git textconv driver 'cassette' in %s\n", st.check(), dir)
	fmt.Fprintf(stdout, "  git diff now renders %s as a transcript (local review only; reviewers need `cassette` on PATH)\n", line)
	return exitOK
}

func gitconfigUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: cassette gitconfig --install [dir]")
	return exitUsage
}
