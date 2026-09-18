package analysis

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

func ruleOK(res PolicyResult, rule string) (bool, bool) {
	for _, c := range res.Checks {
		if c.Rule == rule {
			return c.OK, true
		}
	}
	return false, false
}

func TestCheckPolicy_Rules(t *testing.T) {
	corpus := fileOf(
		httpIt("/v1/messages",
			`{"model":"m","messages":[{"role":"user","content":"mail alice@example.com"}]}`,
			`{"usage":{"input_tokens":100,"output_tokens":50}}`, 200, nil),
		mcpList(addSchema),
		mcpCall("add", `{"a":1}`, `{"content":[]}`),
	)
	paths := []string{"corpus.yaml"}
	files := []*wirefmt.File{corpus}
	check := func(p Policy) PolicyResult { return CheckPolicy(p, paths, files) }

	// secrets: an email is not a secret pattern, so the rule passes.
	if ok, found := ruleOK(check(Policy{Secrets: "forbid"}), "secrets"); !found || !ok {
		t.Errorf("secrets rule: found=%v ok=%v (email is not a secret)", found, ok)
	}

	// egress: forbidding email fails on the outbound address.
	var pEgress Policy
	pEgress.Egress.Forbid = []string{"email"}
	if ok, found := ruleOK(check(pEgress), "egress"); !found || ok {
		t.Errorf("egress rule should fail on outbound email: found=%v ok=%v", found, ok)
	}

	// budget: 150 total tokens — passes under 1000, fails under 100.
	var pBudgetOK Policy
	pBudgetOK.Budget.MaxTokens = 1000
	if ok, _ := ruleOK(check(pBudgetOK), "budget"); !ok {
		t.Error("budget 1000 should pass (150 tokens used)")
	}
	var pBudgetBad Policy
	pBudgetBad.Budget.MaxTokens = 100
	if ok, _ := ruleOK(check(pBudgetBad), "budget"); ok {
		t.Error("budget 100 should fail (150 tokens used)")
	}

	// coverage: require_tools matches HTTP tool calls (analysis.Coverage is HTTP-only);
	// this corpus has none, so requiring any tool fails.
	var pCovBad Policy
	pCovBad.Coverage.RequireTools = []string{"subtract"}
	if ok, _ := ruleOK(check(pCovBad), "coverage"); ok {
		t.Error("coverage require subtract should fail (no HTTP tool calls in corpus)")
	}

	// An empty policy enforces nothing.
	if (Policy{}).Enforces() {
		t.Error("empty policy should not enforce")
	}
}

// BuildAudit composes manifest + coverage + egress + taint over a corpus.
func TestBuildAudit_Composes(t *testing.T) {
	corpus := fileOf(httpIt("/v1/messages",
		`{"model":"m","messages":[{"role":"user","content":"alice@example.com"}]}`,
		`{"usage":{"input_tokens":1,"output_tokens":1}}`, 200, nil))
	b := BuildAudit([]string{"c.yaml"}, []*wirefmt.File{corpus})
	if b.Manifest.Combined == "" {
		t.Error("audit bundle missing attestation manifest digest")
	}
	if b.Coverage.Turns == 0 {
		t.Error("audit bundle missing coverage")
	}
	foundEmail := false
	for _, e := range b.Egress {
		if e.Kind == "email" {
			foundEmail = true
		}
	}
	if !foundEmail {
		t.Errorf("audit bundle should report the outbound email: %+v", b.Egress)
	}
	// Each finding carries its origin field (per-field scan, no \n-straddle).
	for _, e := range b.Egress {
		if e.Field == "" {
			t.Errorf("egress finding missing Field origin: %+v", e)
		}
	}
}

// Empty finding sets must serialize as [] (not null) so consumers can rely on the
// array fields always being present.
func TestBuildAudit_EmptyArraysAreStable(t *testing.T) {
	clean := fileOf(httpIt("/v1/messages", `{"model":"m"}`, `{"content":[]}`, 200, nil))
	b := BuildAudit([]string{"c.yaml"}, []*wirefmt.File{clean})
	if b.Egress == nil || b.Taint == nil {
		t.Fatalf("empty egress/taint must be non-nil slices: egress=%v taint=%v", b.Egress, b.Taint)
	}
	js, _ := json.Marshal(b)
	if !strings.Contains(string(js), `"egress":[]`) || !strings.Contains(string(js), `"taint":[]`) {
		t.Fatalf("empty findings should serialize as []:\n%s", js)
	}
}
