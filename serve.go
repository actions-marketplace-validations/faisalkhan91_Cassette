package cassette

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// ServeOptions configures the cassette HTTP listener.
type ServeOptions struct {
	// Pace re-emits recorded SSE frames with their recorded inter-frame delays
	// (honoring request-context cancellation). Default: deliver as fast as the
	// client reads.
	Pace bool
	// OnMiss, if set, handles a request that matches no recorded interaction.
	// Default: a 404 JSON error. The listener NEVER proxies to a real provider.
	OnMiss func(w http.ResponseWriter, r *http.Request)
}

// Handler returns an http.Handler that serves this cassette's recorded
// interactions over real HTTP — speaking each provider's wire protocol
// (Anthropic /v1/messages, OpenAI /chat/completions, Responses /responses are
// routed automatically because the match key includes the path). Any client in
// any language can point its base URL at it for deterministic, zero-network,
// zero-key replay.
//
// The handler holds no http.Client and makes no outbound call: a miss is a 404,
// never a passthrough. SSE frame bytes are reproduced faithfully (written and
// flushed frame-by-frame); the HTTP envelope is reconstructed (Content-Encoding
// and Content-Length are not replayed). Use only against a Cassette opened in
// replay mode.
func (c *Cassette) Handler(opts ServeOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		stored, ok, err := c.matchResponse(r, body)
		if err != nil {
			http.Error(w, fmt.Sprintf("cassette: %v", err), http.StatusBadGateway)
			return
		}
		if !ok {
			if opts.OnMiss != nil {
				opts.OnMiss(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"type":"cassette_miss","message":%q}}`+"\n",
				"no recorded interaction matches "+r.Method+" "+r.URL.Path)
			return
		}
		c.writeStored(w, r, stored, opts.Pace)
	})
}

// writeStored writes a recorded response to w, frame-by-frame (with flush) for
// streaming interactions. It shares the stored-response model and frame splitter
// with the replay transport's synthesize().
func (c *Cassette) writeStored(w http.ResponseWriter, r *http.Request, stored wirefmt.Response, pace bool) {
	for name, vals := range stored.Headers.ToHTTP() {
		switch http.CanonicalHeaderKey(name) {
		case "Content-Encoding", "Content-Length":
			continue // bodies are stored decoded; envelope is reconstructed
		}
		for _, v := range vals {
			w.Header().Add(name, v)
		}
	}
	status := stored.Status
	if status == 0 {
		status = http.StatusOK
	}
	body := stored.Body.Bytes()

	if !stored.Streaming {
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(status)
	flusher, _ := w.(http.Flusher)
	frames := splitSSEFrames(body)
	delays := stored.StreamTiming
	for i, f := range frames {
		if pace && i < len(delays) {
			if err := c.sleep(r.Context(), time.Duration(delays[i])*time.Millisecond); err != nil {
				return
			}
		} else if err := r.Context().Err(); err != nil {
			return
		}
		if _, err := w.Write(f); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
