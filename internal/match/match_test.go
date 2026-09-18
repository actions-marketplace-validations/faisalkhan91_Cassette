package match

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func keyForBytes(t *testing.T, method, url, contentType string, body []byte, cfg Config, mutate func(*http.Request)) string {
	t.Helper()
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if mutate != nil {
		mutate(req)
	}
	in, err := FromRequest(req, body)
	if err != nil {
		t.Fatalf("FromRequest: %v", err)
	}
	k, err := Key(in, cfg)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	return k
}

func TestMatchKey_ShuffledJSONKeysEqual(t *testing.T) {
	a := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	b := []byte(`{"max_tokens":10,"messages":[{"content":"hi","role":"user"}],"model":"claude"}`)
	ka := keyForBytes(t, "POST", "https://api/x", "application/json", a, Config{}, nil)
	kb := keyForBytes(t, "POST", "https://api/x", "application/json", b, Config{}, nil)
	if ka != kb {
		t.Fatalf("shuffled JSON keys must match:\n a=%q\n b=%q", ka, kb)
	}
}

func TestMatchKey_GzipVsIdentityEqual(t *testing.T) {
	body := []byte(`{"model":"claude","max_tokens":5}`)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	identity := keyForBytes(t, "POST", "https://api/x", "application/json", body, Config{}, nil)
	gzKey := keyForBytes(t, "POST", "https://api/x", "application/json", gz.Bytes(), Config{}, func(r *http.Request) {
		r.Header.Set("Content-Encoding", "gzip")
	})
	if identity != gzKey {
		t.Fatalf("gzip and identity bodies must match:\n id=%q\n gz=%q", identity, gzKey)
	}
}

func TestMatchKey_VolatileFieldsStillMatch(t *testing.T) {
	cfg := Config{VolatileJSONPaths: []string{"metadata.ts"}}
	base := `{"model":"claude","max_tokens":5,"metadata":{"ts":%q,"user":"u"}}`

	mk := func(ts, apiKey, reqID, idem string, shuffle bool) string {
		body := []byte(strings.Replace(base, `%q`, `"`+ts+`"`, 1))
		if shuffle {
			body = []byte(`{"max_tokens":5,"metadata":{"user":"u","ts":"` + ts + `"},"model":"claude"}`)
		}
		return keyForBytes(t, "POST", "https://api/v1/messages", "application/json", body, cfg, func(r *http.Request) {
			r.Header.Set("x-api-key", apiKey)
			r.Header.Set("X-Request-Id", reqID)
			r.Header.Set("Idempotency-Key", idem)
			r.Header.Set("Date", ts)
		})
	}

	k1 := mk("2026-01-01T00:00:00Z", "sk-ant-AAA", "req-1", "idem-1", false)
	k2 := mk("2026-06-17T12:00:00Z", "sk-ant-BBB", "req-2", "idem-2", true)
	if k1 != k2 {
		t.Fatalf("requests differing only in volatile fields must match:\n k1=%q\n k2=%q", k1, k2)
	}
}

func TestMatchKey_DifferentBodiesDiffer(t *testing.T) {
	a := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"a":1}`), Config{}, nil)
	b := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"a":2}`), Config{}, nil)
	if a == b {
		t.Fatal("different bodies must produce different keys")
	}
}

func TestMatchKey_IntFloatDistinct(t *testing.T) {
	// 1 and 1.0 must NOT collapse — the canonicalizer preserves number text.
	a := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"n":1}`), Config{}, nil)
	b := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"n":1.0}`), Config{}, nil)
	if a == b {
		t.Fatal("1 and 1.0 must produce different keys (int/float distinction)")
	}
}

func TestMatchKey_MethodAndPathMatter(t *testing.T) {
	body := []byte(`{"a":1}`)
	post := keyForBytes(t, "POST", "https://api/v1/messages", "application/json", body, Config{}, nil)
	get := keyForBytes(t, "GET", "https://api/v1/messages", "application/json", body, Config{}, nil)
	other := keyForBytes(t, "POST", "https://api/v1/other", "application/json", body, Config{}, nil)
	if post == get || post == other {
		t.Fatal("method and path must contribute to the key")
	}
}

func TestMatchKey_HostExcluded(t *testing.T) {
	body := []byte(`{"a":1}`)
	h1 := keyForBytes(t, "POST", "https://api.anthropic.com/v1/messages", "application/json", body, Config{}, nil)
	h2 := keyForBytes(t, "POST", "http://127.0.0.1:5391/v1/messages", "application/json", body, Config{}, nil)
	if h1 != h2 {
		t.Fatalf("host must be excluded from key:\n h1=%q\n h2=%q", h1, h2)
	}
}

func TestMatchKey_HeaderAllowlist(t *testing.T) {
	body := []byte(`{"a":1}`)
	cfg := Config{HeaderAllowlist: []string{"X-Custom"}}
	with := keyForBytes(t, "POST", "https://api/x", "application/json", body, cfg, func(r *http.Request) {
		r.Header.Set("X-Custom", "v1")
	})
	other := keyForBytes(t, "POST", "https://api/x", "application/json", body, cfg, func(r *http.Request) {
		r.Header.Set("X-Custom", "v2")
	})
	if with == other {
		t.Fatal("allowlisted header values must contribute to the key")
	}
}

