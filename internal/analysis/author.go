package analysis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

// Screenplay is a terse, human-authored description of a conversation that
// Compile turns into a fully valid, replayable cassette — no live capture, no API
// key, no network. It is the from-scratch generalization of graft: where graft
// edits one recorded turn, a screenplay AUTHORS a whole multi-turn run from
// intent. Only possible because cassette owns a round-trippable Transcript⇄wire
// codec (internal/wireenc).
type Screenplay struct {
	// Provider selects the wire dialect: "anthropic" (default), "openai-chat",
	// or "openai-responses".
	Provider string `yaml:"provider"`
	// Model is echoed into each synthesized request and response envelope.
	Model string           `yaml:"model"`
	Turns []ScreenplayTurn `yaml:"turns"`
}

// ScreenplayTurn is one request/response pair: the user prompt added this turn and
// the assistant reply to synthesize.
type ScreenplayTurn struct {
	// Kind selects the turn type: "" or "http" (the default — an LLM HTTP/SSE turn)
	// or "mcp" (a recorded MCP JSON-RPC call). MCP turns ignore the HTTP-only fields
	// and use Method/Tool/Args/Result/ToolError instead.
	Kind      string           `yaml:"kind"`
	User      string           `yaml:"user"`
	Text      string           `yaml:"text"`
	ToolCalls []ScreenplayTool `yaml:"tool_calls"`
	Finish    string           `yaml:"finish"`
	// Request optionally overrides the synthesized request body with a raw JSON
	// object, for exact control over the match key (e.g. to mirror a real SDK).
	Request string `yaml:"request"`
	// URL optionally overrides the request path (default: the provider's path).
	URL string `yaml:"url"`
	// InputTokens/OutputTokens optionally stamp the response's usage block, so an
	// authored corpus can exercise cost/dashboard/budget tooling (default 0).
	// Anthropic, OpenAI (Chat + Responses), and Gemini encoders emit the usage;
	// Ollama's native NDJSON intentionally carries no telemetry, so authored Ollama
	// turns report usage as "unknown" (never zero) downstream.
	InputTokens  int `yaml:"input_tokens"`
	OutputTokens int `yaml:"output_tokens"`

	// --- MCP turns (Kind: mcp) ---
	// Method is the JSON-RPC method: "tools/call" (default) or "tools/list".
	Method string `yaml:"method"`
	// Tool is the tool name for a tools/call turn.
	Tool string `yaml:"tool"`
	// Args is the raw JSON arguments (tools/call) or list params (tools/list);
	// defaults to "{}". The match key is derived from the tool + canonical args,
	// identical to the in-process MCP recorder, so it replays through `mcp-serve`.
	Args string `yaml:"args"`
	// Result is the raw JSON result object (a CallToolResult or ListToolsResult). If
	// empty, one is synthesized from Text/ToolError/ToolCalls.
	Result string `yaml:"result"`
	// ToolError, when set, synthesizes an error CallToolResult (isError: true) whose
	// text content is this message.
	ToolError string `yaml:"tool_error"`
}

// ScreenplayTool is one authored tool call with canonical JSON arguments.
type ScreenplayTool struct {
	Name string `yaml:"name"`
	Args string `yaml:"args"`
}

