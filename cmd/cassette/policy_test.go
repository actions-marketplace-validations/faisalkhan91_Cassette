package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditCassette (defined in audit_test.go) carries an email in an outbound request.
func TestCLI_Policy_FailAndPass(t *testing.T) {
	cass := writeTemp(t, auditCassette)

	// A policy that forbids outbound email must FAIL on this corpus.
	failPolicy := writeTemp(t, "egress:\n  forbid: [email]\n")
	code, stdout, _ := runArgs("policy", cass, "--policy", failPolicy)
	if code != exitFail {
		t.Fatalf("policy forbidding email should fail: code=%d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "egress") {
		t.Fatalf("expected the egress rule in output:\n%s", stdout)
	}

	// A policy with no-secrets + a generous token budget passes (an email is not a
	// secret, and the fixture reports no usage).
	passPolicy := writeTemp(t, "secrets: forbid\nbudget:\n  max_tokens: 1000000\n")
	if code, out, _ := runArgs("policy", cass, "--policy", passPolicy); code != exitOK {
		t.Fatalf("benign policy should pass: code=%d\n%s", code, out)
	}
}

func TestCLI_Policy_EmptyPolicyIsUsageError(t *testing.T) {
	cass := writeTemp(t, auditCassette)
	empty := writeTemp(t, "# no rules\n")
	if code, _, _ := runArgs("policy", cass, "--policy", empty); code != exitUsage {
		t.Fatalf("a policy with no rules should be a usage error: code=%d", code)
	}
}

func TestCLI_Policy_Usage(t *testing.T) {
	if code, _, _ := runArgs("policy"); code != exitUsage {
		t.Fatalf("policy no args: code=%d want exitUsage", code)
	}
}

func TestCLI_Policy_Init(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cassette.policy.yaml")

	if code, _, stderr := runArgs("policy", "init", "-o", out); code != exitOK {
		t.Fatalf("policy init: code=%d stderr=%s", code, stderr)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("scaffold not written: %v", err)
	}
	if !strings.Contains(string(body), "secrets: forbid") {
		t.Fatalf("scaffold missing safe defaults:\n%s", body)
	}
	// Refuses to clobber without --force, succeeds with it.
	if code, _, _ := runArgs("policy", "init", "-o", out); code != exitFail {
		t.Fatalf("policy init should refuse to overwrite without --force: code=%d", code)
	}
	if code, _, _ := runArgs("policy", "init", "-o", out, "--force"); code != exitOK {
		t.Fatalf("policy init --force should overwrite: code=%d", code)
	}

	// The scaffolded policy is valid and enforcing: it forbids outbound email, which
	// auditCassette contains, so the gate fires.
	cass := writeTemp(t, auditCassette)
	if code, sout, _ := runArgs("policy", cass, "--policy", out); code != exitFail || !strings.Contains(sout, "egress") {
		t.Fatalf("scaffolded policy should enforce egress on the email fixture: code=%d\n%s", code, sout)
	}
}
