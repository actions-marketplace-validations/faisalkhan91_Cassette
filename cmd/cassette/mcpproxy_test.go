package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cassette "github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// fakeMCPServer is a minimal stdio JSON-RPC server: it reads newline-delimited
// requests from r and writes canned responses to w, closing w on EOF so the
// proxy's server→client pump terminates. tools/call returns a result containing a
// planted AWS-shaped secret to exercise scrub-on-save.
func fakeMCPServer(t *testing.T, r io.Reader, w io.WriteCloser) {
	t.Helper()
	go func() {
		defer w.Close()
		sc := bufio.NewScanner(r)
		enc := json.NewEncoder(w)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || len(req.ID) == 0 {
				continue // notification: no response
			}
			var result any
			switch req.Method {
			case "initialize":
				result = map[string]any{"protocolVersion": "2025-06-18"}
			case "tools/list":
				result = map[string]any{"tools": []any{map[string]any{"name": "add"}}}
			case "tools/call":
				result = map[string]any{"content": []any{
					map[string]any{"type": "text", "text": "sum=3 token=AKIAIOSFODNN7EXAMPLE"},
				}}
			default:
				continue
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
	}()
}

func TestMCPProxy_RecordReplayLoop(t *testing.T) {
	clientReqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // notification, no id
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":2}}}`,
	}, "\n") + "\n"

	srPipeR, srPipeW := io.Pipe() // proxy → server
	soPipeR, soPipeW := io.Pipe() // server → proxy
	fakeMCPServer(t, srPipeR, soPipeW)

	var clientOut bytes.Buffer
	f := pumpMCPProxy(strings.NewReader(clientReqs), &clientOut, srPipeW, soPipeR)

	// Only tools/list and tools/call are teed; initialize and the notification pass
	// through verbatim but are not recorded.
	if len(f.Interactions) != 2 {
		t.Fatalf("recorded %d interactions, want 2 (tools/list + tools/call)", len(f.Interactions))
	}
	var list, call int
	for _, it := range f.Interactions {
		if it.Kind != "mcp" {
			t.Fatalf("interaction kind = %q, want mcp", it.Kind)
		}
		switch it.Request.MCPMethod {
		case "tools/list":
			list++
		case "tools/call":
			call++
			if it.Request.MCPTool != "add" {
				t.Errorf("tools/call tool = %q, want add", it.Request.MCPTool)
			}
		}
	}
	if list != 1 || call != 1 {
		t.Fatalf("teed list=%d call=%d, want 1/1", list, call)
	}
	// All four client frames were forwarded verbatim to the client-facing output.
	if got := bytes.Count(clientOut.Bytes(), []byte("\"jsonrpc\"")); got != 3 {
		t.Errorf("forwarded %d server responses to client, want 3 (init+list+call)", got)
	}

	// Save with scrub-on-save: the planted AKIA key must be redacted, otherwise the
	// SecretScan backstop refuses to write and saveScrubbed returns nonzero.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.yaml")
	if code := saveScrubbedRekeyed(t, path, f); code != exitOK {
		t.Fatalf("saveScrubbed returned %d (secret not scrubbed?)", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("AKIAIOSFODNN7EXAMPLE")) {
		t.Fatal("planted secret survived scrub-on-save")
	}

	// mcp-serve replays the recording for the same client requests with no
	// subprocess and identical (scrubbed) results.
	c, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	replayReqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":1,"b":2}}}`,
	}, "\n") + "\n"
	var replayOut bytes.Buffer
	if err := serveMCP(c, strings.NewReader(replayReqs), &replayOut); err != nil {
		t.Fatalf("serveMCP replay: %v", err)
	}
	out := replayOut.String()
	if !strings.Contains(out, "sum=3") {
		t.Errorf("replay missing recorded tools/call result; got:\n%s", out)
	}
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("replay leaked the planted secret")
	}
	if !strings.Contains(out, `"name":"add"`) {
		t.Errorf("replay missing tools/list result; got:\n%s", out)
	}
}

// TestCmdMCPProxy_Wrappers covers the cmdMCPProxy wrapper: usage on missing
// flags/server command, and the "nothing recorded" exit when the spawned server
// emits no MCP frames. mcpProxyStdin is swapped for an empty reader so the proxy's
// client-facing input EOFs immediately and the test never blocks on a terminal.
func TestCmdMCPProxy_Wrappers(t *testing.T) {
	var out, errb bytes.Buffer

	// No -o and no server command → usage.
	if code := cmdMCPProxy(nil, &out, &errb); code != exitUsage {
		t.Errorf("no args: code=%d want exitUsage", code)
	}
	// -o given but no server command after `--` → usage.
	if code := cmdMCPProxy([]string{"-o", filepath.Join(t.TempDir(), "x.yaml")}, &out, &errb); code != exitUsage {
		t.Errorf("missing server cmd: code=%d want exitUsage", code)
	}

	// A real subprocess that emits no MCP frames → "nothing recorded" + exitFail.
	prev := mcpProxyStdin
	mcpProxyStdin = strings.NewReader("") // EOF immediately → proxy closes server stdin
	defer func() { mcpProxyStdin = prev }()
	errb.Reset()
	out.Reset()
	code := cmdMCPProxy([]string{"-o", filepath.Join(t.TempDir(), "none.yaml"), "--", "true"}, &out, &errb)
	if code != exitFail {
		t.Errorf("no frames: code=%d want exitFail; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "nothing recorded") {
		t.Errorf("expected 'nothing recorded'; got stderr:\n%s", errb.String())
	}
}

// saveScrubbedRekeyed mirrors cmdMCPProxy's tail (rekey + scrub-save) for tests.
func saveScrubbedRekeyed(t *testing.T, path string, f *wirefmt.File) int {
	t.Helper()
	if err := rekey.File(f, match.Config{}); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	return saveScrubbed(path, f, &stderr)
}
