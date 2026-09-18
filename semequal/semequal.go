// Package semequal is cassette's SEMANTIC-equivalence yardstick: it reduces a
// streamed LLM response to a normalized Transcript (role, assembled text, tool
// calls with canonicalized arguments, finish reason) with response-side volatile
// fields (ids, usage, timestamps, system_fingerprint, per-chunk ids) dropped, and
// reduces that to a stable SHA-256. Two runs are semantically equivalent iff
// their transcripts' digests are equal — regardless of SSE chunk boundaries or
// volatile fields.
//
// The SSE→Transcript decoders parse raw on-wire bytes directly (they do NOT use a
// provider SDK to accumulate), so this package is provider-SDK-free and usable as
// the single definition of "equivalent" across providers. Provider-SDK adapters
// (e.g. internal/atrans for Anthropic) produce the same Transcript type.
package semequal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/canon"
)

// ToolCall is one tool invocation with canonicalized arguments (parsed JSON, not
// a raw-string compare).
type ToolCall struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// Transcript is the normalized, volatile-field-free view of a model turn.
type Transcript struct {
	Role         string     `json:"role"`
	Text         string     `json:"text"`
	ToolCalls    []ToolCall `json:"tool_calls"`
	FinishReason string     `json:"finish_reason"`
}

// Normalize sorts tool calls deterministically and defaults the role, so two
// transcripts that differ only in incidental ordering compare equal.
func (t Transcript) Normalize() Transcript {
	out := t
	if out.Role == "" {
		out.Role = "assistant"
	}
	out.ToolCalls = append([]ToolCall(nil), t.ToolCalls...)
	sort.Slice(out.ToolCalls, func(i, j int) bool {
		if out.ToolCalls[i].Name != out.ToolCalls[j].Name {
			return out.ToolCalls[i].Name < out.ToolCalls[j].Name
		}
		return out.ToolCalls[i].Args < out.ToolCalls[j].Args
	})
	return out
}

// Digest returns a stable hex SHA-256 of the normalized transcript.
func (t Transcript) Digest() string {
	raw, _ := json.Marshal(t.Normalize())
	c, ok := canon.Canonicalize(raw)
	if !ok {
		c = raw
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:])
}

// String renders the transcript deterministically (for diffs).
func (t Transcript) String() string {
	t = t.Normalize()
	var b strings.Builder
	fmt.Fprintf(&b, "role: %s\n", t.Role)
	fmt.Fprintf(&b, "finish_reason: %s\n", t.FinishReason)
	fmt.Fprintf(&b, "text: %q\n", t.Text)
	for _, tc := range t.ToolCalls {
		fmt.Fprintf(&b, "tool_call: %s %s\n", tc.Name, tc.Args)
	}
	return b.String()
}

// CombinedDigest reduces an ordered multi-turn conversation to one digest.
func CombinedDigest(turns []Transcript) string {
	norm := make([]Transcript, len(turns))
	for i, t := range turns {
		norm[i] = t.Normalize()
	}
	raw, _ := json.Marshal(norm)
	c, ok := canon.Canonicalize(raw)
	if !ok {
		c = raw
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:])
}

// Equal reports whether two transcripts are semantically equivalent.
func Equal(a, b Transcript) bool { return a.Digest() == b.Digest() }

// Diff returns a human-readable line diff of two transcripts, or "" if equal.
func Diff(a, b Transcript) string {
	if Equal(a, b) {
		return ""
	}
	al := strings.Split(strings.TrimRight(a.String(), "\n"), "\n")
	bl := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	var d strings.Builder
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x == y {
			d.WriteString("  " + x + "\n")
			continue
		}
		if x != "" {
			d.WriteString("- " + x + "\n")
		}
		if y != "" {
			d.WriteString("+ " + y + "\n")
		}
	}
	return d.String()
}

// AssertSemanticEqual fails the test if got and want are not semantically
// equivalent, printing a transcript diff.
func AssertSemanticEqual(t testing.TB, got, want Transcript) {
	t.Helper()
	if !Equal(got, want) {
		t.Fatalf("semantic transcripts differ:\n%s", Diff(want, got))
	}
}

// --- raw SSE parsing -------------------------------------------------------

type sseEvent struct {
	typ  string
	data []byte
}

// parseSSE splits a raw text/event-stream byte slice into events the same way an
// SSE client does: `event:`/`data:` lines, data lines joined by '\n', dispatch on
// a blank line, comment/empty-name lines ignored.
func parseSSE(raw []byte) []sseEvent {
	var events []sseEvent
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	event := ""
	var data bytes.Buffer
	flush := func() {
		if event == "" && data.Len() == 0 {
			return
		}
		events = append(events, sseEvent{typ: event, data: append([]byte(nil), data.Bytes()...)})
		event = ""
		data.Reset()
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			flush()
			continue
		}
		name, value, _ := bytes.Cut(line, []byte(":"))
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		switch string(name) {
		case "":
			continue // comment
		case "event":
			event = string(value)
		case "data":
			data.Write(value)
			data.WriteByte('\n')
		}
	}
	flush()
	return events
}

