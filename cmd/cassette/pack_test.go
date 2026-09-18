package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// --- trailer / bundle primitives (pure, in-process) -------------------------

func TestTrailer_RoundTrip(t *testing.T) {
	host := []byte("PRETEND-HOST-BINARY-BYTES-0123456789")
	// A payload that itself contains the magic bytes must not confuse detection.
	payload := append([]byte("front "), trailerMagic[:]...)
	payload = append(payload, []byte(" back")...)

	var buf bytes.Buffer
	buf.Write(host)
	if err := writeTrailer(&buf, payload); err != nil {
		t.Fatal(err)
	}
	full := buf.Bytes()
	got, ok, err := readTrailerAt(bytes.NewReader(full), int64(len(full)))
	if err != nil || !ok {
		t.Fatalf("expected packed, got ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: %q vs %q", got, payload)
	}
	// The host prefix must be untouched (we appended, not overwrote).
	if !bytes.Equal(full[:len(host)], host) {
		t.Fatal("host prefix was modified")
	}
}

func TestReadTrailer_PlainIsPlainMode(t *testing.T) {
	plain := bytes.Repeat([]byte("not-a-packed-binary"), 8)
	got, ok, err := readTrailerAt(bytes.NewReader(plain), int64(len(plain)))
	if got != nil || ok || err != nil {
		t.Fatalf("plain file must be plain mode, got %q ok=%v err=%v", got, ok, err)
	}
	// Too small to hold a trailer.
	if _, ok, err := readTrailerAt(bytes.NewReader([]byte("x")), 1); ok || err != nil {
		t.Fatalf("tiny file: ok=%v err=%v", ok, err)
	}
}

func TestReadTrailer_TruncatedTrailer(t *testing.T) {
	// Valid magic at EOF but a length that overruns the file → loud error, no panic.
	var buf bytes.Buffer
	buf.Write([]byte("short"))
	var lenbytes [8]byte
	// claim a 1<<40-byte payload that cannot exist
	lenbytes[5] = 1
	buf.Write(lenbytes[:])
	buf.Write(trailerMagic[:])
	full := buf.Bytes()
	if _, ok, err := readTrailerAt(bytes.NewReader(full), int64(len(full))); ok || err == nil {
		t.Fatalf("overrunning trailer must error: ok=%v err=%v", ok, err)
	}
}

func TestBundle_RoundTrip(t *testing.T) {
	in := []bundleFile{
		{Name: "cassette.yaml", Data: []byte("schema_version: 1\n")},
		{Name: "empty", Data: nil},
		{Name: "bin", Data: []byte{0, 1, 2, 255, 254}},
	}
	out, err := decodeBundle(encodeBundle(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("count: %d vs %d", len(out), len(in))
	}
	for i := range in {
		if out[i].Name != in[i].Name || !bytes.Equal(out[i].Data, in[i].Data) {
			t.Fatalf("frame %d mismatch: %+v vs %+v", i, out[i], in[i])
		}
	}
}

func TestBundle_RejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{
		nil,
		{1, 2, 3},                         // truncated count
		{2, 0, 0, 0, 9, 0},                // count=2 but truncated
		{1, 0, 0, 0, 4, 0, 'a', 'b', 'c'}, // nameLen=4 overruns
		{1, 0, 0, 0, 1, 0, 'x', 99, 0, 0, 0, 0, 0, 0, 0}, // dataLen=99 overruns buffer
	} {
		if _, err := decodeBundle(b); err == nil {
			t.Fatalf("expected error for %v", b)
		}
	}
}

// --- transcript projection --------------------------------------------------

func anthropicToolUseFile(t *testing.T) *wirefmt.File {
	t.Helper()
	return &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind:     "http",
		Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)},
	}}}
}

func TestRenderTranscript_AnthropicToolUse(t *testing.T) {
	var turns []packTurn
	if err := json.Unmarshal(renderTranscriptJSON(anthropicToolUseFile(t)), &turns); err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	tr := turns[0]
	if tr.Kind != "http" || tr.URL != "/v1/messages" || tr.Status != 200 {
		t.Fatalf("turn meta wrong: %+v", tr)
	}
	if !strings.Contains(tr.Text, "Let me check the weather.") {
		t.Fatalf("text missing: %q", tr.Text)
	}
	if len(tr.ToolCalls) == 0 || tr.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("expected get_weather tool call, got %+v", tr.ToolCalls)
	}
}

func TestRenderTranscript_Coffee(t *testing.T) {
	streams := wirefix.CoffeeTurns()
	if len(streams) == 0 {
		t.Skip("no coffee fixtures")
	}
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion}
	for _, s := range streams {
		f.Interactions = append(f.Interactions, &wirefmt.Interaction{
			Kind:     "http",
			Request:  wirefmt.Request{Method: "POST", URL: "/v1/messages"},
			Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(s)},
		})
	}
	var turns []packTurn
	if err := json.Unmarshal(renderTranscriptJSON(f), &turns); err != nil {
		t.Fatal(err)
	}
	if len(turns) != len(streams) {
		t.Fatalf("turns %d != streams %d", len(turns), len(streams))
	}
	// The coffee scenario calls a sequence of tools then ends with prose.
	tools := 0
	for _, tr := range turns {
		tools += len(tr.ToolCalls)
	}
	if tools < 4 {
		t.Fatalf("expected several coffee tool calls, got %d", tools)
	}
	last := turns[len(turns)-1]
	if last.FinishReason == "" {
		t.Fatalf("final coffee turn should carry a finish reason: %+v", last)
	}
}

