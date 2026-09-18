package analysis

import (
	"sort"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// This file holds the shared engine internals reused across the CLI and library
// features (bisect, diff, doctor, eval, dataset, attest, explain): one response
// decoder, one request-axis differ, and one turn collector. Keeping a single
// implementation avoids the drift that previously existed between bisect.go,
// cmd/cassette decodeTranscript, and doc.go.

// DecodeInteraction decodes a recorded interaction's response into a semantic
// Transcript.
//
//   - renderable is true only for a real decoded STREAMING http transcript (safe
//     to show to humans and feed to invariants).
//   - unary is true for a non-streaming (unary) JSON http response: it has no SSE
//     frames, so we return a content-addressed synthetic transcript
//     (Text "unary:<hex>") so divergence detection (bisect/diff) still sees two
//     different answers as different — while renderers skip it (renderable=false).
//   - both false for non-http or empty-body interactions.
//
// decodeMCP brings a recorded MCP interaction into the same Transcript model as
// HTTP turns, so doc/explain/dataset/diff/cost see the tool side of the agentic
// stack too. The tool call becomes a ToolCall (name + canonical arguments); the
// result's text content becomes the Text; isError/list/other set the finish.
func decodeMCP(it *wirefmt.Interaction) semequal.Transcript {
	args := string(it.Request.Body.Bytes())
	if c, ok := canon.Canonicalize(it.Request.Body.Bytes()); ok {
		args = string(c)
	}
	name := it.Request.MCPTool
	if name == "" {
		name = it.Request.MCPMethod
	}
	tr := semequal.Transcript{Role: "tool", ToolCalls: []semequal.ToolCall{{Name: name, Args: args}}}
	body := it.Response.Body.Bytes()
	switch it.Request.MCPMethod {
	case "tools/call":
		tr.Text = mcpResultText(body) // shared content[].text flattener (see mcp.go)
		if mcpIsError(body) {         // shared: result-level isError OR a JSON-RPC envelope error
			tr.FinishReason = "tool_error"
		} else {
			tr.FinishReason = "tool_result"
		}
	case "tools/list":
		var names []string
		gjson.GetBytes(body, "tools").ForEach(func(_, b gjson.Result) bool {
			names = append(names, b.Get("name").String())
			return true
		})
		tr.Text = strings.Join(names, ",")
		tr.FinishReason = "tool_list"
	default:
		tr.FinishReason = "mcp"
	}
	return tr.Normalize()
}

func DecodeInteraction(it *wirefmt.Interaction) (tr semequal.Transcript, unary, renderable bool) {
	if it.Kind == "mcp" {
		return decodeMCP(it), false, true
	}
	if it.Kind != "http" || it.Response.Body == nil {
		return semequal.Transcript{}, false, false
	}
	body := it.Response.Body.Bytes()
	if !it.Response.Streaming {
		c, ok := canon.Canonicalize(body)
		if !ok {
			c = body
		}
		return semequal.Transcript{Role: "assistant", Text: "unary:" + canon.SumHex(c)}, true, false
	}
	// Route through the canonical dialect registry instead of re-deriving the
	// URL→decoder ladder (which drifted from wireenc.ProviderForURL).
	t, _ := wireenc.Decode(wireenc.ProviderForURL(it.Request.URL), body)
	return t, false, true
}

// CollectTurns returns the renderable transcripts of a recording in order
// (skipping unary/non-http interactions), for invariant checks and datasets.
func CollectTurns(f *wirefmt.File) []semequal.Transcript {
	var turns []semequal.Transcript
	for _, it := range f.Interactions {
		if tr, _, ok := DecodeInteraction(it); ok {
			turns = append(turns, tr)
		}
	}
	return turns
}

// httpTurns returns the http interactions of a recording in order.
func httpTurns(f *wirefmt.File) []*wirefmt.Interaction {
	var out []*wirefmt.Interaction
	for _, it := range f.Interactions {
		if it.Kind == "http" {
			out = append(out, it)
		}
	}
	return out
}

// requestAxisKeys are the independent variables we attribute behavior changes to.
var requestAxisKeys = []string{"model", "system", "tools", "messages", "temperature", "top_p", "max_tokens"}

// AxisDiff reports which request-input axes differ between two request bodies
// (model/system/tools/messages/sampling). The sampling knobs collapse to a
// single "sampling" axis.
func AxisDiff(a, b []byte) []string {
	var diff []string
	for _, k := range requestAxisKeys {
		if gjson.GetBytes(a, k).Raw != gjson.GetBytes(b, k).Raw {
			axis := k
			switch k {
			case "temperature", "top_p", "max_tokens":
				axis = "sampling"
			}
			if !containsStr(diff, axis) {
				diff = append(diff, axis)
			}
		}
	}
	sort.Strings(diff)
	return diff
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
