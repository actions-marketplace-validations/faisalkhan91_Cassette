package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const openUsageText = "usage: cassette open <cassette.yaml|dir> [-o report.html] [--no-open]"

// cmdOpen is the unified "look at it" home — a shortcut over the `dashboard` + `doc`
// primitives: it renders the self-contained offline HTML dashboard for a cassette or
// corpus, opens it in the browser (best effort), and prints the transcript to stdout.
// Read-only; no network.
func cmdOpen(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("no-open").valFlag("out").alias("o", "out"), args, openUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" || in.nargs() > 1 {
		fmt.Fprintln(stderr, openUsageText)
		return exitUsage
	}

	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	files, err := loadAll(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	html, err := renderDashboard(buildDashboard(paths, files))
	if err != nil {
		fmt.Fprintf(stderr, "cassette: render: %v\n", err)
		return exitFail
	}
	out := in.str("out")
	persisted := out != ""
	if !persisted {
		tmp, terr := os.CreateTemp("", "cassette-*.html")
		if terr != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", terr)
			return exitFail
		}
		out = tmp.Name()
		_ = tmp.Close()
	}
	if err := os.WriteFile(out, html, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}

	// Print the transcript inline so a terminal-only / headless run still gets the
	// content (a single file renders its full transcript; a corpus lists its files).
	st := newStyle(stdout)
	if len(paths) == 1 {
		fmt.Fprint(stdout, renderDoc(paths[0], files[0]))
	} else {
		fmt.Fprintf(stdout, "%d cassette(s):\n", len(paths))
		for _, p := range paths {
			fmt.Fprintf(stdout, "  %s\n", p)
		}
	}
	fmt.Fprintf(stdout, "\n%s report: %s\n", st.check(), out)

	if !in.boolv("no-open") {
		if err := openBrowser(out); err != nil {
			// Not fatal: headless/CI or no opener — the path is already printed.
			fmt.Fprintf(stderr, "%s could not open a browser (%v); open %s manually\n", st.warn(), err, out)
		}
	}
	return exitOK
}

// openBrowser best-effort launches the OS default handler for path. Errors are
// advisory (headless environments have no opener).
func openBrowser(path string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "cmd", []string{"/c", "start", ""}
	default: // linux, *bsd
		name = "xdg-open"
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("no opener (%s) on PATH", name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return exec.Command(name, append(args, abs)...).Start()
}