// --- serving (in-process httptest; counts the handler bodies) ---------------

func TestServePackedMux(t *testing.T) {
	f := anthropicToolUseFile(t)
	raw, err := wirefmt.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cassette.OpenBytes("packed", raw, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(servePackedMux(c, f))
	defer srv.Close()

	// GET / → UI HTML.
	body := mustGet(t, srv.URL+"/")
	if !bytes.Contains(body, []byte("<!doctype html")) || !bytes.Contains(body, []byte("cassette pack")) {
		t.Fatalf("index not served: %s", body[:min(80, len(body))])
	}
	// GET /transcript.json → turns.
	var turns []packTurn
	if err := json.Unmarshal(mustGet(t, srv.URL+"/transcript.json"), &turns); err != nil {
		t.Fatalf("transcript.json: %v", err)
	}
	if len(turns) != 1 || turns[0].ToolCalls[0].Name != "get_weather" {
		t.Fatalf("transcript turns wrong: %+v", turns)
	}
	// An unrecorded provider POST is a hard 404 miss (never proxied).
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"unrecorded":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("miss should be 404, got %d", resp.StatusCode)
	}
	if c.Dials() != 0 {
		t.Fatalf("packed serving must make zero dials, got %d", c.Dials())
	}
}

func mustGet(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return b
}

// --- cmdPack (in-process) ---------------------------------------------------

func TestPack_ArgErrors(t *testing.T) {
	good := filepath.Join(t.TempDir(), "g.yaml")
	if err := wirefmt.Save(good, anthropicToolUseFile(t)); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runArgs("pack"); code != exitUsage {
		t.Fatalf("no args: %d", code)
	}
	if code, _, _ := runArgs("pack", good); code != exitUsage {
		t.Fatalf("missing -o: %d", code)
	}
	if code, _, _ := runArgs("pack", "/no/such.yaml", "-o", filepath.Join(t.TempDir(), "x")); code != exitFail {
		t.Fatalf("bad path: %d", code)
	}
	if code, _, _ := runArgs("pack", good, "--bogus"); code != exitUsage {
		t.Fatalf("unknown flag: %d", code)
	}
	// The -o=PATH / --out=PATH forms also produce a packed binary.
	out := filepath.Join(t.TempDir(), "demo")
	if code, _, errOut := runArgs("pack", good, "--out="+out); code != exitOK {
		t.Fatalf("--out= form: %d %s", code, errOut)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("--out= produced no binary: %v", err)
	}
}

func TestPack_GeneratesPackedFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.yaml")
	if err := wirefmt.Save(src, anthropicToolUseFile(t)); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "demo")
	code, stdout, stderr := runArgs("pack", src, "-o", out)
	if code != exitOK {
		t.Fatalf("pack failed: %d %s %s", code, stdout, stderr)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("packed file missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("packed file is not executable")
	}
	// The trailer must decode back to the source cassette.
	rf, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()
	payload, ok, err := readTrailerAt(rf, info.Size())
	if err != nil || !ok {
		t.Fatalf("produced binary is not packed: ok=%v err=%v", ok, err)
	}
	files, err := decodeBundle(payload)
	if err != nil {
		t.Fatal(err)
	}
	var yaml []byte
	for _, bf := range files {
		if bf.Name == "cassette.yaml" {
			yaml = bf.Data
		}
	}
	want, _ := os.ReadFile(src)
	if !bytes.Equal(yaml, want) {
		t.Fatal("embedded cassette.yaml does not match source")
	}
}

// setupPacked does all the fallible work (decode → open → listen); drive its
// success and error branches in-process without entering the serve loop.
func TestSetupPacked(t *testing.T) {
	raw, err := wirefmt.Marshal(anthropicToolUseFile(t))
	if err != nil {
		t.Fatal(err)
	}
	good := encodeBundle([]bundleFile{{Name: "cassette.yaml", Data: raw}})

	// Success: binds a listener and returns a server; we close instead of serving.
	var out, errb bytes.Buffer
	srv, ln, code := setupPacked(good, nil, []string{"--addr", "127.0.0.1:0"}, &out, &errb)
	if srv == nil || ln == nil || code != exitOK {
		t.Fatalf("setupPacked success: code=%d srv=%v err=%s", code, srv, errb.String())
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:") {
		t.Fatalf("address not announced: %s", out.String())
	}
	_ = ln.Close()

	// Error branches.
	if _, _, c := setupPacked(nil, errMarker, nil, io.Discard, io.Discard); c != exitFail {
		t.Fatalf("detect error → exitFail, got %d", c)
	}
	if _, _, c := setupPacked([]byte{9, 9, 9}, nil, nil, io.Discard, io.Discard); c != exitFail {
		t.Fatalf("garbage bundle → exitFail, got %d", c)
	}
	noYAML := encodeBundle([]bundleFile{{Name: "other", Data: []byte("x")}})
	if _, _, c := setupPacked(noYAML, nil, nil, io.Discard, io.Discard); c != exitFail {
		t.Fatalf("missing cassette.yaml → exitFail, got %d", c)
	}
	badYAML := encodeBundle([]bundleFile{{Name: "cassette.yaml", Data: []byte("schema_version: 2\n}{")}})
	if _, _, c := setupPacked(badYAML, nil, nil, io.Discard, io.Discard); c != exitFail {
		t.Fatalf("unparseable cassette → exitFail, got %d", c)
	}
}

