package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"gopkg.in/yaml.v3"
)

func TestCLI_Graft_Semantic(t *testing.T) {
	in := saveCass(t, "in.yaml",
		anthroTurn("m", "the answer is 42", "end_turn", "", ""),
		anthroTurn("m", "second turn", "end_turn", "", ""))
	out := filepath.Join(t.TempDir(), "out.yaml")
	if code, _, e := runArgs("graft", in, "--turn", "0", "--set", "text=the answer is 43", "-o", out); code != exitOK {
		t.Fatalf("graft: %d %s", code, e)
	}
	gf, _ := wirefmt.Load(out)
	tr, _, ok := analysis.DecodeInteraction(gf.Interactions[0])
	if !ok || tr.Text != "the answer is 43" {
		t.Fatalf("grafted turn 0 text = %q", tr.Text)
	}
	// The request key is unchanged (we only edited the response) → still replayable.
	orig, _ := wirefmt.Load(in)
	if gf.Interactions[0].Request.MatchKey != orig.Interactions[0].Request.MatchKey {
		t.Fatal("graft must not change the request match key")
	}
	// Turn 1 response is byte-identical (only turn 0 was grafted).
	if string(gf.Interactions[1].Response.Body.Bytes()) != string(orig.Interactions[1].Response.Body.Bytes()) {
		t.Fatal("graft changed an untouched turn")
	}
}

func TestCLI_Graft_ToolsAndFinish(t *testing.T) {
	in := saveCass(t, "in.yaml", anthroTurn("m", "ok", "tool_use", "get_weather", `{"city":"Paris"}`))
	out := filepath.Join(t.TempDir(), "o.yaml")
	// Change the tool args + finish reason.
	if code, _, e := runArgs("graft", in, "--turn", "0", "--set", `tool.0.args={"city":"London"}`, "--set", "finish=end_turn", "-o", out); code != exitOK {
		t.Fatalf("graft tools: %d %s", code, e)
	}
	gf, _ := wirefmt.Load(out)
	tr, _, _ := analysis.DecodeInteraction(gf.Interactions[0])
	if len(tr.ToolCalls) != 1 || !strings.Contains(tr.ToolCalls[0].Args, "London") || tr.FinishReason != "end_turn" {
		t.Fatalf("graft tool edit wrong: %+v", tr)
	}
	// drop-tool removes it.
	out2 := filepath.Join(t.TempDir(), "o2.yaml")
	if code, _, _ := runArgs("graft", in, "--turn", "0", "--drop-tool", "0", "-o", out2); code != exitOK {
		t.Fatalf("graft drop-tool: %d", code)
	}
	gf2, _ := wirefmt.Load(out2)
	if tr2, _, _ := analysis.DecodeInteraction(gf2.Interactions[0]); len(tr2.ToolCalls) != 0 {
		t.Fatalf("drop-tool left %d tools", len(tr2.ToolCalls))
	}
}

