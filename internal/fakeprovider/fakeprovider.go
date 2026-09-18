// Package fakeprovider builds an in-process HTTP server (httptest) that replays
// committed real-wire fixtures. It is the permanent, offline RECORD source for
// cassette's self-tests: the real provider SDK talks to it over real HTTP, and
// cassette records exactly the bytes on the wire. The fake exists ONLY for
// record — replay tests run with the server torn down.
package fakeprovider

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

// Chunker splits a response body into the write/flush units the streaming
// handler emits. Use it to exercise adversarial framing (mid-event, multibyte,
// multiple-events-per-write) on the wire.
type Chunker func(body []byte) [][]byte

// PerFrame splits an SSE body into one chunk per event frame (each terminated by
// a blank line), so the client observes events one flush at a time.
func PerFrame(body []byte) [][]byte {
	sep := []byte("\n\n")
	var chunks [][]byte
	rest := body
	for {
		i := bytes.Index(rest, sep)
		if i < 0 {
			if len(rest) > 0 {
				chunks = append(chunks, rest)
			}
			break
		}
		chunks = append(chunks, rest[:i+len(sep)])
		rest = rest[i+len(sep):]
	}
	return chunks
}

// Whole returns the body as a single chunk.
func Whole(body []byte) [][]byte { return [][]byte{body} }

// Adversarial splits the body into deliberately awkward write boundaries: a
// large chunk that spans multiple events, interleaved with 1- and 2-byte chunks
// that fall mid-event and inside multibyte UTF-8 characters. This stresses the
// SDK's scanner and the streaming-capture tee.
func Adversarial(body []byte) [][]byte {
	// Repeating size pattern; small sizes guarantee mid-event and mid-rune splits,
	// the large size guarantees multiple events land in one write.
	sizes := []int{1, 2, 1, 64, 3, 1, 7, 2}
	var chunks [][]byte
	pos, si := 0, 0
	for pos < len(body) {
		n := sizes[si%len(sizes)]
		si++
		if pos+n > len(body) {
			n = len(body) - pos
		}
		chunks = append(chunks, body[pos:pos+n])
		pos += n
	}
	return chunks
}

// StreamHandler returns a handler that streams body as text/event-stream,
// writing and flushing each chunk so the client observes events incrementally.
// It honors request-context cancellation.
func StreamHandler(body []byte, chunk Chunker) http.HandlerFunc {
	if chunk == nil {
		chunk = PerFrame
	}
	chunks := chunk(body)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			w.Write(c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// JSONHandler returns a handler that writes a unary JSON response.
func JSONHandler(status int, body []byte, extra http.Header) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for k, vs := range extra {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// GzipJSONHandler writes a gzip-compressed JSON response with Content-Encoding:
// gzip so the recorder's decompression path is exercised.
func GzipJSONHandler(status int, body []byte) http.HandlerFunc {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(body)
	zw.Close()
	gz := buf.Bytes()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		w.Write(gz)
	}
}

// RawHandler writes a fully custom response (status, headers, body).
func RawHandler(status int, header http.Header, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		w.Write(body)
	}
}

// Sequence returns a handler that dispatches the i-th request to handlers[i],
// clamping at the last handler. Used to record ordered sequences such as a
// 429-then-200 retry path.
func Sequence(handlers ...http.Handler) http.HandlerFunc {
	var n int64
	return func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt64(&n, 1) - 1
		idx := int(i)
		if idx >= len(handlers) {
			idx = len(handlers) - 1
		}
		handlers[idx].ServeHTTP(w, r)
	}
}

// NewServer starts an httptest.Server for handler. Caller must Close it.
func NewServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(handler)
}

// Mux returns an http.ServeMux mapping each path to its handler.
func Mux(routes map[string]http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.Handle(path, h)
	}
	return mux
}
