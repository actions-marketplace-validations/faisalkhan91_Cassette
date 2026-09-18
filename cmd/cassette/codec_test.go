package main

import (
	"strings"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func analysisCodecTurn(stable, det bool) analysis.CodecTurn {
	return analysis.CodecTurn{Stable: stable, Deterministic: det}
}

// codecStreamTurn builds a streaming interaction with a real encoded SSE body.
func codecStreamTurn(url string, tr semequal.Transcript) *wirefmt.Interaction {
	sse := wireenc.Encode(wireenc.ProviderForURL(url), tr, wireenc.Envelope{})
	return &wirefmt.Interaction{Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: url,
			Headers: wirefmt.Headers{{Name: "Content-Type", Values: []string{"application/json"}}},
			Body:    wirefmt.NewBody([]byte(`{"stream":true}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(sse)}}
}

func TestCLI_CodecVerify(t *testing.T) {
	good := seedFile(t,
		codecStreamTurn("/v1/messages", semequal.Transcript{Text: "hi", FinishReason: "end_turn"}),
		// unary turn → skipped
		&wirefmt.Interaction{Kind: "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"id":"x"}`))}})

	code, out, _ := runArgs("codec", "verify", good)
	if code != exitOK {
		t.Fatalf("codec verify exit=%d\n%s", code, out)
	}
	for _, want := range []string{"stable, deterministic", "skipped", "round-trips 1 streaming"} {
		if !strings.Contains(out, want) {
			t.Fatalf("codec verify output missing %q:\n%s", want, out)
		}
	}

	// --json carries the structured report.
	if _, jout, _ := runArgs("codec", "verify", good, "--json"); !strings.Contains(jout, `"checked":1`) {
		t.Fatalf("codec verify --json missing checked:\n%s", jout)
	}

	// Usage errors.
	if code, _, _ := runArgs("codec"); code != exitUsage {
		t.Fatalf("codec no-subcommand exit=%d", code)
	}
	if code, _, _ := runArgs("codec", "verify"); code != exitUsage {
		t.Fatalf("codec verify no-arg exit=%d", code)
	}
	if code, _, _ := runArgs("codec", "bogus", good); code != exitUsage {
		t.Fatalf("codec bad-subcommand exit=%d", code)
	}
}

func TestCodecFailLabel(t *testing.T) {
	cases := []struct {
		stable, det bool
		want        string
	}{
		{false, false, "not stable and not deterministic"},
		{false, true, "not a round-trip fixpoint"},
		{true, false, "encoding not deterministic"},
	}
	for _, c := range cases {
		got := codecFail(analysisCodecTurn(c.stable, c.det))
		if got != c.want {
			t.Fatalf("codecFail(%v,%v)=%q want %q", c.stable, c.det, got, c.want)
		}
	}
}

func TestIndentLines(t *testing.T) {
	got := indentLines("a\n\nb", "  ")
	if got != "  a\n\n  b\n" {
		t.Fatalf("indentLines = %q", got)
	}
	if got := indentLines("", "  "); got != "" {
		t.Fatalf("indentLines empty = %q", got)
	}
}
