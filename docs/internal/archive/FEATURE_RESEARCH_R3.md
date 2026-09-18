# cassette — feature research (round 3)

Third exhaustive ideation pass, run after the PLAN6 feature wedge **and** the
maturation work shipped (51 commands, real identity, honest gate, secured brand,
docs + visual coverage). Methodology: 8 forward lenses fanned out (web + codebase
grounded) → **59 ideas** → each **adversarially vetted** against the full shipped
*and* prior-rejected lists → **26 survivors** → synthesized twice (reach vs moat)
and reconciled below. 70 agents, ~2.07M tokens.

## Headline

cassette has wide *capability* but narrow *coverage*: it ships a round-trippable
`Transcript⇄wire` codec and 51 commands, yet still decodes/encodes only **three
SSE dialects** (Anthropic Messages, OpenAI Chat, OpenAI Responses) + MCP stdio.
Both framings independently put the same thing at #1:

> **Provider breadth is the force-multiplier.** Every command dispatches off
> `wireenc.Provider` + `ProviderForURL`, so a *single* new (decoder, encoder) pair
> instantly lights up `port`, `coverage`, `author`, `graft`, `distill`, `scenario`,
> `conformance`, `lint`, and `eval` for that provider. **Gemini is the largest
> provider with zero support** — it is the highest behavior-coverage-per-line
> addition cassette can make.

The second consensus: now that `proxy` is the any-language entry point, a thin
**CI gate** (strict on-miss manifest) and a **deterministic eval-scoring stack**
(judge-as-fixture, byte-reproducible) are the cheapest ways to convert the proxy's
reach into adoption and the codec's depth into a moat.

## Themes

- **Provider breadth (reach × moat multiplier)** — Gemini, Ollama (native NDJSON),
  Cohere v2, AWS Bedrock. New dialects validate that the Transcript model is truly
  framing-agnostic (NDJSON and binary eventstream are framings the engine has never
  had) and are structurally impossible for a live-only competitor to replay offline.
- **Bulk / async wire shapes** — JSONL-of-results (batch APIs), embeddings — wire
  shapes cassette currently can't express.
- **Deterministic eval-as-fixture depth** — `eval` ships one judge + a substring
  pass-marker; a scored-verdict primitive unlocks scorecards, regression suites,
  judge ensembles, and corpus A/B — all byte-reproducible because the judge *is* a
  fixture (impossible for promptfoo/LangSmith, which judge live).
- **Contract governance** — validate emitted tool-call args against the JSON Schema
  the request itself advertised (`schema-check`); consumer-driven agent/tool pacts.
- **Language-agnostic CI** — turn `proxy`/`serve` replay into a CI gate (`--on-miss=fail`
  + a miss-manifest), plus an `init` scaffolder and per-test header routing.
- **Corpus intelligence** — a frozen query grammar over the `dataset` projection,
  group-by stats, behavioral-digest dedup.

## The best features (reconciled, best-first)

| # | Feature | Effort | Nov | Imp | Why uniquely cassette |
|---|---------|--------|-----|-----|------------------------|
| 1 | **Gemini dialect** (dual-framing: `alt=sse` *and* the default top-level JSON-array `streamGenerateContent`) | M–L | 4 | 5 | Largest provider, zero support; one codec pair lights up the whole command surface. Dual framing for the *same* endpoint is genuinely Gemini-specific hardness no other dialect needs. |
| 2 | **schema-check** — validate recorded tool-call args against the JSON Schema advertised in the request's `tools[]` | M | 4 | 4 | Pure, deterministic, offline. cassette owns both the advertised schema (request) and the emitted args (response) — it can prove the model honored its own tool contract. Nothing live-only can do this byte-reproducibly. |
| 3 | **scorecard** — a scored-verdict extension to `eval` (numeric score + threshold, not just substring pass) | M | 3 | 4 | The keystone for all eval depth; byte-reproducible because the judge is a stored fixture. |
| 4 | **ci-guard** — `serve`/`proxy --mode replay --on-miss=fail` writes a miss-manifest and exits nonzero | M | 3 | 4 | Turns the any-language proxy into a real CI gate ("every request the agent made was covered"), the missing piece for non-Go adoption. |
| 5 | **Ollama native NDJSON dialect** (`/api/chat`) | M | 4 | 4 | The one popular local runtime that is *not* OpenAI-shaped; exercises line-delimited framing the engine has never had, proving the Transcript model is framing-agnostic. |
| 6 | **suite** — corpus regression runner with a scored baseline (+ optional judge **ensemble**/panel folded in) | L | 4 | 4 | Replays a whole corpus through the scored judge offline with pass/fail gating — a deterministic eval gate competitors can't reproduce. |
| 7 | **jsonl codec** — decode + verify bulk JSONL/array result payloads (the honest core of the batch APIs) | S | 4 | 3 | A new wire shape (newline-delimited results) with high leverage and low cost; the replayable slice of async batch without the lifecycle complexity. |
| 8 | **Cohere v2** (`/v2/chat`) dialect | M | 3 | 3 | Another zero-support provider; same force-multiplier, low marginal cost once the dialect-adding pattern is established. |
| 9 | **ab** — A/B win/loss over two frozen corpora (aggregate, beyond pairwise `diff`) | L | 4 | 4 | Differential testing across model versions, byte-reproducible from fixtures. |
| 10 | **embed assert** — embeddings codec + an in-cassette vector-relation contract (e.g. "A nearer B than C") | M | 4 | 3 | Makes RAG/embedding tests deterministic and offline; the relation contract (not raw float compare) is the cassette-unique part. |
| 11 | **dataset --query** — a frozen boolean/relational query grammar over the corpus projection | M | 3 | 3 | Turns the flat `dataset` rows into a queryable, governed index — no embeddings, fully deterministic. |
| 12 | **AWS Bedrock eventstream codec** (Claude-on-Bedrock first) | L | 5 | 4 | Highest novelty: a pure-Go `vnd.amazon.eventstream` binary-frame parser (no cgo) over the existing Anthropic semantic path. Hardest wire format in the set; do it last, scoped to Claude-on-Bedrock. |
| 13 | **stats** — group-by corpus aggregation (the reduce-half of `dataset`) | S | 3 | 3 | Cheap analytics over what cassette already projects. |
| 14 | **pact** — consumer-driven agent/tool contracts extracted from a recording | L | 4 | 3 | A signable, standalone contract artifact derived from cross-turn ownership; long-term category bet (overlaps `schema-check`, so sequence after it). |

