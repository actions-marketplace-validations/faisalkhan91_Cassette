package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// TestPack_Boot_Serve_Shutdown is the headline end-to-end test: build the CLI,
// pack a fixture into a self-contained binary, run that binary, confirm it serves
// the UI + transcript + a replayed provider response offline, then shut it down.
func TestPack_Boot_Serve_Shutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pack is unsupported on Windows")
	}
	bin := buildCLI(t)

	src := filepath.Join(t.TempDir(), "src.yaml")
	f := &wirefmt.File{SchemaVersion: wirefmt.SchemaVersion, Interactions: []*wirefmt.Interaction{{
		Kind: "http",
		Request: wirefmt.Request{Method: "POST", URL: "/v1/messages",
			Body: wirefmt.NewBody([]byte(`{"model":"claude","max_tokens":10}`))},
		Response: wirefmt.Response{Status: 200, Streaming: true, Body: wirefmt.NewBody(wirefix.AnthropicToolUse)},
	}}}
	if err := wirefmt.Save(src, f); err != nil {
		t.Fatal(err)
	}

	demo := filepath.Join(t.TempDir(), "demo")
	if code, out, errOut := runCLI(t, bin, "pack", src, "-o", demo); code != exitOK {
		t.Fatalf("pack failed: %d\n%s\n%s", code, out, errOut)
	}

	cmd := exec.Command(demo, "--addr", "127.0.0.1:0")
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

	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if i := strings.Index(sc.Text(), "http://"); i >= 0 {
				addrCh <- strings.TrimSpace(sc.Text()[i:])
				return
			}
		}
		addrCh <- ""
	}()
	var base string
	select {
	case base = <-addrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("packed binary did not announce its address")
	}
	if base == "" {
		t.Fatal("could not parse packed-binary address")
	}

	// Poll until the server is accepting connections.
	waitUp(t, base+"/")

	// GET / → UI.
	if body := smokeGet(t, base+"/"); !bytes.Contains(body, []byte("<!doctype html")) {
		t.Fatalf("UI not served: %s", body[:min(80, len(body))])
	}
	// GET /transcript.json → recorded turns.
	var turns []packTurn
	if err := json.Unmarshal(smokeGet(t, base+"/transcript.json"), &turns); err != nil {
		t.Fatalf("transcript.json: %v", err)
	}
	if len(turns) != 1 || len(turns[0].ToolCalls) == 0 || turns[0].ToolCalls[0].Name != "get_weather" {
		t.Fatalf("unexpected transcript: %+v", turns)
	}
	// POST the recorded request → byte-replayed SSE.
	resp, err := http.Post(base+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"claude","max_tokens":10}`))
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Contains(rb, []byte("get_weather")) {
		t.Fatalf("served API did not replay the recording: %s", rb)
	}

	// Graceful shutdown on SIGTERM, within a deadline (Kill is the deferred backstop).
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("packed binary did not exit after SIGTERM")
	}
}

func waitUp(t *testing.T, url string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server never came up")
}

func smokeGet(t *testing.T, url string) []byte {
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
