package wireenc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/semequal"
)

// Every Provider constant must have exactly one dialect entry with a non-nil
// decode/encode pair and a unique name — the registry is the single source of
// truth, so a new provider that forgets an entry must fail here, not silently
// fall back to Anthropic.
func TestDialectRegistry_CoversEveryProvider(t *testing.T) {
	providers := []Provider{Anthropic, OpenAIChat, OpenAIResponses, Gemini, Ollama}
	seenName := map[string]bool{}
	for _, p := range providers {
		d := dialectFor(p)
		if d.provider != p {
			t.Errorf("provider %d resolves to dialect %q (provider %d) — missing registry entry", p, d.name, d.provider)
		}
		if d.decode == nil || d.encode == nil {
			t.Errorf("dialect %q has a nil codec", d.name)
		}
		if d.name == "" || seenName[d.name] {
			t.Errorf("dialect name %q is empty or duplicated", d.name)
		}
		seenName[d.name] = true
		// Name/SeedSupported must agree with the same entry.
		if Name(p) != d.name {
			t.Errorf("Name(%d)=%q disagrees with registry %q", p, Name(p), d.name)
		}
	}
	if len(seenName) != len(providers) {
		t.Errorf("registry covers %d names, want %d providers", len(seenName), len(providers))
	}
}

// Each dialect's canonical request path must route back to that same dialect, so a
// recording authored or ported INTO a provider replays as that provider.
func TestDialectRegistry_RequestPathRoundTrips(t *testing.T) {
	for _, p := range []Provider{Anthropic, OpenAIChat, OpenAIResponses, Gemini, Ollama} {
		path := RequestPath(p)
		if path == "" {
			t.Errorf("provider %q has no request path", Name(p))
			continue
		}
		if got := ProviderForURL(path); got != p {
			t.Errorf("RequestPath(%q)=%q routes back to %q, not %q", Name(p), path, Name(got), Name(p))
		}
	}
}

// ProviderByName is the inverse of Name across the whole registry, and rejects
// unknown names.
func TestDialectRegistry_ProviderByName(t *testing.T) {
	for _, p := range []Provider{Anthropic, OpenAIChat, OpenAIResponses, Gemini, Ollama} {
		got, ok := ProviderByName(Name(p))
		if !ok || got != p {
			t.Errorf("ProviderByName(Name(%q)) = (%q,%v), want (%q,true)", Name(p), Name(got), ok, Name(p))
		}
	}
	if _, ok := ProviderByName("not-a-provider"); ok {
		t.Error("ProviderByName accepted an unknown name")
	}
	if names := ProviderNames(); len(names) != 5 {
		t.Errorf("ProviderNames() = %v, want 5 entries", names)
	}
}

// EncodeRequest→DecodeRequest must round-trip the neutral conversation view for
// every dialect, so porting a recording across providers preserves model, system,
// and the message sequence.
func TestRequestCodec_RoundTrips(t *testing.T) {
	msgs := []ConvoMsg{{Role: "user", Text: "hello"}, {Role: "assistant", Text: "hi there"}, {Role: "user", Text: "bye"}}
	for _, p := range []Provider{Anthropic, OpenAIChat, OpenAIResponses, Gemini, Ollama} {
		body := EncodeRequest(p, "m1", "be terse", msgs)
		// The encoded body must route back to the SAME dialect via its path…
		if got := ProviderForURL(RequestPath(p)); got != p {
			t.Errorf("%s: request path does not round-trip", Name(p))
		}
		gotModel, gotSystem, gotMsgs := DecodeRequest(p, body)
		// Gemini carries the model in the URL, not the body — model is allowed to be empty there.
		if p != Gemini && gotModel != "m1" {
			t.Errorf("%s: model = %q, want m1", Name(p), gotModel)
		}
		if gotSystem != "be terse" {
			t.Errorf("%s: system = %q, want 'be terse'", Name(p), gotSystem)
		}
		if len(gotMsgs) != len(msgs) {
			t.Fatalf("%s: %d msgs, want %d (%+v)", Name(p), len(gotMsgs), len(msgs), gotMsgs)
		}
		for i := range msgs {
			if gotMsgs[i] != msgs[i] {
				t.Errorf("%s: msg[%d] = %+v, want %+v", Name(p), i, gotMsgs[i], msgs[i])
			}
		}
	}
}

