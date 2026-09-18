package fakeprovider

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

var sample = []byte("event: a\ndata: 1\n\nevent: b\ndata: 2\n\n")

func TestChunkersReassemble(t *testing.T) {
	for name, ck := range map[string]Chunker{"perframe": PerFrame, "whole": Whole, "adversarial": Adversarial} {
		var got []byte
		for _, c := range ck(sample) {
			got = append(got, c...)
		}
		if !bytes.Equal(got, sample) {
			t.Fatalf("%s: reassembly != original", name)
		}
	}
}

func TestPerFrameCount(t *testing.T) {
	if n := len(PerFrame(sample)); n != 2 {
		t.Fatalf("perframe produced %d chunks, want 2", n)
	}
}

func TestAdversarialSplits(t *testing.T) {
	chunks := Adversarial(sample)
	if len(chunks) < 3 {
		t.Fatalf("adversarial should produce many small chunks, got %d", len(chunks))
	}
}

func TestStreamHandler(t *testing.T) {
	h := StreamHandler(sample, PerFrame)
	rr := httptest.NewRecorder() // ResponseRecorder implements http.Flusher
	h(rr, httptest.NewRequest("POST", "/v1/messages", nil))
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	if !bytes.Equal(rr.Body.Bytes(), sample) {
		t.Fatalf("stream body mismatch")
	}
}

func TestJSONHandler(t *testing.T) {
	h := JSONHandler(201, []byte(`{"ok":true}`), http.Header{"X-Test": {"v"}})
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("POST", "/x", nil))
	if rr.Code != 201 {
		t.Fatalf("code = %d", rr.Code)
	}
	if rr.Header().Get("X-Test") != "v" {
		t.Fatal("extra header missing")
	}
}

func TestGzipJSONHandler(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	h := GzipJSONHandler(200, body)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("missing gzip content-encoding")
	}
	zr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if !bytes.Equal(got, body) {
		t.Fatalf("gzip body mismatch: %s", got)
	}
}

func TestSequence(t *testing.T) {
	h := Sequence(
		JSONHandler(429, []byte(`{}`), nil),
		JSONHandler(200, []byte(`{}`), nil),
	)
	codes := []int{}
	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest("GET", "/x", nil))
		codes = append(codes, rr.Code)
	}
	// First 429, then 200, then clamped to last (200).
	if codes[0] != 429 || codes[1] != 200 || codes[2] != 200 {
		t.Fatalf("sequence codes = %v", codes)
	}
}

func TestServerAndMux(t *testing.T) {
	srv := NewServer(Mux(map[string]http.Handler{
		"/blob": RawHandler(200, http.Header{"Content-Type": {"application/octet-stream"}}, []byte("xyz")),
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/blob")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "xyz" {
		t.Fatalf("got %q", b)
	}
}
