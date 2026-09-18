package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func cmdInspect(args []string, stdout, stderr io.Writer) int {
	path, ok := oneArg(args)
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette inspect <cassette.yaml>")
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	st := newStyle(stdout)
	var nHTTP, nMCP int
	for _, it := range f.Interactions {
		switch it.Kind {
		case "http":
			nHTTP++
		case "mcp":
			nMCP++
		}
	}

	fmt.Fprintf(stdout, "%s %s\n", st.dim("cassette"), st.bold(path))
	fmt.Fprintln(stdout, st.dim(fmt.Sprintf("schema %d · %d interaction(s) · %d http, %d mcp",
		f.SchemaVersion, len(f.Interactions), nHTTP, nMCP)))
	if f.Notice != "" {
		fmt.Fprintf(stdout, "%s %s\n", st.dim("notice:"), f.Notice)
	}
	if len(f.Interactions) == 0 {
		fmt.Fprintln(stdout, st.dim("  (empty)"))
		return exitOK
	}
	for i, it := range f.Interactions {
		idx := st.dim(fmt.Sprintf("%3d", i))
		size := st.dim("(" + humanBytes(len(it.Response.Body.Bytes())) + ")")
		switch it.Kind {
		case "http":
			meta := size
			if it.Response.Streaming {
				meta = st.dim("(stream · " + humanBytes(len(it.Response.Body.Bytes())) + ")")
			}
			fmt.Fprintf(stdout, "  %s %s %s %s → %s %s\n",
				idx, st.cyan("http"), st.bold(it.Request.Method), it.Request.URL,
				st.statusColor(it.Response.Status), meta)
		case "mcp":
			tool := it.Request.MCPTool
			if tool != "" {
				tool = " " + st.bold(tool)
			}
			fmt.Fprintf(stdout, "  %s %s %s%s %s\n",
				idx, st.yellow("mcp"), it.Request.MCPMethod, tool, size)
		default:
			fmt.Fprintf(stdout, "  %s %s\n", idx, it.Kind)
		}
	}
	return exitOK
}

func cmdScrub(args []string, stdout, stderr io.Writer) int {
	path, ok := oneArg(args)
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette scrub <cassette.yaml>")
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	scrub.DefaultConfig().File(f)
	if err := wirefmt.Save(path, f); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s scrubbed %s\n", st.check(), path)
	return exitOK
}

func cmdVerify(args []string, stdout, stderr io.Writer) int {
	path, ok := oneArg(args)
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette verify <cassette.yaml>")
		return exitUsage
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	st := newStyle(stderr)
	if _, err := wirefmt.Unmarshal(raw); err != nil {
		fmt.Fprintf(stderr, "%s cassette: malformed: %v\n", st.cross(), err)
		return exitFail
	}
	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "%s cassette: secret pattern(s) found: %v\n", st.cross(), hits)
		return exitFail
	}
	so := newStyle(stdout)
	fmt.Fprintf(stdout, "%s %s — well-formed, no secret patterns\n", so.check(), so.bold(path))
	return exitOK
}