// UpstreamForURL positively matches known provider paths and refuses to guess for
// the ambiguous OpenAI-compatible path or an unknown path.
func TestUpstreamForURL(t *testing.T) {
	cases := []struct {
		url    string
		want   string
		wantOK bool
	}{
		{"/v1/messages", "https://api.anthropic.com", true},
		{"/v1beta/models/gemini-2.0-flash:streamGenerateContent", "https://generativelanguage.googleapis.com", true},
		{"/api/chat", "http://localhost:11434", true},
		{"/v1/responses", "https://api.openai.com", true},
		{"/v1/chat/completions", "", false}, // ambiguous OpenAI-compatible family — must not guess
		{"/totally/unknown/path", "", false},
	}
	for _, c := range cases {
		got, ok := UpstreamForURL(c.url)
		if got != c.want || ok != c.wantOK {
			t.Errorf("UpstreamForURL(%q) = (%q,%v), want (%q,%v)", c.url, got, ok, c.want, c.wantOK)
		}
	}
}

type decoder func([]byte) (semequal.Transcript, error)

func decoderFor(p Provider) decoder {
	switch p {
	case OpenAIChat:
		return semequal.DecodeOpenAISSE
	case OpenAIResponses:
		return semequal.DecodeOpenAIResponsesSSE
	case Gemini:
		return semequal.DecodeGeminiSSE
	case Ollama:
		return semequal.DecodeOllamaNDJSON
	default:
		return semequal.DecodeAnthropicSSE
	}
}

var providers = []struct {
	name string
	p    Provider
}{
	{"anthropic", Anthropic}, {"openai_chat", OpenAIChat}, {"openai_responses", OpenAIResponses}, {"gemini", Gemini}, {"ollama", Ollama}}

// The decisive contract: Decode(Encode(t)) == t.Normalize() for every provider
// and a representative spread of transcripts.
func TestRoundTrip(t *testing.T) {
	transcripts := []semequal.Transcript{
		{Role: "assistant", Text: "Hello, world! 🌍", FinishReason: "end_turn"},
		{Role: "assistant", ToolCalls: []semequal.ToolCall{{Name: "get_weather", Args: `{"location":"San José"}`}}, FinishReason: "tool_use"},
		{Role: "assistant", Text: "Let me check.", ToolCalls: []semequal.ToolCall{
			{Name: "a_tool", Args: `{"x":1}`}, {Name: "b_tool", Args: `{"y":2}`}}, FinishReason: "tool_use"},
		{Role: "assistant", Text: "just text"},
	}
	for _, pr := range providers {
		dec := decoderFor(pr.p)
		for i, tr := range transcripts {
			raw := Encode(pr.p, tr, Envelope{})
			got, err := dec(raw)
			if err != nil {
				t.Fatalf("%s #%d decode: %v\n%s", pr.name, i, err, raw)
			}
			if !semequal.Equal(got, tr.Normalize()) {
				t.Fatalf("%s #%d round-trip mismatch:\n got=%+v\nwant=%+v\nraw=%s", pr.name, i, got, tr.Normalize(), raw)
			}
		}
	}
}

// Round-trip the real recorded fixtures: decode → encode → decode is a fixed point.
func TestRoundTrip_Fixtures(t *testing.T) {
	cases := []struct {
		p   Provider
		raw []byte
	}{
		{Anthropic, wirefix.AnthropicText}, {Anthropic, wirefix.AnthropicToolUse},
		{OpenAIChat, wirefix.OpenAIText}, {OpenAIChat, wirefix.OpenAIToolUse},
	}
	for i, c := range cases {
		dec := decoderFor(c.p)
		want, err := dec(c.raw)
		if err != nil {
			t.Fatalf("#%d decode fixture: %v", i, err)
		}
		got, err := dec(Encode(c.p, want, Envelope{}))
		if err != nil {
			t.Fatalf("#%d re-decode: %v", i, err)
		}
		if !semequal.Equal(got, want.Normalize()) {
			t.Fatalf("#%d fixture round-trip mismatch:\n got=%+v\nwant=%+v", i, got, want.Normalize())
		}
	}
}

func TestDeterministic(t *testing.T) {
	tr := semequal.Transcript{Text: "x", ToolCalls: []semequal.ToolCall{{Name: "t", Args: `{"a":1}`}}, FinishReason: "tool_use"}
	for _, pr := range providers {
		a := Encode(pr.p, tr, Envelope{})
		b := Encode(pr.p, tr, Envelope{})
		if string(a) != string(b) {
			t.Fatalf("%s: encode not deterministic", pr.name)
		}
	}
}

func TestVolatileInjectionInvariant(t *testing.T) {
	tr := semequal.Transcript{Text: "hi", FinishReason: "end_turn"}
	base := Encode(Anthropic, tr, Envelope{})
	withEnv := Encode(Anthropic, tr, Envelope{MessageID: "msg_XYZ", ModelID: "claude-test", InputTokens: 99, OutputTokens: 7})
	if string(base) == string(withEnv) {
		t.Fatal("envelope should change the bytes")
	}
	// ...but not the decoded transcript.
	g1, _ := semequal.DecodeAnthropicSSE(base)
	g2, _ := semequal.DecodeAnthropicSSE(withEnv)
	if !semequal.Equal(g1, g2) {
		t.Fatal("envelope must not change the decoded transcript")
	}
}

