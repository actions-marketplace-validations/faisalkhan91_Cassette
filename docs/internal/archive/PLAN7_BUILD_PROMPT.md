# PLAN7 build prompt — provider breadth + deterministic eval/CI

> Paste as the task for an autonomous build loop. Implements the round-3 roadmap in
> [`FEATURE_RESEARCH_R3.md`](FEATURE_RESEARCH_R3.md), one independently-shippable,
> gate-green slice at a time. Theme: **light up the whole command surface for new
> providers, then turn the proxy's reach and the codec's depth into a deterministic
> eval + CI moat.**

## Your mission

Build the PLAN7 slices below **in order**. After **each** slice: `gofmt -w`,
`go build ./...`, `go vet ./...`, add table-driven offline tests, run
`bash scripts/ci.sh` (must exit 0, meet the coverage floor), update docs/CHANGELOG,
wire any new command into the registry + `commandGroup` + `help_test` +
`docs/cli.md` (the doc-lint test enforces this), and commit the slice separately.
Do not stop between slices to ask for approval — keep going until done or blocked.

## Non-negotiable constraints

Pure Go; offline CI (`GOFLAGS=-mod=vendor`, `GOPROXY=off`); **no new heavy deps**
(stdlib + the existing handful; prefer a hand-rolled structural check over a
JSON-schema library); zero outbound network in the default path (opt-in only, like
`watch`/`proxy --mode record`); deterministic/byte-exact; secrets never written;
`gofmt`+`go vet` clean; coverage floor (excludes data/example/fixture packages, with
a per-package floor — keep it green); curated public API (root package only;
`analysis` is internal). New commands must be grouped in `commandGroup` and pass
the registry + doc-lint tests.

## The dialect-adding pattern (slices 1, 5, 10, 12 all follow it)

