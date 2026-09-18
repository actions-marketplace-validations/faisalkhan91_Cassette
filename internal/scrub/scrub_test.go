package scrub

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func TestNamedCredentialShapes(t *testing.T) {
	// Each named shape is both detected by the scanner and redacted from bodies.
	cases := map[string]string{
		"aws":    "AKIAIOSFODNN7EXAMPLE",
		"github": "ghp_0123456789012345678901234567890123456789",
		"jwt":    "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc-_DEF",
		"google": "AIzaSyA1234567890123456789012345678901234",
	}
	for name, secret := range cases {
		if hits := SecretScan([]byte("token=" + secret)); len(hits) == 0 {
			t.Errorf("%s: SecretScan did not flag %q", name, secret)
		}
		b := &wirefmt.Body{Data: []byte(`{"k":"` + secret + `"}`)}
		DefaultConfig().body(b)
		if bytes.Contains(b.Data, []byte(secret)) {
			t.Errorf("%s: body scrub left the secret in place: %s", name, b.Data)
		}
	}
	// A non-secret high-entropy / Luhn-shaped value must NOT trip the hard gate.
	if hits := SecretScan([]byte("id=4111111111111111 base64likeAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")); len(hits) != 0 {
		t.Errorf("scanner should not flag card/entropy shapes (false positives): %v", hits)
	}
}

func TestScrub_RemovesSecrets(t *testing.T) {
	f := &wirefmt.File{
		SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{{
			Kind: "http",
			Request: wirefmt.Request{
				Method: "POST",
				URL:    "/v1/messages",
				Headers: wirefmt.HeadersFromHTTP(http.Header{
					"Authorization": {"Bearer sk-ant-SENTINEL123"},
					"X-Api-Key":     {"sk-SENTINEL123"},
					"Content-Type":  {"application/json"},
				}),
				Body: wirefmt.NewBody([]byte(`{"api_key":"sk-SENTINEL123","model":"claude"}`)),
			},
			Response: wirefmt.Response{
				Status:  200,
				Headers: wirefmt.HeadersFromHTTP(http.Header{"Set-Cookie": {"sess=sk-SENTINEL123"}}),
				Body:    wirefmt.NewBody([]byte(`{"id":"msg_real_123","created":1700000000,"content":"hi"}`)),
			},
		}},
	}

	DefaultConfig().File(f)

	data, err := wirefmt.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, sentinel := range []string{"sk-ant-SENTINEL123", "sk-SENTINEL123", "sk-SENTINEL", "Bearer sk", "sk-"} {
		if bytes.Contains(data, []byte(sentinel)) {
			t.Fatalf("scrubbed cassette still contains secret %q:\n%s", sentinel, data)
		}
	}
	if hits := SecretScan(data); len(hits) != 0 {
		t.Fatalf("SecretScan found secrets after scrub: %v\n%s", hits, data)
	}

	// Volatile response fields stamped.
	got := f.Interactions[0].Response.Body.Bytes()
	if !bytes.Contains(got, []byte("[SCRUBBED]")) {
		t.Fatalf("expected response id to be stamped: %s", got)
	}
	if bytes.Contains(got, []byte("msg_real_123")) {
		t.Fatalf("real response id should be stamped out: %s", got)
	}
}

func TestSecretScan_FlagsAndPasses(t *testing.T) {
	bad := [][]byte{
		[]byte("authorization: Bearer sk-ant-abc123"),
		[]byte(`{"key":"sk-abc123"}`),
		[]byte("x-api-key: whatever"),
	}
	for _, b := range bad {
		if !HasSecrets(b) {
			t.Fatalf("expected secrets flagged in %q", b)
		}
	}
	good := [][]byte{
		[]byte("Authorization: [REDACTED]"),
		[]byte(`{"model":"claude","max_tokens":5}`),
		[]byte("X-Api-Key:\n  - REDACTED"),
	}
	for _, b := range good {
		if HasSecrets(b) {
			t.Fatalf("false positive on clean data %q: %v", b, SecretScan(b))
		}
	}
}

func TestScrub_ReplayIndependentOfScrubbedValues(t *testing.T) {
	// The match-relevant content (method, path, non-secret body) must be
	// unchanged by scrubbing — only secret values and volatile response fields
	// change.
	orig := []byte(`{"model":"claude","max_tokens":5}`)
	f := &wirefmt.File{
		SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{{
			Kind: "http",
			Request: wirefmt.Request{
				Method:  "POST",
				URL:     "/v1/messages",
				Headers: wirefmt.HeadersFromHTTP(http.Header{"Authorization": {"Bearer sk-ant-X"}}),
				Body:    wirefmt.NewBody(orig),
			},
			Response: wirefmt.Response{Status: 200, Body: wirefmt.NewBody([]byte(`{"content":"hi"}`))},
		}},
	}
	DefaultConfig().File(f)
	if got := string(f.Interactions[0].Request.Body.Bytes()); got != string(orig) {
		t.Fatalf("non-secret request body altered by scrub: %s", got)
	}
	if v := f.Interactions[0].Request.Headers.Get("Authorization"); v != Replacement {
		t.Fatalf("authorization not redacted: %q", v)
	}
	if strings.Contains(string(f.Interactions[0].Request.Headers.Get("Authorization")), "Bearer") {
		t.Fatal("redacted header should not retain Bearer prefix")
	}
}

func TestScrub_GoogleAndAWSHeaders(t *testing.T) {
	f := &wirefmt.File{
		SchemaVersion: wirefmt.SchemaVersion,
		Interactions: []*wirefmt.Interaction{{
			Kind: "http",
			Request: wirefmt.Request{
				Method: "POST", URL: "/v1beta/models/gemini:generateContent",
				Headers: wirefmt.HeadersFromHTTP(http.Header{
					"X-Goog-Api-Key":       {"AIzaSyREALKEY123"},
					"X-Amz-Security-Token": {"FwoGZXIvSECRET"},
					"X-Amz-Date":           {"20260622T000000Z"},
				}),
			},
			Response: wirefmt.Response{Status: 200},
		}},
	}
	DefaultConfig().File(f)
	h := f.Interactions[0].Request.Headers
	for _, name := range []string{"X-Goog-Api-Key", "X-Amz-Security-Token", "X-Amz-Date"} {
		if got := h.Get(name); got != Replacement {
			t.Fatalf("%s = %q, want %q", name, got, Replacement)
		}
	}
	data, _ := wirefmt.Marshal(f)
	if bytes.Contains(data, []byte("AIzaSyREALKEY123")) || bytes.Contains(data, []byte("FwoGZXIvSECRET")) {
		t.Fatalf("provider secret leaked:\n%s", data)
	}
}
