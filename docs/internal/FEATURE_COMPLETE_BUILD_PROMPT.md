# Feature-complete build prompt — cassette

> Paste as the task for an autonomous build loop (e.g. `/loop`). Implements the
> definitive scope in [`FEATURE_COMPLETE_PLAN.md`](FEATURE_COMPLETE_PLAN.md): 21
> slices that take cassette to feature complete, one independently-shippable,
> gate-green slice at a time. Slices 1–20 are fully autonomous; slice 21 (the
> release tag + secrets) is owner-gated — prepare it, don't fire it.

## Loop discipline (every slice)

1. Implement the smallest correct version that fully satisfies the slice's done-criteria.
2. `gofmt -w` changed files; `go build ./...`; `go vet ./...`.
3. Add table-driven, offline tests. Run `bash scripts/ci.sh` — it must exit 0 and
   hold the aggregate **and** per-package coverage floors.
4. Wire any new command into the registry + `commandGroup` + `help_test` +
   `docs/cli.md` (the doc-lint and registry tests enforce this).
5. Update `CHANGELOG.md` / `docs/*` / `DECISIONS.md` as warranted.
6. Commit the slice separately (conventional prefix; no AI footer). Then continue.

Work the slices **in order** (dependencies are real). Don't stop between slices to
ask for approval — keep going until all 1–20 are done or you hit a stop condition.

## Non-negotiable constraints

Pure Go; offline CI (`GOFLAGS=-mod=vendor`, `GOPROXY=off`); **no new heavy deps**
(stdlib + the existing handful — prefer hand-rolled structural checks over a
JSON-schema library; pure-Go binary parsing over any cgo); zero outbound network in
the default path (opt-in only, like `watch`/`proxy --mode record`); deterministic /
byte-exact (no time/rand/map-order in serialized output; seed PRNGs); secrets never
written; gofmt+vet clean; coverage floors held; curated public API (root package
only — `analysis` is internal). New commands grouped + doc-linted.

## The dialect-adding pattern (slices 1, 12, 13, 16; routing-only for 2)

1. Add the provider to `internal/wireenc` `Provider` + `ProviderForURL` (ordered so
   it can't fall through to the Anthropic default).
2. `semequal.Decode<Name>` → normalized `Transcript` (text; tool calls with
   `canon`-canonicalized args; **provider-native** finish reason); drop volatile/aux
   fields (thinking, citations, telemetry, ids, usage) so they never enter the digest.
3. `wireenc.Encode<Name>` — deterministic re-emit from `(Transcript, Envelope)`;
   prove the `wireenc.RoundTrip` fixpoint + a `codec verify` test on a **real-wire
   fixture** for both text and tool-use.
4. `analysis/port.go` `pathFor`/`extractRequest`/`buildRequest` cases, so `port`,
   `coverage`, `scenario`, `conformance`, `author`, `graft` light up for free.
5. `analysis.DecodeInteraction` routes the new provider.

---

## Slices

**1 — Gemini dialect (dual-framing).** Routes `:streamGenerateContent` on both
`generativelanguage.googleapis.com` (v1beta) and Vertex `*-aiplatform` hosts.
Decoder concatenates `candidates[0].content.parts[].text`, maps
`parts[].functionCall{name,args}`→`ToolCall` (args via `canon.Canonicalize` — a
structured object, not a JSON string), **skips** `thinking`/`thought` +
`citationMetadata`, keeps `finishReason` raw. **Both framings:** the `alt=sse`
`text/event-stream` path AND the default top-level JSON-array stream via a pure-Go
streaming array splitter tolerant of a mid-element cut; framing selected on encode by
recorded Content-Type. (`X-Goog-Api-Key` is already scrubbed.)

**2 — OpenAI-compatible host routing.** No new codec: document + table-test that
`ProviderForURL` returns the OpenAI dialects for Azure deployment paths and
representative vLLM/Together/Groq/OpenRouter/Mistral/DeepSeek base URLs; a
fixture-backed record→replay against one non-`openai.com` OpenAI-shaped host; list
the family in `docs/cli.md`.

**3 — schema-check (HTTP + MCP).** `cassette schema-check <cassette> [--fail-on] [--json]`:
extract advertised tool schemas (`tools[].input_schema` Anthropic, `tools[].function.parameters`
OpenAI, `functionDeclarations` Gemini, MCP tool input schema) and validate each
recorded tool call's canonical args (required keys, types, `additionalProperties:false`
unknown-key rejection). Hand-rolled structural validation — **no schema dependency**.

**4 — ci-guard.** `--on-miss=fail` on `serve` and `proxy --mode replay`: collect
unmatched requests into a stable, versioned miss-manifest (path + missing axis) and
exit nonzero on shutdown.

