package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
)

// missRecorder collects replay misses (live requests matching no recorded
// interaction) for the `--on-miss=fail` CI gate. Deduped by method+path,
// concurrency-safe, deterministic order.
type missRecorder struct {
	mu   sync.Mutex
	seen map[string]*missEntry
}

type missEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Count  int    `json:"count"`
}

func newMissRecorder() *missRecorder { return &missRecorder{seen: map[string]*missEntry{}} }

func (m *missRecorder) record(method, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := method + " " + path
	e := m.seen[k]
	if e == nil {
		e = &missEntry{Method: method, Path: path}
		m.seen[k] = e
	}
	e.Count++
}

func (m *missRecorder) empty() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.seen) == 0
}

// manifest returns the deduped misses in deterministic (method, path) order.
func (m *missRecorder) manifest() []missEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]missEntry, 0, len(m.seen))
	for _, e := range m.seen {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// missManifestVersion versions the on-disk manifest so CI consumers can rely on its shape.
const missManifestVersion = 1

type missManifest struct {
	Version int         `json:"version"`
	Misses  []missEntry `json:"misses"`
}

func (m *missRecorder) writeManifest(path string) error {
	data, err := json.MarshalIndent(missManifest{Version: missManifestVersion, Misses: m.manifest()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// recordingOnMiss is a ServeOptions.OnMiss that records the miss, logs it, and
// returns the standard cassette_miss 404 (so the client still gets a response).
func recordingOnMiss(rec *missRecorder, stderr io.Writer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		fmt.Fprintf(stderr, "cassette: MISS %s %s\n", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":{"type":"cassette_miss","message":%q}}`+"\n", "no recorded interaction matches "+r.URL.Path)
	}
}

// finalizeMisses is the shutdown hook for the CI gate: when any request missed, it
// writes the manifest and returns exitFail; otherwise exitOK. nil recorder = gate off.
func finalizeMisses(rec *missRecorder, manifestPath string, stdout, stderr io.Writer) int {
	if rec == nil || rec.empty() {
		return exitOK
	}
	n := len(rec.manifest())
	if err := rec.writeManifest(manifestPath); err != nil {
		fmt.Fprintf(stderr, "cassette: write miss-manifest: %v\n", err)
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s %d uncovered request(s) — wrote %s (CI gate failed)\n", st.cross(), n, manifestPath)
	return exitFail
}

// missManifestPath resolves the --miss-manifest flag (default cassette-misses.json).
func missManifestPath(in *Inv) string {
	if p := in.str("miss-manifest"); p != "" {
		return p
	}
	return "cassette-misses.json"
}
