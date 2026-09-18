// Package match computes the cassette match key for a request. The SAME function
// is used to index interactions at record time and to look them up at replay
// time, so any request that should "be the same recorded call" must produce a
// byte-identical key on both paths.
//
// Key = tuple(method, normalized URL path, canonicalized+scrubbed body digest,
// allowlisted headers). The default header allowlist is EMPTY: nothing from
// headers enters the key for the OpenAI/Anthropic shapes. Volatile-but-non-secret
// request fields (api-key, request-id, idempotency-key, timestamps, reordered
// JSON keys, gzip vs identity encoding) are normalized away so they never change
// the key.
package match

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/faisalkhan91/cassette/internal/canon"
)

// Config controls how the match key is derived.
type Config struct {
	// HeaderAllowlist lists header names (canonical form) whose values DO enter
	// the key. Default empty.
	HeaderAllowlist []string
	// VolatileJSONPaths lists dotted JSON paths dropped from the request body
	// before digesting (match key only). Default empty.
	VolatileJSONPaths []string
	// PathTransform, when set, normalizes the URL path before it enters the key
	// (key derivation only). Use it to collapse volatile path segments, e.g. an
	// Azure deployment name or a tenant id. Runs identically at record and replay.
	PathTransform func(method, path string) string
	// BodyTransform, when set, is applied to the request body before it is
	// digested into the key (key derivation only — the stored body is untouched).
	// Use it to normalize away nondeterminism a JSON-path drop cannot express.
	// It runs identically at record-index and replay-lookup time. The path is the
	// URL path (HTTP) or "mcp:<method>:<tool>" (MCP). Must be deterministic.
	BodyTransform func(path string, body []byte, contentType string) []byte
	// KeyFunc, when set, fully overrides key derivation for HTTP requests. It must
	// be deterministic and is the caller's complete responsibility. (HTTP only;
	// MCP keys are derived from tool name + canonical arguments — see DECISIONS.md.)
	KeyFunc func(Input) (string, error)
}

// azureDeploymentRe matches the Azure OpenAI deployment segment in a path.
var azureDeploymentRe = regexp.MustCompile(`(/openai/deployments/)[^/]+`)

// AzureConfig returns a Config that collapses the Azure OpenAI deployment name
// in the request path (e.g. /openai/deployments/my-gpt4o/chat/completions →
// .../deployments/{deployment}/...), so one cassette replays across deployments.
// The api-version query string and host are already excluded from the key
// (the key uses the path only).
func AzureConfig() Config {
	return Config{
		PathTransform: func(_, path string) string {
			return azureDeploymentRe.ReplaceAllString(path, "${1}{deployment}")
		},
	}
}

// Input is the normalized, transport-decoded view of a request used to derive a
// key. Build it via FromRequest (live/replay request) or directly from a stored
// interaction so both paths run identical logic.
type Input struct {
	Method string
	// Path is the URL path only; host is intentionally excluded so a cassette is
	// portable across base URLs.
	Path string
	// Body is the already-decompressed request body (Content-Encoding removed).
	Body        []byte
	ContentType string
	// Header is consulted only for allowlisted names.
	Header http.Header
}

// FromRequest builds an Input from a live request and its (already buffered) raw
// body bytes. It transparently gunzips a gzip-Content-Encoding body so that a
// gzip request and an identity request with the same logical content produce the
// same key.
func FromRequest(req *http.Request, rawBody []byte) (Input, error) {
	body, err := DecodeBody(rawBody, req.Header.Get("Content-Encoding"))
	if err != nil {
		return Input{}, fmt.Errorf("match: gunzip request body: %w", err)
	}
	return Input{
		Method:      req.Method,
		Path:        req.URL.Path,
		Body:        body,
		ContentType: req.Header.Get("Content-Type"),
		Header:      req.Header,
	}, nil
}