**5 — Per-test routing/isolation.** A request header (e.g. `X-Cassette-Test`) or path
prefix selects a per-test cassette when serving a directory; documented + tested.

**6 — scorecard.** Extend `eval`: a judge fixture may return a numeric score; add a
byte-stable score-extraction contract, `--min-score` threshold gate, and a structured
scorecard (per-assertion + overall). Judge stays a replayed fixture.

**7 — Judge ensemble/panel.** N judge fixtures with a fixed reduction
(majority/mean/min); folded into `eval`/`suite` (not a standalone command).

**8 — suite.** `cassette suite <dir> --suite s.yaml`: replay a corpus through the
scored judge with a **persisted baseline** + drift gate and per-turn/windowed scope.

**9 — ab.** `cassette ab <corpusA> <corpusB>`: aggregate win/loss across two frozen
corpora (reuse `diffrec`/`refusal`).

**10 — dataset --query.** Fold a frozen boolean/relational grammar into `dataset`
(`--query 'tool=x AND refusal=refused'`), erroring on unknown fields (fix the silent
no-match foot-gun); add provider/MCP/system_digest columns.

**11 — stats.** `cassette stats <dir> --by tool|finish|model|refusal|provider`:
group-by aggregation over the projection.

**12 — Ollama NDJSON** (`/api/chat`). NDJSON frame splitter; `message.content` +
`message.tool_calls`; `done_reason`→finish; `eval_count`/durations/`created_at`
Envelope-volatile.

**13 — Cohere v2** (`/v2/chat`) dialect.

**14 — Embeddings + JSONL/array results decode.** A decode path for
OpenAI/Gemini/Cohere `/embeddings` → a stable, volatile-stripped vector projection
(dimension + canonical float encoding) that `codec verify` round-trips; a
JSONL/array-of-results splitter that verifies each line/element decodes; both wired
into `DecodeInteraction`; `verify` treats a malformed line/vector as a hard error.

**15 — embed-assert.** An in-cassette vector-relation contract in `Expect`
(e.g. `nearer: [A,B,C]` = A nearer B than C by cosine) checked by `assert`.

**16 — AWS Bedrock eventstream** (Claude-on-Bedrock). A pure-Go (no cgo)
`vnd.amazon.eventstream` decoder (prelude+CRC, headers, payload) feeding the existing
Anthropic Transcript path; route `bedrock-runtime.*.amazonaws.com` invoke-with-response-stream;
deterministic re-framing encoder with correct CRCs; SigV4 confirmed out of the match
key. Titan/Llama later (out of scope now).

**17 — Governance fold-in.** Opt-in policy-as-code gates in `lint` (allowed
tools/egress hosts/data classes from a small declarative file); bind the egress/taint
data-class profile (counts/kinds, never raw values) into the `attest` manifest +
`verify-attest`; a trust-boundary doc.

**18 — Determinism honesty.** A concurrent same-key replay-ordering diagnostic +
documented order-by-record contract; promote `seeds` to a corpus-wide CI gate.

**19 — Public API leak fix.** Remove internal-type leaks from the root surface
(`Options.Match`/`Scrub` exposing `internal/*`, `RekeyFile`): introduce a public
config shim or move the knobs behind opaque options; add an **external-module compile
test** (a tiny module that imports only the public API) + a CI grep guard against
`internal/` in exported signatures.

**20 — 1.0 docs & onboarding.** `cassette init` scaffolder (CI gate + library test
wiring); runnable godoc `Example` functions (root + `semequal`); a committed
SemVer/stability policy (what v1.0.0 freezes); make every docs/README Go snippet
compile (a docs-compile test); framework recipes (pytest/vitest/LangChain/CrewAI as
docs + thin shims); a reusable **GitHub Action** (`action.yml`) wrapping the GHCR binary.

**21 — Release (OWNER-GATED — prepare, don't fire).** Verify `.goreleaser.yaml` +
`release.yml` are correct; document the exact owner steps (create `faisalkhan91/homebrew-tap`,
add `HOMEBREW_TAP_TOKEN`, confirm GHCR namespace); validate via
`goreleaser release --snapshot --clean` locally if available. **Do not** create
secrets or push a tag — leave a checklist and stop.

## Stop conditions

Stop and surface to the owner only if: a slice genuinely needs a new heavy
dependency (it shouldn't — schema-check is structural, both hard wire formats are
pure-Go), a constraint truly conflicts, or you reach slice 21's owner-only actions.
Otherwise pick the smallest-correct option, note it in the commit, and continue.

When 1–20 are done: run the full gate once more, update `CHANGELOG.md` +
`DECISIONS.md`, tick `FEATURE_COMPLETE_PLAN.md`, and report what shipped + the
slice-21 owner checklist.
