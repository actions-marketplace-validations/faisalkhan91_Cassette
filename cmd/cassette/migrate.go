package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/semequal"
)

// cmdMigrate ports a whole recording or corpus to a target wire dialect and
// verifies that every turn's semantic digest is preserved, failing loudly on any
// drift. It is the "de-risk a provider switch with your existing tests" command:
// record once against provider A, migrate the corpus to provider B's dialect, and
// replay your B-built client offline against the same recorded behavior — with a
// hard guarantee the behavior didn't silently change in translation.
func cmdMigrate(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("to", "out").alias("o", "out"), args, migrateUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	to := in.str("to")
	out := in.str("out")
	if src == "" || to == "" || out == "" || in.nargs() > 1 {
		return migrateUsage(stderr)
	}
	toProv, _, err := analysis.ProviderRouting(to)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitUsage
	}
	info, err := os.Stat(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	// Output mapping: a directory source migrates into an output directory
	// (flat, by basename); a single file migrates to a single output file.
	srcIsDir := info.IsDir()
	if srcIsDir {
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
	}

	st := newStyle(stdout)
	failed := false
	for _, p := range paths {
		f, ok := loadOrErr(p, stderr)
		if !ok {
			failed = true
			continue
		}
		before := analysis.CollectTurns(f)
		ported, n, perr := analysis.Port(f, toProv)
		if perr != nil {
			fmt.Fprintf(stdout, "%s %s: %v\n", st.cross(), filepath.Base(p), perr)
			failed = true
			continue
		}
		if drift := digestDrift(before, analysis.CollectTurns(ported)); drift != "" {
			fmt.Fprintf(stdout, "%s %s: digest drift — %s\n", st.cross(), filepath.Base(p), drift)
			failed = true
			continue
		}
		analysis.StampProvenance(ported, f, "migrate", "→ "+to)
		dst := out
		if srcIsDir {
			dst = filepath.Join(out, filepath.Base(p))
		}
		if code := saveScrubbed(dst, ported, stderr); code != exitOK {
			failed = true
			continue
		}
		fmt.Fprintf(stdout, "%s %s → %s (%d turn(s), digests preserved)\n", st.check(), filepath.Base(p), dst, n)
	}
	if failed {
		fmt.Fprintf(stdout, "%s migration failed — digests not preserved (no provider switch is safe here)\n", st.cross())
		return exitFail
	}
	fmt.Fprintf(stdout, "%s migrated %d cassette(s) to %s — every turn's behavior preserved\n", st.check(), len(paths), to)
	return exitOK
}

// digestDrift returns "" when the two turn lists carry identical per-turn semantic
// digests in order; otherwise a short description of the first divergence. This is
// the migration safety net: porting must re-target the wire dialect without
// changing any turn's decoded behavior.
func digestDrift(before, after []semequal.Transcript) string {
	if len(before) != len(after) {
		return fmt.Sprintf("turn count changed (%d → %d)", len(before), len(after))
	}
	for i := range before {
		if before[i].Digest() != after[i].Digest() {
			return fmt.Sprintf("turn %d digest changed (%s → %s)",
				i, short(before[i].Digest()), short(after[i].Digest()))
		}
	}
	return ""
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

const migrateUsageText = "usage: cassette migrate <in|dir> --to anthropic|openai-chat|openai-responses|gemini|ollama -o <out|dir>"

func migrateUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, migrateUsageText)
	return exitUsage
}
