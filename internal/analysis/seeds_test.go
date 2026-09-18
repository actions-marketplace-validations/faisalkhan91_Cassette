package analysis_test

import (
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func reqBody(url, body string) *wirefmt.Interaction {
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: url, Body: wirefmt.NewBody([]byte(body))},
		Response: wirefmt.Response{Status: 200}}
}

func TestSamplingKnobs_Reproducible(t *testing.T) {
	cases := []struct {
		url, body  string
		wantRepro  bool
		wantSeedNA bool // provider has no seed param
	}{
		{"/v1/chat/completions", `{"seed":42,"temperature":0.7}`, true, false},
		{"/v1/chat/completions", `{"temperature":0}`, true, false},
		{"/v1/chat/completions", `{"temperature":0.7}`, false, false},
		{"/v1/chat/completions", `{}`, false, false},
		{"/v1/chat/completions", `{"top_p":0.1}`, false, false}, // top_p alone ≠ reproducible
		{"/v1/messages", `{"temperature":0}`, true, true},       // anthropic greedy
		{"/v1/messages", `{"temperature":0.5}`, false, true},    // anthropic, no seed param
		{"/v1/responses", `{"temperature":0}`, true, true},      // responses greedy
		{"/v1/responses", `{"temperature":0.9}`, false, true},
	}
	for _, c := range cases {
		k := analysis.SamplingKnobs(reqBody(c.url, c.body))
		got, reason := k.Reproducible()
		if got != c.wantRepro {
			t.Errorf("%s %s: Reproducible=%v (%s), want %v", c.url, c.body, got, reason, c.wantRepro)
		}
		if k.SeedSupported == c.wantSeedNA {
			t.Errorf("%s: SeedSupported=%v, want seed-n/a=%v", c.url, k.SeedSupported, c.wantSeedNA)
		}
	}
}

// Gemini and Ollama turns must be labelled by their own dialect, not folded
// into the "anthropic" default (regression: the old ladder only knew openai).
func TestSamplingKnobs_ProviderLabel(t *testing.T) {
	cases := []struct{ url, want string }{
		{"/v1/messages", "anthropic"},
		{"/v1/chat/completions", "openai-chat"},
		{"/v1/responses", "openai-responses"},
		{"/v1beta/models/gemini-2.0-flash:generateContent", "gemini"},
		{"/v1beta/models/gemini-2.0-flash:streamGenerateContent", "gemini"},
		{"/api/chat", "ollama"},
	}
	for _, c := range cases {
		if k := analysis.SamplingKnobs(reqBody(c.url, `{}`)); k.Provider != c.want {
			t.Errorf("%s: Provider=%q, want %q", c.url, k.Provider, c.want)
		}
	}
}