// Compile turns a screenplay into a replayable cassette. Each turn synthesizes a
// deterministic provider request body (accumulating the conversation so far,
// unless overridden by Turn.Request) and encodes the assistant transcript into a
// wire stream via internal/wireenc. Match keys are computed so the result replays
// without a rekey. The synthesized request body is a deterministic placeholder
// (model + accumulated messages + stream); for byte-fidelity with a specific SDK,
// supply Turn.Request explicitly.
func Compile(s Screenplay) (*wirefmt.File, error) {
	model := s.Model
	if model == "" {
		model = "model"
	}
	if len(s.Turns) == 0 {
		return nil, fmt.Errorf("screenplay has no turns")
	}

	// Provider routing is only needed for HTTP turns; a pure-MCP screenplay needs no
	// provider, so resolve it lazily on the first HTTP turn.
	var (
		p           wireenc.Provider
		path        string
		routed      bool
		routeErr    error
		ensureRoute = func() error {
			if !routed {
				p, path, routeErr = ProviderRouting(s.Provider)
				routed = true
			}
			return routeErr
		}
	)

	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
	var msgs []map[string]any
	for i, t := range s.Turns {
		if t.Kind == "mcp" {
			it, err := compileMCPTurn(i, t)
			if err != nil {
				return nil, err
			}
			f.Interactions = append(f.Interactions, it)
			continue
		}
		if err := ensureRoute(); err != nil {
			return nil, err
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": t.User})

		reqURL := path
		if t.URL != "" {
			reqURL = t.URL
		}
		var reqBody []byte
		if strings.TrimSpace(t.Request) != "" {
			if !json.Valid([]byte(t.Request)) {
				return nil, fmt.Errorf("turn %d: request override is not valid JSON", i)
			}
			reqBody = []byte(t.Request)
		} else {
			reqBody = synthRequest(s.Provider, model, msgs)
		}

		tr := semequal.Transcript{Role: "assistant", Text: t.Text, FinishReason: t.Finish}
		for _, tc := range t.ToolCalls {
			tr.ToolCalls = append(tr.ToolCalls, semequal.ToolCall{Name: tc.Name, Args: tc.Args})
		}
		tr = tr.Normalize()
		env := wireenc.Envelope{MessageID: fmt.Sprintf("msg_authored_%d", i+1), ModelID: model,
			InputTokens: t.InputTokens, OutputTokens: t.OutputTokens}
		sse := wireenc.Encode(p, tr, env)

		req := wirefmt.Request{
			Method:  "POST",
			URL:     reqURL,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			Body:    wirefmt.NewBody(reqBody),
		}
		key, err := match.Key(match.Input{Method: req.Method, Path: req.URL, Body: reqBody, ContentType: "application/json"}, match.Config{})
		if err != nil {
			return nil, fmt.Errorf("turn %d: %w", i, err)
		}
		req.MatchKey = key

		f.Interactions = append(f.Interactions, &wirefmt.Interaction{
			Kind:    "http",
			Request: req,
			Response: wirefmt.Response{
				Status:    200,
				Headers:   wirefmt.Headers{{Name: "Content-Type", Values: []string{"text/event-stream"}}},
				Streaming: true,
				Body:      wirefmt.NewBody(sse),
			},
		})

		msgs = append(msgs, map[string]any{"role": "assistant", "content": assistantHistory(t)})
	}
	return f, nil
}

// ProviderRouting maps a provider name (as returned by wireenc.Name; "" is an alias
// for "anthropic") to its wire dialect and canonical request path, both resolved
// from the single dialect registry.
func ProviderRouting(provider string) (wireenc.Provider, string, error) {
	if provider == "" {
		provider = "anthropic"
	}
	p, ok := wireenc.ProviderByName(provider)
	if !ok {
		return 0, "", fmt.Errorf("unknown provider %q (want %s)", provider, strings.Join(wireenc.ProviderNames(), "|"))
	}
	return p, wireenc.RequestPath(p), nil
}

// synthRequest builds a deterministic placeholder request body. Go's json.Marshal
// sorts map keys, so the bytes are stable.
func synthRequest(provider, model string, msgs []map[string]any) []byte {
	body := map[string]any{"model": model, "messages": msgs, "stream": true}
	if provider == "" || provider == "anthropic" {
		body["max_tokens"] = 1024
	}
	b, _ := json.Marshal(body)
	return b
}

func assistantHistory(t ScreenplayTurn) string {
	if t.Text != "" {
		return t.Text
	}
	var names []string
	for _, tc := range t.ToolCalls {
		names = append(names, tc.Name)
	}
	if len(names) > 0 {
		return "[tool_calls: " + strings.Join(names, ",") + "]"
	}
	return ""
}

// compileMCPTurn turns a `kind: mcp` screenplay turn into a recorded MCP
// interaction in the same stored shape the in-process recorder produces, with a
// match key derived identically to mcpKeyFromStored — so authored MCP cassettes
// replay through `cassette mcp-serve` and pass key-integrity conformance.
func compileMCPTurn(i int, t ScreenplayTurn) (*wirefmt.Interaction, error) {
	method := t.Method
	if method == "" {
		method = "tools/call"
	}
	args := strings.TrimSpace(t.Args)
	if args == "" {
		args = "{}"
	}
	if !json.Valid([]byte(args)) {
		return nil, fmt.Errorf("turn %d: mcp args is not valid JSON", i)
	}
	it := &wirefmt.Interaction{Kind: "mcp"}
	it.Request.MCPMethod = method
	it.Request.Body = wirefmt.NewBody([]byte(args))

	switch method {
	case "tools/call":
		if t.Tool == "" {
			return nil, fmt.Errorf("turn %d: mcp tools/call needs a tool name", i)
		}
		it.Request.MCPTool = t.Tool
		result, err := mcpCallResult(i, t)
		if err != nil {
			return nil, err
		}
		it.Response.Body = wirefmt.NewBody(result)
		it.Request.MatchKey = "mcp:tools/call:" + t.Tool + ":" + canon.Digest([]byte(args))
	case "tools/list":
		result, err := mcpListResult(i, t)
		if err != nil {
			return nil, err
		}
		it.Response.Body = wirefmt.NewBody(result)
		it.Request.MatchKey = "mcp:tools/list:" + canon.Digest([]byte(args))
	default:
		return nil, fmt.Errorf("turn %d: unknown mcp method %q (want tools/call or tools/list)", i, method)
	}
	return it, nil
}

// mcpCallResult returns the CallToolResult JSON for a tools/call turn: an explicit
// Result if given, otherwise one synthesized from Text or ToolError.
func mcpCallResult(i int, t ScreenplayTurn) ([]byte, error) {
	if r := strings.TrimSpace(t.Result); r != "" {
		if !json.Valid([]byte(r)) {
			return nil, fmt.Errorf("turn %d: mcp result is not valid JSON", i)
		}
		return []byte(r), nil
	}
	text := t.Text
	res := map[string]any{}
	if t.ToolError != "" {
		text = t.ToolError
		res["isError"] = true
	}
	res["content"] = []any{map[string]any{"type": "text", "text": text}}
	return json.Marshal(res)
}

// mcpListResult returns the ListToolsResult JSON for a tools/list turn: an explicit
// Result if given, otherwise one built from the turn's ToolCalls names.
func mcpListResult(i int, t ScreenplayTurn) ([]byte, error) {
	if r := strings.TrimSpace(t.Result); r != "" {
		if !json.Valid([]byte(r)) {
			return nil, fmt.Errorf("turn %d: mcp result is not valid JSON", i)
		}
		return []byte(r), nil
	}
	tools := make([]any, 0, len(t.ToolCalls))
	for _, tc := range t.ToolCalls {
		tool := map[string]any{"name": tc.Name}
		if a := strings.TrimSpace(tc.Args); a != "" && json.Valid([]byte(a)) {
			var schema any
			_ = json.Unmarshal([]byte(a), &schema)
			tool["inputSchema"] = schema
		}
		tools = append(tools, tool)
	}
	return json.Marshal(map[string]any{"tools": tools})
}
