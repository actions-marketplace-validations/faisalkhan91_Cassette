package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	cassette "github.com/faisalkhan91/cassette"
)

// cmdProxy runs cassette as a language-agnostic reverse proxy so any app records
// or replays by pointing its base URL at cassette — no Go, no SDK wrapper.
//
//	--mode replay  (default): serve the cassette/dir as an offline endpoint; a miss
//	                          is a 404, never a passthrough — zero egress.
//	--mode record:            forward to --upstream, tee the response into the
//	                          cassette frame-exact, scrub auth on save (opt-in net).
//	--mode branch:            replay the recorded prefix, go live to --upstream on
//	                          the first divergent request and append it (opt-in net).
func cmdProxy(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("pace").valFlag("upstream", "mode", "addr", "on-miss", "miss-manifest", "volatile"), args, proxyUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		return proxyUsage(stderr)
	}
	mode := in.str("mode")
	if mode == "" {
		mode = "replay"
	}
	upstream := in.str("upstream")
	if (mode == "record" || mode == "branch") && upstream == "" {
		fmt.Fprintf(stderr, "cassette: --mode %s requires --upstream <URL>\n%s\n", mode, proxyUsageText)
		return exitUsage
	}
	if v := in.str("on-miss"); in.has("on-miss") && v != "fail" {
		fmt.Fprintf(stderr, "cassette: --on-miss only accepts \"fail\" (got %q)\n", v)
		return exitUsage
	}
	// --on-miss=fail is a replay-only CI gate; warn rather than silently ignore it
	// in record/branch (those modes have no misses to gate on).
	if in.has("on-miss") && mode != "replay" {
		fmt.Fprintf(stderr, "cassette: note: --on-miss is ignored in --mode %s (replay only)\n", mode)
	}

	// --on-miss=fail (replay only) turns the proxy into a CI gate.
	var rec *missRecorder
	if mode == "replay" && in.str("on-miss") == "fail" {
		rec = newMissRecorder()
	}
	handler, onShutdown, code := buildProxy(path, mode, upstream, volatileMatch(in), rec, stderr, in.boolv("pace"))
	if code != exitOK {
		return code
	}

	addr := proxyAddr(in)
	st := newStyle(stdout)
	// Record/branch route credentialed traffic to the upstream; if bound to a
	// non-loopback address that turns the host into an open relay for any LAN peer.
	if onShutdown != nil && !isLoopbackAddr(addr) {
		fmt.Fprintf(stderr, "%s proxy %s is bound to a non-loopback address (%s) in %s mode — any host that can reach it can route credentialed requests through you. Use --addr 127.0.0.1:PORT unless this is intentional.\n",
			st.warn(), "record/branch", addr, mode)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: listen: %v\n", err)
		return exitFail
	}
	egress := st.dim("(zero egress)")
	if onShutdown != nil {
		egress = st.yellow("(live → " + upstream + ")")
	}
	fmt.Fprintf(stdout, "%s cassette proxy [%s] %s on http://%s\n", st.check(), mode, egress, ln.Addr())

	if err := serveUntilSignal(ln, handler); err != nil {
		fmt.Fprintf(stderr, "cassette: serve: %v\n", err)
		return exitFail
	}
	if onShutdown != nil {
		if err := onShutdown(); err != nil {
			fmt.Fprintf(stderr, "cassette: save: %v\n", err)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s saved %s\n", st.check(), path)
	}
	return finalizeMisses(rec, missManifestPath(in), stdout, stderr)
}

// buildProxy constructs the proxy handler for a mode (and, for record/branch, the
// save-on-shutdown hook). It is separated from the serve loop so every mode's
// wiring is unit-testable. missRec, when non-nil (replay CI gate), records misses.
// Returns an exit code != exitOK (after printing) on error.
func buildProxy(path, mode, upstream string, match cassette.MatchConfig, missRec *missRecorder, stderr io.Writer, pace bool) (http.Handler, func() error, int) {
	switch mode {
	case "replay":
		c, err := openForReplay(path, match)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, exitFail
		}
		opts := cassette.ServeOptions{Pace: pace}
		if missRec != nil {
			opts.OnMiss = recordingOnMiss(missRec, stderr)
		}
		return c.Handler(opts), nil, exitOK
	case "record":
		rec, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeRecord, Match: match, CaptureTiming: pace})
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, exitFail
		}
		h, err := forwardHandler(rec.HTTPClient(), upstream)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, exitUsage
		}
		return h, rec.VerifyError, exitOK
	case "branch":
		br, err := cassette.Open(path, cassette.Options{Mode: cassette.ModeBranch, Match: match, Live: cassette.LiveTransport(upstream, nil), CaptureTiming: pace})
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, exitFail
		}
		h, err := forwardHandler(br.HTTPClient(), upstream)
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, exitUsage
		}
		return h, br.VerifyError, exitOK
	default:
		fmt.Fprintf(stderr, "cassette: unknown --mode %q (record|replay|branch)\n", mode)
		return nil, nil, exitUsage
	}
}

