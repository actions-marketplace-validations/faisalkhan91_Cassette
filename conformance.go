package cassette

import (
	"bytes"
	"net/http"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/rekey"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Conformance is a closed-loop proof that a cassette actually replays. For every
// recorded interaction it checks two things, fully offline and with zero outbound
// dials:
//
//   - key integrity: the persisted match key re-derives from the stored request
//     (a stale key — from a hand edit that changed the request body — silently
//     desyncs replay; `cassette rekey` fixes it);
//   - replayability: the request resolves through the in-process replay matcher to
//     a recorded response that decodes to the same semantic transcript as the
//     stored response.
//
// It complements `cassette doctor` (which diagnoses a miss reactively) by proving
// proactively that the whole file is internally consistent and replayable before
// CI ever runs it. MCP interactions are checked for key integrity only (they are
// not driven through the HTTP matcher).

// ConformanceTurn is one interaction's replay self-check verdict.
type ConformanceTurn struct {
	Index    int    `json:"index"`
	Kind     string `json:"kind"`
	Method   string `json:"method,omitempty"`
	URL      string `json:"url,omitempty"`
	Tool     string `json:"tool,omitempty"`
	KeyOK    bool   `json:"key_ok"`
	ReplayOK bool   `json:"replay_ok"`
	Detail   string `json:"detail,omitempty"`
}

// OK reports whether the interaction passed both checks.
func (t ConformanceTurn) OK() bool { return t.KeyOK && t.ReplayOK }

// ConformanceReport is the whole-file replay self-check result.
type ConformanceReport struct {
	Turns []ConformanceTurn `json:"turns"`
	Bad   int               `json:"bad"`
	Dials int64             `json:"dials"`
}

// OK reports whether every interaction passed and replay made zero dials.
func (r ConformanceReport) OK() bool { return r.Bad == 0 && r.Dials == 0 }

// Conformance loads the cassette in replay mode and runs the self-check.
func Conformance(path string) (ConformanceReport, error) {
	c, err := Open(path, Options{Mode: ModeReplay})
	if err != nil {
		return ConformanceReport{}, err
	}
	var rep ConformanceReport
	for i, it := range c.file.Interactions {
		ct := ConformanceTurn{Index: i, Kind: it.Kind}
		switch it.Kind {
		case "http":
			ct.Method, ct.URL = it.Request.Method, it.Request.URL
			conformHTTP(c, i, it, &ct)
		case "mcp":
			ct.Tool = it.Request.MCPTool
			conformMCP(c, it, &ct)
		default:
			ct.KeyOK, ct.ReplayOK = true, true
			ct.Detail = "non-replayable kind (skipped)"
		}
		if !ct.OK() {
			rep.Bad++
		}
		rep.Turns = append(rep.Turns, ct)
	}
	rep.Dials = c.Dials()
	return rep, nil
}

func conformHTTP(c *Cassette, i int, it *wirefmt.Interaction, ct *ConformanceTurn) {
	// The match key + BodySHA256 are both digests over the LIVE pre-scrub body; scrub
	// overwrites the secret span with REDACTED in place and recomputes neither, so the
	// original bytes are gone and the key is NOT re-derivable from the stored body.
	// Request-body integrity over the scrubbed span is therefore an accepted gap — we
	// trust the stored key (it's the replay authority; live requests are pre-scrub on
	// both sides) rather than false-fail or advise `rekey` (which would OVERWRITE the
	// correct key). The RESPONSE side is still fully verified below.
	bodyScrubbed := bytes.Contains(it.Request.Body.Bytes(), []byte(scrub.Replacement))

	// (1) Key integrity: the persisted key must re-derive from the stored request,
	// unless the body was scrubbed (then trust the stored key).
	derived, err := rekey.HTTPKey(it.Request, c.match)
	if err != nil {
		ct.Detail = "key derivation failed: " + err.Error()
		return
	}
	switch {
	case it.Request.MatchKey == "":
		ct.KeyOK = true // hand-written cassette; index recomputes from the body
	case derived == it.Request.MatchKey:
		ct.KeyOK = true
	case bodyScrubbed:
		ct.KeyOK = true
		ct.Detail = "key not re-derivable (request body scrubbed); stored key trusted — do not run rekey"
	default:
		ct.Detail = "stale match key (run `cassette rekey`)"
	}

	// (2) Replayability → resolve the response, then (3) verify it decodes the same.
	var matched wirefmt.Response
	if bodyScrubbed && it.Request.MatchKey != "" {
		// Can't re-derive from the scrubbed body, so verify the STORED key resolves to
		// THIS interaction in the index (how a live pre-scrub request matches). The
		// response is still cross-checked below — a hand-edit to the response is caught.
		c.mu.Lock()
		group := c.keys[it.Request.MatchKey]
		c.mu.Unlock()
		found := false
		for _, idx := range group {
			if idx == i {
				found, matched = true, c.file.Interactions[idx].Response
				break
			}
		}
		if !found {
			ct.Detail = appendDetail(ct.Detail, "stored key does not resolve to this interaction")
			return
		}
	} else {
		// Reconstruct the request and resolve it through the real replay matcher. The
		// host is irrelevant (matching is path+body); Content-Encoding is dropped
		// because stored bodies are kept decoded.
		body := it.Request.Body.Bytes()
		req, rerr := http.NewRequest(it.Request.Method, "http://replay.invalid"+it.Request.URL, bytes.NewReader(body))
		if rerr != nil {
			ct.Detail = appendDetail(ct.Detail, "reconstruct request: "+rerr.Error())
			return
		}
		for _, h := range it.Request.Headers {
			if http.CanonicalHeaderKey(h.Name) == "Content-Encoding" {
				continue
			}
			for _, v := range h.Values {
				req.Header.Add(h.Name, v)
			}
		}
		m, ok, merr := c.matchResponse(req, body)
		if merr != nil {
			ct.Detail = appendDetail(ct.Detail, "replay error: "+merr.Error())
			return
		}
		if !ok {
			ct.Detail = appendDetail(ct.Detail, "request does not resolve to a recorded response")
			return
		}
		matched = m
	}
	// (3) The resolved response must decode to the same semantic transcript.
	want, _, _ := analysis.DecodeInteraction(it)
	got, _, _ := analysis.DecodeInteraction(&wirefmt.Interaction{Kind: "http", Request: it.Request, Response: matched})
	if want.Digest() != got.Digest() {
		ct.Detail = appendDetail(ct.Detail, "resolved response decodes to a different transcript")
		return
	}
	ct.ReplayOK = true
}

func conformMCP(c *Cassette, it *wirefmt.Interaction, ct *ConformanceTurn) {
	derived := rekey.MCPKey(it.Request, c.match)
	if it.Request.MatchKey == "" {
		ct.KeyOK = true
	} else if ct.KeyOK = derived == it.Request.MatchKey; !ct.KeyOK {
		ct.Detail = "stale match key (run `cassette rekey`)"
	}
	// MCP is not driven through the HTTP matcher here; key integrity is the proof.
	ct.ReplayOK = ct.KeyOK
	if ct.Detail == "" {
		ct.Detail = "mcp (key-integrity check)"
	}
}

func appendDetail(d, s string) string {
	if d == "" {
		return s
	}
	return d + "; " + s
}
