// Package scrub redacts secrets from a cassette before it is written and stamps
// volatile-but-non-secret response fields to fixed sentinels. Scrubbing and match
// normalization are independent: scrubbing changes what is STORED; it must never
// be required for replay to work. Both are self-checking via SecretScan.
package scrub

import (
	"net/http"
	"regexp"
	"sort"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Replacement is the string substituted for redacted secret values.
const Replacement = "REDACTED"

// Config controls redaction and stamping. The zero value scrubs nothing; use
// DefaultConfig for sensible provider defaults.
type Config struct {
	// RedactHeaders lists header names (canonical-insensitive) whose VALUES are
	// replaced with Replacement, in both request and response headers.
	RedactHeaders []string
	// BodyPatterns are regexps whose matches in any stored body are replaced with
	// Replacement. Used to catch secrets embedded in request/response bodies.
	BodyPatterns []*regexp.Regexp
	// ResponseStamps maps a JSON path (gjson/sjson syntax) to a sentinel value.
	// Applied to non-streaming JSON response bodies, only where the path exists.
	ResponseStamps map[string]any
}

// DefaultConfig returns the provider-agnostic defaults: redact common secret
// headers, scrub key-shaped tokens from bodies, and stamp volatile response
// identifiers.
func DefaultConfig() Config {
	return Config{
		RedactHeaders: []string{
			"Authorization", "X-Api-Key", "Api-Key",
			"OpenAI-Organization", "Cookie", "Set-Cookie",
			// Google Gemini API key header.
			"X-Goog-Api-Key",
			// AWS SigV4: the session token is a secret; the date/content-hash are
			// volatile signing fields redacted for byte-stable re-records.
			"X-Amz-Security-Token", "X-Amz-Date", "X-Amz-Content-Sha256",
		},
		BodyPatterns: append([]*regexp.Regexp{
			// API keys (Anthropic/OpenAI style) and bearer tokens. The bearer
			// pattern intentionally consumes the "Bearer " prefix so no
			// "Bearer <token>" residue remains for the secret scanner.
			regexp.MustCompile(`sk-[A-Za-z0-9._\-]+`),
			regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-]+`),
		}, namedCredentialPatterns...),
		ResponseStamps: map[string]any{
			"id":                 "[SCRUBBED]",
			"created":            0,
			"system_fingerprint": "[SCRUBBED]",
		},
	}
}

// File applies the configured scrubbing to f in place.
func (c Config) File(f *wirefmt.File) {
	for _, it := range f.Interactions {
		c.headers(it.Request.Headers)
		c.headers(it.Response.Headers)
		c.body(it.Request.Body)
		if it.Response.Streaming {
			// Streaming bodies are stored verbatim to preserve framing; only
			// secret patterns are redacted (below), volatile response fields are
			// normalized at compare time, not stamped in-stream.
			c.body(it.Response.Body)
		} else {
			c.body(it.Response.Body)
			c.stampResponse(it.Response.Body)
		}
	}
}

func (c Config) headers(hs wirefmt.Headers) {
	if len(hs) == 0 {
		return
	}
	redact := make(map[string]bool, len(c.RedactHeaders))
	for _, n := range c.RedactHeaders {
		redact[http.CanonicalHeaderKey(n)] = true
	}
	for i := range hs {
		if redact[http.CanonicalHeaderKey(hs[i].Name)] {
			for j := range hs[i].Values {
				hs[i].Values[j] = Replacement
			}
		}
	}
}

func (c Config) body(b *wirefmt.Body) {
	if b == nil || len(b.Data) == 0 {
		return
	}
	for _, re := range c.BodyPatterns {
		b.Data = re.ReplaceAll(b.Data, []byte(Replacement))
	}
}

func (c Config) stampResponse(b *wirefmt.Body) {
	if b == nil || len(b.Data) == 0 || len(c.ResponseStamps) == 0 {
		return
	}
	if !gjson.ValidBytes(b.Data) {
		return
	}
	data := b.Data
	// Apply stamps in sorted path order so the result is deterministic regardless of
	// map iteration (matters only for overlapping/nested paths, but the byte-exact
	// invariant forbids any map-iteration-order dependence in stored output).
	paths := make([]string, 0, len(c.ResponseStamps))
	for path := range c.ResponseStamps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !gjson.GetBytes(data, path).Exists() {
			continue
		}
		if next, err := sjson.SetBytes(data, path, c.ResponseStamps[path]); err == nil {
			data = next
		}
	}
	b.Data = data
}

// secretScanPatterns flag leaked secrets in committed cassettes. They are
// case-sensitive and target secret VALUES, not redacted header names (which are
// stored in canonical "X-Api-Key" form and so never match the lowercase
// "x-api-key:" probe).
var secretScanPatterns = append([]*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9]`),
	regexp.MustCompile(`Bearer [A-Za-z0-9]`),
	regexp.MustCompile(`x-api-key:`),
}, namedCredentialPatterns...)

// namedCredentialPatterns are specific, low-false-positive credential SHAPES
// shared by the body scrubber and the secret scanner: AWS access keys, GitHub
// tokens, JWTs, and Google API keys. They are NOT general heuristics (no Luhn /
// high-entropy / SSN) precisely so the hard-fail scanner doesn't trip on
// legitimate recorded content.
var namedCredentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                     // AWS access key id
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),                           // GitHub token
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`), // JWT
	regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`),                               // Google API key
}

// SecretScan returns the distinct offending substrings found in data, or nil if
// clean. Used by `cassette verify`, the CI secret-scan, and TestScrub.
func SecretScan(data []byte) []string {
	var hits []string
	seen := map[string]bool{}
	for _, re := range secretScanPatterns {
		for _, m := range re.FindAll(data, -1) {
			s := string(m)
			if !seen[s] {
				seen[s] = true
				hits = append(hits, s)
			}
		}
	}
	return hits
}

// HasSecrets reports whether data contains any flagged secret pattern.
func HasSecrets(data []byte) bool {
	return len(SecretScan(data)) > 0
}