// --- Anthropic decoder -----------------------------------------------------

// DecodeAnthropicSSE reassembles an Anthropic Messages SSE byte stream into a
// normalized Transcript, parsing the raw bytes directly (no SDK). If the stream
// carried a provider `error` event, it returns the partial transcript and a
// non-nil error.
func DecodeAnthropicSSE(raw []byte) (Transcript, error) {
	type block struct {
		typ  string
		name string
		text strings.Builder
		args strings.Builder
	}
	blocks := map[int]*block{}
	var order []int
	t := Transcript{Role: "assistant"}
	var streamErr error

	for _, ev := range parseSSE(raw) {
		switch ev.typ {
		case "content_block_start":
			var d struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if json.Unmarshal(ev.data, &d) != nil {
				continue
			}
			blocks[d.Index] = &block{typ: d.ContentBlock.Type, name: d.ContentBlock.Name}
			order = append(order, d.Index)
		case "content_block_delta":
			var d struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.data, &d) != nil {
				continue
			}
			b := blocks[d.Index]
			if b == nil {
				continue
			}
			switch d.Delta.Type {
			case "text_delta":
				b.text.WriteString(d.Delta.Text)
			case "input_json_delta":
				b.args.WriteString(d.Delta.PartialJSON)
			}
		case "message_delta":
			var d struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.data, &d) == nil && d.Delta.StopReason != "" {
				t.FinishReason = d.Delta.StopReason
			}
		case "error":
			var d struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(ev.data, &d)
			streamErr = fmt.Errorf("anthropic stream error: %s: %s", d.Error.Type, d.Error.Message)
		}
	}

	for _, i := range order {
		b := blocks[i]
		switch b.typ {
		case "text":
			t.Text += b.text.String()
		case "tool_use":
			args := b.args.String()
			if c, ok := canon.Canonicalize([]byte(args)); ok {
				args = string(c)
			}
			t.ToolCalls = append(t.ToolCalls, ToolCall{Name: b.name, Args: args})
		}
	}
	return t.Normalize(), streamErr
}

// --- OpenAI decoder --------------------------------------------------------