func proxyAddr(in *Inv) string {
	return in.strOr("addr", "127.0.0.1:8080") // loopback by default — opt into a public bind explicitly
}

// isLoopbackAddr reports whether a listen address binds only the loopback
// interface (so record/branch can't relay credentialed traffic for LAN peers).
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false // empty host (":8080") or a real interface = non-loopback
}

// forwardHandler proxies each incoming request to upstream through client (whose
// transport records or branches), streaming the response back frame-by-frame. The
// recorded request keeps only the path (host scrubbed), so it replays anywhere.
func forwardHandler(client *http.Client, upstream string) (http.HandlerFunc, error) {
	base, err := url.Parse(upstream)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid --upstream %q (want e.g. https://api.anthropic.com)", upstream)
	}
	return func(w http.ResponseWriter, r *http.Request) { forwardTo(w, r, client, base) }, nil
}

// forwardTo proxies one request to base through client (whose transport records or
// branches), streaming the response back frame-by-frame. Shared by forwardHandler
// (fixed upstream) and the `cassette up` auto front door (per-request upstream).
func forwardTo(w http.ResponseWriter, r *http.Request, client *http.Client, base *url.URL) {
	var body []byte
	if r.Body != nil {
		var rerr error
		body, rerr = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if rerr != nil {
			// A truncated read (client disconnect mid-body) must not be forwarded
			// or recorded as if complete — fail loudly instead of corrupting.
			http.Error(w, "cassette proxy: read request body: "+rerr.Error(), http.StatusBadGateway)
			return
		}
	}
	u := *r.URL
	u.Scheme, u.Host = base.Scheme, base.Host
	// Honor an upstream base path (e.g. a corporate gateway at
	// https://host/llm-proxy): join it ahead of the request path, and merge any
	// base query — otherwise the base path/query are silently dropped.
	u.Path = singleJoiningSlash(base.Path, r.URL.Path)
	u.RawPath = ""
	if base.RawQuery != "" {
		if u.RawQuery == "" {
			u.RawQuery = base.RawQuery
		} else {
			u.RawQuery = base.RawQuery + "&" + u.RawQuery
		}
	}
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "cassette proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	reqConn := connectionHeaders(r.Header)
	for k, vs := range r.Header {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Host" || hopByHop[ck] || reqConn[ck] {
			continue
		}
		for _, v := range vs {
			outReq.Header.Add(k, v)
		}
	}
	resp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, "cassette proxy: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respConn := connectionHeaders(resp.Header)
	for k, vs := range resp.Header {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Content-Length" || hopByHop[ck] || respConn[ck] {
			// Content-Length: length may change after the record transport decodes.
			// Hop-by-hop (fixed names + those nominated by Connection): forwarding
			// them verbatim can corrupt SSE framing; the Go server manages them.
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 8192)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// connectionHeaders returns the canonicalized header names a Connection header
// nominates as hop-by-hop (e.g. "Connection: X-Foo, close" → {"X-Foo","Close"}).
func connectionHeaders(h http.Header) map[string]bool {
	out := map[string]bool{}
	for _, v := range h["Connection"] {
		for _, tok := range strings.Split(v, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				out[http.CanonicalHeaderKey(tok)] = true
			}
		}
	}
	return out
}

// hopByHop are the per-connection headers a proxy must not forward (RFC 7230 §6.1);
// the Go client/server manage them, and copying them verbatim can corrupt framing.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Authorization": true, "Te": true, "Trailer": true,
	"Transfer-Encoding": true, "Upgrade": true,
}

// singleJoiningSlash joins two URL path segments with exactly one slash between
// them (mirrors net/http/httputil's reverse-proxy director).
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash && a != "" && b != "":
		return a + "/" + b
	}
	return a + b
}

const proxyUsageText = "usage: cassette proxy <cassette.yaml|dir> [--mode record|replay|branch] [--upstream URL] [--addr :8080] [--pace] [--on-miss fail] [--miss-manifest path] [--volatile a.b,c]"

func proxyUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, proxyUsageText)
	return exitUsage
}
