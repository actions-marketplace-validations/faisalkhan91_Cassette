package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	cassette "github.com/faisalkhan91/cassette"
	analysis "github.com/faisalkhan91/cassette/internal/analysis"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// openForReplay opens a single cassette in replay mode, or merges every cassette
// in a directory into one logical replay endpoint (so `cassette serve <dir>`
// serves a whole fixture library; the match key already routes by path+body).
func openForReplay(path string, match cassette.MatchConfig) (*cassette.Cassette, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return cassette.Open(path, cassette.Options{Mode: cassette.ModeReplay, Match: match})
	}
	files, err := loadCassettes(path)
	if err != nil {
		return nil, err
	}
	merged, _ := analysis.Merge(files...)
	data, err := wirefmt.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return cassette.OpenBytes(path, data, cassette.Options{Mode: cassette.ModeReplay, Match: match})
}

// volatileMatch reads a --volatile "a.b,c" flag into a MatchConfig whose
// VolatileJSONPaths are dropped from the request body before keying — so volatile
// request fields a real client injects (e.g. a per-session id, an env-stamped
// system prompt) don't break replay matching. Must be set identically at record
// and replay.
func volatileMatch(in *Inv) cassette.MatchConfig {
	var paths []string
	for _, p := range strings.Split(in.str("volatile"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	return cassette.MatchConfig{VolatileJSONPaths: paths}
}

// perTestHandler routes each request to a per-test cassette in dir, selected by the
// value of the routing header (e.g. X-Cassette-Test: login -> dir/login.yaml). Each
// test gets an isolated cassette (its own match cursors), so concurrent or
// out-of-order tests can't consume each other's interactions. A missing/unknown
// test name is treated as a miss (recorded by the CI gate when active).
func perTestHandler(dir, header string, match cassette.MatchConfig, opts cassette.ServeOptions, stderr io.Writer) (http.Handler, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	routes := map[string]http.Handler{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		c, err := cassette.Open(filepath.Join(dir, e.Name()), cassette.Options{Mode: cassette.ModeReplay, Match: match})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		routes[strings.TrimSuffix(e.Name(), ext)] = c.Handler(opts)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no .yaml cassettes in %s", dir)
	}
	miss := opts.OnMiss
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.Header.Get(header)
		if h, ok := routes[name]; ok {
			h.ServeHTTP(w, r)
			return
		}
		if miss != nil { // record the unroutable request for the CI gate
			miss(w, r)
			return
		}
		fmt.Fprintf(stderr, "cassette: no test cassette for %s=%q\n", header, name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":{"type":"cassette_no_route","message":%q}}`+"\n", "no cassette for "+header+"="+name)
	}), nil
}

const serveUsageText = "usage: cassette serve <cassette.yaml|dir> [--addr :8080] [--pace] [--log-misses] [--on-miss fail] [--miss-manifest path] [--route-header NAME] [--volatile a.b,c]"

func cmdServe(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("pace", "log-misses", "strict").valFlag("addr", "on-miss", "miss-manifest", "route-header", "volatile"), args, serveUsageText, stderr)
	if !ok {
		return exitUsage
	}
	path := in.arg(0)
	if path == "" || in.nargs() > 1 {
		fmt.Fprintln(stderr, serveUsageText)
		return exitUsage
	}
	if v := in.str("on-miss"); in.has("on-miss") && v != "fail" {
		fmt.Fprintf(stderr, "cassette: --on-miss only accepts \"fail\" (got %q)\n", v)
		return exitUsage
	}
	addr := in.strOr("addr", ":8080")
	// --log-misses logs a 404 for each un-recorded request (no exit-code change).
	// --strict is the deprecated alias for it (gating belongs to --on-miss=fail).
	pace := in.boolv("pace")
	logMisses := in.boolv("log-misses") || in.boolv("strict")

	opts := cassette.ServeOptions{Pace: pace}
	// --on-miss=fail turns serve into a CI gate: record every miss and exit nonzero
	// on shutdown. --log-misses keeps the log-only 404 behavior (no gating).
	var rec *missRecorder
	switch {
	case in.str("on-miss") == "fail":
		rec = newMissRecorder()
		opts.OnMiss = recordingOnMiss(rec, stderr)
	case logMisses:
		opts.OnMiss = recordingOnMiss(newMissRecorder(), stderr)
	}

	// --route-header enables per-test isolation: route by header to dir/<name>.yaml.
	var handler http.Handler
	if rh := in.str("route-header"); rh != "" {
		if info, serr := os.Stat(path); serr != nil || !info.IsDir() {
			fmt.Fprintf(stderr, "cassette: --route-header requires a directory of per-test cassettes\n")
			return exitUsage
		}
		h, herr := perTestHandler(path, rh, volatileMatch(in), opts, stderr)
		if herr != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", herr)
			return exitFail
		}
		handler = h
	} else {
		c, err := openForReplay(path, volatileMatch(in))
		if err != nil {
			fmt.Fprintf(stderr, "cassette: %v\n", err)
			return exitFail
		}
		handler = c.Handler(opts)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: listen %s: %v\n", addr, err)
		return exitFail
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s cassette serving %s on http://%s\n", st.check(), path, ln.Addr())

	if err := serveUntilSignal(ln, handler); err != nil {
		fmt.Fprintf(stderr, "cassette: serve: %v\n", err)
		return exitFail
	}
	return finalizeMisses(rec, missManifestPath(in), stdout, stderr)
}

// serveUntilSignal serves h on ln until SIGINT/SIGTERM, then gracefully shuts down
// (2s grace). Returns the Serve error (nil on a clean signal-triggered shutdown).
// Shared by serve, proxy, and up so the listen/signal/shutdown loop lives once.
func serveUntilSignal(ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
