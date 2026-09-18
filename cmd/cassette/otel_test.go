package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLI_OTel_EmitsOTLP(t *testing.T) {
	cass := writeTemp(t, validCassette)
	code, stdout, stderr := runArgs("otel", cass)
	if code != exitOK {
		t.Fatalf("otel: code=%d stderr=%s", code, stderr)
	}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("otel stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "resourceSpans") || !strings.Contains(stdout, "gen_ai.provider.name") {
		t.Fatalf("otel output missing OTLP/GenAI structure:\n%s", stdout)
	}
}

func TestCLI_OTel_Usage(t *testing.T) {
	if code, _, _ := runArgs("otel"); code != exitUsage {
		t.Fatalf("otel no args: code=%d want exitUsage", code)
	}
}
