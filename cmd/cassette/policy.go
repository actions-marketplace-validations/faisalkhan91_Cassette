package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	analysis "github.com/faisalkhan91/cassette/internal/analysis"
)

// cmdPolicy enforces a declarative cassette.policy.yaml against a recording or corpus
// — one CI gate unifying the secrets / egress / exfil / budget / coverage checks.
// Exits nonzero on any violation. Read-only, offline.
func cmdPolicy(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "init" {
		return policyInit(args[1:], stdout, stderr)
	}
	in, ok := parseOrUsage(newFlags().boolFlag("json").valFlag("policy"), args, policyUsageText, stderr)
	if !ok {
		return exitUsage
	}
	src := in.arg(0)
	if src == "" || in.nargs() > 1 {
		return policyUsage(stderr)
	}
	policyPath := in.str("policy")
	if policyPath == "" {
		policyPath = "cassette.policy.yaml"
	}
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: read policy: %v\n", err)
		return exitFail
	}
	var policy analysis.Policy
	if err := yaml.Unmarshal(raw, &policy); err != nil {
		fmt.Fprintf(stderr, "cassette: parse %s: %v\n", policyPath, err)
		return exitFail
	}
	if !policy.Enforces() {
		fmt.Fprintf(stderr, "cassette: %s declares no rules (secrets/exfil/egress/budget/coverage)\n", policyPath)
		return exitUsage
	}

	paths, err := lintTargets(src)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}
	files, err := loadAll(paths)
	if err != nil {
		fmt.Fprintf(stderr, "cassette: %v\n", err)
		return exitFail
	}

	res := analysis.CheckPolicy(policy, paths, files)

	if in.boolv("json") {
		b, _ := json.Marshal(res)
		fmt.Fprintln(stdout, string(b))
		if !res.OK() {
			return exitFail
		}
		return exitOK
	}

	st := newStyle(stdout)
	for _, c := range res.Checks {
		mark := st.check()
		if !c.OK {
			mark = st.cross()
		}
		fmt.Fprintf(stdout, "%s %-9s %s\n", mark, c.Rule, c.Detail)
	}
	if !res.OK() {
		fmt.Fprintf(stdout, "\n%s %d of %d policy rule(s) failed\n", st.cross(), res.Failed, len(res.Checks))
		return exitFail
	}
	fmt.Fprintf(stdout, "\n%s all %d policy rule(s) passed\n", st.check(), len(res.Checks))
	return exitOK
}

const policyUsageText = "usage: cassette policy <cassette.yaml|dir> [--policy cassette.policy.yaml] [--json]\n   or: cassette policy init [-o cassette.policy.yaml] [--force]"

func policyUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, policyUsageText)
	return exitUsage
}

// starterPolicy is the safe-by-default scaffold: secrets, exfil, and the common PII
// classes are enforced out of the box; the corpus-specific knobs are commented.
const starterPolicy = `# cassette.policy.yaml — declarative guardrail enforced by ` + "`cassette policy <dir>`" + `.
# Every section is optional; an absent section is not enforced. Full schema:
# docs/spec/cassette-format.md and docs/cli.md.

secrets: forbid                 # fail if any known secret pattern appears
exfil: forbid                   # fail if model/tool output is carried back outbound

egress:
  forbid: [email, creditcard, ssn, phone]   # data classes that must not appear outbound

# budget:
#   max_tokens: 200000          # total input+output token ceiling

# coverage:
#   require_tools: [get_weather]  # the corpus must exercise these tools (HTTP tool calls)
`

// policyInit scaffolds a starter cassette.policy.yaml, refusing to clobber an
// existing file unless --force.
func policyInit(args []string, stdout, stderr io.Writer) int {
	in, ok := parseOrUsage(newFlags().boolFlag("force").valFlag("out").alias("o", "out"), args, policyUsageText, stderr)
	if !ok {
		return exitUsage
	}
	if in.nargs() > 0 {
		return policyUsage(stderr)
	}
	out := in.str("out")
	if out == "" {
		out = "cassette.policy.yaml"
	}
	if _, err := os.Stat(out); err == nil && !in.boolv("force") {
		fmt.Fprintf(stderr, "cassette: %s already exists (pass --force to overwrite)\n", out)
		return exitFail
	}
	if err := os.WriteFile(out, []byte(starterPolicy), 0o644); err != nil {
		fmt.Fprintf(stderr, "cassette: write: %v\n", err)
		return exitFail
	}
	st := newStyle(stdout)
	fmt.Fprintf(stdout, "%s wrote %s — edit it, then gate CI with `cassette policy <dir>`\n", st.check(), out)
	return exitOK
}
