package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpenAIDemoRun_AssertsGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := run(&buf, false); err != nil {
		t.Fatalf("demo run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Fatalf("demo did not pass:\n%s", out)
	}
	if !strings.Contains(out, "outbound dials: 0") {
		t.Fatalf("expected zero dials:\n%s", out)
	}
	if !strings.Contains(out, "get_weather") {
		t.Fatalf("expected the OpenAI tool call:\n%s", out)
	}
	if !strings.Contains(out, "chatcmpl_RUN1") || !strings.Contains(out, "chatcmpl_RUN2") {
		t.Fatalf("expected divergent volatile ids:\n%s", out)
	}
}