// DecodeBody returns body with gzip Content-Encoding removed, applying the SAME
// guard FromRequest uses — so keying a STORED request (rekey/conformance/keyless
// index) matches keying the LIVE request. contentEncoding is the request's
// Content-Encoding header value; a non-gzip value returns body unchanged.
func DecodeBody(body []byte, contentEncoding string) ([]byte, error) {
	if strings.Contains(strings.ToLower(contentEncoding), "gzip") && len(body) > 0 {
		return gunzip(body)
	}
	return body, nil
}

// Key derives the match key for in under cfg.
func Key(in Input, cfg Config) (string, error) {
	if cfg.KeyFunc != nil {
		return cfg.KeyFunc(in)
	}
	path := in.Path
	if cfg.PathTransform != nil {
		path = cfg.PathTransform(in.Method, path)
	}
	body := in.Body
	if cfg.BodyTransform != nil {
		body = cfg.BodyTransform(path, body, in.ContentType)
	}
	kind, digest, err := bodyDigest(body, in.ContentType, cfg.VolatileJSONPaths)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(strings.ToUpper(in.Method))
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString(kind)
	b.WriteByte(':')
	b.WriteString(digest)

	// Allowlisted headers, canonicalized + sorted, appended deterministically.
	if len(cfg.HeaderAllowlist) > 0 && in.Header != nil {
		names := make([]string, 0, len(cfg.HeaderAllowlist))
		for _, n := range cfg.HeaderAllowlist {
			names = append(names, http.CanonicalHeaderKey(n))
		}
		sort.Strings(names)
		b.WriteString("\nH")
		for _, n := range names {
			vals := append([]string(nil), in.Header.Values(n)...)
			sort.Strings(vals)
			b.WriteByte('\n')
			b.WriteString(n)
			b.WriteByte('=')
			b.WriteString(strings.Join(vals, ","))
		}
	}
	return b.String(), nil
}

// BodyDigest returns a kind tag ("json"|"multipart"|"raw") and a stable digest of
// the body under the matching rule for its content type. Exposed so recorders can
// compute the stored body_sha256 with the SAME canonicalization used for keys.
func BodyDigest(body []byte, contentType string, dropPaths []string) (kind, digest string, err error) {
	return bodyDigest(body, contentType, dropPaths)
}

// bodyDigest returns a kind tag ("json"|"multipart"|"raw") and a stable digest of
// the body under the matching rule for its content type.
func bodyDigest(body []byte, contentType string, dropPaths []string) (kind, digest string, err error) {
	mediaType, params, _ := mime.ParseMediaType(contentType)

	switch {
	case strings.Contains(mediaType, "json") || (mediaType == "" && looksJSON(body)):
		if c, ok := canon.CanonicalizeDropping(body, dropPaths); ok {
			return "json", canon.SumHex(c), nil
		}
		// Declared JSON but unparseable: fall back to raw so we never silently
		// match two different malformed bodies.
		return "raw", canon.SumHex(body), nil

	case strings.HasPrefix(mediaType, "multipart/"):
		d, err := multipartDigest(body, params["boundary"])
		if err != nil {
			return "", "", err
		}
		return "multipart", d, nil

	default:
		return "raw", canon.SumHex(body), nil
	}
}

// multipartDigest keys on the ordered (field-name, content-type, content-digest)
// of each part and deliberately EXCLUDES the random boundary token.
func multipartDigest(body []byte, boundary string) (string, error) {
	if boundary == "" {
		return canon.SumHex(body), nil
	}
	r := multipart.NewReader(bytes.NewReader(body), boundary)
	var desc bytes.Buffer
	for {
		part, err := r.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("match: parse multipart: %w", err)
		}
		content, err := io.ReadAll(part)
		if err != nil {
			return "", fmt.Errorf("match: read multipart part: %w", err)
		}
		fmt.Fprintf(&desc, "%s\x1f%s\x1f%s\x1e",
			part.FormName(), part.Header.Get("Content-Type"), canon.SumHex(content))
		_ = part.Close()
	}
	return canon.SumHex(desc.Bytes()), nil
}

func looksJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
