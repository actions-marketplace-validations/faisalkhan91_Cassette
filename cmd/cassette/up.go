package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	cassette "github.com/faisalkhan91/cassette"
	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

const upUsageText = "usage: cassette up [dir] [--upstream URL] [--addr 127.0.0.1:PORT] [--pace] [--volatile a.b,c]"

// cmdUp is the zero-config front door: point your provider's base URL at it once and
// it does the rest. With no flags it auto-picks a free loopback port, decides
// record-vs-replay from whether the directory already holds cassettes (the library's
// ModeAuto semantics, at the directory grain), auto-detects the upstream from the
// first request's path when recording, and diagnoses a replay miss instead of just
// 404ing. The whole "install once, point at your provider, it just works" promise.
func cmdUp(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("pace").valFlag("upstream", "addr", "volatile"), args, upUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 1 {
		fmt.Fprintln(stderr, upUsageText)
		return exitUsage
	}
	dir := in.arg(0)
	if dir == "" {
		dir = filepath.Join("testdata", "cassettes")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	addr := in.strOr("addr", "127.0.0.1:0") // auto-pick a free loopback port unless told otherwise
	match := volatileMatch(in)
	st := newStyle(stdout)

	handler, onShutdown, recording, recPath, code := buildUp(dir, in.str("upstream"), match, in.boolv("pace"), stderr)
	if code != exitOK {
		return code
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: listen %s: %v\n", addr, err)
		return exitFail
	}
	base := "http://" + ln.Addr().String()
	if recording {
		tgt := in.str("upstream")
		if tgt == "" {
			tgt = "auto-detected per provider"
		}
		fmt.Fprintf(stdout, "%s cassette up — recording to %s %s\n", st.check(), recPath, st.yellow("(live → "+tgt+")"))
	} else {
		fmt.Fprintf(stdout, "%s cassette up — replaying %s %s\n", st.check(), dir, st.dim("(zero egress)"))
	}
	fmt.Fprintf(stdout, "  point your provider's base URL here: %s\n", st.bold(base))
	fmt.Fprintf(stdout, "  %s\n", st.dim("e.g. ANTHROPIC_BASE_URL="+base+"   OPENAI_BASE_URL="+base+"/v1"))
	if recording {
		fmt.Fprintf(stdout, "  %s\n", st.dim("records on first call (needs your provider key); re-run to replay offline."))
	} else {
		fmt.Fprintf(stdout, "  %s\n", st.dim("no key, no network. Delete the dir to re-record; a miss is diagnosed below."))
	}

	if err := serveUntilSignal(ln, handler); err != nil {
		fmt.Fprintf(stderr, "cassette: serve: %v\n", err)
		return exitFail
	}
	if onShutdown != nil {
		if err := onShutdown(); err != nil {
			fmt.Fprintf(stderr, "cassette: save: %v\n", err)
			return exitFail
		}
		fmt.Fprintf(stdout, "%s saved %s\n", st.check(), recPath)
	}
	return exitOK
}

// buildUp constructs the front-door handler and decides record-vs-replay from whether
// dir already holds cassettes (ModeAuto at the directory grain). It is separated from
// the serve loop so the handler is unit-testable without binding a port. On record it
// also returns the save-on-shutdown hook and the file it records into.
func buildUp(dir, upstream string, match cassette.MatchConfig, pace bool, stderr io.Writer) (handler http.Handler, onShutdown func() error, recording bool, recPath string, code int) {
	ys, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	yml, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
	recording = len(ys)+len(yml) == 0

	if recording {
		recPath = filepath.Join(dir, "recording.yaml")
		rec, err := cassette.Open(recPath, cassette.Options{Mode: cassette.ModeRecord, Match: match, CaptureTiming: pace})
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return nil, nil, false, "", exitFail
		}
		client := rec.HTTPClient()
		st := newStyle(stderr)
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			base, ok := resolveUpstream(upstream, r.URL.Path)
			if !ok {
				fmt.Fprintf(stderr, "%s cassette up: could not auto-detect the provider for %s — re-run with --upstream <URL>\n", st.warn(), r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprintf(w, `{"error":{"type":"cassette_unknown_upstream","message":%q}}`+"\n",
					"could not auto-detect the upstream for "+r.URL.Path+"; re-run `cassette up --upstream <URL>`")
				return
			}
			forwardTo(w, r, client, base)
		})
		return handler, rec.VerifyError, true, recPath, exitOK
	}

	c, err := openForReplay(dir, match)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, nil, false, "", exitFail
	}
	// Load the corpus once more, merged, so a miss can be diagnosed (doctor-on-miss).
	files, err := loadCassettes(dir)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return nil, nil, false, "", exitFail
	}
	merged, _ := analysis.Merge(files...)
	return c.Handler(cassette.ServeOptions{Pace: pace, OnMiss: doctorOnMiss(merged, stderr)}), nil, false, "", exitOK
}

// resolveUpstream picks the live host to record against: an explicit --upstream wins;
// otherwise it is auto-detected from the request path via the dialect registry. ok is
// false when neither is available (unknown or ambiguous provider) so the caller can
// ask for --upstream rather than dial the wrong host.
func resolveUpstream(fixed, path string) (*url.URL, bool) {
	raw := fixed
	if raw == "" {
		u, ok := wireenc.UpstreamForURL(path)
		if !ok {
			return nil, false
		}
		raw = u
	}
	base, err := url.Parse(raw)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, false
	}
	return base, true
}

// doctorOnMiss turns a replay miss into a diagnosis instead of a bare 404: it logs the
// nearest recorded turn and the per-axis fix to the server's stderr, and adds an
// additive "hint" field to the (still-404) JSON body so existing clients/CI gates that
// key off status or error.type are unaffected.
func doctorOnMiss(f *wirefmt.File, stderr io.Writer) func(http.ResponseWriter, *http.Request) {
	st := newStyle(stderr)
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		_, summary, detail, derr := diagnoseMiss(f, r.Method, r.URL.Path, r.Header.Get("Content-Type"), body)
		hint := ""
		if derr == nil {
			fmt.Fprintf(stderr, "%s %s\n", st.cross(), summary)
			for _, line := range detail {
				fmt.Fprintf(stderr, "    %s\n", strings.TrimPrefix(line, "fix "))
			}
			hint = summary
			if len(detail) > 0 {
				hint += "; " + strings.TrimPrefix(detail[0], "fix ")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":{"type":"cassette_miss","message":%q,"hint":%q}}`+"\n",
			"no recorded interaction matches "+r.Method+" "+r.URL.Path, hint)
	}
}
