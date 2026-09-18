// Package otrans adapts an accumulated OpenAI Chat Completion into the
// provider-agnostic semequal.Transcript — the OpenAI counterpart of
// internal/atrans. It is the SDK-coupling boundary so core/semequal stay free of
// any provider SDK import.
package otrans

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/semequal"
)

// FromChatCompletion builds a semequal.Transcript from an accumulated OpenAI
// ChatCompletion (e.g. ChatCompletionAccumulator.ChatCompletion).
func FromChatCompletion(cc openai.ChatCompletion) semequal.Transcript {
	t := semequal.Transcript{Role: "assistant"}
	if len(cc.Choices) == 0 {
		return t.Normalize()
	}
	ch := cc.Choices[0]
	t.Text = ch.Message.Content
	t.FinishReason = string(ch.FinishReason)
	for _, tc := range ch.Message.ToolCalls {
		args := tc.Function.Arguments
		if c, ok := canon.Canonicalize([]byte(args)); ok {
			args = string(c)
		}
		t.ToolCalls = append(t.ToolCalls, semequal.ToolCall{Name: tc.Function.Name, Args: args})
	}
	return t.Normalize()
}

// FromResponse builds a semequal.Transcript from an OpenAI Responses API
// Response (e.g. the terminal response.completed event's Response).
func FromResponse(r responses.Response) semequal.Transcript {
	t := semequal.Transcript{Role: "assistant", FinishReason: string(r.Status)}
	var text string
	for _, item := range r.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				text += c.Text
			}
		case "function_call":
			fc := item.AsFunctionCall()
			args := fc.Arguments
			if c, ok := canon.Canonicalize([]byte(args)); ok {
				args = string(c)
			}
			t.ToolCalls = append(t.ToolCalls, semequal.ToolCall{Name: fc.Name, Args: args})
		}
	}
	t.Text = text
	return t.Normalize()
}
