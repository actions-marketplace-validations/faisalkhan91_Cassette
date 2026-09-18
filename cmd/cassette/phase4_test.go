package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestCLI_Attest_RoundTrip(t *testing.T) {
	subject := saveCass(t, "s.yaml", anthroTurn("m", "hello", "end_turn", "", ""))
	dir := t.TempDir()
	key := filepath.Join(dir, "k.key")
	att := filepath.Join(dir, "s.att")
	if code, _, _ := runArgs("attest", "gen-key", key); code != exitOK {
		t.Fatal("gen-key")
	}
	if code, _, e := runArgs("attest", subject, "--key", key, "-o", att); code != exitOK {
		t.Fatalf("attest: %d %s", code, e)
	}
	if code, _, e := runArgs("verify-attest", subject, att); code != exitOK {
		t.Fatalf("verify should pass: %d %s", code, e)
	}
	// Tamper with behavior → verify fails.
	changed := saveCass(t, "c.yaml", anthroTurn("m", "different answer", "end_turn", "", ""))
	if code, _, _ := runArgs("verify-attest", changed, att); code != exitFail {
		t.Fatalf("behavior change must fail verify: %d", code)
	}
	// Usage errors.
	if code, _, _ := runArgs("attest"); code != exitUsage {
		t.Fatalf("attest no-args usage: %d", code)
	}
	if code, _, _ := runArgs("verify-attest", subject); code != exitUsage {
		t.Fatalf("verify-attest one-arg usage: %d", code)
	}
}

func TestAttest_BenignReRecordStillVerifies(t *testing.T) {
	// Same behavior, different volatile ids → semantic manifest identical.
	a := saveCass(t, "a.yaml", anthroTurn("m", "hi", "end_turn", "", ""))
	b := saveCass(t, "b.yaml", anthroTurn("m", "hi", "end_turn", "", ""))
	fa, _ := wirefmt.Load(a)
	fb, _ := wirefmt.Load(b)
	if !analysis.BuildAttestManifest(fa).SemanticEqual(analysis.BuildAttestManifest(fb)) {
		t.Fatal("identical behavior should produce equal semantic manifests")
	}
}

func TestCLI_Refusal(t *testing.T) {
	// A refusal turn.
	p := saveCass(t, "r.yaml", anthroTurn("m", "I cannot help with that request.", "end_turn", "", ""))
	if _, out, _ := runArgs("refusal", p); !strings.Contains(out, "refused") {
		t.Fatalf("expected refused:\n%s", out)
	}
	if code, _, _ := runArgs("refusal"); code != exitUsage {
		t.Fatalf("refusal usage: %d", code)
	}
}

func TestCLI_Redteam(t *testing.T) {
	dir := t.TempDir()
	// One complies, one refuses.
	wirefmt.Save(filepath.Join(dir, "comply.yaml"), &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		anthroTurn("m", "Sure, here you go.", "end_turn", "", "")}})
	wirefmt.Save(filepath.Join(dir, "refuse.yaml"), &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		anthroTurn("m", "I can't do that.", "end_turn", "", "")}})
	// No expect → reports verdicts, exit 0.
	if code, out, _ := runArgs("redteam", dir); code != exitOK || !strings.Contains(out, "refused") || !strings.Contains(out, "complied") {
		t.Fatalf("redteam report: %d\n%s", code, out)
	}
	// Expect manifest that DISAGREES → fail.
	exp := filepath.Join(dir, "expect.yaml")
	os.WriteFile(exp, []byte("expect:\n  refuse.yaml: complied\n"), 0o644)
	if code, _, _ := runArgs("redteam", dir, "--expect", exp); code != exitFail {
		t.Fatalf("redteam mismatch should fail: %d", code)
	}
	// Expect that AGREES → pass.
	os.WriteFile(exp, []byte("expect:\n  refuse.yaml: refused\n  comply.yaml: complied\n"), 0o644)
	if code, _, _ := runArgs("redteam", dir, "--expect", exp); code != exitOK {
		t.Fatalf("redteam agreement should pass: %d", code)
	}
}

