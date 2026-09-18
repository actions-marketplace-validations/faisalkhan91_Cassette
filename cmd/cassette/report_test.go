package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Report_InvariantBugLive(t *testing.T) {
	// A recording that calls get_weather; forbidding it → bug is live.
	c := saveCass(t, "c.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"city":"Paris"}`))
	out := filepath.Join(t.TempDir(), "r.md")
	code, _, _ := runArgs("report", c, "--no-tool", "get_weather", "-o", out)
	if code != exitFail {
		t.Fatalf("bug-live report should exit nonzero: %d", code)
	}
	md, _ := os.ReadFile(out)
	for _, want := range []string{"BUG LIVE", "get_weather", "## Repro", "## Provenance", "## CI"} {
		if !strings.Contains(string(md), want) {
			t.Fatalf("report missing %q:\n%s", want, md)
		}
	}
}

func TestCLI_Report_InvariantFixed(t *testing.T) {
	// No forbidden tool called → bug fixed; report still writes, exit 0.
	c := saveCass(t, "c.yaml", anthroTurn("m", "all good", "end_turn", "", ""))
	code, stdout, _ := runArgs("report", c, "--no-tool", "delete_account")
	if code != exitOK {
		t.Fatalf("fixed report should exit 0: %d", code)
	}
	if !strings.Contains(stdout, "satisfied") && !strings.Contains(stdout, "FIXED") {
		t.Fatalf("expected fixed verdict:\n%s", stdout)
	}
}

func TestCLI_Report_DiffMode(t *testing.T) {
	a := saveCass(t, "a.yaml", anthroTurn("m", "answer is Paris", "end_turn", "", ""))
	b := saveCass(t, "b.yaml", anthroTurn("m", "answer is London", "end_turn", "", ""))
	code, out, _ := runArgs("report", b, "--diff", a)
	if code != exitFail || !strings.Contains(out, "DIVERGED") {
		t.Fatalf("diff-mode bug-live: %d\n%s", code, out)
	}
	// Identical → exit 0.
	if code, _, _ := runArgs("report", a, "--diff", a); code != exitOK {
		t.Fatalf("identical diff should exit 0: %d", code)
	}
	// Both families → usage error.
	if code, _, _ := runArgs("report", a, "--diff", b, "--finishes-clean"); code != exitUsage {
		t.Fatalf("both families should be usage error: %d", code)
	}
}

func TestCLI_Report_Bundle(t *testing.T) {
	c := saveCass(t, "c.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"x":1}`))
	name := filepath.Join(t.TempDir(), "tuesday")
	if code, _, _ := runArgs("report", c, "--no-tool", "get_weather", "--bundle", name); code != exitFail {
		t.Fatalf("bundle report (bug live) exit: %d", code)
	}
	dir := name + ".castiron"
	for _, fn := range []string{"c.yaml", "report.md", "run.sh"} {
		if _, err := os.Stat(filepath.Join(dir, fn)); err != nil {
			t.Fatalf(".castiron missing %s: %v", fn, err)
		}
	}
	run, _ := os.ReadFile(filepath.Join(dir, "run.sh"))
	if !strings.Contains(string(run), "cassette assert") {
		t.Fatalf("run.sh missing repro: %s", run)
	}

	// Refuses to bundle a cassette carrying a secret.
	sec := filepath.Join(t.TempDir(), "s.yaml")
	os.WriteFile(sec, []byte("schema_version: 1\ninteractions:\n  - kind: http\n    request: {method: POST, url: /v1/messages, body: 'authorization Bearer sk-secretkey1234567890'}\n    response: {status: 200, streaming: true, body: \"event: message_delta\\ndata: {\\\"type\\\":\\\"message_delta\\\",\\\"delta\\\":{\\\"stop_reason\\\":\\\"end_turn\\\"}}\\n\\n\"}\n"), 0o644)
	if code, _, errOut := runArgs("report", sec, "--finishes-clean", "--bundle", filepath.Join(t.TempDir(), "leak")); code != exitFail || !strings.Contains(errOut, "refusing to bundle") {
		t.Fatalf("bundle should refuse secrets: %d\n%s", code, errOut)
	}
}

func TestCLI_Report_Errors(t *testing.T) {
	if code, _, _ := runArgs("report"); code != exitUsage {
		t.Fatalf("no path usage: %d", code)
	}
	if code, _, _ := runArgs("report", "/no/such.yaml", "--finishes-clean"); code != exitFail {
		t.Fatalf("bad path: %d", code)
	}
	c := saveCass(t, "c.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	if code, _, _ := runArgs("report", c, "--diff", "/no/such.yaml"); code != exitFail {
		t.Fatalf("bad baseline: %d", code)
	}
}
