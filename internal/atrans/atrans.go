// Package atrans adapts an accumulated Anthropic SDK Message into the
// provider-agnostic semequal.Transcript. It is the SDK-coupling boundary: the
// core cassette library and semequal stay free of any provider SDK import, while
// this small package (used by tests and the demo) bridges the SDK to the shared
// semantic-equivalence type.
package atrans

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/semequal"
)

// FromMessage builds a semequal.Transcript from an accumulated Anthropic Message.
func FromMessage(m anthropic.Message) semequal.Transcript {
	t := semequal.Transcript{
		Role:         string(m.Role),
		FinishReason: string(m.StopReason),
	}
	var text string
	for _, block := range m.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "tool_use":
			args := string(block.Input)
			if c, ok := canon.Canonicalize(block.Input); ok {
				args = string(c)
			}
			t.ToolCalls = append(t.ToolCalls, semequal.ToolCall{Name: block.Name, Args: args})
		}
	}
	t.Text = text
	return t.Normalize()
}
