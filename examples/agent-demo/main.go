// Command agent-demo is cassette's north-star, run on a realistic multi-tool
// "coffee shop" agent. A barista assistant answers "what's the latte like?" by
// making FIVE sequential tool calls — list_menu, get_price, check_inventory,
// caffeine_mg, brew_minutes — then giving a final recommendation. The streams are
// REAL Claude output captured from a provider session.
//
// The SAME agent code runs once "live" (HTTP to an in-process fake provider that
// replays the captured streams, recording a cassette) and once in "replay" with
// network egress made impossible. Both must produce a SEMANTICALLY IDENTICAL
// six-turn transcript (equal SHA-256) with PROVABLY ZERO outbound dials. A second
// live "control" run sees DIFFERENT volatile message ids yet the same SHA.
//
//	go run ./examples/agent-demo                 # assert against committed expected.sha
//	go run ./examples/agent-demo -write-golden    # (re)write expected.sha
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/semequal"
)

func main() {
	update := flag.Bool("write-golden", false, "write expected.sha instead of asserting it")
	flag.Parse()
	if err := run(os.Stdout, *update); err != nil {
		fmt.Fprintln(os.Stderr, "demo FAILED:", err)
		os.Exit(1)
	}
}

func run(out io.Writer, update bool) error {
	tmp, err := os.MkdirTemp("", "cassette-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	cassettePath := filepath.Join(tmp, "agent.yaml")

	turns := wirefix.CoffeeTurns()
	if len(turns) < 2 {
		return fmt.Errorf("expected captured coffee turns, got %d", len(turns))
	}

	var counter int64
	srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": coffeeHandler(turns, &counter),
	}))

	// --- LIVE leg: record the multi-tool agent against the fake provider ---
	recCas, err := cassette.Open(cassettePath, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		return err
	}
	live, err := runAgent(recCas, srv.URL)
	if err != nil {
		return fmt.Errorf("live leg: %w", err)
	}
	if err := recCas.VerifyError(); err != nil {
		return fmt.Errorf("record/save: %w", err)
	}
	liveSHA := semequal.CombinedDigest(live.turns)

	// --- CONTROL live leg: different volatile ids, same normalized transcript ---
	ctlCas, err := cassette.Open(filepath.Join(tmp, "control.yaml"), cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		return err
	}
	ctl, err := runAgent(ctlCas, srv.URL)
	if err != nil {
		return fmt.Errorf("control leg: %w", err)
	}
	if semequal.CombinedDigest(ctl.turns) != liveSHA {
		return fmt.Errorf("volatile fields leaked into transcript: live and control differ")
	}
	if live.firstID == ctl.firstID {
		return fmt.Errorf("fake provider did not vary volatile id (%q == %q)", live.firstID, ctl.firstID)
	}
	srv.Close() // replay must be server-independent

	// --- REPLAY leg: network egress impossible ---
	rpCas, err := cassette.Open(cassettePath, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		return err
	}
	replay, err := runAgent(rpCas, "http://replay.invalid")
	if err != nil {
		return fmt.Errorf("replay leg: %w", err)
	}
	if err := rpCas.VerifyError(); err != nil {
		return fmt.Errorf("replay verify: %w", err)
	}
	if rpCas.Dials() != 0 {
		return fmt.Errorf("replay made %d outbound dial(s); expected 0", rpCas.Dials())
	}
	if semequal.CombinedDigest(replay.turns) != liveSHA {
		return fmt.Errorf("transcripts differ:\n--- live ---\n%s\n--- replay ---\n%s", live, replay)
	}

	// --- Golden SHA (so equality is not self-referential) ---
	goldenPath := goldenFile()
	if update {
		if err := os.WriteFile(goldenPath, []byte(liveSHA+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote golden %s = %s\n", goldenPath, liveSHA)
	} else {
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			return fmt.Errorf("read golden (run with -write-golden first): %w", err)
		}
		if got := string(bytes.TrimSpace(want)); got != liveSHA {
			return fmt.Errorf("live transcript SHA %s != committed golden %s", liveSHA, got)
		}
	}

	c := ansi(out)
	fmt.Fprintln(out, c("1;32", "north-star demo (multi-tool coffee agent): PASS"))
	for i, call := range live.toolCalls {
		fmt.Fprintf(out, "  %s %d: %s -> %s\n", c("36", "tool call"), i+1, call, live.toolResults[i])
	}
	fmt.Fprintf(out, "  final answer : %s\n", truncate(live.finalText, 80))
	fmt.Fprintf(out, "  turns        : %d (%d tool calls)\n", len(live.turns), len(live.toolCalls))
	fmt.Fprintf(out, "  live msg ids : %s\n", c("2", fmt.Sprintf("%v", live.ids)))
	fmt.Fprintf(out, "  control ids  : %s  %s\n", c("2", fmt.Sprintf("%v", ctl.ids)), c("2", "(different volatile values, ignored)"))
	fmt.Fprintf(out, "  liveSHA      : %s\n", c("2", liveSHA))
	fmt.Fprintf(out, "  replaySHA    : %s\n", c("2", semequal.CombinedDigest(replay.turns)))
	fmt.Fprintf(out, "  outbound dials: %s  %s\n", c("32", fmt.Sprintf("%d", rpCas.Dials())), c("2", "(network egress was impossible)"))
	return nil
}

