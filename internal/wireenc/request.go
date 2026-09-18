package wireenc

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// ConvoMsg is the dialect-neutral conversation view that request transpilation
// flows through: a role plus its flattened text. Tool-call history is flattened
// to text — request-shape fidelity is not the goal, a stable coherent target
// request is.
type ConvoMsg struct {
	Role string
	Text string
}

// DecodeRequest pulls (model, system, messages-as-text) out of a request body in
// dialect p — the inverse of EncodeRequest. It is best-effort over the common
// text-conversation shape. Routing through the dialect registry keeps request and
// response dialect handling in one place (see dialects in wireenc.go).
func DecodeRequest(p Provider, body []byte) (model, system string, msgs []ConvoMsg) {
	model = gjson.GetBytes(body, "model").String()
	system, msgs = dialectFor(p).reqDecode(body)
	return model, system, msgs
}

// EncodeRequest emits a deterministic target-dialect request body from the neutral
// conversation view. Go's json.Marshal sorts map keys, so the bytes are stable.
func EncodeRequest(p Provider, model, system string, msgs []ConvoMsg) []byte {
	if model == "" {
		model = "model"
	}
	return dialectFor(p).reqEncode(model, system, msgs)
}

// --- request decoders (one per body shape) ---

// decodeReqMessages handles the messages[] shape shared by Anthropic, OpenAI Chat,
// and Ollama (an inline system-role message is lifted into system).
func decodeReqMessages(body []byte) (system string, msgs []ConvoMsg) {
	system = contentText(gjson.GetBytes(body, "system"))
	gjson.GetBytes(body, "messages").ForEach(func(_, m gjson.Result) bool {
		role := roleOr(m.Get("role").String(), "user")
		if role == "system" {
			if t := contentText(m.Get("content")); t != "" {
				system = t
			}
			return true
		}
		msgs = append(msgs, ConvoMsg{Role: role, Text: contentText(m.Get("content"))})
		return true
	})
	return system, msgs
}

func decodeReqGemini(body []byte) (system string, msgs []ConvoMsg) {
	// Gemini carries the model in the URL path, the system prompt in
	// systemInstruction, and turns in contents[].parts[].text.
	system = contentText(gjson.GetBytes(body, "systemInstruction.parts"))
	gjson.GetBytes(body, "contents").ForEach(func(_, m gjson.Result) bool {
		role := roleOr(m.Get("role").String(), "user")
		if role == "model" {
			role = "assistant"
		}
		msgs = append(msgs, ConvoMsg{Role: role, Text: contentText(m.Get("parts"))})
		return true
	})
	return system, msgs
}

func decodeReqResponses(body []byte) (system string, msgs []ConvoMsg) {
	system = gjson.GetBytes(body, "instructions").String()
	input := gjson.GetBytes(body, "input")
	if input.Type == gjson.String {
		return system, []ConvoMsg{{Role: "user", Text: input.String()}}
	}
	input.ForEach(func(_, m gjson.Result) bool {
		msgs = append(msgs, ConvoMsg{Role: roleOr(m.Get("role").String(), "user"), Text: contentText(m.Get("content"))})
		return true
	})
	return system, msgs
}

// --- request encoders (one per body shape) ---

func encodeReqGemini(model, system string, msgs []ConvoMsg) []byte {
	var contents []map[string]any
	for _, m := range msgs {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role,
			"parts": []map[string]any{{"text": m.Text}}})
	}
	body := map[string]any{"contents": contents}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	b, _ := json.Marshal(body)
	return b
}

func encodeReqResponses(model, system string, msgs []ConvoMsg) []byte {
	var input []map[string]any
	for _, m := range msgs {
		input = append(input, map[string]any{"role": m.Role,
			"content": []map[string]any{{"type": "input_text", "text": m.Text}}})
	}
	body := map[string]any{"model": model, "input": input, "stream": true}
	if system != "" {
		body["instructions"] = system
	}
	b, _ := json.Marshal(body)
	return b
}

// encodeReqMessages emits the OpenAI Chat / Ollama messages[] shape (system is a
// leading system-role message).
func encodeReqMessages(model, system string, msgs []ConvoMsg) []byte {
	var out []map[string]any
	if system != "" {
		out = append(out, map[string]any{"role": "system", "content": system})
	}
	for _, m := range msgs {
		out = append(out, map[string]any{"role": m.Role, "content": m.Text})
	}
	b, _ := json.Marshal(map[string]any{"model": model, "messages": out, "stream": true})
	return b
}

// encodeReqAnthropic is the messages[] shape with a top-level system field and the
// required max_tokens.
func encodeReqAnthropic(model, system string, msgs []ConvoMsg) []byte {
	var out []map[string]any
	for _, m := range msgs {
		out = append(out, map[string]any{"role": m.Role, "content": m.Text})
	}
	body := map[string]any{"model": model, "max_tokens": 1024, "messages": out, "stream": true}
	if system != "" {
		body["system"] = system
	}
	b, _ := json.Marshal(body)
	return b
}

// --- shared helpers ---

// contentText flattens a message "content" field (string, or an array of blocks
// each with a "text"/"input_text" field) into a single string.
func contentText(c gjson.Result) string {
	if c.Type == gjson.String {
		return c.String()
	}
	var s string
	c.ForEach(func(_, b gjson.Result) bool {
		if t := b.Get("text"); t.Exists() {
			s += t.String()
		}
		return true
	})
	return s
}

func roleOr(r, def string) string {
	if r == "" {
		return def
	}
	return r
}
