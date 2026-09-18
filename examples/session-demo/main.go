// Command session-demo is a 5-prompt conversational "live session": a user
// pair-programs with Claude over five turns. The session runs once LIVE
// (recording each turn against an in-process fake provider) and once in REPLAY
// with the network blocked — the same five replies, byte-identical, zero dials.
//
//	go run ./examples/session-demo            # run it (colorized on a TTY / FORCE_COLOR)
//
// The assistant replies are synthesized with cassette's own wireenc encoder, so
// the demo needs no API key and no captured fixtures.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/semequal"
)

// The five-prompt scenario: debugging a flaky test, ending (on brand) at cassette.
var convo = []struct{ user, assistant string }{
	{"My Go test TestCheckout is flaky — it fails about 1 run in 5. Any ideas?",
		"Flaky 1-in-5 is almost always a race or a time dependency. Is it order-dependent, or timing-based?"},
	{"Timing — it calls time.Now() and then sleeps 50ms.",
		"That's the cause. Inject a clock instead of sleeping, and assert against a fixed instant."},
	{"How do I inject a clock cleanly in Go?",
		"Add a `clock func() time.Time` field that defaults to time.Now; in the test, set it to a fixed time."},
	{"Done — it's deterministic now. How do I lock that in for CI?",
		"Record the run with cassette and replay it offline: same bytes every run, no network, no API key."},
	{"And if the behavior regresses later?",
		"`cassette diff` pinpoints the first divergent turn — commit a .castiron so CI reproduces it and goes green when fixed."},
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "session-demo FAILED:", err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	c := ansi(out)
	path := filepath.Join(os.TempDir(), "cassette-session.yaml")

	// --- LIVE: record the five-turn session against the fake provider. ---
	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		return err
	}
	replies := make([]http.Handler, len(convo))
	for i, t := range convo {
		sse := wireenc.EncodeAnthropicSSE(
			semequal.Transcript{Role: "assistant", Text: t.assistant, FinishReason: "end_turn"},
			wireenc.Envelope{MessageID: fmt.Sprintf("msg_live_%d", i+1), ModelID: "claude-opus-4-6"})
		replies[i] = fakeprovider.StreamHandler(sse, fakeprovider.PerFrame)
	}
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.Sequence(replies...),
	}))
	defer srv.Close()

	live, err := converse(rec.HTTPClient(), srv.URL, out, c, true)
	if err != nil {
		return err
	}
	if err := rec.VerifyError(); err != nil { // saves the cassette
		return err
	}

	// --- REPLAY: same five prompts, network blocked. ---
	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, c("1;34", "● same session — replayed offline (network blocked)"))
	replay, err := converse(rp.HTTPClient(), "http://replay.invalid", out, c, false)
	if err != nil {
		return err
	}
	if err := rp.VerifyError(); err != nil {
		return err
	}

	identical := len(live) == len(replay)
	for i := range live {
		if i < len(replay) && live[i] != replay[i] {
			identical = false
		}
	}
	status := c("32", "✓ identical transcript")
	if !identical {
		status = c("31", "✗ transcript differs")
	}
	fmt.Fprintf(out, "%s  ·  %d prompts  ·  outbound dials: %s\n",
		status, len(convo), c("32", fmt.Sprintf("%d", rp.Dials())))
	fmt.Fprintln(out, c("2", fmt.Sprintf("recorded to %s — `go test` now replays this session forever, offline.", path)))
	if !identical {
		return fmt.Errorf("replay did not match the live session")
	}
	return nil
}

// converse drives the five-prompt conversation through client, returning each
// assistant reply. When show is true it prints the turns as a chat.
func converse(hc *http.Client, baseURL string, out io.Writer, c colorer, show bool) ([]string, error) {
	client := anthropic.NewClient(
		option.WithHTTPClient(hc),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test"),
		option.WithMaxRetries(0),
	)
	var msgs []anthropic.MessageParam
	var replies []string
	for _, t := range convo {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(t.user)))
		s := client.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
			Model: anthropic.ModelClaudeOpus4_6, MaxTokens: 512, Messages: msgs,
		})
		m := anthropic.Message{}
		for s.Next() {
			if err := m.Accumulate(s.Current()); err != nil {
				s.Close()
				return nil, err
			}
		}
		if err := s.Err(); err != nil {
			s.Close()
			return nil, err
		}
		s.Close()
		reply := atrans.FromMessage(m).Text
		replies = append(replies, reply)
		if show {
			fmt.Fprintf(out, "  %s %s\n", c("1;36", "you    ▸"), t.user)
			fmt.Fprintf(out, "  %s %s\n\n", c("1;32", "claude ▸"), reply)
			if os.Getenv("CASSETTE_DEMO_PACE") != "" { // unfold turns for the recording
				time.Sleep(750 * time.Millisecond)
			}
		}
		msgs = append(msgs, m.ToParam())
	}
	return replies, nil
}

type colorer = func(code, s string) string

// ansi returns a colorizer for out: on for a TTY or FORCE_COLOR/CLICOLOR_FORCE;
// NO_COLOR disables it (and wins). Keeps piped output plain.
func ansi(out io.Writer) colorer {
	on := os.Getenv("NO_COLOR") == ""
	if on && os.Getenv("FORCE_COLOR") == "" && os.Getenv("CLICOLOR_FORCE") == "" {
		on = false
		if f, ok := out.(*os.File); ok {
			if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
				on = true
			}
		}
	}
	return func(code, s string) string {
		if !on {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
}
