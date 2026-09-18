package main

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// buildCLI builds the cassette binary once and returns its path.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cassette")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}
	return bin
}

func runCLI(t *testing.T, bin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return code, stdout.String(), stderr.String()
}

const validCassette = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","max_tokens":5}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"id":"[SCRUBBED]","content":"hi"}'
`

// A cassette carrying realistic synthetic secrets in headers and body.
const secretCassette = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Authorization: ['Bearer sk-ant-SECRET123']
        Content-Type: [application/json]
      body: '{"api_key":"sk-SECRET123","model":"claude"}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"id":"msg_real","content":"hi"}'
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_Inspect(t *testing.T) {
	bin := buildCLI(t)

	t.Run("valid", func(t *testing.T) {
		code, out, _ := runCLI(t, bin, "inspect", writeTemp(t, validCassette))
		if code != 0 {
			t.Fatalf("exit %d, want 0", code)
		}
		for _, want := range []string{"http", "POST /v1/messages", "200"} {
			if !bytes.Contains([]byte(out), []byte(want)) {
				t.Fatalf("inspect output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("malformed", func(t *testing.T) {
		code, _, errOut := runCLI(t, bin, "inspect", writeTemp(t, "schema_version: 2\ninteractions: ]["))
		if code == 0 {
			t.Fatal("expected nonzero exit on malformed cassette")
		}
		if errOut == "" {
			t.Fatal("expected error message on stderr")
		}
	})

	t.Run("missing arg", func(t *testing.T) {
		code, _, _ := runCLI(t, bin, "inspect")
		if code != 2 {
			t.Fatalf("usage error should exit 2, got %d", code)
		}
	})
}

func TestCLI_Verify(t *testing.T) {
	bin := buildCLI(t)

	t.Run("clean", func(t *testing.T) {
		code, _, _ := runCLI(t, bin, "verify", writeTemp(t, validCassette))
		if code != 0 {
			t.Fatalf("clean cassette should verify (exit 0), got %d", code)
		}
	})

	t.Run("secrets", func(t *testing.T) {
		code, _, errOut := runCLI(t, bin, "verify", writeTemp(t, secretCassette))
		if code != 1 {
			t.Fatalf("cassette with secrets should exit 1, got %d", code)
		}
		if !bytes.Contains([]byte(errOut), []byte("secret")) {
			t.Fatalf("expected secret warning, got: %s", errOut)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		code, _, _ := runCLI(t, bin, "verify", writeTemp(t, "not: [valid"))
		if code != 1 {
			t.Fatalf("malformed should exit 1, got %d", code)
		}
	})
}

func TestCLI_ScrubIdempotent(t *testing.T) {
	bin := buildCLI(t)
	path := writeTemp(t, secretCassette)

	// First scrub removes secrets.
	if code, _, _ := runCLI(t, bin, "scrub", path); code != 0 {
		t.Fatalf("scrub exit %d", code)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first, []byte("sk-")) || bytes.Contains(first, []byte("Bearer ")) {
		t.Fatalf("secrets remain after scrub:\n%s", first)
	}
	// Verify now passes.
	if code, _, _ := runCLI(t, bin, "verify", path); code != 0 {
		t.Fatalf("verify after scrub should pass, got %d", code)
	}

	// Second scrub is idempotent: byte-identical output.
	if code, _, _ := runCLI(t, bin, "scrub", path); code != 0 {
		t.Fatalf("second scrub exit %d", code)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("scrub not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	bin := buildCLI(t)
	code, _, _ := runCLI(t, bin, "frobnicate")
	if code != 2 {
		t.Fatalf("unknown command should exit 2, got %d", code)
	}
}

// In-process run() tests — exercise the CLI dispatch directly (the os/exec tests
// above validate real exit codes; these attribute coverage and cover edge paths).
func runArgs(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_InProcess(t *testing.T) {
	good := writeTemp(t, validCassette)
	secret := writeTemp(t, secretCassette)
	bad := writeTemp(t, "schema_version: 2\ninteractions: ]")
	mcpCas := writeTemp(t, "schema_version: 1\ninteractions:\n  - kind: mcp\n    request:\n      mcp_method: tools/call\n      mcp_tool: add\n      body: '{\"a\":1}'\n    response:\n      body: '{\"sum\":1}'\n")

	if code, out, _ := runArgs("inspect", good); code != 0 || !bytes.Contains([]byte(out), []byte("http POST")) {
		t.Fatalf("inspect good: code=%d out=%s", code, out)
	}
	if code, out, _ := runArgs("inspect", mcpCas); code != 0 || !bytes.Contains([]byte(out), []byte("mcp tools/call add")) {
		t.Fatalf("inspect mcp: code=%d out=%s", code, out)
	}
	if code, _, _ := runArgs("inspect", bad); code != exitFail {
		t.Fatalf("inspect malformed: code=%d", code)
	}
	if code, _, _ := runArgs("inspect"); code != exitUsage {
		t.Fatalf("inspect no-arg: code=%d", code)
	}
	if code, _, _ := runArgs("verify", good); code != 0 {
		t.Fatalf("verify good: code=%d", code)
	}
	if code, _, _ := runArgs("verify", secret); code != exitFail {
		t.Fatalf("verify secret: code=%d", code)
	}
	if code, _, _ := runArgs("verify", "/no/such/file.yaml"); code != exitFail {
		t.Fatalf("verify missing: code=%d", code)
	}
	if code, _, _ := runArgs("verify"); code != exitUsage {
		t.Fatalf("verify no-arg: code=%d", code)
	}
	// scrub with leading --update flag tolerated.
	if code, _, _ := runArgs("scrub", "--update", secret); code != 0 {
		t.Fatalf("scrub: code=%d", code)
	}
	if code, _, _ := runArgs("scrub", "/no/such/file.yaml"); code != exitFail {
		t.Fatalf("scrub missing: code=%d", code)
	}
	if code, _, _ := runArgs("scrub"); code != exitUsage {
		t.Fatalf("scrub no-arg: code=%d", code)
	}
	if code, _, _ := runArgs(); code != exitUsage {
		t.Fatalf("no command: code=%d", code)
	}
	if code, _, _ := runArgs("help"); code != exitOK {
		t.Fatalf("help: code=%d", code)
	}
	if code, _, _ := runArgs("bogus"); code != exitUsage {
		t.Fatalf("bogus: code=%d", code)
	}
	if code, _, _ := runArgs("inspect", "a", "b"); code != exitUsage {
		t.Fatalf("two args: code=%d", code)
	}
}

const twoInteractionCassette = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","user":"alice","nonce":"AAA"}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"id":"[SCRUBBED]","content":"one"}'
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","user":"bob","nonce":"BBB"}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"id":"[SCRUBBED]","content":"two"}'
`

