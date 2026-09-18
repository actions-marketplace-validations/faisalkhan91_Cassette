package analysis

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Usage is the token accounting decoded from a recorded response. It is
// deliberately NOT part of semequal.Transcript (usage is volatile and must not
// affect semantic digests); it is read straight from the recorded bytes.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
	// Known is false when the recording carries no usage data — e.g. an OpenAI
	// stream captured without stream_options.include_usage. Callers MUST treat
	// Known==false as "unknown", never as zero cost.
	Known bool
}

// Total returns input+output (excludes cache-accounting detail).
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }

// DecodeUsage extracts token usage from a recorded http interaction's response,
// across Anthropic Messages, OpenAI Chat Completions, OpenAI Responses, Google
// Gemini (usageMetadata), and Ollama (prompt_eval_count/eval_count), in both
// streaming and unary form. It scans every SSE data frame (or the unary JSON body)
// for the provider-specific usage fields.
func DecodeUsage(it *wirefmt.Interaction) Usage {
	if it.Kind != "http" || it.Response.Body == nil {
		return Usage{}
	}
	body := it.Response.Body.Bytes()
	var u Usage
	// set updates a field from the first existing of the given gjson paths.
	set := func(dst *int, j string, paths ...string) {
		for _, p := range paths {
			if v := gjson.Get(j, p); v.Exists() {
				*dst = int(v.Int())
				u.Known = true
				return
			}
		}
	}
	consider := func(j string) {
		// Anthropic message_start nests usage under "message.usage"; message_delta
		// carries top-level "usage.output_tokens". Gemini reports usageMetadata.*;
		// Ollama reports top-level prompt_eval_count/eval_count. Only one provider's
		// fields exist in any given frame, so the first-existing-wins set is safe.
		set(&u.InputTokens, j, "usage.input_tokens", "message.usage.input_tokens",
			"usageMetadata.promptTokenCount", "prompt_eval_count")
		set(&u.OutputTokens, j, "usage.output_tokens", "message.usage.output_tokens",
			"usageMetadata.candidatesTokenCount", "eval_count")
		set(&u.CacheRead, j, "usage.cache_read_input_tokens", "message.usage.cache_read_input_tokens",
			"usageMetadata.cachedContentTokenCount")
		set(&u.CacheWrite, j, "usage.cache_creation_input_tokens", "message.usage.cache_creation_input_tokens")
		// OpenAI Chat Completions: usage.prompt_tokens / completion_tokens.
		if v := gjson.Get(j, "usage.prompt_tokens"); v.Exists() {
			u.InputTokens = int(v.Int())
			u.Known = true
		}
		if v := gjson.Get(j, "usage.completion_tokens"); v.Exists() {
			u.OutputTokens = int(v.Int())
			u.Known = true
		}
		// OpenAI Responses: response.usage.input_tokens / output_tokens.
		if v := gjson.Get(j, "response.usage.input_tokens"); v.Exists() {
			u.InputTokens = int(v.Int())
			u.Known = true
		}
		if v := gjson.Get(j, "response.usage.output_tokens"); v.Exists() {
			u.OutputTokens = int(v.Int())
			u.Known = true
		}
	}

	if !it.Response.Streaming {
		consider(string(body))
		return u
	}
	for _, frame := range bytes.Split(body, []byte("\n\n")) {
		for _, line := range bytes.Split(frame, []byte("\n")) {
			line = bytes.TrimSpace(line)
			// Strip the SSE "data:" prefix if present; Ollama NDJSON lines have none.
			// Any line that is then valid JSON is a candidate (SSE "event:"/comment
			// lines and "[DONE]" are not valid JSON and fall through).
			line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(line) == 0 || string(line) == "[DONE]" || !gjson.ValidBytes(line) {
				continue
			}
			consider(string(line))
		}
	}
	return u
}

// RequestModel returns the model an interaction targeted: the request body's "model"
// field, or — for Gemini, which carries no body model — the model segment of the URL
// path (.../models/<model>:<action>). Returns "" if neither is present.
func RequestModel(it *wirefmt.Interaction) string {
	if it.Request.Body != nil {
		if m := strings.TrimSpace(gjson.GetBytes(it.Request.Body.Bytes(), "model").String()); m != "" {
			return m
		}
	}
	if i := strings.Index(it.Request.URL, "/models/"); i >= 0 {
		rest := it.Request.URL[i+len("/models/"):]
		if c := strings.IndexAny(rest, ":/?"); c >= 0 {
			rest = rest[:c]
		}
		return rest
	}
	return ""
}
