package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const auditCassette = `schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","messages":[{"role":"user","content":"reach me at alice@example.com"}]}'
    response:
      status: 200
      headers:
        Content-Type: [application/json]
      body: '{"content":[{"type":"text","text":"ok"}]}'
`

func TestCLI_Audit_BundleAndGate(t *testing.T) {
	cass := writeTemp(t, auditCassette)

	// Default: emit the bundle, exit 0 (pure evidence generation).
	code, stdout, stderr := runArgs("audit", cass)
	if code != exitOK {
		t.Fatalf("audit: code=%d stderr=%s", code, stderr)
	}
	var bundle map[string]any
	if err := json.Unmarshal([]byte(stdout), &bundle); err != nil {
		t.Fatalf("audit stdout not valid JSON: %v\n%s", err, stdout)
	}
	for _, k := range []string{"manifest", "coverage", "egress", "cassettes"} {
		if _, ok := bundle[k]; !ok {
			t.Errorf("bundle missing %q section: %v", k, stdout)
		}
	}
	if !strings.Contains(stdout, `"email"`) {
		t.Fatalf("expected an email egress finding in the bundle:\n%s", stdout)
	}

	// --fail-on the matching class -> nonzero (governance gate).
	if code, _, _ := runArgs("audit", cass, "--fail-on", "email"); code != exitFail {
		t.Fatalf("--fail-on email should fail on the email finding: code=%d", code)
	}
	// --fail-on a non-present class -> still exits 0.
	if code, _, _ := runArgs("audit", cass, "--fail-on", "ssn"); code != exitOK {
		t.Fatalf("--fail-on ssn (absent) should pass: code=%d", code)
	}
}

func TestCLI_Audit_Usage(t *testing.T) {
	if code, _, _ := runArgs("audit"); code != exitUsage {
		t.Fatalf("audit no args: code=%d want exitUsage", code)
	}
}