func TestCLI_Graft_DownstreamWarningAndTruncate(t *testing.T) {
	// Turn 1's request embeds turn 0's output → grafting turn 0 makes it counterfactual.
	t0 := anthroTurn("m", "SECRETPLAN", "end_turn", "", "")
	t1 := &wirefmt.Interaction{Kind: "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages", Body: wirefmt.NewBody([]byte(`{"model":"m","messages":[{"role":"assistant","content":"SECRETPLAN"}]}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))}}
	in := saveCass(t, "in.yaml", t0, t1)
	out := filepath.Join(t.TempDir(), "o.yaml")
	_, _, errOut := runArgs("graft", in, "--turn", "0", "--set", "text=NEWPLAN", "-o", out)
	if !strings.Contains(errOut, "counterfactual") {
		t.Fatalf("expected downstream warning:\n%s", errOut)
	}
	// --truncate-after 0 drops turn 1.
	out2 := filepath.Join(t.TempDir(), "o2.yaml")
	runArgs("graft", in, "--turn", "0", "--set", "text=NEWPLAN", "--truncate-after", "0", "-o", out2)
	gf, _ := wirefmt.Load(out2)
	if len(gf.Interactions) != 1 {
		t.Fatalf("truncate-after should leave 1 interaction, got %d", len(gf.Interactions))
	}
}

func TestCLI_Graft_Errors(t *testing.T) {
	in := saveCass(t, "in.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	out := filepath.Join(t.TempDir(), "o.yaml")
	if code, _, _ := runArgs("graft", in, "--turn", "0"); code != exitUsage {
		t.Fatalf("missing -o usage: %d", code)
	}
	if code, _, _ := runArgs("graft", in, "--turn", "9", "--set", "text=x", "-o", out); code != exitFail {
		t.Fatalf("out-of-range turn: %d", code)
	}
	if code, _, _ := runArgs("graft", in, "--turn", "0", "--set", "bogus", "-o", out); code != exitFail {
		t.Fatalf("bad set: %d", code)
	}
	if code, _, _ := runArgs("graft", in, "--turn", "0", "--set", `tool.0.args=notjson`, "-o", out); code != exitFail {
		t.Fatalf("invalid tool args json: %d", code)
	}
	// Non-streaming (unary) turn cannot be grafted.
	unary := filepath.Join(t.TempDir(), "u.yaml")
	wirefmt.Save(unary, &wirefmt.File{SchemaVersion: 1, Interactions: []*wirefmt.Interaction{
		{Kind: "http", Request: wirefmt.Request{Method: "POST", URL: "/v1/messages"}, Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"x":1}`))}}}})
	if code, _, _ := runArgs("graft", unary, "--turn", "0", "--set", "text=x", "-o", out); code != exitFail {
		t.Fatalf("graft unary should fail: %d", code)
	}
}

func TestCLI_Distill(t *testing.T) {
	in := saveCass(t, "in.yaml", anthroTurn("m", "placeholder", "end_turn", "", ""))
	dir := t.TempDir()
	vals := filepath.Join(dir, "vals.txt")
	os.WriteFile(vals, []byte("# comment\nParis\n\nLondon\nTokyo\n"), 0o644)
	outDir := filepath.Join(dir, "corpus")
	if code, _, e := runArgs("distill", in, "--turn", "0", "--slot", "text", "--values", vals, "-o", outDir); code != exitOK {
		t.Fatalf("distill: %d %s", code, e)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "corpus.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Corpus []corpusEntry `yaml:"corpus"`
	}
	yaml.Unmarshal(raw, &c)
	if len(c.Corpus) != 3 {
		t.Fatalf("expected 3 corpus entries, got %d", len(c.Corpus))
	}
	// Each generated cassette replays to its recorded expected digest.
	for _, e := range c.Corpus {
		f, err := wirefmt.Load(filepath.Join(outDir, e.Cassette))
		if err != nil {
			t.Fatal(err)
		}
		tr, _, _ := analysis.DecodeInteraction(f.Interactions[0])
		if tr.Digest() != e.ExpectedDigest {
			t.Fatalf("%s: digest %s != expected %s", e.Cassette, tr.Digest(), e.ExpectedDigest)
		}
		if tr.Text != e.Value {
			t.Fatalf("%s: text %q != value %q", e.Cassette, tr.Text, e.Value)
		}
	}
}

func TestCLI_Distill_Errors(t *testing.T) {
	in := saveCass(t, "in.yaml", anthroTurn("m", "x", "end_turn", "", ""))
	dir := t.TempDir()
	if code, _, _ := runArgs("distill", in, "--turn", "0", "--slot", "text"); code != exitUsage {
		t.Fatalf("missing values/out usage: %d", code)
	}
	if code, _, _ := runArgs("distill", in, "--turn", "0", "--slot", "bad", "--values", "/x", "-o", dir); code != exitUsage {
		t.Fatalf("bad slot usage: %d", code)
	}
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, []byte("\n#only comment\n"), 0o644)
	if code, _, _ := runArgs("distill", in, "--turn", "0", "--slot", "text", "--values", empty, "-o", dir); code != exitFail {
		t.Fatalf("no values should fail: %d", code)
	}
}
