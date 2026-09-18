package main

import (
	"fmt"
	"io"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdDiff renders a signal-only semantic diff between two recordings: what the
// model actually did differently (text, tool calls, finish reason) plus which
// request-input axis changed — with volatile id/usage/SSE churn suppressed.
// Nonzero exit on any divergence (so it works as a review/CI gate).
func cmdDiff(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("md"), args, diffUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() != 2 {
		return diffUsage(stderr)
	}
	pa, pb := in.arg(0), in.arg(1)
	md := in.boolv("md")
	fa, ok := loadOrErr(pa, stderr)
	if !ok {
		return exitFail
	}
	fb, ok := loadOrErr(pb, stderr)
	if !ok {
		return exitFail
	}
	d := analysis.DiffRecordings(fa, fb)
	st := newStyle(stdout)
	if !d.Diverged() {
		fmt.Fprintf(stdout, "%s recordings are semantically identical\n", st.check())
		return exitOK
	}
	if md {
		renderDiffMarkdown(stdout, d)
	} else {
		renderDiffText(stdout, st, d)
	}
	return exitFail
}

func renderDiffText(w io.Writer, st style, d analysis.RecordingDiff) {
	for _, t := range d.Turns {
		tag := ""
		if t.Root {
			tag = st.yellow(" (root)")
		}
		fmt.Fprintf(w, "%s turn %d%s\n", st.bold("≠"), t.Turn, tag)
		if len(t.Axes) > 0 {
			fmt.Fprintf(w, "    input axis: %s\n", strings.Join(t.Axes, ", "))
		}
		if t.TextChanged {
			fmt.Fprintf(w, "    text: %s\n", wordDiff(t.TextA, t.TextB, st))
		}
		for _, tc := range t.Tools {
			switch tc.Kind {
			case "added":
				fmt.Fprintf(w, "    %s tool %s(%s)\n", st.green("+"), tc.Name, truncate(tc.ArgsB, 120))
			case "removed":
				fmt.Fprintf(w, "    %s tool %s(%s)\n", st.red("-"), tc.Name, truncate(tc.ArgsA, 120))
			case "args":
				fmt.Fprintf(w, "    %s tool %s args: %s\n", st.yellow("~"), tc.Name, wordDiff(tc.ArgsA, tc.ArgsB, st))
			}
		}
		if t.FinishA != t.FinishB {
			fmt.Fprintf(w, "    finish: %s → %s\n", st.red(t.FinishA), st.green(t.FinishB))
		}
	}
	if d.ExtraA > 0 {
		fmt.Fprintf(w, "%s %d turn(s) only in A\n", st.red("-"), d.ExtraA)
	}
	if d.ExtraB > 0 {
		fmt.Fprintf(w, "%s %d turn(s) only in B\n", st.green("+"), d.ExtraB)
	}
}

func renderDiffMarkdown(w io.Writer, d analysis.RecordingDiff) {
	fmt.Fprintln(w, "| turn | axis | change |")
	fmt.Fprintln(w, "|------|------|--------|")
	for _, t := range d.Turns {
		var changes []string
		if t.TextChanged {
			changes = append(changes, "text changed")
		}
		for _, tc := range t.Tools {
			changes = append(changes, fmt.Sprintf("tool %s %s", tc.Name, tc.Kind))
		}
		if t.FinishA != t.FinishB {
			changes = append(changes, fmt.Sprintf("finish %s→%s", t.FinishA, t.FinishB))
		}
		turn := fmt.Sprintf("%d", t.Turn)
		if t.Root {
			turn += " (root)"
		}
		fmt.Fprintf(w, "| %s | %s | %s |\n", turn, strings.Join(t.Axes, ", "), strings.Join(changes, "; "))
	}
	if d.ExtraA > 0 || d.ExtraB > 0 {
		fmt.Fprintf(w, "| length | | A has %d extra, B has %d extra |\n", d.ExtraA, d.ExtraB)
	}
}

// wordDiff produces a compact inline word-level diff using an LCS over words.
func wordDiff(a, b string, st style) string {
	wa, wb := strings.Fields(a), strings.Fields(b)
	// LCS table.
	m, n := len(wa), len(wb)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if wa[i] == wb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < m && j < n {
		if wa[i] == wb[j] {
			out = append(out, wa[i])
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			out = append(out, st.red("-"+wa[i]))
			i++
		} else {
			out = append(out, st.green("+"+wb[j]))
			j++
		}
	}
	for ; i < m; i++ {
		out = append(out, st.red("-"+wa[i]))
	}
	for ; j < n; j++ {
		out = append(out, st.green("+"+wb[j]))
	}
	return truncate(strings.Join(out, " "), 400)
}

const diffUsageText = "usage: cassette diff <a.yaml> <b.yaml> [--md]"

func diffUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, diffUsageText)
	return exitUsage
}
