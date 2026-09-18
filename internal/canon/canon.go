// Package canon provides deterministic JSON canonicalization and stable digests
// used to build match keys and cassette body checksums. Canonicalization is the
// single source of truth for "the same request" — it must behave identically at
// record-index time and replay-lookup time.
package canon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// Canonicalize returns the canonical form of JSON input b and true, or (nil,
// false) when b is not a single valid JSON value.
//
// Canonical form: object keys sorted lexically, no insignificant whitespace,
// number textual form preserved (1 and 1.0 stay distinct; large integers keep
// full precision via json.Number), and HTML-escaping disabled so UTF-8 is
// emitted without unnecessary \u escapes.
func Canonicalize(b []byte) ([]byte, bool) {
	v, ok := decode(b)
	if !ok {
		return nil, false
	}
	return encode(v)
}

// CanonicalizeDropping behaves like Canonicalize but first removes the given
// dotted JSON paths (e.g. "metadata.trace_id") from the decoded value. Missing
// paths are ignored. Used to drop volatile-but-non-secret request fields from
// the match key only.
func CanonicalizeDropping(b []byte, dropPaths []string) ([]byte, bool) {
	v, ok := decode(b)
	if !ok {
		return nil, false
	}
	for _, p := range dropPaths {
		if p == "" {
			continue
		}
		v = dropPath(v, strings.Split(p, "."))
	}
	return encode(v)
}

// SumHex returns the lowercase hex SHA-256 of b (raw bytes).
func SumHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Digest returns the canonical digest of b: the SHA-256 of its canonical JSON form
// when b is valid JSON, otherwise the SHA-256 of the raw bytes. It is the shared
// body-digest rule used by both the MCP recorder (BodySHA256) and MCP key
// derivation, so the two never diverge.
func Digest(b []byte) string {
	if c, ok := Canonicalize(b); ok {
		return SumHex(c)
	}
	return SumHex(b)
}

func decode(b []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	// Reject trailing tokens: a body with two concatenated JSON values is not
	// a single canonical document.
	if dec.More() {
		return nil, false
	}
	return v, true
}

func encode(v any) ([]byte, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, false
	}
	// Encoder.Encode appends exactly one '\n'; strip it so the digest input is
	// stable and free of trailing whitespace.
	return bytes.TrimRight(buf.Bytes(), "\n"), true
}

func dropPath(v any, segs []string) any {
	if len(segs) == 0 {
		return v
	}
	switch node := v.(type) {
	case map[string]any:
		if len(segs) == 1 {
			delete(node, segs[0])
			return node
		}
		if child, ok := node[segs[0]]; ok {
			node[segs[0]] = dropPath(child, segs[1:])
		}
		return node
	case []any:
		// Numeric segment indexes into an array (gjson/sjson path syntax), so
		// volatile-field dropping and `redact-field` work on array paths like
		// "choices.0.message.content".
		idx, err := strconv.Atoi(segs[0])
		if err != nil || idx < 0 || idx >= len(node) {
			return node
		}
		if len(segs) == 1 {
			// Drop the element by replacing it with null (preserves array shape /
			// other indices in the canonical digest).
			node[idx] = nil
			return node
		}
		node[idx] = dropPath(node[idx], segs[1:])
		return node
	default:
		return v
	}
}
