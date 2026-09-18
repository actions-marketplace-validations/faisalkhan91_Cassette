package cassette_test

import (
	"testing"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/cassettetest"
	"github.com/faisalkhan91/cassette/internal/atrans"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
	"github.com/faisalkhan91/cassette/internal/wirefix"
)

// TestExample_HelperRecordReplay exercises the idiomatic test-helper surface end
// to end and maintains a committed sample cassette.
//
//   - Plain `go test` REPLAYS testdata/cassettes/anthropic_text_helper.yaml with
//     the network blocked (the committed sample is validated on every run).
//   - `go test -run TestExample_HelperRecordReplay -update` RECORDS it afresh from
//     the in-process fake provider.
//
// New registers a t.Cleanup verify, which asserts all interactions were consumed
// and zero dials occurred (replay) or saves the cassette (record).
func TestExample_HelperRecordReplay(t *testing.T) {
	c := cassettetest.New(t, "anthropic_text_helper")

	baseURL := "http://replay.invalid"
	if c.Mode() == cassette.ModeRecord {
		srv := fakeprovider.NewServer(messagesServer(wirefix.AnthropicText, fakeprovider.PerFrame))
		defer srv.Close()
		baseURL = srv.URL
	}

	msg, order, err := driveStream(t, anthropicClient(c, baseURL), "Hi there")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if !equalStrings(order, expectedTextOrder) {
		t.Fatalf("event order: %v", order)
	}
	if got := atrans.FromMessage(msg).Text; got != "Hello, world! 🌍" {
		t.Fatalf("assembled text = %q", got)
	}
}
