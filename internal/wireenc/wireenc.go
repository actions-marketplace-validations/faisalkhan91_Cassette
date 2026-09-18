// Package wireenc is the inverse of semequal.Decode*: it re-emits a byte-
// plausible, decoder-round-trippable SSE stream from a provider-agnostic
// semequal.Transcript. It is the keystone that lets a recording be EDITED at the
// semantic layer (graft) or PARAMETERIZED (distill) and written back as a valid
// replayable cassette. Output is deterministic — all volatile fields (ids/model/
// usage) come from a caller-supplied Envelope, never from time/rand — so encoding
// the same (Transcript, Envelope) always yields identical bytes.
package wireenc

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"strconv"
	"strings"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/semequal"
)

// Provider selects the wire dialect to emit.
type Provider int

const (
	Anthropic Provider = iota
	OpenAIChat
	OpenAIResponses
	Gemini
	Ollama
)

// dialect is the single source of truth for one provider wire format: how to
// recognize it from a request URL, its stable name, whether it supports a sampling
// seed, the canonical request path to author/port INTO it, and the matched
// request- and response-codec pairs. Adding a provider means adding ONE entry here
// instead of touching parallel switches in ProviderForURL, Encode, Decode,
// ProviderName, the sampling-knob extractor, and the author/port request ladders —
// the drift that let a Gemini turn silently be labelled "anthropic". (Fields are
// named in the literals below so a new field can't be mis-positioned.)
type dialect struct {
	provider        Provider
	name            string                                     // stable human/JSON label
	seedSupported   bool                                       // exposes a sampling seed
	urlMarkers      []string                                   // any-substring match on the path; nil = fallback
	requestPath     string                                     // canonical path to author/port into this dialect
	defaultUpstream string                                     // canonical live host for zero-config record; "" = ambiguous/self-hosted
	decode          func([]byte) (semequal.Transcript, error)  // response wire → transcript
	encode          func(semequal.Transcript, Envelope) []byte // transcript → response wire
	reqDecode       func([]byte) (system string, msgs []ConvoMsg)
	reqEncode       func(model, system string, msgs []ConvoMsg) []byte
}

// dialects is ordered by ProviderForURL precedence; the markerless entry
// (Anthropic) is the fallback. The URL is path-only (host scrubbed), so routing
// keys off the path — Gemini's `…:streamGenerateContent`/`…:generateContent` action
// suffix is unique and covers both generativelanguage.googleapis.com and Vertex
// *-aiplatform paths. Each requestPath is chosen so ProviderForURL(requestPath) ==
// provider (asserted by TestDialectRegistry_RequestPathRoundTrips), so a recording
// authored/ported into a dialect routes back to that same dialect.
var dialects = []dialect{
	{
		provider: Gemini, name: "gemini", urlMarkers: []string{":streamGenerateContent", ":generateContent"},
		requestPath: "/v1beta/models/gemini:streamGenerateContent", defaultUpstream: "https://generativelanguage.googleapis.com",
		decode: semequal.DecodeGeminiSSE, encode: EncodeGeminiSSE,
		reqDecode: decodeReqGemini, reqEncode: encodeReqGemini,
	},
	{
		provider: Ollama, name: "ollama", urlMarkers: []string{"/api/chat"}, requestPath: "/api/chat", defaultUpstream: "http://localhost:11434",
		decode: semequal.DecodeOllamaNDJSON, encode: EncodeOllamaNDJSON,
		reqDecode: decodeReqMessages, reqEncode: encodeReqMessages,
	},
	{
		// Responses is OpenAI-proper, so its upstream is unambiguous.
		provider: OpenAIResponses, name: "openai-responses", urlMarkers: []string{"/responses"}, requestPath: "/v1/responses", defaultUpstream: "https://api.openai.com",
		decode: semequal.DecodeOpenAIResponsesSSE, encode: EncodeOpenAIResponsesSSE,
		reqDecode: decodeReqResponses, reqEncode: encodeReqResponses,
	},
	{
		// /chat/completions is the OpenAI-COMPATIBLE family path (OpenAI, Azure, vLLM,
		// Together, Groq, …) — the upstream is ambiguous, so leave it blank (zero-config
		// asks the user for --upstream rather than guessing wrong).
		provider: OpenAIChat, name: "openai-chat", seedSupported: true, urlMarkers: []string{"/chat/completions"}, requestPath: "/v1/chat/completions",
		decode: semequal.DecodeOpenAISSE, encode: EncodeOpenAISSE,
		reqDecode: decodeReqMessages, reqEncode: encodeReqMessages,
	},
	{
		provider: Anthropic, name: "anthropic", urlMarkers: nil, requestPath: "/v1/messages", defaultUpstream: "https://api.anthropic.com", // fallback
		decode: semequal.DecodeAnthropicSSE, encode: EncodeAnthropicSSE,
		reqDecode: decodeReqMessages, reqEncode: encodeReqAnthropic,
	},
}

