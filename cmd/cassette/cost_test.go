package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// usageTurn builds an Anthropic streaming interaction carrying token usage.
func usageTurn(model string, in, out int) *wirefmt.Interaction {
	body := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":" + itoa(in) + ",\"output_tokens\":1}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":" + itoa(out) + "}}\n\n"
	return &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"model":"` + model + `","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte(body))}}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func costCassette(t *testing.T) string {
	t.Helper()
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
		usageTurn("claude-opus-4-6", 100, 50),
		usageTurn("claude-opus-4-6", 200, 25),
	}}
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := wirefmt.Save(p, f); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_Cost(t *testing.T) {
	p := costCassette(t)
	code, out, _ := runArgs("cost", p, "--by", "model")
	if code != exitOK {
		t.Fatalf("cost: %d", code)
	}
	// 100+200 in, 50+25 out.
	if !strings.Contains(out, "300 in + 75 out") {
		t.Fatalf("cost totals wrong:\n%s", out)
	}
	if !strings.Contains(out, "claude-opus-4-6") {
		t.Fatalf("by-model missing:\n%s", out)
	}

	// Token budget gate.
	if code, _, _ := runArgs("cost", p, "--budget", "tokens=1000"); code != exitOK {
		t.Fatalf("under token budget should pass: %d", code)
	}
	if code, _, _ := runArgs("cost", p, "--budget", "tokens=100"); code != exitFail {
		t.Fatalf("over token budget should fail: %d", code)
	}

	// Pricing + usd budget.
	prices := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(prices, []byte(`{"claude-opus-4-6":{"input":15.0,"output":75.0}}`), 0o644)
	if _, o, _ := runArgs("cost", p, "--prices", prices); !strings.Contains(o, "cost   : $") {
		t.Fatalf("priced cost missing:\n%s", o)
	}
	// 300/1e6*15 + 75/1e6*75 = 0.0045 + 0.005625 = 0.010125 → over usd=0.005, within usd=1.
	if code, _, _ := runArgs("cost", p, "--prices", prices, "--budget", "usd=0.005"); code != exitFail {
		t.Fatalf("over usd budget should fail: %d", code)
	}
	if code, _, _ := runArgs("cost", p, "--prices", prices, "--budget", "usd=1"); code != exitOK {
		t.Fatalf("under usd budget should pass: %d", code)
	}
	// usd budget without prices is a usage error.
	if code, _, _ := runArgs("cost", p, "--budget", "usd=1"); code != exitUsage {
		t.Fatalf("usd budget without prices should be usage: %d", code)
	}

	// Unknown-usage note.
	noUsage := filepath.Join(t.TempDir(), "n.yaml")
	wirefmt.Save(noUsage, &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/chat/completions", Body: wirefmt.NewBody([]byte(`{"model":"gpt"}`))},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"))}},
	}})
	if _, o, _ := runArgs("cost", noUsage); !strings.Contains(o, "unknown") {
		t.Fatalf("expected unknown-usage note:\n%s", o)
	}

	if code, _, _ := runArgs("cost"); code != exitUsage {
		t.Fatalf("cost no-path usage: %d", code)
	}
	if code, _, _ := runArgs("cost", "/no/such.yaml"); code != exitFail {
		t.Fatalf("cost bad path: %d", code)
	}
}
