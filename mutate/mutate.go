// Package mutate turns a recorded SSE stream into deterministic, hostile-but-
// plausible variants — the failure modes a flaky model or buggy proxy actually
// produces (truncated mid-stream, dropped terminal event, malformed tool-call
// arguments, reordered or duplicated frames, an injected error event). One golden
// recording becomes a reproducible fuzz corpus for agent-resilience testing,
// entirely offline. Operators re-emit valid SSE wire framing so the provider SDK
// scanner still consumes the bytes; each is keyed by an explicit seed so a
// failure reproduces as (operator, seed).
package mutate

import (
	"bytes"
	"math/rand"
)

// Op transforms an ordered list of SSE frames (each frame retains its trailing
// blank-line separator) using rng for any randomized choice.
type Op func(frames [][]byte, rng *rand.Rand) [][]byte

// Apply splits raw into SSE frames, runs ops in order (seeded by seed for
// reproducibility), and rejoins them into a byte stream.
func Apply(raw []byte, seed int64, ops ...Op) []byte {
	rng := rand.New(rand.NewSource(seed))
	frames := splitFrames(raw)
	for _, op := range ops {
		if op != nil {
			frames = op(frames, rng)
		}
	}
	var out bytes.Buffer
	for _, f := range frames {
		out.Write(f)
	}
	return out.Bytes()
}

// splitFrames splits an SSE byte stream on the blank-line separator, keeping the
// separator with each frame; a trailing remainder becomes its own frame.
func splitFrames(raw []byte) [][]byte {
	sep := []byte("\n\n")
	var frames [][]byte
	rest := raw
	for {
		i := bytes.Index(rest, sep)
		if i < 0 {
			if len(rest) > 0 {
				frames = append(frames, rest)
			}
			break
		}
		frames = append(frames, rest[:i+len(sep)])
		rest = rest[i+len(sep):]
	}
	return frames
}

// TruncateAfterFrame keeps only the first n frames (a stream cut off mid-flight).
func TruncateAfterFrame(n int) Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		if n < 0 || n >= len(frames) {
			return frames
		}
		return frames[:n]
	}
}

// DropTerminalEvent removes the last frame (e.g. message_stop / data: [DONE]).
func DropTerminalEvent() Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		if len(frames) == 0 {
			return frames
		}
		return frames[:len(frames)-1]
	}
}

// DuplicateFrame duplicates the frame at index i (a stuttering proxy).
func DuplicateFrame(i int) Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		if i < 0 || i >= len(frames) {
			return frames
		}
		out := make([][]byte, 0, len(frames)+1)
		out = append(out, frames[:i+1]...)
		out = append(out, frames[i])
		out = append(out, frames[i+1:]...)
		return out
	}
}

// ReorderContentDeltas reverses the order of content-delta frames (the events
// that carry text/argument deltas), scrambling the assembled output while every
// frame stays individually valid.
func ReorderContentDeltas() Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		var deltaIdx []int
		for i, f := range frames {
			if bytes.Contains(f, []byte("content_block_delta")) || bytes.Contains(f, []byte(`"delta"`)) {
				deltaIdx = append(deltaIdx, i)
			}
		}
		for a, b := 0, len(deltaIdx)-1; a < b; a, b = a+1, b-1 {
			frames[deltaIdx[a]], frames[deltaIdx[b]] = frames[deltaIdx[b]], frames[deltaIdx[a]]
		}
		return frames
	}
}

// CorruptToolArgs malforms the first tool-call argument fragment so the
// reassembled tool input is no longer valid JSON (a model that emitted broken
// arguments). It targets Anthropic input_json_delta and OpenAI tool_calls deltas.
func CorruptToolArgs() Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		for i, f := range frames {
			for _, marker := range [][]byte{[]byte(`"partial_json":"`), []byte(`"arguments":"`)} {
				if j := bytes.Index(f, marker); j >= 0 {
					// Inject an unbalanced brace into the argument fragment value.
					at := j + len(marker)
					nf := make([]byte, 0, len(f)+1)
					nf = append(nf, f[:at]...)
					nf = append(nf, '}')
					nf = append(nf, f[at:]...)
					frames[i] = nf
					return frames
				}
			}
		}
		return frames
	}
}

// InjectError inserts a provider-style error event before the terminal frame,
// simulating a mid-stream provider failure. Anthropic shape by default.
func InjectError(message string) Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		ev := []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"" + message + "\"}}\n\n")
		if len(frames) == 0 {
			return [][]byte{ev}
		}
		at := len(frames) - 1
		out := make([][]byte, 0, len(frames)+1)
		out = append(out, frames[:at]...)
		out = append(out, ev)
		out = append(out, frames[at:]...)
		return out
	}
}

// DropLastByteOfMultibyte truncates the final byte of the first multibyte UTF-8
// sequence found in a frame, producing an invalid-UTF-8 stream.
func DropLastByteOfMultibyte() Op {
	return func(frames [][]byte, _ *rand.Rand) [][]byte {
		for i, f := range frames {
			for j := 0; j < len(f); j++ {
				if f[j] >= 0xC0 { // UTF-8 lead byte of a multibyte sequence
					n := 2
					switch {
					case f[j] >= 0xF0:
						n = 4
					case f[j] >= 0xE0:
						n = 3
					}
					if j+n <= len(f) {
						nf := make([]byte, 0, len(f)-1)
						nf = append(nf, f[:j+n-1]...)
						nf = append(nf, f[j+n:]...)
						frames[i] = nf
						return frames
					}
				}
			}
		}
		return frames
	}
}
