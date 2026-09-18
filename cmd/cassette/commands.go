package main

import (
	"io"
	"strings"
)

// runFunc is the signature every subcommand implements.
type runFunc = func(args []string, stdout, stderr io.Writer) int

// Command is one CLI subcommand. The registry below is the SINGLE source of
// truth: dispatch (run), the top-level usage list, and per-command --help all
// derive from it; the one-line summary is derived from Help’s title line.
type Command struct {
	Name string
	Run  runFunc
	Help string
}

var commands = []*Command{
	{
		Name: "up",
		Run:  cmdUp,
		Help: `cassette up — the zero-config front door: point your provider at it and go

Usage:
  cassette up [dir] [--upstream URL] [--addr 127.0.0.1:PORT] [--pace] [--volatile a.b,c]

  --pace       timing-faithful mode: capture per-frame SSE inter-arrival deltas while
               recording, and re-emit them with those delays on replay (off by
               default for fast CI).
  --volatile a.b,c   drop these volatile request JSON paths from the match key (e.g. a
               per-session id), identically at record and replay; persisted into the cassette.

The "install once, point at your provider, it just works" command. With no flags it:
  - picks a free loopback port and prints the base URL to paste;
  - records if the directory (default ./testdata/cassettes) is empty, else replays
    OFFLINE with zero egress — the same record-if-absent-else-replay rule as ModeAuto;
  - auto-detects the upstream from the first request's path when recording
    (Anthropic / Gemini / OpenAI Responses / Ollama); pass --upstream for the
    OpenAI-compatible /chat/completions path or any custom host;
  - diagnoses a replay miss (nearest turn + the field that broke the match key + a
    one-line fix) instead of a bare 404.

Point your SDK's base URL at the printed address, run your app once to record, then
re-run forever offline. To add turns to an existing corpus, use 'cassette proxy
--mode record'; 'up' never dials once a corpus exists.

Example:
  cassette up                       # record (first run) or replay (offline) ./testdata/cassettes
  cassette up ./fixtures --upstream https://my-vllm.internal   # custom/openai-compatible host
`,
	},
	{
		Name: "open",
		Run:  cmdOpen,
		Help: `cassette open — render the offline dashboard, open it, and print the transcript

Usage:
  cassette open <cassette.yaml|dir> [-o report.html] [--no-open]

The unified "look at it" home: a shortcut over 'dashboard' + 'doc' — renders the
self-contained offline HTML dashboard (coverage matrix, token totals, refusal
verdicts, per-cassette transcripts), opens it in your browser (best effort), and
prints the transcript to stdout. Read-only; no network.
See also: doc (transcript), dashboard (HTML report), inspect (structure/sizes).

  -o report.html   persist the HTML (default: a temp file)
  --no-open        write/print only; don't launch a browser (headless/CI)

Example:
  cassette open session.yaml                 # dashboard + transcript
  cassette open ./testdata/cassettes -o report.html --no-open
`,
	},
	{
		Name: "inspect",
		Run:  cmdInspect,
		Help: `cassette inspect — pretty-print a recording's structure

Usage:
  cassette inspect <cassette.yaml>

Prints the schema version, interaction count (http/mcp), per-interaction method,
URL/tool, status, body sizes, and whether each response streamed. Exits nonzero
if the file is malformed. Read-only.

Example:
  cassette inspect testdata/cassettes/anthropic_text_helper.yaml
`,
	},
	{
		Name: "scrub",
		Run:  cmdScrub,
		Help: `cassette scrub — redact secrets and stamp volatile fields in place

Usage:
  cassette scrub <cassette.yaml> [--update]

Re-applies the provider-aware secret scrubbers (API keys, auth headers, etc.) and
volatile-field stamping, then rewrites the file. Idempotent: running it twice
produces no further changes. --update is accepted for parity with the test
workflow (scrub always rewrites in place).

Example:
  cassette scrub session.yaml
`,
	},
	{
		Name: "verify",
		Run:  cmdVerify,
		Help: `cassette verify — check a recording is well-formed and secret-free

Usage:
  cassette verify <cassette.yaml>

Exits 0 only if the cassette parses cleanly and contains no known secret patterns.
The fast, zero-config subset of 'lint' for a single file — intended as a CI /
pre-commit gate. Read-only.

This checks the file is SAFE, not that it will REPLAY. To prove a request still
resolves to its recorded response with zero dials, use 'cassette conformance'.

Example:
  cassette verify session.yaml && echo ok
`,
	},
	{
		Name: "prune",
		Run:  cmdPrune,
		Help: `cassette prune — drop interactions by index

Usage:
  cassette prune <cassette.yaml> --index N[,M,...]

Removes the listed interactions (0-based), re-scrubs, re-keys, and re-saves.
Use 'cassette inspect' first to find the indices.

Example:
  cassette prune session.yaml --index 2,5
`,
	},
	{
		Name: "rekey",
		Run:  cmdRekey,
		Help: `cassette rekey — recompute match keys in place

Usage:
  cassette rekey <cassette.yaml> [--force]

Recomputes the request match keys after a hand edit to a recorded request body or
the matching config. Idempotent. Run this if a replay stops matching after you
edited the cassette by hand.

Refuses to rekey an interaction whose request body was scrubbed and whose new key
would differ from the stored one — re-deriving from redacted bytes would overwrite
the replay-authoritative key. Mark the secret field volatile (cassette redact-field
--path …) so it is excluded from keying, or pass --force if the body is intact.

Example:
  cassette rekey session.yaml
`,
	},
	{
		Name: "redact-field",
		Run:  cmdRedactField,
		Help: `cassette redact-field — redact a JSON field in stored bodies

Usage:
  cassette redact-field <cassette.yaml> --path a.b.c

Replaces the value at the given JSON path (dotted; array indices supported, e.g.
choices.0.message.content) in stored request/response bodies with a redaction
marker, then re-keys so replay still matches. Idempotent.

Example:
  cassette redact-field session.yaml --path messages.0.content
`,
	},
	{
		Name: "serve",
		Run:  cmdServe,
		Help: `cassette serve — serve a recording as a local offline LLM endpoint

Usage:
  cassette serve <cassette.yaml|dir> [--addr :8080] [--pace] [--log-misses] [--on-miss fail] [--miss-manifest path] [--route-header NAME] [--volatile a.b,c]

Serves the cassette (or a whole directory, merged) over real HTTP, speaking each
provider's wire protocol and routing by request path (/v1/messages,
/chat/completions, /responses, :streamGenerateContent, /api/chat). Holds no HTTP
client: a request matching no recorded interaction returns 404 cassette_miss —
never a passthrough. Any client/language can point its base URL here for
deterministic, key-less, zero-network replay.

Flags:
  --addr ADDR          listen address (default :8080; 127.0.0.1:0 for an ephemeral port)
  --pace               re-emit SSE frames with their recorded inter-frame delays
  --log-misses         log misses to stderr (still returns 404; does NOT change the
                       exit code — use --on-miss fail to gate). --strict is a
                       deprecated alias.
  --on-miss fail       CI gate: record misses to a manifest and EXIT NONZERO on shutdown
  --miss-manifest PATH where to write the miss manifest (default cassette-misses.json)
  --route-header NAME  per-test isolation: route by this header to <dir>/<value>.yaml

Examples:
  cassette serve session.yaml --addr :9000
  cassette serve testdata/ --route-header X-Cassette-Test --on-miss fail   # hermetic CI server
`,
	},
	{
		Name: "mutate",
		Run:  cmdMutate,
		Help: `cassette mutate — derive a hostile-but-plausible stream variant

Usage:
  cassette mutate <cassette.yaml> --op OP [--frame N] [--seed N] [--out path]

Produces a corrupted SSE variant for resilience/chaos testing of your client,
without changing the original recording. Deterministic per --seed.

Ops:
  truncate         cut the stream after frame N (--frame)
  drop-terminal    drop the terminal event (message_stop / [DONE])
  duplicate        duplicate a frame
  reorder          reorder content deltas
  corrupt-tool     corrupt tool-call arguments
  inject-error     splice in an error event
  drop-multibyte   drop the last byte of a multibyte rune

Example:
  cassette mutate session.yaml --op truncate --frame 3 --out truncated.yaml
`,
	},
	{
		Name: "assert",
		Run:  cmdAssert,
		Help: `cassette assert — check behavioral invariants over a transcript

Usage:
  cassette assert <cassette.yaml> [--no-tool NAME]... [--no-duplicate-tools] [--finishes-clean]

Exits nonzero if the recorded transcript violates an invariant. Useful as a
guardrail on golden recordings.

Flags:
  --no-tool NAME         fail if the named tool was ever called (repeatable)
  --no-duplicate-tools   fail if the same tool+args is called twice
  --finishes-clean       fail if the conversation ends on a dangling tool call

Example:
  cassette assert session.yaml --no-tool delete_account --finishes-clean
`,
	},
	{
		Name: "doc",
		Run:  cmdDoc,
		Help: `cassette doc — render a recording as a Markdown transcript

Usage:
  cassette doc <cassette.yaml> [--check doc.md]

Prints a human-readable Markdown transcript (assistant text, tool calls, finish
reasons). With --check, compares against an existing file and exits nonzero if it
is stale — so committed docs can't drift from the recording (living docs).

Examples:
  cassette doc session.yaml > session.md
  cassette doc session.yaml --check session.md
`,
	},
	{
		Name: "bisect",
		Run:  cmdBisect,
		Help: `cassette bisect — localize where two recordings diverge

Usage:
  cassette bisect <a.yaml> <b.yaml>

Finds the first turn whose semantic transcript differs between two recordings and
attributes the changed request-input axis (model / system / tools / messages /
sampling). Useful for "which turn, and what input, changed the behavior?"

Example:
  cassette bisect before.yaml after.yaml
`,
	},
	{
		Name: "pack",
		Run:  cmdPack,
		Help: `cassette pack — build a single self-contained offline demo binary

Usage:
  cassette pack <cassette.yaml> -o <output-binary>

Produces one executable that, when run, serves an interactive steppable browser
transcript of the recording plus the serve API, fully offline. No Go toolchain is
used: the cassette is appended to a copy of the running binary and detected at
startup. Refuses to embed secrets.

Caveats: same OS/arch only (no cross-compile); on macOS the appended bytes
invalidate the code signature (it still runs locally); don't post-process the
output (strip/UPX); Windows is unsupported.

Flags:
  -o, --out PATH   output binary path (required)

Example:
  cassette pack session.yaml -o demo && ./demo
`,
	},
	{
		Name: "explain",
		Run:  cmdExplain,
		Help: `cassette explain — per-turn microscope

Usage:
  cassette explain <cassette.yaml> [--turn N] [--raw]

Shows everything the engine knows about one interaction: the request method/URL,
the persisted match key, the canonicalized request body that derives it, the
decoded semantic transcript (text / tool calls / finish reason), and the response
wire-grammar (event sequence). --raw also dumps the raw SSE frames.

Example:
  cassette explain session.yaml --turn 2
`,
	},
	{
		Name: "expect",
		Run:  cmdExpect,
		Help: `cassette expect — embed a behavioral contract in a recording

Usage:
  cassette expect <cassette.yaml> [--no-tool NAME]... [--no-duplicate-tools] [--finishes-clean] [--clear]

With flags, writes a contract into the cassette; with no flags, prints the current
one; --clear removes it. The contract is preserved across re-record, and
'cassette assert <cassette.yaml>' with no invariant flags runs it — so a recording
carries its own CI guardrail.

Examples:
  cassette expect session.yaml --no-tool delete_account --finishes-clean
  cassette assert session.yaml          # runs the embedded contract
`,
	},
	{
		Name: "orphans",
		Run:  cmdOrphans,
		Help: `cassette orphans — find dead / missing cassette fixtures

Usage:
  cassette orphans [dir]

Scans *_test.go for cassettetest.New(t, "name") calls and reconciles them against the
fixture files under testdata/cassettes/: reports fixtures no test references (dead)
and New(...) calls whose fixture file is missing. New(t, "") resolves to the
enclosing test function's name. Nonzero exit if anything is flagged.

Example:
  cassette orphans ./...
`,
	},
	{
		Name: "diff",
		Run:  cmdDiff,
		Help: `cassette diff — signal-only semantic diff of two recordings

Usage:
  cassette diff <a.yaml> <b.yaml> [--md]

Aligns the two recordings by turn and shows what BEHAVIOR changed — assistant
text (word-level), tool calls added/removed/args-changed, finish-reason flips —
plus which request-input axis moved, with volatile id/usage/SSE churn suppressed.
The first divergence is marked (root); later turns are usually cascade. --md emits
a PR-ready table. Nonzero exit on any divergence (use it as a review/CI gate).

Example:
  cassette diff before.yaml after.yaml --md
`,
	},
	{
		Name: "doctor",
		Run:  cmdDoctor,
		Help: `cassette doctor — diagnose a replay miss

Usage:
  cassette doctor <cassette.yaml> --request req.json

req.json is {"method","url","content_type","body":{…}}. doctor recomputes the
match key for that request; if nothing matches, it finds the nearest recorded
interaction (same method+path, fewest differing request axes) and names the fix
(re-record / redact-field / VolatileJSONPaths / rekey). Fully offline.

Example:
  cassette doctor session.yaml --request failed.json
`,
	},
	{
		Name: "egress-audit",
		Run:  cmdEgressAudit,
		Help: `cassette egress-audit — scan outbound requests for sensitive data

Usage:
  cassette egress-audit <cassette.yaml> [--fail-on email,creditcard,...]

Runs a typed detector bank (email, ssn, phone, jwt, aws_key, github_token,
creditcard [Luhn], high_entropy) over every recorded request body, header value,
and URL path — finding UNKNOWN sensitive data the agent emitted (beyond the known
credential shapes scrub redacts). Findings are masked. Exits nonzero on any
finding matching --fail-on (or any finding when --fail-on is omitted). Query
strings are not recorded (path-only key) and are out of scope.

Example:
  cassette egress-audit session.yaml --fail-on ssn,creditcard
`,
	},
	{
		Name: "audit",
		Run:  cmdAudit,
		Help: `cassette audit — emit a deterministic offline compliance evidence bundle

Usage:
  cassette audit <cassette.yaml|dir> [-o bundle.json] [--fail-on email,creditcard,exfil,...]

Composes the primitives a SOC2 / EU AI Act / ISO 42001 review asks for into ONE
byte-stable, offline artifact: the behavioral attestation manifest (semantic digests
+ tool set), the coverage matrix, the typed PII/data-class findings in outbound
requests, and the cross-turn taint flows. No network, no timestamp — commit it or
attach it to CI; stamp the date via the filename.

With --fail-on it doubles as a governance gate: list egress data classes and/or the
keyword "exfil" (any model/tool-output value carried back outbound) to exit nonzero.
Without --fail-on it is pure evidence generation (always exits 0). Pair with
'cassette attest --key' to sign the manifest.

Example:
  cassette audit ./testdata/cassettes -o audit-$(date +%F).json
  cassette audit session.yaml --fail-on ssn,creditcard,exfil   # CI governance gate
`,
	},
	{
		Name: "policy",
		Run:  cmdPolicy,
		Help: `cassette policy — enforce a declarative cassette.policy.yaml (one CI gate)

Usage:
  cassette policy <cassette.yaml|dir> [--policy cassette.policy.yaml] [--json]
  cassette policy init [-o cassette.policy.yaml] [--force]

'cassette policy init' scaffolds a safe-by-default starter policy (secrets, exfil,
and common PII classes enforced; budget/coverage commented). 'cassette policy <dir>'
evaluates a committed policy file against a recording or corpus, unifying the
otherwise-scattered secrets / egress / exfil / budget / coverage gates into one
exit code. Every section is optional; an absent section is not enforced:

  secrets: forbid                 # no known secret pattern may appear
  exfil: forbid                   # no model/tool output may be carried back outbound
  egress:
    forbid: [email, creditcard]   # these data classes may not appear in outbound requests
  budget:
    max_tokens: 200000            # total input+output tokens must not exceed this
  coverage:
    require_tools: [get_weather]  # the corpus must exercise these tools

Exits nonzero if any enforced rule fails. Read-only, offline. Defaults to
cassette.policy.yaml in the working directory.

Example:
  cassette policy ./testdata/cassettes
  cassette policy session.yaml --policy ci/strict.policy.yaml --json
`,
	},
	{
		Name: "cost",
		Run:  cmdCost,
		Help: `cassette cost — offline token/cost report and budget gate

Usage:
  cassette cost <cassette.yaml> [--prices p.json] [--budget tokens=N|usd=X] [--by model]

Reads the byte-exact token usage recorded in each response and reports total
input/output (and cache) tokens — always offline, no provider call. With --prices
(a JSON map model→{"input","output"} per-million-token rates) it also reports USD.
--budget tokens=N or usd=X exits nonzero when exceeded (a deterministic cost
regression gate). Turns with no recorded usage (e.g. an OpenAI stream without
include_usage) are reported as "unknown", never counted as zero.

Examples:
  cassette cost session.yaml --by model
  cassette cost session.yaml --prices prices.json --budget usd=0.05
`,
	},
	{
		Name: "dataset",
		Run:  cmdDataset,
		Help: `cassette dataset — index recordings into a queryable eval dataset

Usage:
  cassette dataset [dir] [--where field=value]... [--format jsonl|csv|table]

Walks a directory of cassettes and projects every decodable http interaction into
a flat row (cassette, index, model, system_digest, user_text, tools_offered,
finish_reason, tool_calls). The rows ARE the byte-exact replay fixtures, with
semantic fields attached. --where filters by flat equality on model / finish /
cassette / tool. Default format is jsonl.

Examples:
  cassette dataset ./testdata --where model=claude-opus-4-6 --format table
  cassette dataset . --where tool=get_weather --format jsonl > weather.jsonl
`,
	},
	{
		Name: "gitconfig",
		Run:  cmdGitconfig,
		Help: `cassette gitconfig — install a local git textconv diff driver

Usage:
  cassette gitconfig --install [dir]

Configures git so 'git diff', 'git log -p', and 'git show' render cassettes as the
human 'cassette doc' transcript instead of raw YAML SSE. Writes the
diff.cassette.textconv git config and a testdata/cassettes/*.yaml diff=cassette
line in .gitattributes. LOCAL review only: GitHub does not run textconv
server-side, and each reviewer must run --install and have 'cassette' on PATH.

Example:
  cassette gitconfig --install
`,
	},
	{
		Name: "eval",
		Run:  cmdEval,
		Help: `cassette eval — run an assertion suite (incl. an offline LLM judge)

Usage:
  cassette eval <subject.yaml> --suite suite.yaml [--judge judge.yaml]

The suite YAML has a 'deterministic:' block (no_tools / finishes_clean /
no_duplicate_tools — reusing semequal invariants) and an optional 'judge:' block
(rubric, model, cassette, and EITHER pass_marker OR min_score). With min_score, the
judge reply is decoded and a score extracted (score: N, or an N/100 ratio) and the
assertion passes iff score >= min_score; otherwise the substring pass_marker is
used. The judge renders the subject into a DETERMINISTIC prompt and replays it
against a JUDGE CASSETTE — so the judge model is itself a fixture and the entire
eval is zero-dial and bit-reproducible. Nonzero exit on any failed assertion.

Example:
  cassette eval subject.yaml --suite suite.yaml --judge judge.yaml
`,
	},
	{
		Name: "attest",
		Run:  cmdAttest,
		Help: `cassette attest — sign a recording's behavioral manifest

Usage:
  cassette attest gen-key <keyfile>
  cassette attest <cassette.yaml> --key <keyfile> -o <out.att>

Binds the recording's BEHAVIOR (combined + per-turn semantic digests, wire
shapes, tool set, embedded contract) into an ed25519-signed manifest — a
behavioral SBOM. Verify with 'cassette verify-attest'. The raw byte hash is
advisory, so a benign re-record (same behavior) still verifies.

Example:
  cassette attest gen-key ed25519.key
  cassette attest session.yaml --key ed25519.key -o session.att
`,
	},
	{
		Name: "verify-attest",
		Run:  cmdVerifyAttest,
		Help: `cassette verify-attest — verify a behavioral attestation

Usage:
  cassette verify-attest <cassette.yaml> <out.att>

Checks the ed25519 signature AND that the cassette still has the attested
behavior (semantic digests match). A benign re-record passes; a behavior change
fails. Exits nonzero on either failure.

Example:
  cassette verify-attest session.yaml session.att
`,
	},
	{
		Name: "refusal",
		Run:  cmdRefusal,
		Help: `cassette refusal — classify turns refused/complied/unknown

Usage:
  cassette refusal <cassette.yaml>

A deterministic, offline content classifier over the decoded transcript: a turn
that calls a tool is "complied"; refusal phrases mark "refused"; empty is
"unknown". Prints a per-turn verdict and the final verdict.

Example:
  cassette refusal session.yaml
`,
	},
	{
		Name: "redteam",
		Run:  cmdRedteam,
		Help: `cassette redteam — diff refusal verdicts across recordings

Usage:
  cassette redteam [dir] [--expect manifest.yaml]

Classifies the final verdict of every cassette under a directory; with --expect
(a YAML map of cassette-basename → expected verdict) it fails when a verdict
differs — catching when a model upgrade silently flips refuse↔comply. HONEST:
frozen replay re-classifies identically, so this is a re-record-then-diff gate,
not a live probe. Bring your own cassettes (no harmful-prompt corpus is bundled).

Example:
  cassette redteam ./redteam-cassettes --expect expected.yaml
`,
	},
	{
		Name: "watch",
		Run:  cmdWatch,
		Help: `cassette watch — live provider-drift probe (DIALS THE NETWORK)

Usage:
  cassette watch <golden.yaml> --base-url URL [--policy prose:0.15] [--json]

The deliberate inverse of offline replay: re-issues each recorded request against
a LIVE provider and compares the response to the golden recording under a drift
Policy (e.g. tool calls exact, prose within a budget). It makes REAL network
calls (opt-in via --base-url) and never writes the cassette. Nonzero exit on drift.

Example:
  cassette watch golden.yaml --base-url https://api.anthropic.com --policy prose:0.2
`,
	},
	{
		Name: "graft",
		Run:  cmdGraft,
		Help: `cassette graft — rewrite one turn's output (counterfactual)

Usage:
  cassette graft <in.yaml> --turn N [--set text=…|append=…|finish=…|tool.<i>.name=…|tool.<i>.args=JSON]... [--drop-tool i]... [--truncate-after M] -o out.yaml

Decodes turn N, applies the semantic edits, re-encodes a valid wire stream, and
writes a NEW cassette (others byte-identical). This is a SINGLE-TURN counterfactual,
not a re-simulation: replay matches on the request, so editing a response does not
make the agent re-derive later turns. Graft warns when a downstream request still
embeds the pre-edit output; --truncate-after drops that now-inconsistent tail.
Refuses to write secrets. (To change the request itself, re-record.)

Example:
  cassette graft session.yaml --turn 1 --set text="the answer is 43" -o what-if.yaml
`,
	},
	{
		Name: "distill",
		Run:  cmdDistill,
		Help: `cassette distill — parameterize one turn into an eval corpus

Usage:
  cassette distill <in.yaml> --turn N --slot text|tool.<i>.args --values vals.txt -o <dir>

Templates a single response slot (the assistant text, or a tool's args) across the
values in vals.txt (one per line), re-encoding each via the keystone, and writes
one cassette per value plus a self-checking corpus.yaml of expected semantic
digests. It RECOMBINES recorded behavior — it cannot synthesize a response shape
the model never produced, and does not vary the request.

Example:
  cassette distill session.yaml --turn 0 --slot text --values answers.txt -o corpus/
`,
	},
	{
		Name: "seeds",
		Run:  cmdSeeds,
		Help: `cassette seeds — report per-turn sampling determinism

Usage:
  cassette seeds <cassette.yaml> [--strict] [--json]

Reads the sampling knobs (seed / temperature / top_p) from each recorded request
and classifies the turn reproducible (seed set, or temperature=0) vs
nondeterministic — so you know whether a forward-branch re-run will reproduce.
OpenAI Responses and Anthropic have no seed param (shown as "n/a"). --strict exits
nonzero on any nondeterministic turn; --json emits one record per turn. It reports
INTENT (knobs set), not realized determinism — providers do not guarantee
bitwise-identical output across model/system_fingerprint versions.

Example:
  cassette seeds session.yaml --strict
`,
	},
	{
		Name: "branch",
		Run:  cmdBranch,
		Help: `cassette branch — prepare a forward-branch seed (edit a step, re-run forward)

Usage:
  cassette branch <in.yaml> --from N [--graft text=…|finish=…|tool.<i>.args=JSON]... -o seed.yaml

Prepares a SEED cassette: optionally grafts turn N's output, then truncates after
N so the seed holds only the deterministic prefix. It does NOT re-run your agent —
cassette sits on the RoundTripper and cannot drive an arbitrary agent loop. To
re-run FORWARD, open the seed in code with Mode: ModeBranch + Options.Live
(cassette.LiveTransport(baseURL, auth)) and run your agent: turns 0..N replay
(turn N returns the edit), and the first request that diverges goes LIVE, is
recorded, and appended — then Save() the grown branch. Prints a determinism banner.

Example:
  cassette branch run.yaml --from 2 --graft text="use tool foo instead" -o seed.yaml
`,
	},
	{
		Name: "report",
		Run:  cmdReport,
		Help: `cassette report — a committable, CI-runnable bug report (.castiron)

Usage:
  cassette report <cassette.yaml> [--no-tool NAME|--no-duplicate-tools|--finishes-clean | --diff baseline.yaml] [--attest c.att] [--bundle name] [-o report.md] [--repro CMD] [--title T]

Turns a failing recording into a Markdown bug report by composing the existing
checks: an invariant violation (reusing assert's flags / the embedded Expect
contract) OR a semantic diff vs a baseline (naming the first divergent turn +
axis). report RE-RUNS the check, so it exits nonzero IFF the bug still
reproduces — a green build means the bug is gone. --attest references a signature
so the fixture is tamper-evident; --bundle writes a plain, offline-runnable
.castiron/ directory (cassette + report + run.sh, secrets refused) you commit and
CI re-runs. "It failed once last Tuesday" becomes a committed regression test.

Examples:
  cassette report run.yaml --no-tool delete_account --bundle tuesday
  cassette report after.yaml --diff before.yaml -o regression.md
`,
	},
	{
		Name: "codec",
		Run:  cmdCodec,
		Help: `cassette codec — verify the Transcript⇄wire codec round-trips

Usage:
  cassette codec verify <cassette.yaml> [--json]

Proves the keystone codec is behavior-preserving and deterministic for every
recorded STREAMING turn: decode→encode→decode reaches a stable transcript fixpoint
(no field is lost) and re-encoding the decoded transcript is byte-deterministic.
The encoder underpins graft/distill/branch and the authoring commands, so this is
the load-bearing check that they emit valid, replayable wire bytes. Unary (non-SSE)
responses and error streams are skipped. Fully offline; nonzero exit on any
non-preserving turn.

Example:
  cassette codec verify session.yaml
`,
	},
	{
		Name: "conformance",
		Run:  cmdConformance,
		Help: `cassette conformance — prove a recording actually replays

Usage:
  cassette conformance <cassette.yaml> [--json]

A closed-loop self-check: for every interaction it verifies (1) the persisted
match key re-derives from the stored request (a stale key from a hand edit
silently desyncs replay — run 'cassette rekey' to fix) and (2) the request
resolves through the in-process replay matcher to a response that decodes to the
stored semantic transcript — all with ZERO outbound dials. Where 'doctor'
diagnoses a miss after the fact, this proactively certifies the whole file is
internally consistent before CI runs it. MCP turns are checked for key integrity.
Nonzero exit on any failure.

Example:
  cassette conformance session.yaml
`,
	},
	{
		Name: "author",
		Run:  cmdAuthor,
		Help: `cassette author — compile a screenplay into a replayable cassette

Usage:
  cassette author <screenplay.yaml> -o <cassette.yaml>

Builds a fully valid, replayable cassette from a terse, hand-written conversation
spec — no API key, no network, deterministic bytes. Each turn names the user
prompt and the assistant reply to synthesize (text and/or tool_calls + finish);
the response is encoded through the wireenc codec and a deterministic request body
is synthesized (or supplied per-turn via 'request:'). The result is verified to
replay (conformance, zero dials) before it is written, and secrets are never
written. The from-scratch generalization of 'graft'.

Screenplay shape:
  provider: anthropic        # anthropic | openai-chat | openai-responses | gemini | ollama
  model: claude-opus-4-6
  turns:
    - user: "What's the weather in Paris?"
      tool_calls: [{name: get_weather, args: '{"city":"Paris"}'}]
      finish: tool_use
    - user: "Thanks!"
      text: "You're welcome."
      finish: end_turn

Example:
  cassette author scenario.yaml -o fixture.yaml
`,
	},
	{
		Name: "proxy",
		Run:  cmdProxy,
		Help: `cassette proxy — record/replay/branch as a language-agnostic reverse proxy

Usage:
  cassette proxy <cassette.yaml|dir> [--mode record|replay|branch] [--upstream URL] [--addr :8080] [--pace] [--on-miss fail] [--miss-manifest path] [--volatile a.b,c]

Runs cassette as a reverse proxy so an app in ANY language records or replays by
pointing its provider base URL (or HTTPS_PROXY) at cassette — no Go, no SDK wrapper.

  --pace         timing-faithful mode: capture per-frame SSE inter-arrival deltas
                 when recording (or branching), and re-emit them with those delays
                 on replay. Off by default (fast CI); pair record --pace with
                 replay --pace for a faithful round trip.

  --mode replay  (default): serve the cassette/dir as an offline endpoint; a miss
                            is a 404, never a passthrough — strictly zero egress.
                            Add --on-miss fail to make an uncovered request fail CI
                            (writes a miss-manifest, exits nonzero).
  --mode record:            forward to --upstream and tee each response into the
                            cassette frame-exact; auth is scrubbed on save (opt-in
                            network).
  --mode branch:            replay the recorded prefix and, on the first divergent
                            request, go live to --upstream and append it (opt-in
                            network).

Binds 127.0.0.1 by default; record/branch warn on a non-loopback bind. Point your
client at it, e.g.:
  ANTHROPIC_BASE_URL=http://localhost:8080  python app.py
  cassette proxy run.yaml --mode record --upstream https://api.anthropic.com
`,
	},
	{
		Name: "mcp-serve",
		Run:  cmdMCPServe,
		Help: `cassette mcp-serve — replay a recorded MCP session over stdio

Usage:
  cassette mcp-serve <cassette.yaml>

Exposes a recorded MCP session as a real stdio JSON-RPC server, so any MCP client
(an IDE, an agent, Claude Desktop) talks to the recording with zero subprocess
launch and zero network — the MCP analogue of 'serve'. initialize/ping are
synthesized; tools/list and tools/call replay recorded results (matching is
identical to record/replay). Pair with the MCP-aware analysis surface: doc,
explain, dataset, diff, and cost now decode recorded MCP turns too.

Example:
  cassette mcp-serve session.yaml
`,
	},
	{
		Name: "mcp-proxy",
		Run:  cmdMCPProxy,
		Help: `cassette mcp-proxy — record a live MCP stdio session via a passthrough proxy

Usage:
  cassette mcp-proxy -o <cassette.yaml> -- <server-cmd> [args...]

Spawns the real MCP server as a subprocess and sits transparently between your MCP
client (this process's stdin/stdout) and that server, forwarding every
newline-delimited JSON-RPC frame verbatim in both directions. Each tools/call and
tools/list request is teed — paired with its result by JSON-RPC id — into a
cassette that 'cassette mcp-serve' then replays with zero subprocess launch and
zero network. It reimplements no MCP semantics: pure passthrough + record. Tool
results are scrubbed on save like any other recording.

Example:
  cassette mcp-proxy -o session.yaml -- npx @modelcontextprotocol/server-everything
  cassette mcp-serve session.yaml      # replay, no subprocess
`,
	},
	{
		Name: "mcp-diff",
		Run:  cmdMCPDiff,
		Help: `cassette mcp-diff — diff two MCP recordings' tool contracts (breaking-change gate)

Usage:
  cassette mcp-diff <old.yaml> <new.yaml> [--fail-on-breaking] [--json]

Compares the tools an MCP server advertises (tools/list input schemas) and how its
tools/call interactions behave between two recordings, and reports the changes —
flagging the BREAKING ones: a tool removed, an input schema made stricter (a property
removed, a type changed, or a new required property), or a call that used to succeed
and now errors. Additive changes (a new tool, a new optional property) are reported
but not breaking.

  --fail-on-breaking   exit nonzero if any breaking change is found (CI gate)
  --json               machine-readable report

Record a golden MCP session, re-record against the updated server, and gate CI:
  cassette mcp-proxy -o golden.yaml -- my-server
  # …server updated…
  cassette mcp-proxy -o new.yaml    -- my-server
  cassette mcp-diff golden.yaml new.yaml --fail-on-breaking
`,
	},
	{
		Name: "mcp-steps",
		Run:  cmdMCPSteps,
		Help: `cassette mcp-steps — walk a recorded MCP session step by step

Usage:
  cassette mcp-steps <session.yaml> [--json]

Prints the JSON-RPC exchanges of a recorded MCP session in order: the method
sequence, the tools/list roster, and each tools/call's arguments → result (or
error). The protocol lens on a session — what the server did, step by step —
complementing the LLM-transcript view from 'doc'/'explain' and the contract diff
from 'mcp-diff'. Read-only, offline.

Example:
  cassette mcp-steps calculator.yaml
  cassette mcp-steps session.yaml --json | jq '.[] | select(.is_error)'
`,
	},
	{
		Name: "provenance",
		Run:  cmdProvenance,
		Help: `cassette provenance — show a derived cassette's lineage chain

Usage:
  cassette provenance <cassette.yaml> [--json]

Prints the recording's behavioral digest and the chain of operations that produced
it (port / migrate / graft / distill) with each source's behavioral digest — so you
can prove what a recording was derived from. The chain uses semantic digests (stable
across benign re-records), is stamped automatically by the derivation commands, and
carries no secrets. An original recording has no lineage.

Example:
  cassette port anthropic.yaml --to openai-chat -o ported.yaml
  cassette provenance ported.yaml      # → port from <anthropic behavior digest>
`,
	},
	{
		Name: "otel",
		Run:  cmdOTel,
		Help: `cassette otel — export a recording as OpenTelemetry GenAI spans (OTLP/JSON)

Usage:
  cassette otel <cassette.yaml|dir> [-o spans.json]

Maps each recorded interaction to an OTLP/JSON span using the OpenTelemetry GenAI
semantic conventions (gen_ai.provider.name, gen_ai.operation.name, gen_ai.request.model,
gen_ai.usage.*, gen_ai.response.finish_reasons, and the MCP convention mcp.method.name /
mcp.tool.name). A bridge INTO Langfuse / Phoenix / SigNoz / any OTLP backend — import a
cassette into your existing observability stack instead of running a second tool.

Deterministic: span/trace IDs are content digests (never random) and durations come
from recorded --pace timing when present, so the same cassette always exports the same
bytes. The GenAI conventions are still pre-stable; the mapping tracks the current names.

Example:
  cassette otel session.yaml | curl -X POST http://localhost:4318/v1/traces -H 'Content-Type: application/json' -d @-
  cassette otel ./testdata/cassettes -o spans.json
`,
	},
	{
		Name: "stitch",
		Run:  cmdStitch,
		Help: `cassette stitch — compose cassettes into one society replay

Usage:
  cassette stitch <a.yaml> <b.yaml> [more...] -o <society.yaml>

Concatenates several single-agent / tool-server recordings into one ordered
cassette (argument order preserved, match keys travel with each interaction), so a
multi-agent run replays deterministically from leaf fixtures. Inverse of split.

Example:
  cassette stitch planner.yaml worker.yaml tools.yaml -o society.yaml
`,
	},
	{
		Name: "split",
		Run:  cmdSplit,
		Help: `cassette split — decompose a recording by provider or tool

Usage:
  cassette split <cassette.yaml> --by provider|tool -o <dir>

Carves a recording into per-provider (anthropic / openai-chat / openai-responses /
gemini / ollama / mcp) or per-tool cassettes, preserving recorded order — the
inverse of stitch.

Example:
  cassette split society.yaml --by provider -o parts/
`,
	},
	{
		Name: "whatif",
		Run:  cmdWhatif,
		Help: `cassette whatif — batch-graft a turn across alternatives

Usage:
  cassette whatif <in.yaml> --turn N --slot text --values alts.txt [--report matrix.md]

Grafts turn N across each alternative value (one per line in --values) and reports,
for each, the resulting combined semantic digest and how many downstream turns
become counterfactual (their request still embeds the pre-edit output) — a one-shot
what-if matrix instead of N manual graft+inspect cycles. --slot is a graft target
(text / append / finish / tool.<i>.args).

Example:
  cassette whatif run.yaml --turn 0 --slot text --values answers.txt --report whatif.md
`,
	},
	{
		Name: "fuzz",
		Run:  cmdFuzz,
		Help: `cassette fuzz — generate valid re-framings to test your parser

Usage:
  cassette fuzz <cassette.yaml> [--turn N] [--reframings 20] [--seed 1] -o <dir>

The inverse of mutate: from one turn, emits many VALID, byte-different streams
(text split across deltas at random boundaries, varied envelope ids/usage,
shuffled independent tool order) that all decode to the SAME transcript. Writes
them as replayable cassettes — a property-test corpus that stress-tests YOUR
SSE/agent parser against chunkings a single recording never produced.
Deterministic for a given --seed.

Example:
  cassette fuzz golden.yaml --turn 0 --reframings 50 --seed 7 -o variants/
`,
	},
	{
		Name: "scenario",
		Run:  cmdScenario,
		Help: `cassette scenario — author an edge/error matrix from one golden turn

Usage:
  cassette scenario <golden.yaml> [--turn N] -o <dir>

From one recorded turn, emits a family of valid cassettes covering the behavior
matrix agents actually break on: each finish reason (end_turn / max_tokens /
stop_sequence / tool_use), truncated and empty text, malformed-but-legal tool
args, and an injected provider error. Writes the named variant cassettes plus a
self-checking corpus.yaml of expected semantic digests. --turn selects the HTTP
turn (default 0). Combines the encoder with authored faults — offline.

Example:
  cassette scenario golden.yaml --turn 0 -o scenarios/
`,
	},
	{
		Name: "coverage",
		Run:  cmdCoverage,
		Help: `cassette coverage — behavioral coverage matrix of a corpus

Usage:
  cassette coverage [dir] [--require tool=NAME,finish=X,...] [--format table|json]

Reports which behaviors a corpus actually exercises — tools called, finish reasons,
providers, refusal verdicts, stream/unary — measured on the normalized Transcript
axis. --require gates CI on the presence of named cells (e.g. is the corpus
actually testing the delete_account tool, or refusals?). Defaults to the current
directory.

Example:
  cassette coverage testdata/cassettes --require tool=delete_account,refusal=refused
`,
	},
	{
		Name: "dashboard",
		Run:  cmdDashboard,
		Help: `cassette dashboard — render a corpus to a single offline HTML report

Usage:
  cassette dashboard [dir] -o <report.html>

Builds one self-contained HTML file (inline CSS, no external assets, no server, no
scripts) summarizing a corpus: the behavioral coverage matrix, token totals,
per-model token breakdown, refusal verdicts, tool usage, and a per-cassette
transcript. The dashboard value of a hosted platform with none of the egress — the
output is deterministic for a given corpus (no timestamps), so it can be committed
or attached to CI. Defaults to the current directory.

Example:
  cassette dashboard testdata/cassettes -o report.html
`,
	},
	{
		Name: "shrink",
		Run:  cmdShrink,
		Help: `cassette shrink — minimal behavior-preserving subset of a corpus

Usage:
  cassette shrink [dir] [-o kept/]

Computes the smallest subset of cassettes whose union of behavioral signals
(per-turn semantic digests + tool/finish/provider/refusal coverage) equals the
whole corpus's — a greedy set cover keyed on SEMANTIC digests, not bytes. Turns
"we have 4000 cassettes" into "these N cover every behavior". With -o, copies the
kept cassettes into that directory; otherwise lists them. Deterministic.

Example:
  cassette shrink testdata/cassettes -o minimal/
`,
	},
	{
		Name: "schema-check",
		Run:  cmdSchemaCheck,
		Help: `cassette schema-check — validate tool-call args against the advertised schema

Usage:
  cassette schema-check <cassette.yaml> [--json]

Proves the model honored its own tool contract: every emitted tool call's arguments
are validated against the JSON Schema the request advertised for that tool
(Anthropic input_schema / OpenAI function.parameters / Gemini functionDeclarations;
MCP tools/list inputSchema). Hand-rolled structural validation of the common subset
(type, required, properties, additionalProperties:false, enum) — no schema library,
fully offline. Nonzero exit on any violation.

Example:
  cassette schema-check session.yaml
`,
	},
	{
		Name: "taint",
		Run:  cmdTaint,
		Help: `cassette taint — trace sensitive values that cross turns

Usage:
  cassette taint <cassette.yaml> [--fail-on email,creditcard,...] [--json]

Where egress-audit finds typed PII per request, taint follows the FLOW: a value
that first appeared in an earlier turn — as user input, or as untrusted model/tool
OUTPUT — and is then carried OUTBOUND in a later request. Output-sourced flows
(potential exfiltration / injection relay) are highlighted. A report by default;
--fail-on <classes> makes it a CI gate. Masked samples only; raw secrets are never
printed. Fully offline.

Example:
  cassette taint session.yaml --fail-on ssn,creditcard
`,
	},
	{
		Name: "lint",
		Run:  cmdLint,
		Help: `cassette lint — one-pass corpus health gate

Usage:
  cassette lint <cassette.yaml|dir> [--json] [--strict] [--max-bytes N]

Runs every per-cassette health check across a file or directory (recursively) and
emits findings: secret patterns and stale match keys are errors; oversized bodies,
undecodable or finish-less streams, and empty files are warnings. Exits nonzero on
any error, or — with --strict — any warning. The CI front door for a fixture
library. --json emits the findings as machine-readable JSON.

Example:
  cassette lint testdata/cassettes --strict
`,
	},
	{
		Name: "merge",
		Run:  cmdMerge,
		Help: `cassette merge — union several cassettes into one fixture

Usage:
  cassette merge <a.yaml|dir> [more...] -o <out.yaml> [--strict]

Unions the interactions of several cassettes (or directories of cassettes) into
one, preserving argument order, dropping exact duplicates (same request key +
identical bytes) and reporting conflicts (same request, different response).
Passing a shared-prefix cassette first and per-test tails after yields a layered
fixture deterministically. A directory argument folds in every *.yaml cassette it
contains (sorted). --strict exits nonzero if any conflict is found.

(Relatedly, 'cassette serve <dir>' serves a whole directory as one endpoint.)

Example:
  cassette merge login.yaml checkout.yaml -o suite.yaml
`,
	},
	{
		Name: "port",
		Run:  cmdPort,
		Help: `cassette port — transpile a recording to another provider's wire dialect

Usage:
  cassette port <cassette.yaml> --to anthropic|openai-chat|openai-responses|gemini|ollama [-o out.yaml]

Re-encodes each streaming turn into the target dialect (decode→Transcript→encode)
and re-shapes the request path + body so a client built for the target provider
can replay the SAME recorded behavior offline. The per-turn semantic digest is
preserved. Unary/error turns (which have no SSE to transpile) are copied through
unchanged. This is a wire-dialect re-targeting, NOT a claim that two providers
produce equivalent model output. The output records its lineage (see 'cassette provenance').

Example:
  cassette port anthropic-run.yaml --to openai-chat -o openai-run.yaml
`,
	},
	{
		Name: "migrate",
		Run:  cmdMigrate,
		Help: `cassette migrate — port a whole corpus to a dialect, verifying behavior is preserved

Usage:
  cassette migrate <in|dir> --to anthropic|openai-chat|openai-responses|gemini|ollama -o <out|dir>

The "de-risk a provider switch with your existing tests" command. Ports a recording
or an entire corpus (a directory migrates flat into an output directory; a single
file migrates to a single file) to the target wire dialect via 'port', then VERIFIES
that every turn's semantic digest is preserved — failing loudly (nonzero exit, with
a per-file report) on any drift. Record once against provider A, migrate to provider
B, and replay your B-built client offline against the same recorded behavior with a
hard guarantee nothing silently changed in translation. Each output records its
lineage (see 'cassette provenance').

Example:
  cassette migrate testdata/cassettes --to openai-chat -o migrated/
`,
	},
	{
		Name: "canonicalize",
		Run:  cmdCanonicalize,
		Help: `cassette canonicalize — re-emit streams to byte-stable canonical form

Usage:
  cassette canonicalize <cassette.yaml> [--check] [-o out.yaml]

Re-emits every codec-preserving STREAMING response through the encoder with a
zeroed envelope, so the volatile ids/usage and incidental SSE chunk boundaries
collapse to a fixed shape. Two recordings of the same behavior then serialize
byte-identically, so 'git diff' on a re-record shows only what the model actually
did differently. Semantic digests are preserved; unary and error streams are left
untouched; it is idempotent. --check is a read-only pre-commit gate (nonzero exit
if not already canonical); -o writes a copy instead of editing in place. Secrets
are never written.

Examples:
  cassette canonicalize session.yaml
  cassette canonicalize session.yaml --check
`,
	},
	{
		Name: "completion",
		Run:  cmdCompletion,
		Help: `cassette completion — print a shell completion script

Usage:
  cassette completion bash|zsh|fish

Generated from the command registry, so completions never drift from the actual
commands. Install, e.g.:
  bash:  eval "$(cassette completion bash)"
  zsh:   cassette completion zsh  > "${fpath[1]}/_cassette"
  fish:  cassette completion fish > ~/.config/fish/completions/cassette.fish
`,
	},
	{
		Name: "version",
		Run:  cmdVersion,
		Help: `cassette version — print the version

Usage:
  cassette version

Prints the cassette version (set at release time) plus the Go runtime and OS/arch.
`,
	},
	{
		Name: "init",
		Run:  cmdInit,
		Help: `cassette init — scaffold an offline-replay setup in a project

Usage:
  cassette init [dir] [--stack python|node|go|promptfoo]

Creates testdata/cassettes/, adds cassette-misses.json to .gitignore (idempotent),
and prints a copy-paste snippet for pointing your provider base URL at
'cassette proxy' — the 60-second adoption path for any language. Detects the stack
from go.mod / package.json / pyproject.toml / requirements.txt / promptfooconfig.yaml(.js)
when --stack is omitted. See docs/recipes/
for a ready-made pytest conftest.py and a reusable GitHub Action.

Example:
  cassette init --stack python
`,
	},
}

