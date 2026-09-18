// Package wirefix embeds verbatim real-wire SSE fixtures used as the permanent,
// offline RECORD source for cassette's self-tests and demo. See NOTICE for
// provenance. The bytes are served unchanged by internal/fakeprovider so the
// real provider SDK decoder consumes exactly what it would from a live stream.
package wirefix

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// AnthropicText is a complete Anthropic Messages SSE stream producing a short
// text response (includes a ping keep-alive and a multibyte emoji).
//
//go:embed wire/anthropic_text.sse
var AnthropicText []byte

// AnthropicToolUse is an Anthropic Messages SSE stream with a leading text block
// followed by a tool_use block whose arguments arrive as multiple fragmented
// input_json_delta events (including a split inside a string value and a
// multibyte character that the fake server can further split at the byte level).
//
//go:embed wire/anthropic_tooluse.sse
var AnthropicToolUse []byte

// AnthropicMidStreamError is a 200 SSE stream that emits a provider `error`
// event partway through, after some content has streamed.
//
//go:embed wire/anthropic_midstream_error.sse
var AnthropicMidStreamError []byte

// OpenAIText is a complete OpenAI Chat Completions SSE stream (role + content
// deltas, terminal data: [DONE]) producing a short text response.
//
//go:embed wire/openai_text.sse
var OpenAIText []byte

// OpenAIToolUse is an OpenAI Chat Completions SSE stream whose tool-call
// arguments arrive as multiple fragmented tool_calls deltas (including a split
// inside a string value), finishing with finish_reason "tool_calls".
//
//go:embed wire/openai_tooluse.sse
var OpenAIToolUse []byte

// OpenAIResponsesText and OpenAIResponsesToolUse are OpenAI Responses API SSE
// streams (typed events): a short text response, and a get_weather function call
// whose arguments arrive as fragmented function_call_arguments deltas.
//
//go:embed wire/openai_responses_text.sse
var OpenAIResponsesText []byte

//go:embed wire/openai_responses_tooluse.sse
var OpenAIResponsesToolUse []byte

// wireFS embeds every fixture so a variable-length conversation (the coffee
// demo) can be loaded by name.
//
//go:embed wire
var wireFS embed.FS

// CoffeeTurns returns the ordered SSE streams of a REAL multi-tool "coffee"
// conversation captured from the provider: five sequential tool_use turns
// (list_menu, get_price, check_inventory, caffeine_mg, brew_minutes) followed by
// a final natural-language recommendation. Used by the north-star demo.
func CoffeeTurns() [][]byte {
	entries, err := fs.ReadDir(wireFS, "wire")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "coffee_turn_") && strings.HasSuffix(e.Name(), ".sse") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // zero-padded names sort into turn order
	out := make([][]byte, 0, len(names))
	for _, n := range names {
		b, err := wireFS.ReadFile("wire/" + n)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	return out
}
