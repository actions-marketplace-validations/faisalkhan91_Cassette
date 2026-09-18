package cassette_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

type leg struct {
	msg   anthropic.Message
	order []string
	err   error
}

// recordReplayStream records a streaming call against the fake provider (with the
// given chunker), then replays it with the server down. It returns both legs and
// the cassette path.
func recordReplayStream(t *testing.T, fixture []byte, chunk fakeprovider.Chunker) (live, replay leg, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "s.yaml")

	srv := fakeprovider.NewServer(messagesServer(fixture, chunk))
	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	live.msg, live.order, live.err = driveStream(t, anthropicClient(rec, srv.URL), "Hi")
	if err := rec.VerifyError(); err != nil {
		t.Fatalf("record save: %v", err)
	}
	srv.Close()

	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	replay.msg, replay.order, replay.err = driveStream(t, anthropicClient(rp, "http://replay.invalid"), "Hi")
	if rp.Dials() != 0 {
		t.Fatalf("replay made %d dials, want 0", rp.Dials())
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("replay verify: %v", err)
	}
	return live, replay, path
}

func capturedStreamBytes(t *testing.T, path string) []byte {
	t.Helper()
	f, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range f.Interactions {
		if it.Kind == "http" && it.Response.Streaming {
			return it.Response.Body.Bytes()
		}
	}
	t.Fatal("no streaming interaction recorded")
	return nil
}

func TestRecordReplay_SSE_AdversarialFraming(t *testing.T) {
	// The fake server writes the stream with adversarial chunk boundaries
	// (mid-event, multibyte-internal, multiple events per write). The SDK scanner
	// must yield the identical decoded sequence live and on replay.
	live, replay, path := recordReplayStream(t, wirefix.AnthropicText, fakeprovider.Adversarial)
	if live.err != nil {
		t.Fatalf("live err: %v", live.err)
	}
	if replay.err != nil {
		t.Fatalf("replay err: %v", replay.err)
	}
	if !equalStrings(live.order, expectedTextOrder) {
		t.Fatalf("live order under adversarial framing: %v", live.order)
	}
	if !equalStrings(live.order, replay.order) {
		t.Fatalf("decoded event sequence differs live vs replay:\n live=%v\n replay=%v", live.order, replay.order)
	}
	if atrans.FromMessage(live.msg).Digest() != atrans.FromMessage(replay.msg).Digest() {
		t.Fatal("semantic transcript differs live vs replay under adversarial framing")
	}
	// Despite adversarial wire chunking, the cassette captured the bytes verbatim.
	if !bytes.Equal(capturedStreamBytes(t, path), wirefix.AnthropicText) {
		t.Fatal("adversarial chunking corrupted the verbatim capture")
	}
}

func TestRecordReplay_ToolCall_Fragments(t *testing.T) {
	// Tool arguments arrive as 4 input_json_delta fragments (split inside a string
	// value), and the adversarial chunker further splits the wire bytes inside the
	// multibyte 'é'. Reassembled args must be byte-identical and the SDK must
	// surface the same final tool call on both legs.
	live, replay, _ := recordReplayStream(t, wirefix.AnthropicToolUse, fakeprovider.Adversarial)
	if live.err != nil {
		t.Fatalf("live err: %v", live.err)
	}
	if replay.err != nil {
		t.Fatalf("replay err: %v", replay.err)
	}

	lt := atrans.FromMessage(live.msg)
	rt := atrans.FromMessage(replay.msg)
	if len(lt.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d (%s)", len(lt.ToolCalls), lt)
	}
	tc := lt.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Fatalf("tool name = %q, want get_weather", tc.Name)
	}
	wantArgs := `{"location":"San José","unit":"celsius"}`
	if tc.Args != wantArgs {
		t.Fatalf("reassembled tool args = %q, want %q", tc.Args, wantArgs)
	}
	if lt.Digest() != rt.Digest() {
		t.Fatalf("tool call differs live vs replay:\n live=%s\n replay=%s", lt, rt)
	}
	if !strings.Contains(string(live.msg.Content[1].Input), "San José") {
		t.Fatalf("multibyte char not preserved in tool input: %s", live.msg.Content[1].Input)
	}
}

func TestRecordReplay_MidStreamError(t *testing.T) {
	// A 200 stream that emits a provider `error` event mid-way. Both legs must
	// surface the same error after the same partial content.
	live, replay, _ := recordReplayStream(t, wirefix.AnthropicMidStreamError, fakeprovider.PerFrame)
	if live.err == nil {
		t.Fatal("expected a streaming error on the live leg")
	}
	if replay.err == nil {
		t.Fatal("expected a streaming error on the replay leg")
	}
	// The SDK error is formatted as `POST "<url>": <status> <body>`, so it embeds
	// the request URL. Record and replay legs use different base URLs by design
	// (the real fake-provider host vs the replay.invalid sentinel that proves replay
	// dials nothing), so compare the provider error payload — from the first JSON
	// brace on — not the host-specific prefix.
	if liveBody, replayBody := errBody(live.err), errBody(replay.err); liveBody != replayBody {
		t.Fatalf("error payload differs live vs replay:\n live=%q\n replay=%q", liveBody, replayBody)
	}
	if !strings.Contains(live.err.Error(), "overloaded_error") {
		t.Fatalf("unexpected error text: %v", live.err)
	}
	// Partial content streamed before the error must match.
	if atrans.FromMessage(live.msg).Text != "Partial" {
		t.Fatalf("partial text = %q, want %q", atrans.FromMessage(live.msg).Text, "Partial")
	}
	if atrans.FromMessage(live.msg).Text != atrans.FromMessage(replay.msg).Text {
		t.Fatal("partial content differs live vs replay")
	}
}

// errBody returns the provider error payload from an SDK error string, dropping
// the leading `METHOD "url": status` prefix (which embeds a host that legitimately
// differs between the live and replay legs). It returns the substring from the
// first "{" on, or the whole string if there is no JSON body.
func errBody(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '{'); i >= 0 {
		return s[i:]
	}
	return s
}
