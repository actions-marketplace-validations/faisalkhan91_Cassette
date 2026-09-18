package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDemoRun_AssertsGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := run(&buf, false); err != nil {
		t.Fatalf("demo run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Fatalf("demo did not pass:\n%s", out)
	}
	if !strings.Contains(out, "outbound dials: 0") {
		t.Fatalf("demo did not report zero dials:\n%s", out)
	}
	// Multi-tool coffee agent: five sequential tool calls then a final answer.
	if !strings.Contains(out, "5 tool calls") {
		t.Fatalf("expected 5 tool calls:\n%s", out)
	}
	for _, tool := range []string{"list_menu", "get_price", "check_inventory", "caffeine_mg", "brew_minutes"} {
		if !strings.Contains(out, tool) {
			t.Fatalf("expected tool %q in transcript:\n%s", tool, out)
		}
	}
	// Live and control runs must see different volatile message ids.
	if !strings.Contains(out, "msg_RUN_1 ") || !strings.Contains(out, "msg_RUN_7") {
		t.Fatalf("expected divergent volatile ids across live/control runs:\n%s", out)
	}
}