## Recommended build sequence (PLAN7)

Dependency-ordered; each slice independently shippable and gate-green (pure Go,
offline, no heavy deps, secrets never written, coverage floor). Provider dialects
share one pattern — establish it on Gemini, then each subsequent dialect is cheap.

1. **Gemini dialect (dual-framing)** — `wireenc.Provider` Gemini + `ProviderForURL`
   (`:streamGenerateContent`, both `generativelanguage.googleapis.com` and Vertex
   `*-aiplatform` hosts); `DecodeGeminiSSE` (concatenate `candidates[].content.parts[].text`,
   map `functionCall{name,args}`→ToolCall with `canon`-canonicalized args, **drop
   thinking/citation parts**, keep `finishReason` **raw**); a streaming JSON-array
   framing splitter (pure Go, tolerant of a mid-element cut) selected by recorded
   Content-Type; deterministic encoder; `port.go` cases; a `codec verify` round-trip test.
2. **schema-check** — extract `tools[].input_schema`/`function.parameters` from the
   request, validate each recorded tool call's canonical args against it (stdlib
   JSON-schema-lite or a tiny vendored validator if truly needed — prefer hand-rolled
   structural checks to avoid a heavy dep); `--fail-on` gate.
3. **ci-guard** — `--on-miss=fail` on `serve`/`proxy` replay: collect unmatched
   requests into a manifest and exit nonzero (the CI contract).
4. **scorecard** — scored-verdict extension to `eval` (judge returns a number;
   threshold gate); the eval-depth keystone.
5. **Ollama NDJSON** (`/api/chat`) — NDJSON frame splitter; telemetry
   (`eval_count`/durations/`created_at`) treated as Envelope-owned volatile; encoder
   round-trip test.
6. **jsonl codec** — decode + `verify` bulk JSONL/array result payloads.
7. **suite** (+ ensemble) — corpus regression runner over the scored judge.
8. **ab + stats** — corpus A/B win/loss and group-by aggregation (share the
   `diffrec`/`refusal`/`dataset` reducers).
9. **dataset --query** — frozen query grammar (fold in, not a 52nd command).
10. **Cohere v2** dialect (breadth follow-on once the pattern is proven).
11. **embed assert** — embeddings codec + vector-relation contract.
12. **AWS Bedrock eventstream** (Claude-on-Bedrock) — the hard binary-framing slice, last.
13. **pact** — consumer-driven contracts (after `schema-check` proves the contract surface).

## Rejected / deferred (do not re-propose without defeating the reason)

- **batch (async submit→poll lifecycle)** — net-new value is just JSONL-results
  decoding (shipped as the `jsonl` codec); the poll lifecycle falls out of normal
  record/replay. Don't build a lifecycle state machine.
- **otlp / waterfall / timing** — all gated on `StreamTiming`, which is **off by
  default** and dropped by `canonicalize`; reach is narrow. `waterfall` also
  duplicates `pack`'s embedded browser. Revisit only if timing capture becomes common.
- **rank (Elo/Bradley-Terry leaderboard)** — niche pairwise-tournament workflow, L effort.
- **blame (behavioral git-blame)** — clever but narrow; needs `git cat-file` per-rev
  decode (not `git log -L`, which fragments on `canonicalize`).
- **sidecar (survive-re-record tags/labels)** — genuine governance primitive but the
  keying is fragile (`match.Key` is config-parameterized); internal-hygiene reach.
- **dedupe (behavioral duplicate report)** — overlaps `shrink`/`prune`/`orphans`;
  curation, not adoption. Keep only if a real large-corpus pain appears.
- **standalone `panel` / `query` commands** — fold into `suite` and `dataset --query`
  rather than adding near-duplicate top-level commands.
- **Realtime/WebSocket (voice) duplex** — surfaced but not in the top set: bidirectional
  binary audio + timing-sensitivity is a large, brand-stretching project; defer until
  there's demand. (Honestly flagged as the hard frontier, not killed.)

## Constraint reminders that shaped the cut

Bedrock's binary eventstream and Gemini's array framing are the only genuinely hard
wire formats — both are doable in **pure Go** (no cgo); SigV4 is a *request*-signing
concern that the match key already excludes (host/auth out of the key), so Bedrock
replay doesn't need to re-sign. Everything else is "one more dialect" or a pure
analysis/derivation over bytes cassette already owns.