func TestCLI_Prune(t *testing.T) {
	p := writeTemp(t, twoInteractionCassette)
	if code, out, errb := runArgs("prune", p, "--index", "0"); code != exitOK {
		t.Fatalf("prune: code=%d out=%s err=%s", code, out, errb)
	}
	raw, _ := os.ReadFile(p)
	if bytes.Contains(raw, []byte(`"content":"one"`)) || !bytes.Contains(raw, []byte(`"content":"two"`)) {
		t.Fatalf("prune did not drop interaction 0:\n%s", raw)
	}
	if code, _, _ := runArgs("verify", p); code != exitOK {
		t.Fatal("pruned cassette should still verify")
	}
	// bad index
	if code, _, _ := runArgs("prune", p, "--index", "99"); code != exitFail {
		t.Fatal("out-of-range index should fail")
	}
	if code, _, _ := runArgs("prune", p); code != exitUsage {
		t.Fatal("missing --index should be usage error")
	}
}

func TestCLI_Rekey_Idempotent(t *testing.T) {
	p := writeTemp(t, twoInteractionCassette)
	if code, _, errb := runArgs("rekey", p); code != exitOK {
		t.Fatalf("rekey: %s", errb)
	}
	first, _ := os.ReadFile(p)
	if !bytes.Contains(first, []byte("match_key:")) {
		t.Fatalf("rekey should populate match_key:\n%s", first)
	}
	if code, _, _ := runArgs("rekey", p); code != exitOK {
		t.Fatal("second rekey failed")
	}
	second, _ := os.ReadFile(p)
	if !bytes.Equal(first, second) {
		t.Fatalf("rekey not idempotent")
	}
}

