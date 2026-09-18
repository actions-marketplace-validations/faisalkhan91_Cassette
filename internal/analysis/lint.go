package analysis

import (
	"bytes"
	"fmt"

	"github.com/faisalkhan91/cassette/internal/match"
	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wireenc"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// DefaultMaxBodyBytes is the lint warning threshold for a single response body.
const DefaultMaxBodyBytes = 1 << 20 // 1 MiB

// Finding is one lint result. Index is -1 for a file-level finding.
type Finding struct {
	File     string `json:"file"`
	Index    int    `json:"index"`
	Severity string `json:"severity"` // "error" | "warn"
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// LintFile runs every per-cassette health check over one recording and returns
// its findings. raw is the on-disk bytes (for the secret scan); f is the parsed
// file. Errors (secrets, stale match keys) should fail CI; warnings (missing
// finish reason, oversized body, empty/undecodable stream, empty file) are advisory
// unless a caller treats them as strict. maxBody is the oversized-body threshold
// (0 → DefaultMaxBodyBytes).
func LintFile(name string, raw []byte, f *wirefmt.File, maxBody int) []Finding {
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}
	var out []Finding
	add := func(idx int, sev, code, msg string) {
		out = append(out, Finding{File: name, Index: idx, Severity: sev, Code: code, Message: msg})
	}

	if hits := scrub.SecretScan(raw); len(hits) > 0 {
		add(-1, "error", "secret", fmt.Sprintf("secret pattern(s) present: %v", hits))
	}
	if len(f.Interactions) == 0 {
		add(-1, "warn", "empty", "cassette has no interactions")
	}

	// Re-derive keys under the cassette's own persisted match normalization, so a
	// recording made with --volatile isn't reported as stale.
	var matchCfg match.Config
	if f.Match != nil {
		matchCfg = match.Config{VolatileJSONPaths: f.Match.VolatileJSONPaths, HeaderAllowlist: f.Match.HeaderAllowlist}
	}

	for i, it := range f.Interactions {
		if it.Kind != "http" && it.Kind != "mcp" {
			continue
		}
		// The key is derived at record from the LIVE (pre-scrub) body under the
		// cassette's own match normalization. Re-derive under that same normalization
		// (the persisted match: block), and treat a scrubbed body as not re-derivable
		// — the stored key is then the authority for replay, so it's NOT stale and
		// must NOT be "fixed" with rekey (which would overwrite the correct key).
		bodyScrubbed := bytes.Contains(it.Request.Body.Bytes(), []byte(scrub.Replacement))
		if key, ok := recomputeKey(it, matchCfg); ok && it.Request.MatchKey != "" && key != it.Request.MatchKey && !bodyScrubbed {
			add(i, "error", "stale_key", "persisted match key does not re-derive from the request (run `cassette rekey`)")
		}
		if n := len(it.Response.Body.Bytes()); n > maxBody {
			add(i, "warn", "oversized", fmt.Sprintf("response body is %d bytes (> %d)", n, maxBody))
		}
		if it.Kind != "http" {
			continue
		}
		tr, unary, renderable := DecodeInteraction(it)
		if !renderable || unary {
			continue
		}
		// A streaming turn that decodes to nothing is almost always broken framing.
		if tr.Text == "" && len(tr.ToolCalls) == 0 && tr.FinishReason == "" {
			if _, err := wireenc.RoundTrip(wireenc.ProviderForURL(it.Request.URL), it.Response.Body.Bytes()); err != nil {
				continue // a decoded error stream is legitimate, not undecodable
			}
			add(i, "warn", "undecodable", "streaming response decodes to an empty transcript")
			continue
		}
		if tr.FinishReason == "" {
			add(i, "warn", "no_finish", "streaming response has no finish reason")
		}
	}
	return out
}

// recomputeKey derives the http match key from the stored request. Only http is
// handled: match.Key IS the canonical http algorithm, whereas the canonical MCP
// key algorithm lives in the root package, so checking MCP here would risk false
// positives.
func recomputeKey(it *wirefmt.Interaction, cfg match.Config) (string, bool) {
	if it.Kind != "http" {
		return "", false
	}
	k, err := match.Key(match.Input{
		Method:      it.Request.Method,
		Path:        it.Request.URL,
		Body:        it.Request.Body.Bytes(),
		ContentType: it.Request.Headers.Get("Content-Type"),
	}, cfg)
	return k, err == nil
}

// LintSummary counts findings by severity.
func LintSummary(fs []Finding) (errors, warnings int) {
	for _, f := range fs {
		if f.Severity == "error" {
			errors++
		} else {
			warnings++
		}
	}
	return errors, warnings
}