func TestCLI_Eval_JudgeFixture(t *testing.T) {
	subject := saveCass(t, "subj.yaml", anthroTurn("m", "Paris is the capital of France.", "end_turn", "", ""))
	fs, _ := wirefmt.Load(subject)
	turns := analysis.CollectTurns(fs)

	// Build a judge cassette whose recorded request is EXACTLY the deterministic
	// judge prompt for this subject, answering PASS — the judge is a fixture.
	rubric := "Is the answer correct? Reply PASS or FAIL."
	prompt := analysis.JudgePrompt(rubric, "judge", turns)
	judgePath := filepath.Join(t.TempDir(), "judge.yaml")
	jf := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Headers: wirefmt.HeadersFromHTTP(map[string][]string{"Content-Type": {"application/json"}}),
			Body:    wirefmt.NewBody(prompt)},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"verdict":"PASS"}`))},
	}}}
	wirefmt.Save(judgePath, jf)
	if code, _, _ := runArgs("rekey", judgePath); code != exitOK {
		t.Fatal("rekey judge")
	}

	suite := filepath.Join(t.TempDir(), "suite.yaml")
	os.WriteFile(suite, []byte("deterministic:\n  finishes_clean: true\njudge:\n  rubric: \""+rubric+"\"\n  model: judge\n  cassette: judge.yaml\n  pass_marker: PASS\n"), 0o644)

	code, out, errOut := runArgs("eval", subject, "--suite", suite, "--judge", judgePath)
	if code != exitOK {
		t.Fatalf("eval should pass: %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "judge verdict: PASS") || !strings.Contains(out, "deterministic assertion") {
		t.Fatalf("eval output:\n%s", out)
	}
	if code, _, _ := runArgs("eval", subject); code != exitUsage {
		t.Fatalf("eval missing suite usage: %d", code)
	}
}

func TestCLI_Watch_DriftAgainstFake(t *testing.T) {
	golden := saveCass(t, "g.yaml", anthroTurn("m", "the answer is 42", "end_turn", "", ""))

	// Fake provider that echoes the SAME response → no drift.
	same := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(anthroSSE("the answer is 42", "end_turn")))
	}))
	defer same.Close()
	if code, out, _ := runArgs("watch", golden, "--base-url", same.URL, "--policy", "prose:0.1"); code != exitOK || !strings.Contains(out, "matches golden") {
		t.Fatalf("no-drift watch: %d\n%s", code, out)
	}

	// Fake provider that returns a very different answer → drift.
	drift := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(anthroSSE("completely different and much longer answer here", "end_turn")))
	}))
	defer drift.Close()
	if code, out, _ := runArgs("watch", golden, "--base-url", drift.URL, "--policy", "prose:0.1"); code != exitFail || !strings.Contains(out, "DRIFT") {
		t.Fatalf("drift watch should fail: %d\n%s", code, out)
	}
	// Missing --base-url → usage.
	if code, _, _ := runArgs("watch", golden); code != exitUsage {
		t.Fatalf("watch no base-url usage: %d", code)
	}
}

func TestPhase4_ErrorPaths(t *testing.T) {
	subject := saveCass(t, "s.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"c":"P"}`))

	// eval: bad subject, bad suite, parse error, deterministic failure, judge miss.
	suite := filepath.Join(t.TempDir(), "su.yaml")
	os.WriteFile(suite, []byte("deterministic:\n  no_tools: [get_weather]\n"), 0o644)
	if code, _, _ := runArgs("eval", subject, "--suite", suite); code != exitFail {
		t.Fatalf("eval deterministic fail expected: %d", code)
	}
	if code, _, _ := runArgs("eval", subject, "--suite", "/no/such.yaml"); code != exitFail {
		t.Fatalf("eval bad suite: %d", code)
	}
	if code, _, _ := runArgs("eval", "/no/such.yaml", "--suite", suite); code != exitFail {
		t.Fatalf("eval bad subject: %d", code)
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte(":\n  not yaml }{"), 0o644)
	if code, _, _ := runArgs("eval", subject, "--suite", bad); code != exitFail {
		t.Fatalf("eval bad suite parse: %d", code)
	}
	// judge with a missing recorded prompt → judge error.
	js := filepath.Join(t.TempDir(), "js.yaml")
	empty := filepath.Join(t.TempDir(), "empty.yaml")
	wirefmt.Save(empty, &wirefmt.File{SchemaVersion: 1})
	os.WriteFile(js, []byte("judge:\n  rubric: hi\n  cassette: "+empty+"\n  pass_marker: PASS\n"), 0o644)
	if code, _, _ := runArgs("eval", subject, "--suite", js); code != exitFail {
		t.Fatalf("eval judge miss should fail: %d", code)
	}

	// attest: bad key, bad cassette.
	if code, _, _ := runArgs("attest", subject, "--key", "/no/key", "-o", filepath.Join(t.TempDir(), "x.att")); code != exitFail {
		t.Fatalf("attest bad key: %d", code)
	}
	badKey := filepath.Join(t.TempDir(), "k")
	os.WriteFile(badKey, []byte("zzzz"), 0o600)
	if code, _, _ := runArgs("attest", subject, "--key", badKey, "-o", filepath.Join(t.TempDir(), "x.att")); code != exitFail {
		t.Fatalf("attest short key: %d", code)
	}
	// verify-attest: malformed att file.
	mal := filepath.Join(t.TempDir(), "m.att")
	os.WriteFile(mal, []byte("not json"), 0o644)
	if code, _, _ := runArgs("verify-attest", subject, mal); code != exitFail {
		t.Fatalf("verify malformed att: %d", code)
	}
	if code, _, _ := runArgs("verify-attest", subject, "/no/such.att"); code != exitFail {
		t.Fatalf("verify missing att: %d", code)
	}

	// redteam bad flag.
	if code, _, _ := runArgs("redteam", "--bad"); code != exitUsage {
		t.Fatalf("redteam bad flag: %d", code)
	}
}

// anthroSSE builds a minimal Anthropic text stream (helper for watch fake).
func anthroSSE(text, stop string) string {
	return "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"" + stop + "\"}}\n\n"
}
