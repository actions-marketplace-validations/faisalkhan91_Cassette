package cassette_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/faisalkhan91/cassette"
	"github.com/faisalkhan91/cassette/internal/fakeprovider"
)

// noAutoGzipClient builds an Anthropic client whose record-side base transport
// has DisableCompression set, so Go does NOT transparently decompress: the
// gzip-Content-Encoding response reaches cassette's own decompression path.
func noAutoGzipClient(c *cassette.Cassette, baseURL string) anthropic.Client {
	base := &http.Transport{DisableCompression: true}
	hc := &http.Client{Transport: c.Transport(base)}
	return anthropic.NewClient(
		option.WithHTTPClient(hc),
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key-not-a-real-secret"),
		option.WithMaxRetries(0),
	)
}

func TestRecordReplay_Gzip_NonUTF8(t *testing.T) {
	t.Run("gzip_json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gzip.yaml")
		srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
			"/v1/messages": fakeprovider.GzipJSONHandler(http.StatusOK, []byte(okMessageJSON)),
		}))

		rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
		if err != nil {
			t.Fatal(err)
		}
		recCl := noAutoGzipClient(rec, srv.URL)
		msg, err := recCl.Messages.New(context.Background(), streamParams("hi"))
		if err != nil {
			t.Fatalf("record New over gzip: %v", err)
		}
		if err := rec.VerifyError(); err != nil {
			t.Fatalf("record save: %v", err)
		}
		srv.Close()
		if len(msg.Content) == 0 || msg.Content[0].Text != "ok" {
			t.Fatalf("record decode != ok: %+v", msg.Content)
		}

		// Replay: server down. The SDK must decode the identical message.
		rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
		if err != nil {
			t.Fatal(err)
		}
		rpCl := noAutoGzipClient(rp, "http://replay.invalid")
		rmsg, err := rpCl.Messages.New(context.Background(), streamParams("hi"))
		if err != nil {
			t.Fatalf("replay New: %v", err)
		}
		if len(rmsg.Content) == 0 || rmsg.Content[0].Text != "ok" {
			t.Fatalf("replay decode != ok: %+v", rmsg.Content)
		}
		if rp.Dials() != 0 {
			t.Fatalf("replay dials = %d, want 0", rp.Dials())
		}
		if err := rp.VerifyError(); err != nil {
			t.Fatalf("replay verify: %v", err)
		}
	})

	t.Run("non_utf8_body", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "binary.yaml")
		// Bytes that are not valid UTF-8 and include NUL — must survive verbatim.
		raw := []byte{0x00, 0x01, 0xff, 0xfe, 0xc3, 0x28, 0x0a, 0x7f, 0x80}
		srv := fakeprovider.NewServer(fakeprovider.Mux(map[string]http.Handler{
			"/blob": fakeprovider.RawHandler(http.StatusOK,
				http.Header{"Content-Type": {"application/octet-stream"}}, raw),
		}))

		rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord})
		if err != nil {
			t.Fatal(err)
		}
		recBody := getBytes(t, rec.HTTPClient(), srv.URL+"/blob")
		if !bytes.Equal(recBody, raw) {
			t.Fatalf("record leg body mismatch: %v", recBody)
		}
		if err := rec.VerifyError(); err != nil {
			t.Fatalf("record save: %v", err)
		}
		srv.Close()

		rp, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay})
		if err != nil {
			t.Fatal(err)
		}
		rpBody := getBytes(t, rp.HTTPClient(), "http://replay.invalid/blob")
		if !bytes.Equal(rpBody, raw) {
			t.Fatalf("non-UTF8 body not byte-identical on replay:\n got=%v\n want=%v", rpBody, raw)
		}
		if rp.Dials() != 0 {
			t.Fatalf("replay dials = %d, want 0", rp.Dials())
		}
		if err := rp.VerifyError(); err != nil {
			t.Fatalf("replay verify: %v", err)
		}
	})
}

func getBytes(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b
}
