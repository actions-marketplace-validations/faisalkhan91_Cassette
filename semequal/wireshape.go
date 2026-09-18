package semequal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// WireShape returns a provider-agnostic signature of an SSE stream's GRAMMAR —
// the ordered sequence of event types — independent of volatile data values
// (ids, usage, timestamps). It distinguishes "bytes drifted but meaning
// identical" (same shape) from "the event grammar changed" (different shape):
// the WIRE-vs-SEMANTIC distinction at the framing level. Used by drift/conformance
// checks against a provider's wire behavior.
func WireShape(raw []byte) string {
	types := WireEvents(raw)
	return strings.Join(types, ",")
}

// WireEvents returns the ordered event-type sequence of an SSE stream. The type
// is the `event:` field when present (Anthropic), else the data JSON's "type"
// field (OpenAI Responses), else "[DONE]" / "data".
func WireEvents(raw []byte) []string {
	var types []string
	for _, ev := range parseSSE(raw) {
		typ := ev.typ
		if typ == "" {
			d := bytes.TrimSpace(ev.data)
			switch {
			case string(d) == "[DONE]":
				typ = "[DONE]"
			default:
				var m struct {
					Type string `json:"type"`
				}
				switch {
				case json.Unmarshal(d, &m) == nil && m.Type != "":
					typ = m.Type
				default:
					// OpenAI Chat Completions chunks carry no top-level "type";
					// derive a grammar token from the choice delta so the shape is
					// still sensitive to that wire grammar (role/content/tool_call/
					// finish) rather than collapsing every chunk to "data".
					typ = chatChunkToken(d)
				}
			}
		}
		types = append(types, typ)
	}
	return types
}

// chatChunkToken derives a grammar token from an OpenAI Chat Completions
// streaming chunk's first choice. It returns "data" when the payload is not a
// recognizable chat chunk.
func chatChunkToken(d []byte) string {
	var cc struct {
		Choices []struct {
			Delta struct {
				Role      string            `json:"role"`
				Content   *string           `json:"content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(d, &cc) != nil || len(cc.Choices) == 0 {
		return "data"
	}
	ch := cc.Choices[0]
	switch {
	case len(ch.Delta.ToolCalls) > 0:
		return "delta.tool_calls"
	case ch.Delta.Role != "":
		return "delta.role"
	case ch.FinishReason != nil && *ch.FinishReason != "":
		return "delta.finish:" + *ch.FinishReason
	case ch.Delta.Content != nil:
		return "delta.content"
	default:
		return "delta"
	}
}

// WireShapeDigest is a short hex digest of the wire shape.
func WireShapeDigest(raw []byte) string {
	sum := sha256.Sum256([]byte(WireShape(raw)))
	return hex.EncodeToString(sum[:])
}
