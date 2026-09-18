package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// anthroTurn builds a streaming Anthropic interaction with the given text +
// stop_reason and (optionally) a single tool_use block.
func anthroTurn(model, text, stop, toolName, toolArgs string) *wirefmt.Interaction {
	var b strings.Builder
	b.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	b.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n")
	if toolName != "" {
		b.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t1\",\"name\":\"" + toolName + "\"}}\n\n")
		b.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"" + strings.ReplaceAll(toolArgs, `"`, `\"`) + "\"}}\n\n")
	}
	b.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"" + stop + "\"}}\n\n")
	return &wirefmt.Interaction{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.HeadersFromHTTP(map[string][]string{"Content-Type": {"application/json"}}),
			Body:    wirefmt.NewBody([]byte(`{"model":"` + model + `","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte(b.String()))},
	}
}

func saveCass(t *testing.T, name string, its ...*wirefmt.Interaction) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := wirefmt.Save(p, &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: its}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runArgs("rekey", p); code != exitOK {
		t.Fatal("rekey failed")
	}
	return p
}

func TestPhase2_LoadErrors(t *testing.T) {
	ok := saveCass(t, "ok.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	bad := "/no/such.yaml"
	for _, args := range [][]string{
		{"diff", bad, ok}, // first file missing
		{"diff", ok, bad}, // second file missing
		{"doctor", bad, "--request", "/x"},
		{"egress-audit", bad},
		{"explain", bad},
	} {
		if code, _, _ := runArgs(args...); code != exitFail {
			t.Errorf("%v: expected exitFail, got %d", args, code)
		}
	}
}

func TestCLI_Diff(t *testing.T) {
	a := saveCass(t, "a.yaml", anthroTurn("claude-x", "the answer is Paris", "end_turn", "", ""))
	b := saveCass(t, "b.yaml", anthroTurn("claude-y", "the answer is London", "end_turn", "", ""))

	code, out, _ := runArgs("diff", a, b)
	if code != exitFail {
		t.Fatalf("diff of differing recordings should be nonzero: %d", code)
	}
	for _, want := range []string{"turn 0", "root", "model", "text:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff output missing %q:\n%s", want, out)
		}
	}
	// Identical → exit 0.
	if code, o, _ := runArgs("diff", a, a); code != exitOK || !strings.Contains(o, "identical") {
		t.Fatalf("diff of identical should pass: %d\n%s", code, o)
	}
	// --md table.
	if _, mo, _ := runArgs("diff", a, b, "--md"); !strings.Contains(mo, "| turn |") {
		t.Fatalf("--md should emit a table:\n%s", mo)
	}
	// tool diff: one calls a tool, the other doesn't.
	c := saveCass(t, "c.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"city":"Paris"}`))
	d := saveCass(t, "d.yaml", anthroTurn("m", "ok", "end_turn", "", ""))
	if _, o, _ := runArgs("diff", c, d); !strings.Contains(o, "get_weather") || !strings.Contains(o, "finish") {
		t.Fatalf("tool/finish diff missing:\n%s", o)
	}
	// args-changed on the same tool.
	e := saveCass(t, "e.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"city":"London"}`))
	if _, o, _ := runArgs("diff", c, e); !strings.Contains(o, "args") {
		t.Fatalf("args-change diff missing:\n%s", o)
	}
	// length difference (A longer than B).
	long := saveCass(t, "long.yaml", anthroTurn("m", "x", "end_turn", "", ""), anthroTurn("m", "y", "end_turn", "", ""))
	short := saveCass(t, "short.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	if code, o, _ := runArgs("diff", long, short); code != exitFail || !strings.Contains(o, "only in A") {
		t.Fatalf("length diff: %d\n%s", code, o)
	}
	// usage errors.
	if code, _, _ := runArgs("diff"); code != exitUsage {
		t.Fatalf("diff no-args usage: %d", code)
	}
	if code, _, _ := runArgs("diff", a, b, "--bad"); code != exitUsage {
		t.Fatalf("diff bad flag usage: %d", code)
	}
}

func TestCLI_Doctor(t *testing.T) {
	p := saveCass(t, "c.yaml", anthroTurn("claude-x", "hi", "end_turn", "", ""))

	// A request that matches → success.
	match := filepath.Join(t.TempDir(), "match.json")
	os.WriteFile(match, []byte(`{"method":"POST","url":"/v1/messages","body":{"model":"claude-x","max_tokens":10}}`), 0o644)
	if code, out, _ := runArgs("doctor", p, "--request", match); code != exitOK || !strings.Contains(out, "matches recorded turn") {
		t.Fatalf("doctor match: %d\n%s", code, out)
	}
	// A request differing in model → miss, nearest names the model axis.
	miss := filepath.Join(t.TempDir(), "miss.json")
	os.WriteFile(miss, []byte(`{"method":"POST","url":"/v1/messages","body":{"model":"claude-Z","max_tokens":10}}`), 0o644)
	code, out, _ := runArgs("doctor", p, "--request", miss)
	if code != exitFail || !strings.Contains(out, "no recorded interaction") || !strings.Contains(out, "model") {
		t.Fatalf("doctor miss: %d\n%s", code, out)
	}
	// A request on an unrecorded path.
	other := filepath.Join(t.TempDir(), "other.json")
	os.WriteFile(other, []byte(`{"method":"POST","url":"/v1/elsewhere","body":{}}`), 0o644)
	if _, o, _ := runArgs("doctor", p, "--request", other); !strings.Contains(o, "no recording on this method+path") {
		t.Fatalf("doctor unknown path:\n%s", o)
	}
	// A request differing on several axes exercises the remedy table.
	multi := filepath.Join(t.TempDir(), "multi.json")
	os.WriteFile(multi, []byte(`{"method":"POST","url":"/v1/messages","body":{"model":"claude-x","max_tokens":99,"messages":[1],"system":"s","tools":[1]}}`), 0o644)
	_, mo, _ := runArgs("doctor", p, "--request", multi)
	for _, want := range []string{"messages", "system", "tools", "sampling"} {
		if !strings.Contains(mo, want) {
			t.Fatalf("doctor remedy missing %q:\n%s", want, mo)
		}
	}
	if code, _, _ := runArgs("doctor", p); code != exitUsage {
		t.Fatalf("doctor missing --request should be usage: %d", code)
	}
	// Missing and malformed request files → failure, not panic.
	if code, _, _ := runArgs("doctor", p, "--request", "/no/such.json"); code != exitFail {
		t.Fatalf("doctor missing req file: %d", code)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("not json"), 0o644)
	if code, _, _ := runArgs("doctor", p, "--request", bad); code != exitFail {
		t.Fatalf("doctor malformed req: %d", code)
	}
}

func TestCLI_EgressAudit(t *testing.T) {
	dirty := filepath.Join(t.TempDir(), "d.yaml")
	os.WriteFile(dirty, []byte(`schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers: {Content-Type: [application/json]}
      body: 'email jane.doe@example.com card 4111 1111 1111 1111'
    response: {status: 200, body: '{}'}
`), 0o644)
	code, out, _ := runArgs("egress-audit", dirty)
	if code != exitFail || !strings.Contains(out, "email") || !strings.Contains(out, "creditcard") {
		t.Fatalf("egress-audit should flag email+card: %d\n%s", code, out)
	}
	// --fail-on narrows the gate (no ssn present → pass, still reports).
	if code, _, _ := runArgs("egress-audit", dirty, "--fail-on", "ssn"); code != exitOK {
		t.Fatalf("egress --fail-on ssn should pass: %d", code)
	}
	// Clean cassette.
	clean := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(clean, []byte(`schema_version: 1
interactions:
  - kind: http
    request: {method: POST, url: /v1/messages, body: '{"q":"weather in Paris"}'}
    response: {status: 200, body: '{}'}
`), 0o644)
	if code, o, _ := runArgs("egress-audit", clean); code != exitOK || !strings.Contains(o, "no sensitive") {
		t.Fatalf("clean egress: %d\n%s", code, o)
	}
	if code, _, _ := runArgs("egress-audit"); code != exitUsage {
		t.Fatalf("egress no-path usage: %d", code)
	}
}