var commandByName = func() map[string]*Command {
	m := make(map[string]*Command, len(commands))
	for _, c := range commands {
		m[c.Name] = c
	}
	return m
}()

// groupOrder is the order command sections appear in `cassette --help`.
var groupOrder = []string{
	"Record & serve",
	"Author & transform",
	"Inspect & review",
	"Verify & integrity",
	"Safety & scrubbing",
	"Eval & cost",
	"Maintain & distribute",
	"Other",
}

// commandGroup maps every command to its help section. A registry test asserts it
// covers every command and uses only groupOrder values, so help can't silently
// drop a command into a void or drift out of order.
var commandGroup = map[string]string{
	// Record & serve
	"up":    "Record & serve",
	"serve": "Record & serve", "proxy": "Record & serve", "mcp-serve": "Record & serve", "mcp-proxy": "Record & serve", "watch": "Record & serve",
	// Author & transform
	"author": "Author & transform", "graft": "Author & transform", "branch": "Author & transform",
	"distill": "Author & transform", "port": "Author & transform", "migrate": "Author & transform", "canonicalize": "Author & transform",
	"merge": "Author & transform", "stitch": "Author & transform", "split": "Author & transform",
	"whatif": "Author & transform", "scenario": "Author & transform", "fuzz": "Author & transform",
	"mutate": "Author & transform",
	// Inspect & review
	"open":    "Inspect & review",
	"inspect": "Inspect & review", "doc": "Inspect & review", "explain": "Inspect & review",
	"diff": "Inspect & review", "mcp-diff": "Inspect & review", "mcp-steps": "Inspect & review", "otel": "Inspect & review", "provenance": "Inspect & review", "bisect": "Inspect & review", "doctor": "Inspect & review",
	"dataset": "Inspect & review", "coverage": "Inspect & review", "dashboard": "Inspect & review",
	// Verify & integrity
	"verify": "Verify & integrity", "assert": "Verify & integrity", "expect": "Verify & integrity",
	"conformance": "Verify & integrity", "codec": "Verify & integrity", "attest": "Verify & integrity", "policy": "Verify & integrity",
	"verify-attest": "Verify & integrity", "seeds": "Verify & integrity", "schema-check": "Verify & integrity",
	// Safety & scrubbing
	"scrub": "Safety & scrubbing", "egress-audit": "Safety & scrubbing", "audit": "Safety & scrubbing", "taint": "Safety & scrubbing",
	"refusal": "Safety & scrubbing", "redteam": "Safety & scrubbing", "lint": "Safety & scrubbing",
	"orphans": "Safety & scrubbing", "shrink": "Safety & scrubbing", "redact-field": "Safety & scrubbing",
	// Eval & cost
	"eval": "Eval & cost", "cost": "Eval & cost", "report": "Eval & cost",
	// Maintain & distribute
	"prune": "Maintain & distribute", "rekey": "Maintain & distribute", "pack": "Maintain & distribute",
	"gitconfig": "Maintain & distribute", "completion": "Maintain & distribute",
	// Other
	"version": "Other", "init": "Other",
}

// summary is the one-line description shown in the top-level usage list, derived
// from the title line of Help ("cassette <name> — <summary>") so it never drifts.
func (c *Command) summary() string {
	line := c.Help
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if _, s, ok := strings.Cut(line, "— "); ok {
		return s
	}
	return ""
}

// printCommandHelp writes a command's detailed help, returning whether it exists.
func printCommandHelp(cmd string, w io.Writer) bool {
	c := commandByName[cmd]
	if c == nil {
		return false
	}
	io.WriteString(w, c.Help)
	return true
}

// wantsHelp reports whether args request help (-h / --help / help anywhere).
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}
