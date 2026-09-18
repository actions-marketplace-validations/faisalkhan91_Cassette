package cassette_test

import (
	"path/filepath"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/otrans"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/semequal"
)

// TestCrossProvider_ToolContract pins one provider-agnostic behavioral contract
// — "the agent calls exactly the get_weather tool and ends cleanly" — and checks
// the SAME agent behavior satisfies it whether driven through Anthropic or
// OpenAI, both replayed offline (dials==0). Models differ on prose/args, so the
// contract is over the tool SET, not the exact digest.
func TestCrossProvider_ToolContract(t *testing.T) {
	contract := func(t *testing.T, tr semequal.Transcript) {
		names := semequal.ToolNames([]semequal.Transcript{tr})
		if len(names) != 1 || names[0] != "get_weather" {
			t.Fatalf("contract violated: tools = %v, want [get_weather]", names)
		}
	}

	// Anthropic leg.
	aPath := filepath.Join(t.TempDir(), "a.yaml")
	asrv := fakeprovider.NewServer(messagesServer(wirefix.AnthropicToolUse, fakeprovider.PerFrame))
	arec, _ := cassette.Open(aPath, cassette.Options{Mode: cassette.ModeRecord})
	amsg, _, _ := driveStream(t, anthropicClient(arec, asrv.URL), "weather?")
	arec.VerifyError()
	asrv.Close()
	arp, _ := cassette.Open(aPath, cassette.Options{Mode: cassette.ModeReplay})
	amsg2, _, _ := driveStream(t, anthropicClient(arp, "http://replay.invalid"), "weather?")
	if arp.Dials() != 0 {
		t.Fatalf("anthropic replay dials=%d", arp.Dials())
	}
	contract(t, atrans.FromMessage(amsg))
	contract(t, atrans.FromMessage(amsg2))

	// OpenAI leg.
	oPath := filepath.Join(t.TempDir(), "o.yaml")
	osrv := fakeprovider.NewServer(chatServer(wirefix.OpenAIToolUse, fakeprovider.PerFrame))
	orec, _ := cassette.Open(oPath, cassette.Options{Mode: cassette.ModeRecord})
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4o,
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("weather?")},
		Tools: []openai.ChatCompletionToolUnionParam{openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name: "get_weather", Parameters: shared.FunctionParameters{"type": "object"},
		})},
	}
	occ, _, _ := driveOpenAIStream(t, openaiClient(orec, osrv.URL), params)
	orec.VerifyError()
	osrv.Close()
	orp, _ := cassette.Open(oPath, cassette.Options{Mode: cassette.ModeReplay})
	occ2, _, _ := driveOpenAIStream(t, openaiClient(orp, "http://replay.invalid"), params)
	if orp.Dials() != 0 {
		t.Fatalf("openai replay dials=%d", orp.Dials())
	}
	contract(t, otrans.FromChatCompletion(occ))
	contract(t, otrans.FromChatCompletion(occ2))

}