// dialectFor returns the registry entry for p, falling back to Anthropic (the
// historical default) for an unknown provider.
func dialectFor(p Provider) dialect {
	for _, d := range dialects {
		if d.provider == p {
			return d
		}
	}
	return dialects[len(dialects)-1]
}

// ProviderForURL picks the dialect from a request URL using the SAME precedence
// as the decoders' routing, so record-side and encode-side always agree.
func ProviderForURL(url string) Provider {
	for _, d := range dialects {
		for _, m := range d.urlMarkers {
			if strings.Contains(url, m) {
				return d.provider
			}
		}
	}
	return Anthropic
}

// Name is the stable, human/JSON-facing label for a wire dialect.
func Name(p Provider) string { return dialectFor(p).name }

// SeedSupported reports whether the dialect exposes a sampling seed parameter.
func SeedSupported(p Provider) bool { return dialectFor(p).seedSupported }

// RequestPath is the canonical request path for the dialect — used when authoring
// or porting a recording INTO this dialect. By construction
// ProviderForURL(RequestPath(p)) == p.
func RequestPath(p Provider) string { return dialectFor(p).requestPath }

// UpstreamForURL returns the canonical live upstream host to record against for a
// request path, for the zero-config front door (`cassette up`). It POSITIVELY
// matches a known dialect path — it never falls back, so an unknown path or the
// ambiguous OpenAI-compatible /chat/completions path returns ok=false (the caller
// then asks for an explicit --upstream rather than guessing the wrong provider).
func UpstreamForURL(url string) (string, bool) {
	for _, d := range dialects {
		for _, m := range d.urlMarkers {
			if d.defaultUpstream != "" && strings.Contains(url, m) {
				return d.defaultUpstream, true
			}
		}
	}
	// Anthropic is the markerless fallback dialect; recognize its canonical path
	// positively so a real /v1/messages records to api.anthropic.com, while a truly
	// unknown path still returns false.
	if strings.Contains(url, "/v1/messages") {
		return dialectFor(Anthropic).defaultUpstream, true
	}
	return "", false
}

// ProviderByName resolves a stable dialect name (as returned by Name) back to its
// Provider; ok is false for an unknown name.
func ProviderByName(name string) (Provider, bool) {
	for _, d := range dialects {
		if d.name == name {
			return d.provider, true
		}
	}
	return 0, false
}

// ProviderNames lists every dialect's stable name in registry (routing-precedence)
// order — for help text and error messages that must not drift from the registry.
func ProviderNames() []string {
	names := make([]string, len(dialects))
	for i, d := range dialects {
		names[i] = d.name
	}
	return names
}

// Envelope carries the volatile fields the decoders drop, so the caller controls
// them and the output stays deterministic. Zero values get fixed defaults.
type Envelope struct {
	MessageID    string
	ModelID      string
	ToolCallIDs  []string
	InputTokens  int
	OutputTokens int
}

