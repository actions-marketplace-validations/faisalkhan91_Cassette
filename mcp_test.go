package cassette_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faisalkhan91/cassette"
)

type addArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addResult struct {
	Sum int `json:"sum"`
}

// inMemoryAddServer starts an in-process MCP server exposing "add" and returns a
// connected client session.
func inMemoryAddServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "mem", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "add", Description: "add two ints"},
		func(_ context.Context, _ *mcp.CallToolRequest, in addArgs) (*mcp.CallToolResult, addResult, error) {
			sum := in.A + in.B
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("sum=%d", sum)}},
			}, addResult{Sum: sum}, nil
		})
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty tool result content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content not TextContent: %T", res.Content[0])
	}
	return tc.Text
}

func TestMCP_InMemory_RecordReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mcp_mem.yaml")
	args := map[string]any{"a": 2, "b": 3}

	// RECORD against the real in-process session.
	session := inMemoryAddServer(t)
	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	mc := rec.MCP(session)
	tools, err := mc.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("record ListTools: %v", err)
	}
	if len(tools.Tools) == 0 || tools.Tools[0].Name != "add" {
		t.Fatalf("unexpected tools: %+v", tools.Tools)
	}
	res, err := mc.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: args})
	if err != nil {
		t.Fatalf("record CallTool: %v", err)
	}
	if got := textOf(t, res); got != "sum=5" {
		t.Fatalf("record result = %q, want sum=5", got)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatalf("record save: %v", err)
	}

	// REPLAY with no real session (nil) — proves no transport is touched.
	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	rmc := rp.MCP(nil)
	rtools, err := rmc.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("replay ListTools: %v", err)
	}
	if len(rtools.Tools) == 0 || rtools.Tools[0].Name != "add" {
		t.Fatalf("replay tools mismatch: %+v", rtools.Tools)
	}
	rres, err := rmc.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: args})
	if err != nil {
		t.Fatalf("replay CallTool: %v", err)
	}
	if got := textOf(t, rres); got != "sum=5" {
		t.Fatalf("replay result = %q, want sum=5", got)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("replay verify: %v", err)
	}

	// An unrecorded call must error loudly (no silent miss).
	if _, err := rmc.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: map[string]any{"a": 9, "b": 9}}); err == nil {
		t.Fatal("expected error for unrecorded mcp call")
	}
}

func TestRecordReplay_MCP_Stdio(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mcp_stdio.yaml")

	// Build the fixture server binary from this repo (no network).
	bin := filepath.Join(tmp, "mcpfixture")
	build := exec.Command("go", "build", "-o", bin, "./cmd/mcpfixture")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mcpfixture: %v\n%s", err, out)
	}

	// RECORD: drive a REAL child process over stdio.
	recMarker := filepath.Join(tmp, "rec_marker")
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "CASSETTE_MCP_MARKER="+recMarker)
	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to child: %v", err)
	}

	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	mc := rec.MCP(session)
	if _, err := mc.ListTools(ctx, nil); err != nil {
		t.Fatalf("record ListTools: %v", err)
	}
	res, err := mc.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: map[string]any{"a": 7, "b": 8}})
	if err != nil {
		t.Fatalf("record CallTool: %v", err)
	}
	if got := textOf(t, res); got != "sum=15" {
		t.Fatalf("record result = %q, want sum=15", got)
	}
	if err := rec.VerifyError(); err != nil {
		t.Fatalf("record save: %v", err)
	}
	_ = session.Close()

	if _, err := os.Stat(recMarker); err != nil {
		t.Fatalf("expected child to have exec'd in record (marker missing): %v", err)
	}

	// REPLAY: the child is NEVER launched. We pass a nil caller and a marker path
	// that must remain absent.
	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	rmc := rp.MCP(nil)
	if _, err := rmc.ListTools(ctx, nil); err != nil {
		t.Fatalf("replay ListTools: %v", err)
	}
	rres, err := rmc.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: map[string]any{"a": 7, "b": 8}})
	if err != nil {
		t.Fatalf("replay CallTool: %v", err)
	}
	if got := textOf(t, rres); got != "sum=15" {
		t.Fatalf("replay result = %q, want sum=15", got)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("replay verify: %v", err)
	}
	// The fixture binary was never exec'd in replay.
	replayMarker := filepath.Join(tmp, "replay_marker")
	if _, err := os.Stat(replayMarker); !os.IsNotExist(err) {
		t.Fatalf("replay must not exec the child; marker unexpectedly present: %v", err)
	}
}