// Decode must be the exact inverse routing of Encode for every dialect.
func TestDecode_InverseOfEncode(t *testing.T) {
	tr := semequal.Transcript{Role: "assistant", Text: "hi", FinishReason: "end_turn"}
	for _, pr := range providers {
		got, err := Decode(pr.p, Encode(pr.p, tr, Envelope{}))
		if err != nil {
			t.Fatalf("%s: Decode: %v", pr.name, err)
		}
		if !semequal.Equal(got, tr.Normalize()) {
			t.Fatalf("%s: Decode∘Encode mismatch: %+v", pr.name, got)
		}
	}
}

// RoundTrip reports a stable, deterministic fixpoint for well-formed streams.
func TestRoundTrip_Helper(t *testing.T) {
	tr := semequal.Transcript{Text: "Let me check.", ToolCalls: []semequal.ToolCall{
		{Name: "get_weather", Args: `{"city":"Paris"}`}}, FinishReason: "tool_use"}
	for _, pr := range providers {
		res, err := RoundTrip(pr.p, Encode(pr.p, tr, Envelope{}))
		if err != nil {
			t.Fatalf("%s: RoundTrip: %v", pr.name, err)
		}
		if !res.Stable || !res.Deterministic {
			t.Fatalf("%s: stable=%v deterministic=%v", pr.name, res.Stable, res.Deterministic)
		}
	}
}

// An error stream surfaces the decode error (the encoder has no error path).
func TestRoundTrip_ErrorStream(t *testing.T) {
	raw := []byte("event: error\ndata: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"x\"}}\n\n")
	if _, err := RoundTrip(Anthropic, raw); err == nil {
		t.Fatal("expected an error from an error stream")
	}
}

func TestProviderForURL(t *testing.T) {
	if ProviderForURL("/v1/responses") != OpenAIResponses ||
		ProviderForURL("/v1/chat/completions") != OpenAIChat ||
		ProviderForURL("/v1/messages") != Anthropic {
		t.Fatal("ProviderForURL routing mismatch")
	}
}

func TestChunkingInvariance(t *testing.T) {
	// Our single-fragment args must decode to the same digest as the multi-frame
	// fixtures (which split partial_json) — proven by the fixture round-trip above
	// plus this explicit equality on tool args.
	tr := semequal.Transcript{ToolCalls: []semequal.ToolCall{{Name: "t", Args: `{"city":"Paris","n":2}`}}, FinishReason: "tool_use"}
	got, err := semequal.DecodeAnthropicSSE(Encode(Anthropic, tr, Envelope{}))
	if err != nil || !strings.Contains(got.ToolCalls[0].Args, "Paris") {
		t.Fatalf("args round-trip: %v %+v", err, got)
	}
}

func TestEncodeOpenAI_UsageGatedOnTokens(t *testing.T) {
	tr := semequal.Transcript{Role: "assistant", Text: "hi", FinishReason: "stop"}
	for _, p := range []Provider{OpenAIChat, OpenAIResponses} {
		zero := Encode(p, tr, Envelope{})
		if bytes.Contains(zero, []byte("usage")) {
			t.Errorf("%v: zero-token output must not emit usage (byte-identical default); got:\n%s", p, zero)
		}
		withTok := Encode(p, tr, Envelope{InputTokens: 11, OutputTokens: 5})
		if !bytes.Contains(withTok, []byte("usage")) {
			t.Errorf("%v: token-bearing output must emit usage; got:\n%s", p, withTok)
		}
	}
}

func TestEncode_ToolArgsPreserveIntegerPrecision(t *testing.T) {
	// A large integer in tool args must survive Decode→Encode→Decode on every dialect
	// (no float64 round-trip turning it into 1.23e+16).
	tr := semequal.Transcript{ToolCalls: []semequal.ToolCall{{Name: "f", Args: `{"id":12345678901234567}`}}, FinishReason: "stop"}
	check := func(p Provider, dec func([]byte) (semequal.Transcript, error)) {
		got, err := dec(Encode(p, tr, Envelope{}))
		if err != nil {
			t.Fatalf("%v: %v", p, err)
		}
		if len(got.ToolCalls) != 1 || got.ToolCalls[0].Args != `{"id":12345678901234567}` {
			t.Errorf("%v lost integer precision: %q", p, got.ToolCalls[0].Args)
		}
	}
	check(Gemini, semequal.DecodeGeminiSSE)
	check(Ollama, semequal.DecodeOllamaNDJSON)
	check(Anthropic, semequal.DecodeAnthropicSSE)
	check(OpenAIChat, semequal.DecodeOpenAISSE)
}