func (e Envelope) msgID(def string) string {
	if e.MessageID != "" {
		return e.MessageID
	}
	return def
}

func (e Envelope) model() string {
	if e.ModelID != "" {
		return e.ModelID
	}
	return "model"
}

func (e Envelope) toolID(i int, prefix string) string {
	if i < len(e.ToolCallIDs) && e.ToolCallIDs[i] != "" {
		return e.ToolCallIDs[i]
	}
	return prefix + strconv.Itoa(i)
}

// Encode emits the wire stream for a transcript in the given dialect.
func Encode(p Provider, t semequal.Transcript, env Envelope) []byte {
	return dialectFor(p).encode(t, env)
}

// Decode parses a wire stream in dialect p into a normalized Transcript — the
// exact inverse of Encode, routing to the same semequal decoders so encode and
// decode are a matched pair. It exists so callers (e.g. codec verification) can
// run the decode→encode→decode loop without re-deriving the provider routing.
func Decode(p Provider, raw []byte) (semequal.Transcript, error) {
	return dialectFor(p).decode(raw)
}

// RoundTripResult reports the outcome of the decode→encode→decode codec check.
type RoundTripResult struct {
	// Decoded is the first decode of the input stream.
	Decoded semequal.Transcript
	// Redecoded is the decode of Encode(Decoded) — equal to Decoded iff the codec
	// is behavior-preserving for this stream.
	Redecoded semequal.Transcript
	// Stable reports the fixpoint: Decoded and Redecoded are semantically equal.
	Stable bool
	// Deterministic reports that encoding Decoded twice yields byte-identical
	// streams (the encoder injects no time/rand).
	Deterministic bool
}

// RoundTrip exercises the codec fixpoint for a raw wire stream in dialect p:
// decode → encode (twice, to check determinism) → decode, reporting whether the
// re-decoded transcript is semantically equal to the first decode (Stable) and
// whether encoding is deterministic. If the input stream carried a provider error
// event, the first decode's error is returned with a best-effort Decoded set and
// the loop is not run (the encoder has no error path by design).
func RoundTrip(p Provider, raw []byte) (RoundTripResult, error) {
	t1, err := Decode(p, raw)
	if err != nil {
		return RoundTripResult{Decoded: t1}, err
	}
	a := Encode(p, t1, Envelope{})
	b := Encode(p, t1, Envelope{})
	t2, err2 := Decode(p, a)
	return RoundTripResult{
		Decoded:       t1,
		Redecoded:     t2,
		Stable:        semequal.Equal(t1, t2),
		Deterministic: bytes.Equal(a, b),
	}, err2
}

func j(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

func frame(b *strings.Builder, event, data string) {
	if event != "" {
		b.WriteString("event: " + event + "\n")
	}
	b.WriteString("data: " + data + "\n\n")
}

// Reframe returns n byte streams in dialect p that all decode to the SAME
// Transcript as t, but differ in incidental framing: a varied envelope (ids,
// usage) and — for Anthropic — text split across several text_delta frames at
// random rune boundaries with the tool-call blocks emitted in a shuffled order.
// It is the inverse of mutate (which corrupts): every output is a VALID alternative
// framing, so it stress-tests a consumer's SSE/agent parser against chunkings a
// single recording never produced. Deterministic for a given seed.
func Reframe(p Provider, t semequal.Transcript, seed int64, n int) [][]byte {
	t = t.Normalize()
	rng := rand.New(rand.NewSource(seed))
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		env := Envelope{
			MessageID:    "msg_reframe_" + strconv.Itoa(rng.Intn(1<<30)),
			OutputTokens: rng.Intn(500), // volatile in the digest; just varies the bytes
		}
		if p == Anthropic {
			out = append(out, reframeAnthropic(t, env, rng))
		} else {
			out = append(out, Encode(p, t, env))
		}
	}
	return out
}