func cmdPrune(args []string, stdout, stderr io.Writer) int {
	path, idxStr, ok := parseEditArgs(args, "index")
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette prune <cassette.yaml> --index N[,M,...]")
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	drop := map[int]bool{}
	for _, s := range strings.Split(idxStr, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 0 || n >= len(f.Interactions) {
			fmt.Fprintf(stderr, "cassette: invalid index %q (have %d interactions)\n", s, len(f.Interactions))
			return exitFail
		}
		drop[n] = true
	}
	kept := f.Interactions[:0:0]
	for i, it := range f.Interactions {
		if !drop[i] {
			kept = append(kept, it)
		}
	}
	f.Interactions = kept
	if code := saveScrubbed(path, f, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s pruned %d interaction(s); %d remain in %s\n", st.check(), len(drop), len(kept), path)
	return exitOK
}

func cmdRekey(args []string, stdout, stderr io.Writer) int {
	force := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--force" || a == "-force" || a == "-f" {
			force = true
			continue
		}
		rest = append(rest, a)
	}
	path, ok := oneArg(rest)
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette rekey <cassette.yaml> [--force]")
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	cfg := fileMatchConfig(f)
	// A scrubbed request body no longer contains the LIVE bytes the stored key was
	// derived from, so recomputing would overwrite the correct (replay-authoritative)
	// key with one derived from the redacted span — silently breaking replay. Refuse
	// when rekey would actually CHANGE a key on a scrubbed body; --force overrides.
	// (A legit hand-edit, or a redacted field already in the volatile set, is unaffected:
	// the key either doesn't change or the body carries no scrub sentinel.)
	if !force {
		var corrupt []int
		for i, it := range f.Interactions {
			if it.Kind != "http" || it.Request.MatchKey == "" {
				continue
			}
			if !bytes.Contains(it.Request.Body.Bytes(), []byte(scrub.Replacement)) {
				continue
			}
			if k, err := rekey.HTTPKey(it.Request, cfg); err == nil && k != it.Request.MatchKey {
				corrupt = append(corrupt, i)
			}
		}
		if len(corrupt) > 0 {
			st := newStyle(stderr)
			fmt.Fprintf(stderr, "%s cassette: refusing to rekey — interaction(s) %v have a scrubbed request body whose\n", st.cross(), corrupt)
			fmt.Fprintln(stderr, "  stored key is NOT re-derivable from the redacted bytes. Rekeying would overwrite the")
			fmt.Fprintln(stderr, "  correct key and break replay. Mark the secret field volatile (cassette redact-field")
			fmt.Fprintln(stderr, "  --path …) so it is excluded from keying, or pass --force if you know the body is intact.")
			return exitFail
		}
	}
	// Re-derive under the cassette's OWN persisted match normalization, or a
	// --volatile recording would be rekeyed without its volatile drops (silently
	// breaking replay). The persisted match: block is preserved on save.
	if err := rekey.File(f, cfg); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if code := saveScrubbed(path, f, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s rekeyed %d interaction(s) in %s\n", st.check(), len(f.Interactions), path)
	return exitOK
}

func cmdRedactField(args []string, stdout, stderr io.Writer) int {
	path, jsonPathStr, ok := parseEditArgs(args, "path")
	if !ok {
		fmt.Fprintln(stderr, "usage: cassette redact-field <cassette.yaml> --path a.b.c")
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	redact := func(b *wirefmt.Body) {
		if b == nil || len(b.Data) == 0 || !gjson.ValidBytes(b.Data) {
			return
		}
		if !gjson.GetBytes(b.Data, jsonPathStr).Exists() {
			return
		}
		if next, err := sjson.SetBytes(b.Data, jsonPathStr, "[REDACTED]"); err == nil {
			b.Data = next
		}
	}
	for _, it := range f.Interactions {
		redact(it.Request.Body)
		redact(it.Response.Body)
	}
	// Treat the redacted path as volatile so the recomputed key ignores it: a
	// replayed request carrying the original value still matches. Persist it (unioned
	// with any existing match: block) so replay/conformance/lint are self-describing.
	cfg := fileMatchConfig(f)
	if !slices.Contains(cfg.VolatileJSONPaths, jsonPathStr) {
		cfg.VolatileJSONPaths = append(cfg.VolatileJSONPaths, jsonPathStr)
	}
	f.Match = &wirefmt.MatchSpec{VolatileJSONPaths: cfg.VolatileJSONPaths, HeaderAllowlist: cfg.HeaderAllowlist}
	if err := rekey.File(f, cfg); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	if code := saveScrubbed(path, f, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s redacted %q in %s\n", st.check(), jsonPathStr, path)
	return exitOK
}

// fileMatchConfig builds the match config from a cassette's persisted match: block
// (empty if none), so rekey/redact-field reproduce the keys the recording was made
// with instead of recomputing without its volatile normalization.
func fileMatchConfig(f *wirefmt.File) match.Config {
	if f == nil || f.Match == nil {
		return match.Config{}
	}
	return match.Config{VolatileJSONPaths: f.Match.VolatileJSONPaths, HeaderAllowlist: f.Match.HeaderAllowlist}
}