// A scrubbed request body whose stored key no longer re-derives must NOT be
// silently rekeyed (it would overwrite the replay-authoritative key); --force
// overrides.
func TestCLI_Rekey_RefusesScrubbedBody(t *testing.T) {
	const scrubbed = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      match_key: stale-pre-scrub-key
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","secret":"REDACTED"}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"content":"one"}'
`
	p := writeTemp(t, scrubbed)
	if code, _, errb := runArgs("rekey", p); code != exitFail {
		t.Fatalf("rekey should refuse a scrubbed-body cassette, got code=%d err=%s", code, errb)
	}
	before, _ := os.ReadFile(p)
	if !bytes.Contains(before, []byte("stale-pre-scrub-key")) {
		t.Fatal("refused rekey must not have touched the stored key")
	}
	if code, _, errb := runArgs("rekey", p, "--force"); code != exitOK {
		t.Fatalf("rekey --force should proceed: %s", errb)
	}
	after, _ := os.ReadFile(p)
	if bytes.Contains(after, []byte("stale-pre-scrub-key")) {
		t.Fatal("--force should have recomputed the key")
	}
}

func TestCLI_RedactField_Idempotent(t *testing.T) {
	p := writeTemp(t, twoInteractionCassette)
	if code, _, errb := runArgs("redact-field", p, "--path", "user"); code != exitOK {
		t.Fatalf("redact-field: %s", errb)
	}
	first, _ := os.ReadFile(p)
	if bytes.Contains(first, []byte("alice")) || bytes.Contains(first, []byte("bob")) {
		t.Fatalf("redact-field left the value:\n%s", first)
	}
	if !bytes.Contains(first, []byte("[REDACTED]")) {
		t.Fatalf("redact-field should insert sentinel:\n%s", first)
	}
	if code, _, _ := runArgs("redact-field", p, "--path", "user"); code != exitOK {
		t.Fatal("second redact-field failed")
	}
	second, _ := os.ReadFile(p)
	if !bytes.Equal(first, second) {
		t.Fatal("redact-field not idempotent")
	}
	if code, _, _ := runArgs("verify", p); code != exitOK {
		t.Fatal("redacted cassette should verify")
	}
	if code, _, _ := runArgs("redact-field", p); code != exitUsage {
		t.Fatal("missing --path should be usage error")
	}
}

func TestCLI_Serve(t *testing.T) {
	bin := buildCLI(t)
	path := writeTemp(t, validCassette)

	cmd := exec.Command(bin, "serve", path, "--addr", "127.0.0.1:0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Read the "serving ... on http://HOST:PORT" line to learn the address.
	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			if i := strings.Index(line, "http://"); i >= 0 {
				addrCh <- strings.TrimSpace(line[i:])
				return
			}
		}
		addrCh <- ""
	}()

	var base string
	select {
	case base = <-addrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not announce its address")
	}
	if base == "" {
		t.Fatal("could not parse serve address")
	}

	resp, err := http.Post(base+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"claude","max_tokens":5}`))
	if err != nil {
		t.Fatalf("request to served cassette: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte(`"content":"hi"`)) {
		t.Fatalf("unexpected served response: %s", b)
	}
}

func TestServeArgErrors(t *testing.T) {
	good := writeTemp(t, validCassette)
	if code, _, _ := runArgs("serve"); code != exitUsage {
		t.Fatalf("serve no-path: %d", code)
	}
	if code, _, _ := runArgs("serve", "--addr"); code != exitUsage {
		t.Fatalf("serve dangling --addr: %d", code)
	}
	if code, _, _ := runArgs("serve", "--bogus", good); code != exitUsage {
		t.Fatalf("serve unknown flag: %d", code)
	}
	if code, _, _ := runArgs("serve", good, "extra"); code != exitUsage {
		t.Fatalf("serve two paths: %d", code)
	}
	if code, _, _ := runArgs("serve", "/no/such/file.yaml"); code != exitFail {
		t.Fatalf("serve missing cassette: %d", code)
	}
}