// reframeAnthropic emits an Anthropic stream with the text split into 1..4
// text_delta frames and the tool blocks in a shuffled order.
func reframeAnthropic(t semequal.Transcript, env Envelope, rng *rand.Rand) []byte {
	var b strings.Builder
	frame(&b, "message_start", j(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": env.msgID("msg_0000"), "type": "message", "role": "assistant",
			"model": env.model(), "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": env.InputTokens, "output_tokens": 1},
		},
	}))
	idx := 0
	if t.Text != "" {
		frame(&b, "content_block_start", j(map[string]any{"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "text", "text": ""}}))
		for _, chunk := range splitRunes(t.Text, rng) {
			frame(&b, "content_block_delta", j(map[string]any{"type": "content_block_delta", "index": idx,
				"delta": map[string]any{"type": "text_delta", "text": chunk}}))
		}
		frame(&b, "content_block_stop", j(map[string]any{"type": "content_block_stop", "index": idx}))
		idx++
	}
	order := rng.Perm(len(t.ToolCalls))
	for _, ti := range order {
		tc := t.ToolCalls[ti]
		frame(&b, "content_block_start", j(map[string]any{"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "tool_use", "id": env.toolID(ti, "toolu_"), "name": tc.Name, "input": map[string]any{}}}))
		frame(&b, "content_block_delta", j(map[string]any{"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": canonArgs(tc.Args)}}))
		frame(&b, "content_block_stop", j(map[string]any{"type": "content_block_stop", "index": idx}))
		idx++
	}
	delta := map[string]any{"stop_sequence": nil}
	if t.FinishReason != "" {
		delta["stop_reason"] = t.FinishReason
	}
	frame(&b, "message_delta", j(map[string]any{"type": "message_delta",
		"delta": delta, "usage": map[string]any{"output_tokens": env.OutputTokens}}))
	frame(&b, "message_stop", j(map[string]any{"type": "message_stop"}))
	return []byte(b.String())
}

// splitRunes splits s into 1..4 contiguous chunks at random rune boundaries.
func splitRunes(s string, rng *rand.Rand) []string {
	r := []rune(s)
	if len(r) < 2 {
		return []string{s}
	}
	cuts := rng.Intn(3) + 1 // 1..3 internal cuts → 2..4 chunks
	points := map[int]bool{}
	for i := 0; i < cuts; i++ {
		points[1+rng.Intn(len(r)-1)] = true
	}
	var chunks []string
	prev := 0
	for p := 1; p < len(r); p++ {
		if points[p] {
			chunks = append(chunks, string(r[prev:p]))
			prev = p
		}
	}
	chunks = append(chunks, string(r[prev:]))
	return chunks
}

func canonArgs(s string) string {
	if c, ok := canon.Canonicalize([]byte(s)); ok {
		return string(c)
	}
	return s
}

// argsRaw returns canonical tool arguments as raw JSON, preserving number text so
// large integers don't lose precision through a float64 round-trip (defaults to {}
// for empty/invalid). Emit this rather than unmarshalling to `any` and re-encoding.
func argsRaw(s string) json.RawMessage {
	c := canonArgs(s)
	if c == "" || !json.Valid([]byte(c)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(c)
}

// EncodeAnthropicSSE emits an Anthropic Messages stream.
func EncodeAnthropicSSE(t semequal.Transcript, env Envelope) []byte {
	t = t.Normalize()
	var b strings.Builder
	frame(&b, "message_start", j(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": env.msgID("msg_0000"), "type": "message", "role": "assistant",
			"model": env.model(), "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": env.InputTokens, "output_tokens": 1},
		},
	}))
	idx := 0
	if t.Text != "" {
		frame(&b, "content_block_start", j(map[string]any{"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "text", "text": ""}}))
		frame(&b, "content_block_delta", j(map[string]any{"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "text_delta", "text": t.Text}}))
		frame(&b, "content_block_stop", j(map[string]any{"type": "content_block_stop", "index": idx}))
		idx++
	}
	for i, tc := range t.ToolCalls {
		frame(&b, "content_block_start", j(map[string]any{"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "tool_use", "id": env.toolID(i, "toolu_"), "name": tc.Name, "input": map[string]any{}}}))
		frame(&b, "content_block_delta", j(map[string]any{"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": canonArgs(tc.Args)}}))
		frame(&b, "content_block_stop", j(map[string]any{"type": "content_block_stop", "index": idx}))
		idx++
	}
	// Emit stop_reason faithfully: omit it when the transcript has none, so the
	// round-trip preserves an empty FinishReason rather than inventing one.
	delta := map[string]any{"stop_sequence": nil}
	if t.FinishReason != "" {
		delta["stop_reason"] = t.FinishReason
	}
	frame(&b, "message_delta", j(map[string]any{"type": "message_delta",
		"delta": delta, "usage": map[string]any{"output_tokens": env.OutputTokens}}))
	frame(&b, "message_stop", j(map[string]any{"type": "message_stop"}))
	return []byte(b.String())
}

// EncodeOpenAISSE emits an OpenAI Chat Completions stream (data-only frames).
func EncodeOpenAISSE(t semequal.Transcript, env Envelope) []byte {
	t = t.Normalize()
	var b strings.Builder
	chunk := func(delta map[string]any, finish any) {
		frame(&b, "", j(map[string]any{
			"id": env.msgID("chatcmpl_0000"), "object": "chat.completion.chunk", "created": 0,
			"model":   env.model(),
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}))
	}
	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	if t.Text != "" {
		chunk(map[string]any{"content": t.Text}, nil)
	}
	for i, tc := range t.ToolCalls {
		chunk(map[string]any{"tool_calls": []any{map[string]any{"index": i, "id": env.toolID(i, "call_"),
			"type": "function", "function": map[string]any{"name": tc.Name, "arguments": ""}}}}, nil)
		chunk(map[string]any{"tool_calls": []any{map[string]any{"index": i,
			"function": map[string]any{"arguments": canonArgs(tc.Args)}}}}, nil)
	}
	// finish_reason faithfully reflects the transcript: null when it has none.
	var finish any
	if t.FinishReason != "" {
		finish = t.FinishReason
	}
	chunk(map[string]any{}, finish)
	// Usage carrier (the stream_options.include_usage final chunk: empty choices +
	// a usage object). Emitted only when the caller supplied token counts, so the
	// default zero-token output stays byte-identical.
	if env.InputTokens > 0 || env.OutputTokens > 0 {
		frame(&b, "", j(map[string]any{
			"id": env.msgID("chatcmpl_0000"), "object": "chat.completion.chunk", "created": 0,
			"model": env.model(), "choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":     env.InputTokens,
				"completion_tokens": env.OutputTokens,
				"total_tokens":      env.InputTokens + env.OutputTokens,
			},
		}))
	}
	b.WriteString("data: [DONE]\n\n")
	return []byte(b.String())
}

// EncodeOpenAIResponsesSSE emits an OpenAI Responses typed-event stream.
func EncodeOpenAIResponsesSSE(t semequal.Transcript, env Envelope) []byte {
	t = t.Normalize()
	var b strings.Builder
	id := env.msgID("resp_0000")
	frame(&b, "response.created", j(map[string]any{"type": "response.created",
		"response": map[string]any{"id": id, "status": "in_progress"}}))
	oi := 0
	if t.Text != "" {
		frame(&b, "response.output_item.added", j(map[string]any{"type": "response.output_item.added",
			"output_index": oi, "item": map[string]any{"type": "message", "id": "msg_0", "role": "assistant", "content": []any{}}}))
		frame(&b, "response.output_text.delta", j(map[string]any{"type": "response.output_text.delta",
			"item_id": "msg_0", "output_index": oi, "delta": t.Text}))
		oi++
	}
	for i, tc := range t.ToolCalls {
		fcID := env.toolID(i, "fc_")
		frame(&b, "response.output_item.added", j(map[string]any{"type": "response.output_item.added",
			"output_index": oi, "item": map[string]any{"type": "function_call", "id": fcID,
				"call_id": "call_" + strconv.Itoa(i), "name": tc.Name, "arguments": ""}}))
		frame(&b, "response.function_call_arguments.delta", j(map[string]any{"type": "response.function_call_arguments.delta",
			"item_id": fcID, "output_index": oi, "delta": canonArgs(tc.Args)}))
		oi++
	}
	// status faithfully reflects FinishReason (empty stays empty on round-trip).
	completed := map[string]any{"id": id, "status": t.FinishReason}
	// Usage on the terminal event, only when supplied — keeps the zero-token default
	// byte-identical.
	if env.InputTokens > 0 || env.OutputTokens > 0 {
		completed["usage"] = map[string]any{
			"input_tokens":  env.InputTokens,
			"output_tokens": env.OutputTokens,
			"total_tokens":  env.InputTokens + env.OutputTokens,
		}
	}
	frame(&b, "response.completed", j(map[string]any{"type": "response.completed", "response": completed}))
	return []byte(b.String())
}

// EncodeOllamaNDJSON emits a canonical Ollama /api/chat NDJSON stream: a content
// line carrying text + tool calls (done:false), then a terminal done line with the
// finish reason. The decoder accumulates both into the same Transcript. Telemetry
// is fixed/omitted so the bytes are deterministic.
func EncodeOllamaNDJSON(t semequal.Transcript, env Envelope) []byte {
	t = t.Normalize()
	msg := map[string]any{"role": "assistant", "content": t.Text}
	if len(t.ToolCalls) > 0 {
		var calls []any
		for _, tc := range t.ToolCalls {
			calls = append(calls, map[string]any{"function": map[string]any{"name": tc.Name, "arguments": argsRaw(tc.Args)}})
		}
		msg["tool_calls"] = calls
	}
	var b strings.Builder
	b.WriteString(j(map[string]any{"model": env.model(), "message": msg, "done": false}))
	b.WriteByte('\n')
	done := map[string]any{"model": env.model(), "message": map[string]any{"role": "assistant", "content": ""}, "done": true}
	if t.FinishReason != "" {
		done["done_reason"] = t.FinishReason
	}
	b.WriteString(j(done))
	b.WriteByte('\n')
	return []byte(b.String())
}

// EncodeGeminiSSE emits a Gemini streamGenerateContent stream in the canonical
// `alt=sse` framing (a single `data:` frame carrying one GenerateContentResponse).
// The decoder accepts BOTH this SSE framing and the default top-level JSON-array
// framing, so canonicalizing an array-framed recording normalizes it to SSE while
// preserving the transcript. functionCall.args is emitted as a JSON object (not a
// string), matching the Gemini wire shape.
func EncodeGeminiSSE(t semequal.Transcript, env Envelope) []byte {
	t = t.Normalize()
	var parts []any
	if t.Text != "" {
		parts = append(parts, map[string]any{"text": t.Text})
	}
	for _, tc := range t.ToolCalls {
		parts = append(parts, map[string]any{"functionCall": map[string]any{"name": tc.Name, "args": argsRaw(tc.Args)}})
	}
	cand := map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "index": 0}
	if t.FinishReason != "" {
		cand["finishReason"] = t.FinishReason
	}
	var b strings.Builder
	frame(&b, "", j(map[string]any{
		"candidates":    []any{cand},
		"modelVersion":  env.model(),
		"responseId":    env.msgID("gemini_0000"),
		"usageMetadata": map[string]any{"promptTokenCount": env.InputTokens, "candidatesTokenCount": env.OutputTokens},
	}))
	return []byte(b.String())
}