var errMarker = errReader("boom")

type errReader string

func (e errReader) Error() string { return string(e) }

// readTrailer inspects the running test binary, which is not packed → plain mode.
func TestReadTrailer_OnTestBinary(t *testing.T) {
	payload, ok, err := readTrailer()
	if payload != nil || ok || err != nil {
		t.Fatalf("test binary must be plain: ok=%v err=%v", ok, err)
	}
}

// Packing an already-packed binary must re-base onto the original host prefix,
// not nest payloads (the double-pack trap).
func TestWritePacked_RebasesPacked(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "host")
	if err := os.WriteFile(host, []byte("HOSTBINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(dir, "p1")
	if err := writePacked(host, p1, encodeBundle([]bundleFile{{Name: "cassette.yaml", Data: []byte("v1")}})); err != nil {
		t.Fatal(err)
	}
	// Re-pack p1 (already packed) with a newer payload.
	p2 := filepath.Join(dir, "p2")
	if err := writePacked(p1, p2, encodeBundle([]bundleFile{{Name: "cassette.yaml", Data: []byte("v2-newer")}})); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(p2)
	if !bytes.HasPrefix(b2, []byte("HOSTBINARY")) {
		t.Fatal("re-pack lost the original host prefix")
	}
	info, _ := os.Stat(p2)
	rf, _ := os.Open(p2)
	defer rf.Close()
	payload, ok, err := readTrailerAt(rf, info.Size())
	if err != nil || !ok {
		t.Fatalf("p2 not packed: ok=%v err=%v", ok, err)
	}
	files, _ := decodeBundle(payload)
	if len(files) != 1 || string(files[0].Data) != "v2-newer" {
		t.Fatalf("re-pack did not replace payload: %+v", files)
	}
	// Size must not grow with the stale inner payload (no nesting).
	want := int64(len("HOSTBINARY")) + int64(len(payload)) + trailerSize
	if info.Size() != want {
		t.Fatalf("re-pack nested payloads: size=%d want=%d", info.Size(), want)
	}
}

func TestPack_RefusesSecrets(t *testing.T) {
	src := writeTemp(t, secretCassette)
	out := filepath.Join(t.TempDir(), "demo")
	code, _, stderr := runArgs("pack", src, "-o", out)
	if code != exitFail {
		t.Fatalf("packing a cassette with secrets must fail, got %d", code)
	}
	if !strings.Contains(stderr, "refusing to pack") {
		t.Fatalf("expected refusal message, got: %s", stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("no binary should be produced when secrets are present")
	}
}

func TestRenderTranscript_MCP(t *testing.T) {
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "mcp",
		Request: wirefmt.Request{MCPMethod: "tools/call", MCPTool: "get_weather",
			Body: wirefmt.NewBody([]byte(`{"city":"Paris"}`))},
		Response: wirefmt.Response{Body: wirefmt.NewBody([]byte(`{"temp":"18C"}`))},
	}}}
	var turns []packTurn
	if err := json.Unmarshal(renderTranscriptJSON(f), &turns); err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Kind != "mcp" || turns[0].Tool != "get_weather" {
		t.Fatalf("mcp turn wrong: %+v", turns)
	}
	if !strings.Contains(turns[0].Text, "18C") {
		t.Fatalf("mcp result text missing: %+v", turns[0])
	}

	// A non-streaming HTTP turn has no decodable transcript: it still renders its
	// request/response metadata, with empty role/text.
	nf := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind:     "http",
		Request:  wirefmt.Request{Method: "GET", URL: "/v1/models"},
		Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"data":[]}`))},
	}}}
	var nt []packTurn
	if err := json.Unmarshal(renderTranscriptJSON(nf), &nt); err != nil {
		t.Fatal(err)
	}
	if len(nt) != 1 || nt[0].URL != "/v1/models" || nt[0].Text != "" || nt[0].Role != "" {
		t.Fatalf("non-streaming turn should be meta-only: %+v", nt)
	}
}

func TestPack_EmbeddedUIAssetExists(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("embedded index.html is empty")
	}
	for _, want := range []string{"<!doctype html", "transcript.json", "id=\"step\""} {
		if !bytes.Contains(indexHTML, []byte(want)) {
			t.Fatalf("embedded UI missing %q", want)
		}
	}
}