A new provider is a `(decoder, encoder)` pair that lights up the whole surface:
1. Add the provider to `internal/wireenc` `Provider` enum + `ProviderForURL` (ordered
   so it can't fall through to the Anthropic default).
2. `semequal.Decode<Name>` — reduce wire bytes to a normalized `Transcript` (text,
   tool calls with `canon`-canonicalized args, **provider-native** finish reason);
   drop volatile/aux fields (thinking, citations, telemetry) so they never enter the digest.
3. `wireenc.Encode<Name>` — deterministic re-emit from `(Transcript, Envelope)`;
   all volatile fields come from the Envelope. Prove `wireenc.RoundTrip` fixpoint +
   determinism with a `codec verify` test.
4. `analysis/port.go` `pathFor`/`extractRequest`/`buildRequest` cases so `port`,
   `coverage`, `scenario`, `conformance`, `author`, `graft` light up for free.

---

## Slices

**1 — Gemini dialect (dual-framing).** Routes on `:streamGenerateContent` for both
`generativelanguage.googleapis.com` (v1beta) and Vertex `*-aiplatform` hosts.
`DecodeGeminiSSE`: concatenate `candidates[0].content.parts[].text`, map
`parts[].functionCall{name,args}` → `ToolCall` (args via `canon.Canonicalize` — they
arrive as a structured object, not a JSON string), **skip** `thinking`/`thought` and
`citationMetadata` parts, keep `finishReason` raw (STOP/MAX_TOKENS/SAFETY/RECITATION).
**Both framings:** the `alt=sse` `text/event-stream` path *and* the default top-level
JSON-array stream (`[\n{…},\n{…}\n]`) via a small pure-Go streaming array splitter
that tolerates a mid-element cut; framing selected on encode by recorded Content-Type.
Deterministic encoder + `codec verify` round-trip test. *Done when:* an authored
Gemini cassette replays + round-trips and `port` converts a Gemini run to/from another
dialect with digests preserved.

**2 — schema-check.** `cassette schema-check <cassette.yaml> [--fail-on] [--json]`:
extract the advertised tool schemas from each request (`tools[].input_schema`
(Anthropic), `tools[].function.parameters` (OpenAI), `functionDeclarations`
(Gemini)) and validate every recorded tool call's canonical args against it
(required keys present, types match, no unknown keys if `additionalProperties:false`).
Hand-rolled structural validation — **no JSON-schema dependency**. Reports per-call
violations; `--fail-on` gates CI.

**3 — ci-guard.** Add `--on-miss=fail` to `serve` and `proxy --mode replay`: collect
unmatched requests into a miss-manifest (path + which axis was missing) and exit
nonzero on shutdown. The CI contract: "every request the agent made was covered."

**4 — scorecard.** Extend `eval`: a judge fixture may return a numeric score; add a
threshold gate (`--min-score`) and a structured scorecard (per-assertion + overall).
Keep the judge a replayed fixture (zero network). This is the eval-depth keystone.

**5 — Ollama NDJSON dialect (`/api/chat`).** New `Provider`; an NDJSON frame splitter
(one JSON object per line); extraction into `Transcript` (`message.content`,
`message.tool_calls`, `done_reason`→finish); treat `eval_count`/`*_duration`/
`created_at` as Envelope-owned volatile (stripped from the digest). Deterministic
NDJSON encoder + round-trip test. (Defer `/api/generate`.)

**6 — jsonl codec.** Decode + `verify` bulk JSONL/array result payloads (the
replayable core of the batch APIs). New `internal/jsonl` + `cassette` integration so
`verify`/`doc` understand a JSONL results body. No async lifecycle state machine.

**7 — suite (+ ensemble).** `cassette suite <dir> --suite s.yaml`: replay a whole
corpus through the scored judge (slice 4) with pass/fail gating and an optional
per-subject judge **ensemble** (consensus of N judge fixtures). Reuses the corpus
loaders + `Coverage` iteration pattern.

**8 — ab + stats.** `cassette ab <corpusA> <corpusB>` — aggregate win/loss across two
frozen corpora (beyond pairwise `diff`), reusing `diffrec` + `refusal`. `cassette
stats <dir> --by tool|finish|model|refusal` — group-by aggregation (the reduce-half
of `dataset`).

**9 — dataset --query.** Fold a frozen boolean/relational query grammar into
`dataset` (`--query 'tool=delete_account AND refusal=refused'`) — deterministic,
no embeddings, not a new command.

**10 — Cohere v2 (`/v2/chat`) dialect** — breadth follow-on; the pattern is proven by now.

**11 — embed assert.** Embeddings codec (decode the vector response deterministically)
+ an in-cassette vector-relation contract in `Expect` (e.g. `nearer: [A, B, C]`
meaning A is nearer B than C by cosine) checked by `assert`. The *relation* contract,
not a raw float compare, is the deterministic, cassette-unique part.

**12 — AWS Bedrock eventstream (Claude-on-Bedrock).** A pure-Go
`vnd.amazon.eventstream` binary-frame parser (length-prefixed, CRC32 framed — no cgo)
that yields the inner Anthropic event JSON, then reuse the existing Anthropic
semantic path. SigV4 is a request-signing concern the match key already excludes
(host/auth out of the key), so replay needs no re-signing. Scope strictly to
Claude-on-Bedrock; Titan/Llama later.

**13 — pact.** `cassette pact extract <cassette.yaml> -o pact.yaml` + `cassette pact
verify` — a standalone consumer-driven contract (the tools the agent relies on + the
arg/response shapes it expects), derived from cross-turn ownership. Sequence after
`schema-check`, whose validation it reuses.

## Stop conditions

Stop and surface to the owner only if: a slice genuinely needs a new heavy
dependency (e.g. you conclude a JSON-schema lib is unavoidable for slice 2 — prefer
not), a constraint truly conflicts, or a wire format needs cgo (it shouldn't — both
Gemini array framing and Bedrock eventstream are pure-Go). Otherwise pick the
smallest-correct option, note it in the commit, and continue. When done: run the
full gate, update `CHANGELOG.md` + `DECISIONS.md`, tick `FEATURE_RESEARCH_R3.md`,
and report what shipped + what was deferred.
