package analysis

import (
	"fmt"

	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// ScenarioVariant is one authored edge/error cassette derived from a golden turn.
type ScenarioVariant struct {
	Name   string        // e.g. "finish-max_tokens", "malformed-args", "provider-error"
	File   *wirefmt.File // the golden cassette with the target turn's response replaced
	Digest string        // combined semantic digest of the variant (self-check)
}

// errorStreamSSE is an authored provider-error event stream (Anthropic-shaped; the
// other decoders simply see no content, which is still a valid edge fixture).
var errorStreamSSE = []byte("event: error\n" +
	`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n")

// Scenario derives a family of valid edge/error cassettes from one golden turn:
// each finish reason, truncated and empty text, malformed-but-legal tool args, and
// an injected provider error. turn is the index among the recording's HTTP turns.
// One golden run → a matrix of named fixtures, each self-checking via its combined
// semantic digest. All offline.
func Scenario(f *wirefmt.File, turn int) ([]ScenarioVariant, error) {
	idx, ok := nthHTTPIndex(f, turn)
	if !ok {
		return nil, fmt.Errorf("turn %d is out of range (recording has %d http turn(s))", turn, countHTTP(f))
	}
	it := f.Interactions[idx]
	base, unary, renderable := DecodeInteraction(it)
	if unary || !renderable {
		return nil, fmt.Errorf("turn %d is not a decodable streaming turn", turn)
	}
	p := wireenc.ProviderForURL(it.Request.URL)
	enc := func(tr semequal.Transcript) wirefmt.Response {
		return streamResponse(wireenc.Encode(p, tr.Normalize(), wireenc.Envelope{}))
	}

	var variants []ScenarioVariant
	add := func(name string, resp wirefmt.Response) {
		vf := cloneWithResponse(f, idx, resp)
		variants = append(variants, ScenarioVariant{Name: name, File: vf, Digest: semequal.CombinedDigest(CollectTurns(vf))})
	}

	// Finish-reason matrix (same content, different stop reason).
	for _, fr := range []string{"end_turn", "max_tokens", "stop_sequence", "tool_use"} {
		tr := base
		tr.FinishReason = fr
		if fr == "tool_use" && len(tr.ToolCalls) == 0 {
			tr.ToolCalls = []semequal.ToolCall{{Name: "tool", Args: "{}"}}
		}
		add("finish-"+fr, enc(tr))
	}
	// Truncated text (last-token-truncated, hit the token cap).
	if n := len(base.Text); n > 1 {
		tr := base
		tr.Text = base.Text[:n/2]
		tr.FinishReason = "max_tokens"
		add("truncated", enc(tr))
	}
	// Empty text.
	empty := base
	empty.Text = ""
	empty.FinishReason = "end_turn"
	add("empty-text", enc(empty))
	// Malformed-but-legal tool args (valid JSON, unexpected shape).
	mal := base
	mal.ToolCalls = []semequal.ToolCall{{Name: firstToolName(base), Args: `{"__unexpected__":true}`}}
	mal.FinishReason = "tool_use"
	add("malformed-args", enc(mal))
	// Injected provider error.
	add("provider-error", wirefmt.Response{
		Status:    529,
		Headers:   wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
		Streaming: true,
		Body:      wirefmt.NewBody(errorStreamSSE),
	})
	return variants, nil
}

func streamResponse(sse []byte) wirefmt.Response {
	return wirefmt.Response{
		Status:    200,
		Headers:   wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
		Streaming: true,
		Body:      wirefmt.NewBody(sse),
	}
}

func cloneWithResponse(f *wirefmt.File, idx int, resp wirefmt.Response) *wirefmt.File {
	out := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Notice: f.Notice, Expect: f.Expect}
	out.Interactions = make([]*wirefmt.Interaction, len(f.Interactions))
	copy(out.Interactions, f.Interactions)
	orig := f.Interactions[idx]
	out.Interactions[idx] = &wirefmt.Interaction{Kind: orig.Kind, Request: orig.Request, Response: resp}
	return out
}

func nthHTTPIndex(f *wirefmt.File, n int) (int, bool) {
	count := 0
	for i, it := range f.Interactions {
		if it.Kind == "http" {
			if count == n {
				return i, true
			}
			count++
		}
	}
	return 0, false
}

func countHTTP(f *wirefmt.File) int {
	n := 0
	for _, it := range f.Interactions {
		if it.Kind == "http" {
			n++
		}
	}
	return n
}

func firstToolName(tr semequal.Transcript) string {
	if len(tr.ToolCalls) > 0 {
		return tr.ToolCalls[0].Name
	}
	return "tool"
}
