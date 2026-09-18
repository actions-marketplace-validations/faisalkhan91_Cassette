package cassette_test

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
	"github.com/faisalkhan91/cassette/semequal"
)

func messagesServer(fixture []byte, chunk fakeprovider.Chunker) *http.ServeMux {
	return fakeprovider.Mux(map[string]http.Handler{
		"/v1/messages": fakeprovider.StreamHandler(fixture, chunk),
	})
}

// expected decoded event order for a text fixture (ping is skipped by the SDK).
var expectedTextOrder = []string{
	"message_start",
	"content_block_start",
	"content_block_delta",
	"content_block_delta",
	"content_block_delta",
	"content_block_stop",
	"message_delta",
	"message_stop",
}

func TestRecordReplay_Streaming_SSE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.yaml")

	// --- RECORD leg (live HTTP to fake provider, per-frame flush) ---
	srv := fakeprovider.NewServer(messagesServer(wirefix.AnthropicText, fakeprovider.PerFrame))
	rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
	if err != nil {
		t.Fatal(err)
	}
	liveMsg, liveOrder, err := driveStream(t, anthropicClient(rec, srv.URL), "Hi there")
	if err != nil {
		t.Fatalf("live stream err: %v", err)
	}
	if err := rec.VerifyError(); err != nil { // saves cassette
		t.Fatalf("record verify/save: %v", err)
	}
	srv.Close() // replay must be server-independent

	// Incremental, in-order delivery (not a single buffered blob).
	if !equalStrings(liveOrder, expectedTextOrder) {
		t.Fatalf("event order mismatch:\n got=%v\n want=%v", liveOrder, expectedTextOrder)
	}

	// Cassette captured the SSE bytes VERBATIM (byte-for-byte == fixture).
	f, err := wirefmt.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var streamIt *wirefmt.Interaction
	for _, it := range f.Interactions {
		if it.Kind == "http" && it.Response.Streaming {
			streamIt = it
			break
		}
	}
	if streamIt == nil {
		t.Fatal("no streaming interaction recorded")
	}
	if !bytes.Equal(streamIt.Response.Body.Bytes(), wirefix.AnthropicText) {
		t.Fatalf("recorded SSE bytes are not verbatim:\n got=%q\n want=%q",
			streamIt.Response.Body.Bytes(), wirefix.AnthropicText)
	}

	// --- REPLAY leg (server down, network blocked) ---
	rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	replayMsg, replayOrder, err := driveStream(t, anthropicClient(rp, "http://replay.invalid"), "Hi there")
	if err != nil {
		t.Fatalf("replay stream err: %v", err)
	}
	if err := rp.VerifyError(); err != nil {
		t.Fatalf("replay verify: %v", err)
	}
	if rp.Dials() != 0 {
		t.Fatalf("replay made %d dials, want 0", rp.Dials())
	}

	// SEMANTIC equivalence: identical decoded event order and transcript SHA.
	if !equalStrings(replayOrder, expectedTextOrder) {
		t.Fatalf("replay event order mismatch:\n got=%v\n want=%v", replayOrder, expectedTextOrder)
	}
	liveT := atrans.FromMessage(liveMsg)
	replayT := atrans.FromMessage(replayMsg)
	if liveT.Digest() != replayT.Digest() {
		t.Fatalf("semantic transcript mismatch:\n--- live ---\n%s\n--- replay ---\n%s", liveT, replayT)
	}
	if liveT.Text == "" || liveT.Text != "Hello, world! 🌍" {
		t.Fatalf("unexpected assembled text: %q", liveT.Text)
	}

	// WIRE equivalence: on a FRESH replay cassette, replay the exact recorded
	// request and assert byte-for-byte identical response bytes (== fixture).
	rp2, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
	if err != nil {
		t.Fatal(err)
	}
	status, raw := rawReplay(t, rp2, "http://replay.invalid", streamIt.Request.URL, string(streamIt.Request.Body.Bytes()))
	if status != 200 {
		t.Fatalf("raw replay status %d", status)
	}
	if !bytes.Equal(raw, wirefix.AnthropicText) {
		t.Fatalf("WIRE replay not byte-identical:\n got=%q\n want=%q", raw, wirefix.AnthropicText)
	}
	if rp2.Dials() != 0 {
		t.Fatalf("raw replay made %d dials, want 0", rp2.Dials())
	}
}

// TestSemequal_ByteDecoderMatchesSDK proves the standalone byte-level SSE decoder
// in semequal yields the same normalized transcript as the real SDK's
// accumulation, for the committed fixtures.
func TestSemequal_ByteDecoderMatchesSDK(t *testing.T) {
	for name, fixture := range map[string][]byte{
		"text":    wirefix.AnthropicText,
		"tooluse": wirefix.AnthropicToolUse,
	} {
		t.Run(name, func(t *testing.T) {
			// SDK path: drive the fixture through the real SDK and adapt.
			srv := fakeprovider.NewServer(messagesServer(fixture, fakeprovider.PerFrame))
			defer srv.Close()
			rec, _ := cassette.Open(filepath.Join(t.TempDir(), name+".yaml"), cassette.Options{Mode: cassette.ModeRecord})
			msg, _, err := driveStream(t, anthropicClient(rec, srv.URL), "Hi")
			if err != nil {
				t.Fatalf("sdk drive: %v", err)
			}
			sdkT := atrans.FromMessage(msg)

			// Byte path: decode the raw fixture directly (no SDK).
			byteT, err := semequal.DecodeAnthropicSSE(fixture)
			if err != nil {
				t.Fatalf("byte decode: %v", err)
			}
			if sdkT.Digest() != byteT.Digest() {
				t.Fatalf("byte decoder != SDK:\n%s", semequal.Diff(sdkT, byteT))
			}
		})
	}
}