// DecodeOpenAISSE reassembles an OpenAI Chat Completions SSE byte stream
// (chat.completion.chunk frames terminated by `data: [DONE]`) into a normalized
// Transcript, parsing the raw bytes directly (no SDK). Content deltas are
// concatenated; tool-call argument fragments are concatenated per choice index;
// volatile fields (id, created, system_fingerprint) are ignored by construction.
func DecodeOpenAISSE(raw []byte) (Transcript, error) {
	type chunk struct {
		Choices []struct {
			Delta struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	type toolAcc struct {
		name string
		args strings.Builder
	}
	t := Transcript{Role: "assistant"}
	var text strings.Builder
	tools := map[int]*toolAcc{}
	var order []int

	for _, ev := range parseSSE(raw) {
		data := bytes.TrimSpace(ev.data)
		if len(data) == 0 || string(data) == "[DONE]" {
			continue
		}
		var c chunk
		if json.Unmarshal(data, &c) != nil || len(c.Choices) == 0 {
			continue
		}
		ch := c.Choices[0]
		text.WriteString(ch.Delta.Content)
		for _, tc := range ch.Delta.ToolCalls {
			a := tools[tc.Index]
			if a == nil {
				a = &toolAcc{}
				tools[tc.Index] = a
				order = append(order, tc.Index)
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			a.args.WriteString(tc.Function.Arguments)
		}
		if ch.FinishReason != "" {
			t.FinishReason = ch.FinishReason
		}
	}

	t.Text = text.String()
	for _, i := range order {
		a := tools[i]
		args := a.args.String()
		if c, ok := canon.Canonicalize([]byte(args)); ok {
			args = string(c)
		}
		t.ToolCalls = append(t.ToolCalls, ToolCall{Name: a.name, Args: args})
	}
	return t.Normalize(), nil
}

// DecodeOpenAIResponsesSSE reassembles an OpenAI Responses API SSE byte stream
// (typed events: response.output_text.delta, response.function_call_arguments.delta,
// response.output_item.added, response.completed) into a normalized Transcript,
// parsing the raw bytes directly (no SDK).
func DecodeOpenAIResponsesSSE(raw []byte) (Transcript, error) {
	type tacc struct {
		name string
		args strings.Builder
	}
	t := Transcript{Role: "assistant"}
	var text strings.Builder
	tools := map[int64]*tacc{}
	var order []int64

	for _, ev := range parseSSE(raw) {
		data := bytes.TrimSpace(ev.data)
		if len(data) == 0 {
			continue
		}
		var m struct {
			Type        string `json:"type"`
			Delta       string `json:"delta"`
			OutputIndex int64  `json:"output_index"`
			Item        struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"item"`
			Response struct {
				Status string `json:"status"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "response.output_text.delta":
			text.WriteString(m.Delta)
		case "response.output_item.added":
			if m.Item.Type == "function_call" {
				tools[m.OutputIndex] = &tacc{name: m.Item.Name}
				order = append(order, m.OutputIndex)
			}
		case "response.function_call_arguments.delta":
			if a := tools[m.OutputIndex]; a != nil {
				a.args.WriteString(m.Delta)
			}
		case "response.completed":
			if m.Response.Status != "" {
				t.FinishReason = m.Response.Status
			}
		}
	}

	t.Text = text.String()
	for _, i := range order {
		a := tools[i]
		args := a.args.String()
		if c, ok := canon.Canonicalize([]byte(args)); ok {
			args = string(c)
		}
		t.ToolCalls = append(t.ToolCalls, ToolCall{Name: a.name, Args: args})
	}
	return t.Normalize(), nil
}

// --- Gemini decoder ---------------------------------------------------------

// DecodeGeminiSSE reassembles a Google Gemini streamGenerateContent byte stream
// into a normalized Transcript, accepting BOTH framings the endpoint returns: the
// `alt=sse` framing (one GenerateContentResponse per `data:` frame) and the default
// top-level JSON-array framing (`[{…},\n{…}\n]`, streamed). Text parts concatenate;
// functionCall parts (args are a JSON object, not a string) become canonicalized
// ToolCalls; thinking/citation parts are dropped; finishReason stays provider-native.
// Tolerant of a stream cut mid-array.
func DecodeGeminiSSE(raw []byte) (Transcript, error) {
	var objs [][]byte
	if tr := bytes.TrimLeft(raw, " \t\r\n"); len(tr) > 0 && tr[0] == '[' {
		objs = splitTopLevelJSONObjects(raw)
	} else {
		for _, ev := range parseSSE(raw) {
			if d := bytes.TrimSpace(ev.data); len(d) > 0 {
				objs = append(objs, d)
			}
		}
	}

	t := Transcript{Role: "assistant"}
	var text strings.Builder
	for _, o := range objs {
		var g struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text         string `json:"text"`
						Thought      bool   `json:"thought"`
						FunctionCall *struct {
							Name string          `json:"name"`
							Args json.RawMessage `json:"args"`
						} `json:"functionCall"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
		}
		if json.Unmarshal(o, &g) != nil || len(g.Candidates) == 0 {
			continue
		}
		c := g.Candidates[0]
		for _, part := range c.Content.Parts {
			switch {
			case part.FunctionCall != nil:
				args := string(part.FunctionCall.Args)
				if cc, ok := canon.Canonicalize(part.FunctionCall.Args); ok {
					args = string(cc)
				}
				t.ToolCalls = append(t.ToolCalls, ToolCall{Name: part.FunctionCall.Name, Args: args})
			case part.Text != "" && !part.Thought:
				text.WriteString(part.Text)
			}
		}
		if c.FinishReason != "" {
			t.FinishReason = c.FinishReason
		}
	}
	t.Text = text.String()
	return t.Normalize(), nil
}

// splitTopLevelJSONObjects extracts each top-level {…} object from a (possibly
// truncated) JSON array, ignoring the array brackets/commas and respecting string
// escapes. A trailing incomplete object (stream cut mid-element) is dropped.
func splitTopLevelJSONObjects(raw []byte) [][]byte {
	var out [][]byte
	depth, start := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					out = append(out, raw[start:i+1])
					start = -1
				}
			}
		}
	}
	return out
}

// --- Ollama native NDJSON decoder -------------------------------------------

// DecodeOllamaNDJSON reassembles an Ollama native /api/chat byte stream into a
// normalized Transcript. Ollama is newline-delimited JSON (one object per line,
// not SSE): each line is {"message":{"role","content","tool_calls":[...]},"done":
// bool,"done_reason":...}. Content is concatenated across lines; tool_calls[].
// function{name,arguments(object)} become canonicalized ToolCalls; done_reason is
// the provider-native finish reason; telemetry (eval_count, *_duration, created_at)
// is volatile and dropped (it never enters the Transcript).
func DecodeOllamaNDJSON(raw []byte) (Transcript, error) {
	t := Transcript{Role: "assistant"}
	var text strings.Builder
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var o struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			DoneReason string `json:"done_reason"`
		}
		if json.Unmarshal(line, &o) != nil {
			continue
		}
		text.WriteString(o.Message.Content)
		for _, tc := range o.Message.ToolCalls {
			args := string(tc.Function.Arguments)
			if cc, ok := canon.Canonicalize(tc.Function.Arguments); ok {
				args = string(cc)
			}
			t.ToolCalls = append(t.ToolCalls, ToolCall{Name: tc.Function.Name, Args: args})
		}
		if o.DoneReason != "" {
			t.FinishReason = o.DoneReason
		}
	}
	t.Text = text.String()
	return t.Normalize(), nil
}
