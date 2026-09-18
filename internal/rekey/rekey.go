// Package rekey is the single shared match-key derivation for stored interactions:
// the SAME logic indexes interactions at record time, looks them up at replay time,
// rekeys a hand-edited cassette, and verifies key integrity. Keeping it in one
// internal package (rather than a leaky exported root function over *wirefmt.File)
// lets both the root cassette package and the cmd tooling rekey an in-memory file
// without exposing internal types on the public API.
package rekey

import (
	"fmt"

	"github.com/faisalkhan91/cassette/internal/canon"
	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// HTTPKey computes the match key for a stored HTTP request, using the same logic
// as a live request so record-index and replay-lookup never diverge.
func HTTPKey(r wirefmt.Request, cfg match.Config) (string, error) {
	// Gunzip a gzip-encoded stored body with the SAME guard the live path uses
	// (match.FromRequest), so a stored-request key matches the live-request key.
	body, err := match.DecodeBody(r.Body.Bytes(), r.Headers.Get("Content-Encoding"))
	if err != nil {
		return "", fmt.Errorf("rekey: gunzip request body: %w", err)
	}
	in := match.Input{
		Method:      r.Method,
		Path:        r.URL,
		Body:        body,
		ContentType: r.Headers.Get("Content-Type"),
		Header:      r.Headers.ToHTTP(),
	}
	return match.Key(in, cfg)
}

// CallKey derives the key for an MCP tools/call from the tool name + canonical
// arguments. cfg.BodyTransform (if set) is applied with the same semantics as the
// HTTP path; cfg.KeyFunc is HTTP-only and ignored here (see DECISIONS.md).
func CallKey(tool string, argsJSON []byte, cfg match.Config) string {
	path := "mcp:tools/call:" + tool
	if cfg.BodyTransform != nil {
		argsJSON = cfg.BodyTransform(path, argsJSON, "application/json")
	}
	return path + ":" + canon.Digest(argsJSON)
}

// ListKey derives the key for an MCP tools/list from its canonical params.
func ListKey(paramsJSON []byte, cfg match.Config) string {
	const path = "mcp:tools/list"
	if cfg.BodyTransform != nil {
		paramsJSON = cfg.BodyTransform(path, paramsJSON, "application/json")
	}
	return path + ":" + canon.Digest(paramsJSON)
}

// MCPKey rebuilds the key from a stored MCP request.
func MCPKey(r wirefmt.Request, cfg match.Config) string {
	switch r.MCPMethod {
	case "tools/call":
		return CallKey(r.MCPTool, r.Body.Bytes(), cfg)
	case "tools/list":
		return ListKey(r.Body.Bytes(), cfg)
	default:
		path := "mcp:" + r.MCPMethod
		body := r.Body.Bytes()
		if cfg.BodyTransform != nil {
			body = cfg.BodyTransform(path, body, "application/json")
		}
		return path + ":" + canon.Digest(body)
	}
}

// File recomputes every interaction's persisted MatchKey from its stored request
// under cfg, in place. Use it after editing a cassette by hand (or after redacting
// a field), since index() trusts the persisted key — a stale key silently desyncs
// replay.
func File(f *wirefmt.File, cfg match.Config) error {
	for i, it := range f.Interactions {
		switch it.Kind {
		case "http":
			k, err := HTTPKey(it.Request, cfg)
			if err != nil {
				return fmt.Errorf("rekey: interaction %d: %w", i, err)
			}
			it.Request.MatchKey = k
		case "mcp":
			it.Request.MatchKey = MCPKey(it.Request, cfg)
		}
	}
	return nil
}
