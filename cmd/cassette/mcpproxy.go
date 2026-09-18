package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// mcpProxyStdin is the client-facing input the proxy tees from — os.Stdin in
// production, overridable in tests so they never block on a terminal.
var mcpProxyStdin io.Reader = os.Stdin

// cmdMCPProxy records a live MCP stdio session by sitting transparently between an
// MCP client (this process's stdin/stdout) and the real server it spawns
// (`-- <cmd> <args…>`). Every newline-delimited JSON-RPC frame is forwarded
// verbatim in both directions, and each tools/call and tools/list request is teed
// — paired with its result by JSON-RPC id — into a cassette that `cassette
// mcp-serve` replays with zero subprocess launch. It reimplements no MCP
// semantics: it is a pure passthrough recorder.
func cmdMCPProxy(args []string, stdout, stderr io.Writer) int {
	fs := newFlags().valFlag("out").alias("o", "out")
	in, ok := parseOrUsage(fs, args, mcpProxyUsageText, stderr)
	if !ok {
		return exitUsage
	}
	out := in.str("out")
	server := in.args() // the server command, after "--"
	if out == "" || len(server) == 0 {
		return mcpProxyUsage(stderr)
	}

	cmd := exec.Command(server[0], server[1:]...)
	serverIn, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	cmd.Stderr = stderr // surface the server's own logs unchanged
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "cassette: cannot start MCP server %q: %v\n", server[0], err)
		return exitFail
	}

	f := pumpMCPProxy(mcpProxyStdin, stdout, serverIn, serverOut)
	_ = cmd.Wait()

	if len(f.Interactions) == 0 {
		fmt.Fprintln(stderr, "cassette: no tools/call or tools/list frames seen; nothing recorded")
		return exitFail
	}
	if err := rekey.File(f, match.Config{}); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	return saveScrubbed(out, f, stderr)
}

// rpcFrame is the union of a JSON-RPC request and response, enough to correlate
// tools/* calls with their results by id.
type rpcFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

type pendingCall struct {
	method string
	tool   string
	body   []byte // marshaled exactly as the in-process recorder / mcp-serve would key it
}

// pumpMCPProxy forwards newline-delimited JSON-RPC frames between a client
// (clientIn/clientOut) and a server (serverIn/serverOut), verbatim, while teeing
// tools/call and tools/list pairs into the returned file. It returns once both
// directions reach EOF; on client EOF it closes serverIn (if it is a Closer) so a
// real subprocess sees its stdin close and exits. Frames are recorded in the same
// stored shape as the in-process MCP recorder so `mcp-serve` keys match.
func pumpMCPProxy(clientIn io.Reader, clientOut io.Writer, serverIn io.Writer, serverOut io.Reader) *wirefmt.File {
	var (
		mu      sync.Mutex
		pending = map[string]pendingCall{}
		rec     []*wirefmt.Interaction
		wg      sync.WaitGroup
	)
	const maxLine = 16 * 1024 * 1024

	wg.Add(2)
	// client → server
	go func() {
		defer wg.Done()
		if c, ok := serverIn.(io.Closer); ok {
			defer c.Close()
		}
		sc := bufio.NewScanner(clientIn)
		sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		for sc.Scan() {
			line := sc.Bytes()
			// Register the pending call BEFORE forwarding to the server, so the
			// server's response can never be read on the other pump before its
			// request is recorded (the request/response correlation would miss).
			var fr rpcFrame
			if json.Unmarshal(line, &fr) == nil && len(fr.ID) > 0 && fr.Method != "" {
				switch fr.Method {
				case "tools/call":
					var p struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}
					_ = json.Unmarshal(fr.Params, &p)
					body, _ := json.Marshal(p.Arguments) // nil → "null", matching marshalArgs
					mu.Lock()
					pending[string(fr.ID)] = pendingCall{method: fr.Method, tool: p.Name, body: body}
					mu.Unlock()
				case "tools/list":
					var p mcp.ListToolsParams
					if len(fr.Params) > 0 {
						_ = json.Unmarshal(fr.Params, &p)
					}
					body, _ := json.Marshal(&p) // mirror marshalArgs(&ListToolsParams{})
					mu.Lock()
					pending[string(fr.ID)] = pendingCall{method: fr.Method, body: body}
					mu.Unlock()
				}
			}
			writeLine(serverIn, line)
		}
	}()
	// server → client
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(serverOut)
		sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		for sc.Scan() {
			line := sc.Bytes()
			writeLine(clientOut, line)
			var fr rpcFrame
			if json.Unmarshal(line, &fr) != nil || len(fr.ID) == 0 || len(fr.Result) == 0 {
				continue
			}
			mu.Lock()
			pc, ok := pending[string(fr.ID)]
			if ok {
				delete(pending, string(fr.ID))
				it := &wirefmt.Interaction{Kind: "mcp"}
				it.Request.MCPMethod = pc.method
				it.Request.MCPTool = pc.tool
				it.Request.Body = wirefmt.NewBody(pc.body)
				it.Response.Body = wirefmt.NewBody(append([]byte(nil), fr.Result...))
				rec = append(rec, it)
			}
			mu.Unlock()
		}
	}()
	wg.Wait()

	return &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: rec}
}

// writeLine writes one JSON-RPC frame followed by the newline delimiter, ignoring
// write errors (a broken pipe means the peer is gone and the pump will end).
func writeLine(w io.Writer, line []byte) {
	buf := make([]byte, 0, len(line)+1)
	buf = append(buf, line...)
	buf = append(buf, '\n')
	_, _ = w.Write(buf)
}

const mcpProxyUsageText = "usage: cassette mcp-proxy -o <cassette.yaml> -- <server-cmd> [args...]"

func mcpProxyUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, mcpProxyUsageText)
	return exitUsage
}