// toolResult is the barista's canned answer for each tool.
func toolResult(name string) string {
	switch name {
	case "list_menu":
		return `{"drinks":["espresso","latte","cappuccino","cold_brew","mocha"]}`
	case "get_price":
		return `{"drink":"latte","price_usd":4.50,"currency":"USD"}`
	case "check_inventory":
		return `{"ingredient":"oat_milk","in_stock":true,"units":12}`
	case "caffeine_mg":
		return `{"drink":"latte","caffeine_mg":128}`
	case "brew_minutes":
		return `{"drink":"latte","minutes":4}`
	default:
		return `{"error":"unknown tool"}`
	}
}

type agentResult struct {
	turns       []semequal.Transcript
	ids         []string
	firstID     string
	toolCalls   []string
	toolResults []string
	finalText   string
}

func (a agentResult) String() string {
	s := ""
	for i, t := range a.turns {
		s += fmt.Sprintf("[turn %d]\n%s", i+1, t)
	}
	return s
}

func coffeeTools() []anthropic.ToolUnionParam {
	drink := map[string]any{"drink": map[string]any{"type": "string"}}
	mk := func(name string, props map[string]any, req ...string) anthropic.ToolUnionParam {
		return anthropic.ToolUnionParamOfTool(anthropic.ToolInputSchemaParam{Properties: props, Required: req}, name)
	}
	return []anthropic.ToolUnionParam{
		mk("list_menu", map[string]any{}),
		mk("get_price", drink, "drink"),
		mk("check_inventory", map[string]any{"ingredient": map[string]any{"type": "string"}}, "ingredient"),
		mk("caffeine_mg", drink, "drink"),
		mk("brew_minutes", drink, "drink"),
	}
}

// runAgent runs the multi-tool barista conversation through the cassette client,
// executing each tool locally until the model gives its final answer.
func runAgent(c *cassette.Cassette, baseURL string) (agentResult, error) {
	client := anthropic.NewClient(
		option.WithHTTPClient(c.HTTPClient()),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("demo-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
	tools := coffeeTools()
	toolChoice := anthropic.ToolChoiceUnionParam{
		OfAuto: &anthropic.ToolChoiceAutoParam{DisableParallelToolUse: anthropic.Bool(true)},
	}
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"I'm at the counter deciding on a coffee. Help me step by step, using ONE tool at a time: " +
				"first list the menu, then get the price of a latte, then check if oat milk is in stock, " +
				"then look up the caffeine in a latte, then how many minutes a latte takes to brew. " +
				"After all five lookups, recommend in one sentence whether I should order the latte.")),
	}

	var res agentResult
	for turn := 1; turn <= 12; turn++ {
		msg, err := streamTurn(client, messages, tools, toolChoice)
		if err != nil {
			return res, err
		}
		res.turns = append(res.turns, atrans.FromMessage(msg))
		res.ids = append(res.ids, msg.ID)

		var results []anthropic.ContentBlockParamUnion
		for _, b := range msg.Content {
			if b.Type == "tool_use" {
				res.toolCalls = append(res.toolCalls, fmt.Sprintf("%s(%s)", b.Name, compact(b.Input)))
				out := toolResult(b.Name)
				res.toolResults = append(res.toolResults, out)
				results = append(results, anthropic.NewToolResultBlock(b.ID, out, false))
			}
		}
		if msg.StopReason != "tool_use" {
			res.finalText = atrans.FromMessage(msg).Text
			break
		}
		messages = append(messages, msg.ToParam(), anthropic.NewUserMessage(results...))
	}
	if len(res.ids) > 0 {
		res.firstID = res.ids[0]
	}
	return res, nil
}

func streamTurn(c anthropic.Client, msgs []anthropic.MessageParam, tools []anthropic.ToolUnionParam, tc anthropic.ToolChoiceUnionParam) (anthropic.Message, error) {
	s := c.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model: anthropic.ModelClaudeOpus4_6, MaxTokens: 512, Messages: msgs, Tools: tools, ToolChoice: tc,
	})
	defer s.Close()
	m := anthropic.Message{}
	for s.Next() {
		if err := m.Accumulate(s.Current()); err != nil {
			return m, err
		}
	}
	return m, s.Err()
}

var msgIDRe = regexp.MustCompile(`msg_bdrk_[A-Za-z0-9]+`)

// coffeeHandler replays the captured turns in order. The conversation is strictly
// sequential (one tool per turn), so the turn index equals the number of
// tool_result blocks already in the request. Each response gets a unique message
// id so volatile fields genuinely differ between runs.
func coffeeHandler(turns [][]byte, counter *int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		idx := bytes.Count(body, []byte(`"type":"tool_result"`))
		if idx >= len(turns) {
			idx = len(turns) - 1
		}
		n := atomic.AddInt64(counter, 1)
		fixture := msgIDRe.ReplaceAll(turns[idx], []byte("msg_RUN_"+strconv.FormatInt(n, 10)))
		fakeprovider.StreamHandler(fixture, fakeprovider.PerFrame)(w, r)
	}
}

func compact(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

// ansi returns a colorizer for out: on for a real terminal or when FORCE_COLOR /
// CLICOLOR_FORCE is set; NO_COLOR disables it (and wins). Keeps piped/CI output
// plain and byte-stable.
func ansi(out io.Writer) func(code, s string) string {
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

func truncate(s string, n int) string {
	s = string(bytes.ReplaceAll([]byte(s), []byte("\n"), []byte(" ")))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func goldenFile() string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "expected.sha"
	}
	return filepath.Join(filepath.Dir(self), "expected.sha")
}
