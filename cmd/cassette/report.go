package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdReport turns a failing recording into a committable, CI-runnable bug report.
// It RE-RUNS the documented check (a violated invariant, or a diff vs a baseline)
// and exits nonzero iff the bug still reproduces — so a green build means the bug
// is gone. It composes existing pieces (assert/diff/attest); it adds no new
// detection. With --bundle it emits a .castiron repro directory.
func cmdReport(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(
		newFlags().
			valFlag("no-tool", "diff", "attest", "bundle", "out", "repro", "title").
			boolFlag("no-duplicate-tools", "finishes-clean").
			alias("o", "out"),
		args, reportUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 1 {
		return exitUsage
	}
	path := in.arg(0)
	noTools := in.list("no-tool")
	diffBaseline := in.str("diff")
	attestPath := in.str("attest")
	bundle := in.str("bundle")
	outPath := in.str("out")
	repro := in.str("repro")
	title := in.str("title")
	noDup := in.boolv("no-duplicate-tools")
	finishesClean := in.boolv("finishes-clean")
	invFlag := in.has("no-tool") || noDup || finishesClean
	if path == "" {
		return reportUsage(stderr)
	}
	if diffBaseline != "" && invFlag {
		fmt.Fprintln(stderr, "cassette: choose either invariant flags OR --diff, not both")
		return exitUsage
	}
	f, ok := loadOrErr(path, stderr)
	if !ok {
		return exitFail
	}

	var md strings.Builder
	if title == "" {
		title = "cassette bug report: " + filepath.Base(path)
	}
	bugLive := false
	var verdict string

	if diffBaseline != "" {
		bf, ok := loadOrErr(diffBaseline, stderr)
		if !ok {
			return exitFail
		}
		d := analysis.DiffRecordings(f, bf)
		div := analysis.Bisect(f, bf)
		bugLive = d.Diverged()
		if bugLive {
			verdict = fmt.Sprintf("DIVERGED from %s", filepath.Base(diffBaseline))
			if div.Diverged() {
				verdict += fmt.Sprintf(" at turn %d (root); changed axis: %s", div.Turn, strings.Join(div.Axes, ", "))
			}
		} else {
			verdict = "semantically identical to baseline (no longer reproduces)"
		}
		writeReportHeader(&md, title, verdict, bugLive)
		md.WriteString("\n## What changed\n\n")
		renderDiffMarkdown(&md, d)
		if repro == "" {
			repro = fmt.Sprintf("cassette diff %s %s", filepath.Base(path), filepath.Base(diffBaseline))
		}
	} else {
		invs, names := buildReportInvariants(f, noTools, noDup, finishesClean)
		errs := semequal.Check(analysis.CollectTurns(f), invs...)
		bugLive = len(errs) > 0
		if bugLive {
			verdict = errs[0].Error()
		} else {
			verdict = "all invariants satisfied (no longer reproduces)"
		}
		writeReportHeader(&md, title, verdict, bugLive)
		md.WriteString("\n## What failed\n\n")
		if len(errs) == 0 {
			md.WriteString("_The recording now satisfies every checked invariant._\n")
		}
		for _, e := range errs {
			fmt.Fprintf(&md, "- %s\n", e.Error())
		}
		fmt.Fprintf(&md, "\nInvariants checked: %s\n", strings.Join(names, ", "))
		if repro == "" {
			repro = "cassette assert " + filepath.Base(path) + reproFlags(noTools, noDup, finishesClean, f)
		}
	}

	// Repro + transcript + provenance + tamper-evidence + CI sections.
	fmt.Fprintf(&md, "\n## Repro\n\n```sh\n%s\n```\n", repro)
	writeTranscriptSection(&md, f)
	writeProvenance(&md, path, f)
	if attestPath != "" {
		fmt.Fprintf(&md, "\n## Tamper-evidence\n\nThis fixture is signed. Verify it is exactly the recording that failed:\n\n```sh\ncassette verify-attest %s %s\n```\n", filepath.Base(path), filepath.Base(attestPath))
	}
	writeCISnippet(&md, path, attestPath, repro)

	report := md.String()
	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(report), 0o644); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
	} else if bundle == "" {
		fmt.Fprint(stdout, report)
	}

	if bundle != "" {
		if code := writeCastironBundle(bundle, path, f, diffBaseline, attestPath, report, repro, stderr); code != exitOK {
			return code
		}
		st := newStyle(stdout)
		fmt.Fprintf(stdout, "%s wrote %s.castiron/ (commit it; CI re-runs it)\n", st.check(), strings.TrimSuffix(bundle, ".castiron"))
	}

	st := newStyle(stdout)
	if bugLive {
		fmt.Fprintf(stdout, "%s bug reproduces: %s\n", st.cross(), verdict)
		return exitFail
	}
	fmt.Fprintf(stdout, "%s %s\n", st.check(), verdict)
	return exitOK
}

