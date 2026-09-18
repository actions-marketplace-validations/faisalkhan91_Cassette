package analysis

import (
	"fmt"
	"strings"

	"github.com/faisalkhan91/cassette/internal/scrub"
	"github.com/faisalkhan91/cassette/internal/wirefmt"
)

// Policy is the declarative guardrail a team commits as cassette.policy.yaml: one
// file that unifies the otherwise-scattered verify / egress-audit / taint / cost /
// coverage gates so CI (and save-time tooling) enforces a single source of truth.
// Every section is optional; an absent section is not enforced.
//
//	secrets: forbid                 # no known secret pattern may appear
//	exfil: forbid                   # no model/tool output may be carried back outbound
//	egress:
//	  forbid: [email, creditcard]   # these data classes may not appear in outbound requests
//	budget:
//	  max_tokens: 200000            # total input+output tokens must not exceed this
//	coverage:
//	  require_tools: [get_weather]  # the corpus must exercise these tools
type Policy struct {
	Secrets string `yaml:"secrets"`
	Exfil   string `yaml:"exfil"`
	Egress  struct {
		Forbid []string `yaml:"forbid"`
	} `yaml:"egress"`
	Budget struct {
		MaxTokens int `yaml:"max_tokens"`
	} `yaml:"budget"`
	Coverage struct {
		RequireTools []string `yaml:"require_tools"`
	} `yaml:"coverage"`
}

// Enforces reports whether the policy declares at least one rule.
func (p Policy) Enforces() bool {
	return p.Secrets == "forbid" || p.Exfil == "forbid" || len(p.Egress.Forbid) > 0 ||
		p.Budget.MaxTokens > 0 || len(p.Coverage.RequireTools) > 0
}

// PolicyCheck is one rule's verdict.
type PolicyCheck struct {
	Rule   string `json:"rule"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// PolicyResult is the outcome of evaluating a Policy against a corpus.
type PolicyResult struct {
	Checks []PolicyCheck `json:"checks"`
	Failed int           `json:"failed"`
}

// OK reports whether every enforced rule passed.
func (r PolicyResult) OK() bool { return r.Failed == 0 }

func (r *PolicyResult) add(rule string, ok bool, detail string) {
	if !ok {
		r.Failed++
	}
	r.Checks = append(r.Checks, PolicyCheck{Rule: rule, OK: ok, Detail: detail})
}

// CheckPolicy evaluates p against the corpus (paths and files aligned) and returns
// one verdict per enforced rule. Fully offline and deterministic.
func CheckPolicy(p Policy, paths []string, files []*wirefmt.File) PolicyResult {
	merged, _ := Merge(files...)
	var res PolicyResult

	if p.Secrets == "forbid" {
		var hits []string
		for i, it := range merged.Interactions {
			for _, kind := range secretsIn(it) {
				hits = append(hits, fmt.Sprintf("turn %d: %s", i, kind))
			}
		}
		res.add("secrets", len(hits) == 0, summarize(hits, "no secret patterns"))
	}

	// egress / exfil / coverage all read the composed audit bundle; build it once.
	if len(p.Egress.Forbid) > 0 || p.Exfil == "forbid" || len(p.Coverage.RequireTools) > 0 {
		bundle := BuildAudit(paths, files)
		if len(p.Egress.Forbid) > 0 {
			forbid := toSet(p.Egress.Forbid)
			var v []string
			for _, e := range bundle.Egress {
				if forbid[e.Kind] {
					v = append(v, fmt.Sprintf("turn %d: %s", e.Turn, e.Kind))
				}
			}
			res.add("egress", len(v) == 0, summarize(v, "no forbidden data classes outbound"))
		}
		if p.Exfil == "forbid" {
			var v []string
			for _, t := range bundle.Exfils() {
				v = append(v, fmt.Sprintf("%s turn %d→%d", t.Kind, t.SourceTurn, t.SinkTurn))
			}
			res.add("exfil", len(v) == 0, summarize(v, "no model/tool output carried outbound"))
		}
		if len(p.Coverage.RequireTools) > 0 {
			var missing []string
			for _, tool := range p.Coverage.RequireTools {
				if bundle.Coverage.Tools[tool] == 0 {
					missing = append(missing, tool)
				}
			}
			res.add("coverage", len(missing) == 0, summarize(prefix("missing tool ", missing), "all required tools exercised"))
		}
	}

	if p.Budget.MaxTokens > 0 {
		total := 0
		for _, it := range merged.Interactions {
			total += DecodeUsage(it).Total()
		}
		res.add("budget", total <= p.Budget.MaxTokens,
			fmt.Sprintf("%d / %d total tokens", total, p.Budget.MaxTokens))
	}
	return res
}

// secretsIn scans an interaction's request/response bodies and header values for known
// secret patterns, returning the matched kinds.
func secretsIn(it *wirefmt.Interaction) []string {
	var out []string
	out = append(out, scrub.SecretScan(it.Request.Body.Bytes())...)
	out = append(out, scrub.SecretScan(it.Response.Body.Bytes())...)
	for _, hf := range it.Request.Headers {
		for _, v := range hf.Values {
			out = append(out, scrub.SecretScan([]byte(v))...)
		}
	}
	return out
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func prefix(p string, ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = p + s
	}
	return out
}

func summarize(violations []string, okMsg string) string {
	if len(violations) == 0 {
		return okMsg
	}
	return strings.Join(violations, "; ")
}