const streamCassette = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"x":1}'
    response:
      status: 200
      headers:
        Content-Type: [text/event-stream]
      streaming: true
      body: "event: a\ndata: 1\n\nevent: message_stop\ndata: {}\n\n"
`

func TestCLI_Mutate(t *testing.T) {
	path := writeTemp(t, streamCassette)
	if code, _, errb := runArgs("mutate", path, "--op", "drop-terminal"); code != exitOK {
		t.Fatalf("mutate: code=%d err=%s", code, errb)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("message_stop")) {
		t.Fatalf("drop-terminal should remove message_stop:\n%s", raw)
	}
	// error paths
	if code, _, _ := runArgs("mutate", path); code != exitUsage {
		t.Fatalf("mutate no --op: %d", code)
	}
	if code, _, _ := runArgs("mutate", path, "--op", "bogus"); code != exitUsage {
		t.Fatalf("mutate unknown op: %d", code)
	}
	if code, _, _ := runArgs("mutate", "/no/such.yaml", "--op", "reorder"); code != exitFail {
		t.Fatalf("mutate missing file: %d", code)
	}
}

func TestCLI_Assert(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind:     "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)},
	}}}
	path := filepath.Join(t.TempDir(), "a.yaml")
	if err := wirefmt.Save(path, f); err != nil {
		t.Fatal(err)
	}
	// The recording calls get_weather → forbidding it must fail.
	if code, _, _ := runArgs("assert", path, "--no-tool", "get_weather"); code != exitFail {
		t.Fatalf("forbidden tool present should fail: %d", code)
	}
	// It ends on a tool_use (dangling) → finishes-clean must fail.
	if code, _, _ := runArgs("assert", path, "--finishes-clean"); code != exitFail {
		t.Fatalf("dangling tool finish should fail: %d", code)
	}
	// A benign invariant passes.
	if code, _, _ := runArgs("assert", path, "--no-tool", "delete_account"); code != exitOK {
		t.Fatalf("benign invariant should pass: %d", code)
	}
	if code, _, _ := runArgs("assert"); code != exitUsage {
		t.Fatalf("no path should be usage error: %d", code)
	}
}

func TestCLI_Doc(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind:     "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)},
	}}}
	path := filepath.Join(t.TempDir(), "d.yaml")
	if err := wirefmt.Save(path, f); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runArgs("doc", path)
	if code != exitOK {
		t.Fatalf("doc: %d", code)
	}
	for _, want := range []string{"# Cassette:", "get_weather", "Let me check the weather."} {
		if !strings.Contains(out, want) {
			t.Fatalf("doc output missing %q:\n%s", want, out)
		}
	}
	// --check on a matching file passes; stale fails.
	docPath := filepath.Join(t.TempDir(), "d.md")
	os.WriteFile(docPath, []byte(out), 0o644)
	if code, _, _ := runArgs("doc", path, "--check", docPath); code != exitOK {
		t.Fatalf("doc --check fresh should pass: %d", code)
	}
	os.WriteFile(docPath, []byte("stale"), 0o644)
	if code, _, _ := runArgs("doc", path, "--check", docPath); code != exitFail {
		t.Fatalf("doc --check stale should fail: %d", code)
	}
}

func TestCLI_Bisect(t *testing.T) {
	mk := func(name, model, text string) string {
		body := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}

`
		f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
			Kind:     "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"model":"` + model + `"}`))},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte(body))},
		}}}
		p := filepath.Join(t.TempDir(), name)
		wirefmt.Save(p, f)
		return p
	}
	a := mk("a.yaml", "claude-x", "A")
	b := mk("b.yaml", "claude-y", "B")
	code, out, _ := runArgs("bisect", a, b)
	if code != exitOK || !strings.Contains(out, "turn 0") || !strings.Contains(out, "model") {
		t.Fatalf("bisect output unexpected: code=%d out=%s", code, out)
	}
	// identical
	code, out, _ = runArgs("bisect", a, a)
	if code != exitOK || !strings.Contains(out, "no behavioral divergence") {
		t.Fatalf("identical bisect: code=%d out=%s", code, out)
	}
	if code, _, _ := runArgs("bisect", a); code != exitUsage {
		t.Fatalf("bisect one arg should be usage: %d", code)
	}
}