func TestMatchKey_MultipartExcludesBoundary(t *testing.T) {
	build := func(boundary string) []byte {
		var b strings.Builder
		w := func(s string) { b.WriteString(s) }
		w("--" + boundary + "\r\n")
		w("Content-Disposition: form-data; name=\"field\"\r\n")
		w("Content-Type: text/plain\r\n\r\n")
		w("hello\r\n")
		w("--" + boundary + "--\r\n")
		return []byte(b.String())
	}
	mk := func(boundary string) string {
		body := build(boundary)
		req := httptest.NewRequest("POST", "https://api/upload", bytes.NewReader(body))
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		in, err := FromRequest(req, body)
		if err != nil {
			t.Fatal(err)
		}
		k, err := Key(in, Config{})
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	if mk("AAAAAA") != mk("ZZZZZZ") {
		t.Fatal("multipart key must exclude the random boundary token")
	}
}

func TestMatchKey_RawBodyFallback(t *testing.T) {
	// Non-JSON declared as JSON falls back to raw digest, and distinct content differs.
	a := keyForBytes(t, "POST", "https://api/x", "application/json", []byte("not json {"), Config{}, nil)
	b := keyForBytes(t, "POST", "https://api/x", "application/json", []byte("also not } json"), Config{}, nil)
	if a == b {
		t.Fatal("distinct malformed bodies must not collide")
	}
}

func TestMatchKey_OctetStream(t *testing.T) {
	a := keyForBytes(t, "POST", "https://api/x", "application/octet-stream", []byte{0x00, 0x01}, Config{}, nil)
	b := keyForBytes(t, "POST", "https://api/x", "application/octet-stream", []byte{0x00, 0x02}, Config{}, nil)
	if a == b {
		t.Fatal("distinct binary bodies must differ")
	}
}

func TestMatcher_BodyTransformDropsField(t *testing.T) {
	// Strip a volatile "nonce" field via BodyTransform so two requests match.
	strip := func(_ string, body []byte, _ string) []byte {
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return body
		}
		delete(m, "nonce")
		out, _ := json.Marshal(m)
		return out
	}
	cfg := Config{BodyTransform: strip}
	a := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"q":"hi","nonce":"AAA"}`), cfg, nil)
	b := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"q":"hi","nonce":"ZZZ"}`), cfg, nil)
	if a != b {
		t.Fatalf("BodyTransform should drop nonce so keys match:\n a=%q\n b=%q", a, b)
	}
	// Without the transform the same two bodies must differ (proves it's load-bearing).
	c := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"q":"hi","nonce":"AAA"}`), Config{}, nil)
	d := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"q":"hi","nonce":"ZZZ"}`), Config{}, nil)
	if c == d {
		t.Fatal("control: differing nonce should differ without transform")
	}
}

func TestMatcher_KeyFuncOverride(t *testing.T) {
	cfg := Config{KeyFunc: func(in Input) (string, error) { return "FIXED:" + in.Method, nil }}
	a := keyForBytes(t, "POST", "https://api/x", "application/json", []byte(`{"a":1}`), cfg, nil)
	b := keyForBytes(t, "POST", "https://api/y", "application/json", []byte(`{"b":2}`), cfg, nil)
	if a != b || a != "FIXED:POST" {
		t.Fatalf("KeyFunc override should fully control the key: a=%q b=%q", a, b)
	}
}

func TestMatcher_DefaultUnchanged(t *testing.T) {
	// A zero-value Config must produce the documented key shape (no hooks applied).
	k := keyForBytes(t, "POST", "https://api/v1/messages", "application/json", []byte(`{"b":1,"a":2}`), Config{}, nil)
	want := "POST\n/v1/messages\njson:" // prefix; digest follows
	if len(k) < len(want) || k[:len(want)] != want {
		t.Fatalf("default key shape changed: %q", k)
	}
}

func TestMatchKey_PathTransform(t *testing.T) {
	cfg := Config{PathTransform: func(_, p string) string {
		// collapse a tenant id segment
		return regexp.MustCompile(`/t/[^/]+/`).ReplaceAllString(p, "/t/{id}/")
	}}
	a := keyForBytes(t, "POST", "https://api/t/alice/messages", "application/json", []byte(`{"a":1}`), cfg, nil)
	b := keyForBytes(t, "POST", "https://api/t/bob/messages", "application/json", []byte(`{"a":1}`), cfg, nil)
	if a != b {
		t.Fatalf("PathTransform should collapse the tenant segment:\n a=%q\n b=%q", a, b)
	}
}

func TestAzureConfig_DeploymentAndQueryAndHostInert(t *testing.T) {
	cfg := AzureConfig()
	mk := func(url string) string {
		return keyForBytes(t, "POST", url, "application/json", []byte(`{"model":"x"}`), cfg, nil)
	}
	// Different deployment name, different api-version query, different host —
	// all must collapse to the same key.
	k1 := mk("https://acme-east.openai.azure.com/openai/deployments/gpt4o-prod/chat/completions?api-version=2024-02-01")
	k2 := mk("https://other.openai.azure.com/openai/deployments/gpt4o-staging/chat/completions?api-version=2025-01-01")
	if k1 != k2 {
		t.Fatalf("Azure deployment/query/host must be inert in the key:\n k1=%q\n k2=%q", k1, k2)
	}
	// A different ENDPOINT path must still differ.
	k3 := mk("https://acme.openai.azure.com/openai/deployments/gpt4o/embeddings?api-version=2024-02-01")
	if k1 == k3 {
		t.Fatal("different Azure endpoint paths must produce different keys")
	}
}