func buildReportInvariants(f *wirefmt.File, noTools []string, noDup, finishesClean bool) ([]semequal.Invariant, []string) {
	if len(noTools) == 0 && !noDup && !finishesClean && f.Expect != nil {
		noTools = f.Expect.NoTools
		noDup = f.Expect.NoDuplicateTools
		finishesClean = f.Expect.FinishesClean
	}
	var invs []semequal.Invariant
	var names []string
	for _, n := range noTools {
		invs = append(invs, semequal.NoToolCalled(n))
		names = append(names, "no-tool:"+n)
	}
	if noDup {
		invs = append(invs, semequal.NoDuplicateToolCall())
		names = append(names, "no-duplicate-tools")
	}
	if finishesClean {
		invs = append(invs, semequal.FinishesCleanly())
		names = append(names, "finishes-clean")
	}
	if len(names) == 0 {
		names = []string{"(none — pass an invariant flag or embed an Expect contract)"}
	}
	return invs, names
}

func reproFlags(noTools []string, noDup, finishesClean bool, f *wirefmt.File) string {
	if len(noTools) == 0 && !noDup && !finishesClean {
		return "" // runs the embedded Expect contract zero-arg
	}
	var b strings.Builder
	for _, n := range noTools {
		fmt.Fprintf(&b, " --no-tool %s", n)
	}
	if noDup {
		b.WriteString(" --no-duplicate-tools")
	}
	if finishesClean {
		b.WriteString(" --finishes-clean")
	}
	return b.String()
}

func writeReportHeader(md *strings.Builder, title, verdict string, bugLive bool) {
	status := "✅ FIXED"
	if bugLive {
		status = "🔴 BUG LIVE"
	}
	fmt.Fprintf(md, "# %s\n\n**%s** — %s\n", title, status, verdict)
}

func writeTranscriptSection(md *strings.Builder, f *wirefmt.File) {
	md.WriteString("\n## Recorded transcript\n\n")
	turn := -1
	for _, it := range f.Interactions {
		if it.Kind != "http" {
			continue
		}
		turn++
		tr, _, ok := analysis.DecodeInteraction(it)
		if !ok {
			continue
		}
		fmt.Fprintf(md, "**turn %d** (%s)\n", turn, it.Request.URL)
		if tr.Text != "" {
			fmt.Fprintf(md, "> %s\n", truncate(tr.Text, 300))
		}
		for _, tc := range tr.ToolCalls {
			fmt.Fprintf(md, "- 🔧 `%s(%s)`\n", tc.Name, truncate(tc.Args, 160))
		}
		if tr.FinishReason != "" {
			fmt.Fprintf(md, "- _finish: %s_\n", tr.FinishReason)
		}
	}
}

func writeProvenance(md *strings.Builder, path string, f *wirefmt.File) {
	m := analysis.BuildAttestManifest(f)
	md.WriteString("\n## Provenance\n\n")
	fmt.Fprintf(md, "- cassette: `%s`\n- sha256: `%s`\n- schema: v%d\n", filepath.Base(path), m.CassetteSHA, f.SchemaVersion)
	if f.Notice != "" {
		fmt.Fprintf(md, "- notice: %s\n", f.Notice)
	}
	if commit := gitHead(); commit != "" {
		fmt.Fprintf(md, "- git: `%s`\n", commit)
	}
	fmt.Fprintf(md, "- env: %s / %s\n", runtime.GOOS, runtime.Version())
}

func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeCISnippet(md *strings.Builder, path, attestPath, repro string) {
	md.WriteString("\n## CI\n\nRuns offline (zero network, zero key); nonzero while the bug reproduces:\n\n```sh\n")
	if attestPath != "" {
		fmt.Fprintf(md, "cassette verify-attest %s %s\n", filepath.Base(path), filepath.Base(attestPath))
	}
	fmt.Fprintf(md, "%s\n```\n", repro)
}

// writeCastironBundle writes a plain, offline-runnable .castiron directory. It is
// NOT a packed binary (packed binaries ignore subcommands) so CI runs the real
// cassette CLI against the bundled files.
func writeCastironBundle(name, path string, f *wirefmt.File, baseline, attestPath, report, repro string, stderr io.Writer) int {
	dir := strings.TrimSuffix(name, ".castiron") + ".castiron"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	raw, err := wirefmt.Marshal(f)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "cassette: refusing to bundle — secret pattern(s) present: %v\n", hits)
		return exitFail
	}
	cassetteName := filepath.Base(path)
	if err := os.WriteFile(filepath.Join(dir, cassetteName), raw, 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o644)
	copyInto := func(src string) {
		if src == "" {
			return
		}
		if b, err := os.ReadFile(src); err == nil {
			os.WriteFile(filepath.Join(dir, filepath.Base(src)), b, 0o644)
		}
	}
	copyInto(baseline)
	copyInto(attestPath)
	runSh := "#!/bin/sh\nset -e\n"
	if attestPath != "" {
		runSh += fmt.Sprintf("cassette verify-attest %s %s\n", cassetteName, filepath.Base(attestPath))
	}
	runSh += repro + "\n"
	os.WriteFile(filepath.Join(dir, "run.sh"), []byte(runSh), 0o755)
	return exitOK
}

const reportUsageText = "usage: cassette report <cassette.yaml> [--no-tool NAME|--no-duplicate-tools|--finishes-clean | --diff baseline.yaml] [--attest c.att] [--bundle name] [-o report.md] [--repro CMD] [--title T]"

func reportUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, reportUsageText)
	return exitUsage
}
