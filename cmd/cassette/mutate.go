package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/mutate"
)

func cmdMutate(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().valFlag("op", "out", "seed", "frame").alias("o", "out"), args, mutateUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	op := in.str("op")
	out := in.str("out")
	if path == "" || op == "" || in.nargs() > 1 {
		return mutateUsage(stderr)
	}
	var seed int64
	frame := 0
	if in.has("seed") {
		n, err := strconv.ParseInt(in.str("seed"), 10, 64)
		if err != nil {
			return mutateUsage(stderr)
		}
		seed = n
	}
	if in.has("frame") {
		n, err := strconv.Atoi(in.str("frame"))
		if err != nil {
			return mutateUsage(stderr)
		}
		frame = n
	}
	var fn mutate.Op
	switch op {
	case "truncate":
		fn = mutate.TruncateAfterFrame(frame)
	case "drop-terminal":
		fn = mutate.DropTerminalEvent()
	case "duplicate":
		fn = mutate.DuplicateFrame(frame)
	case "reorder":
		fn = mutate.ReorderContentDeltas()
	case "corrupt-tool":
		fn = mutate.CorruptToolArgs()
	case "inject-error":
		fn = mutate.InjectError("injected by cassette mutate")
	case "drop-multibyte":
		fn = mutate.DropLastByteOfMultibyte()
	default:
		fmt.Fprintf(stderr, "cassette: unknown --op %q\n", op)
		return exitUsage
	}
	f, err := wirefmt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	n := 0
	for _, it := range f.Interactions {
		if it.Response.Streaming && it.Response.Body != nil {
			it.Response.Body.Data = mutate.Apply(it.Response.Body.Bytes(), seed, fn)
			n++
		}
	}
	if out == "" {
		out = path
	}
	if code := saveScrubbed(out, f, stderr); code != exitOK {
		return code
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s mutated %d streaming interaction(s) with %q -> %s\n", st.check(), n, op, out)
	return exitOK
}

const mutateUsageText = "usage: cassette mutate <cassette.yaml> --op truncate|drop-terminal|duplicate|reorder|corrupt-tool|inject-error|drop-multibyte [--frame N] [--seed N] [-o|--out path]"

func mutateUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mutateUsageText)
	return exitUsage
}
