# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **`otel` now emits the model for Gemini** (which carries the model in the URL path, not the body) and walks
  nested corpus directories like `audit`/`open`/`policy`; **`mcp-diff`** no longer false-flags a nullable-union
  type widening as a breaking change and now detects `additionalProperties` tightened to false; **`taint --fail-on`**
  honors repeated flags. (Surfaced by validating the new commands against real Claude Code + Codex recordings.)
- **empty `audit` / `mcp-diff` / `mcp-steps` arrays serialize as `[]`, not `null`** — stable JSON for consumers.
- **`conformance` no longer false-fails (and stops advising a corrupting `rekey`) on scrubbed-body recordings** —
  the match key is derived at record from the pre-scrub body, so when scrub later alters a key-relevant body
  region it isn't re-derivable from the stored body. Conformance now trusts the stored key (it's how live replay
  matches) and resolves replay via the index. Surfaced running cassette over real Claude Code / Codex captures.
- **Match normalization is persisted in the cassette** (a `match:` block with `volatile_json_paths`) — a cassette
  recorded with `--volatile` now reproduces its own keys, so replay/`conformance`/`lint` work without re-supplying
  `--volatile` (record leg only).
- **`proxy` now honors an upstream base path** — `--upstream https://host/llm-proxy` previously dropped the
  `/llm-proxy` prefix when forwarding (the gateway 404'd); the base path is now joined ahead of the request
  path (and a base query merged). Surfaced recording real Claude Code / Codex traffic through a gateway.
- **Gemini/Ollama token usage now decoded** — `cost`/`dashboard`/`--budget` previously reported 0 for every
  recorded Gemini conversation (the live API always returns `usageMetadata`) and the budget gate silently
  passed at zero cost. `DecodeUsage` now reads Gemini `usageMetadata.*` and Ollama `prompt_eval_count`/
  `eval_count`, and the streaming scan accepts NDJSON lines (not only SSE `data:` frames).
- **OpenAI streaming encoders now emit usage** when token counts are supplied (gated, so the zero-token
  default stays byte-identical), so authored OpenAI corpora exercise cost/dashboard like Anthropic/Gemini.
- **Sampling knobs no longer mislabel Gemini/Ollama turns as `anthropic`** — `SamplingKnobs` routed every
  non-OpenAI turn to the Anthropic default; it now derives the provider label and seed support from the
  canonical dialect registry.
- **`rekey` refuses to corrupt a scrubbed-body cassette** — recomputing a key from redacted bytes would
  overwrite the (replay-authoritative) pre-scrub key; `rekey` now refuses when that would change a scrubbed
  interaction's key, with `--force` to override and a `redact-field` remedy.
- **Partial `ScrubConfig` merges onto the defaults** instead of replacing them — setting one field no longer
  silently drops the default secret-header redaction and response-stamp determinism (a byte-stability fix).
- **`canonicalize` preserves `stream_timing` on turns it doesn't rewrite**, so a timing-faithful (`--pace`)
  recording survives `canonicalize`/`--check` untouched.

### Changed
- **Dependencies and Go toolchain updated across the board** — provider SDKs bumped to latest
  (`anthropic-sdk-go` v1.26.0→v1.73.0, `openai-go/v3` v3.41.0→v3.61.0, `modelcontextprotocol/go-sdk`
  v0.8.0→v1.8.0, plus `gjson`/`match`), vendored tree re-synced. The MCP SDK's 0.x→1.x transition and
  `golang.org/x/sync` require Go ≥1.25, so the `go` directive and every CI/Docker/release/action pin move
  from 1.24 to 1.27. No source changes to cassette's logic were needed; the streaming-error test now
  compares the provider error payload rather than the SDK's request-URL prefix (the newer Anthropic SDK
  embeds the URL, which legitimately differs between the live and `replay.invalid` legs).
- **Test helper moved out of the library into `cassette/cassettetest`** *(breaking)* — `New`, `Verify`,
  `AssertNoCanary`, and the `-update` flag now live in the `cassettetest` subpackage, so importing the
  core `cassette` library no longer pulls in `testing` or registers a global `-update` flag. Use
  `cassettetest.New(t, "")`.
- **`Mode` gained `ModeUnset` (iota 0)** — an explicitly set `Mode` (including `ModeReplay`) now wins over
  `CASSETTE_MODE`/`-update`; only an unset mode is resolved from the environment.
- **Provider dialects unified behind one internal registry** — URL routing, name, seed support, request
  path/upstream, request/response codecs, and request-body shaping all derive from a single table (no more
  parallel switches that let a Gemini turn be mislabeled). Behavior of `port`/`migrate` is byte-identical.
- **`cassette --help` opens with a curated "Start here" tier** (`up` · `open` · `proxy`/`serve` ·
  `verify`/`conformance`); `verify` signposts `conformance` for the replay proof; `mutate` documents the
  full op set + `-o` alias.

### Added — front door, ecosystem & governance
- **`cassette up`** — the zero-config front door: auto-picks a loopback port, records if the cassettes dir
  is empty else replays offline (ModeAuto at the directory grain), auto-detects the upstream from the first
  request's path, and **diagnoses a replay miss** (nearest turn + the field that broke the match key + a
  one-line fix) instead of a bare 404.
- **`cassette open`** — the unified "look at it" home: renders the offline HTML dashboard, opens it, and
  prints the transcript.
- **`--pace` (timing-faithful replay)** on `up`/`proxy` (and existing `serve`): capture per-frame SSE
  inter-arrival deltas when recording, re-emit them with those delays on replay (off by default for fast CI).
- **`cassette mcp-diff <old> <new> [--fail-on-breaking]`** — diff two MCP recordings' tool contracts and
  flag breaking changes (tool removed, input schema made stricter, success→error) — a CI gate for MCP servers.
- **`cassette mcp-steps <session.yaml> [--json]`** — walk a recorded MCP session step by step (the JSON-RPC
  method sequence, the tools/list roster, and each tools/call's args → result/error).
- **`cassette provenance <cassette.yaml> [--json]`** — show a derived cassette's lineage chain (the
  `port`/`migrate`/`graft`/`distill` operations that produced it, each with the source's behavioral digest),
  stamped automatically by the derivation commands and bound into the signed `attest` manifest.
- **`cassette otel`** — export a recording as OpenTelemetry GenAI spans (OTLP/JSON) for Langfuse / Phoenix /
  SigNoz; deterministic IDs, `--pace`-derived durations + time-to-first-chunk.
- **`cassette audit`** — a deterministic, offline compliance evidence bundle (behavioral manifest + coverage
  + outbound PII findings + cross-turn taint flows); `--fail-on` doubles it as a governance gate.
- **`cassette policy` + `cassette policy init`** — enforce a declarative `cassette.policy.yaml` (secrets /
  egress / exfil / budget / coverage) as one CI gate; `init` scaffolds a safe-by-default starter.
- **`cassette init --stack promptfoo`** — scaffold a promptfoo provider pointed at the replay endpoint, so
  promptfoo evals run deterministic and offline ([docs/recipes/promptfoo.md](docs/recipes/promptfoo.md)).
- **Portable cassette format spec v1** ([docs/spec/cassette-format.md](docs/spec/cassette-format.md)) — the
  normative, language-independent on-disk format + match-key algorithm, so a recording can be read/replayed
  from any language. Plus a [governance CI recipe](docs/recipes/governance.md).

### Changed (real-world)
- **`serve`/`proxy --volatile a.b,c`** — drop volatile request JSON paths from the match key (applied
  identically at record and replay), so a real agent CLI that stamps a per-session id into each request
  replays cleanly. See [docs/recipes/realworld.md](docs/recipes/realworld.md) (Claude Code: `metadata.user_id,system`;
  Codex: `client_metadata,prompt_cache_key`).
- **`author` screenplays accept per-turn `input_tokens`/`output_tokens`** to stamp the response usage block
  (Anthropic/OpenAI/Gemini; Ollama carries no telemetry by design).
- **`RekeyFile` → `RekeyPath`** *(breaking, but the old symbol was uncallable externally)* — match-key
  derivation moved to an internal package; the public entry point is now
  `cassette.RekeyPath(path string, cfg MatchConfig) error` (all-public types, no internal-type leak).
- **`serve --strict` → `--log-misses`** (`--strict` kept as a deprecated alias); `serve`/`proxy` now reject
  an `--on-miss` value other than `fail` instead of silently ignoring it.

### Added — differentiation & adoption
- **`cassette mcp-proxy -o <out> -- <server-cmd>`** — a stdio MCP **recording proxy**: spawns the real
  MCP server and sits transparently between client and server, forwarding JSON-RPC verbatim both ways
  while teeing each `tools/call`/`tools/list` (paired by id) into a cassette. Replay reuses `mcp-serve`
  (zero subprocess, zero network); tool results scrubbed on save. Reimplements no MCP semantics.
- **`kind: mcp` screenplay turns** — `cassette author` now compiles MCP turns (`tools/call`/`tools/list`,
  with `text`/`result`/`tool_error` variants) into replayable MCP cassettes whose match keys align with
  the recorder, so authored MCP corpora replay through `mcp-serve`. Ships a committed conformance corpus.
- **`cassette dashboard [dir] -o report.html`** — one self-contained, offline HTML report over a corpus
  (coverage matrix, token totals, per-model breakdown, refusal verdicts, tool usage, per-cassette
  transcripts). No server, no external assets, no timestamps — byte-stable and committable.
- **`cassette migrate <in|dir> --to <dialect> -o <out|dir>`** — ports a recording/corpus to a target
  dialect and **verifies every turn's semantic digest is preserved**, failing loudly on drift. De-risks
  a provider switch with your existing tests.
- **`cassette init` + recipes** — `init` scaffolds an offline-replay setup (cassettes dir, gitignore,
  proxy base-url snippet, stack-detected). Adds `docs/recipes/` (pytest `conftest.py`) and a reusable
  GitHub Action (`action.yml`: verify | lint | serve `--on-miss=fail`).
- **Positioning docs** — `docs/comparison.md` (a fact-checked "vs alternatives" matrix with caveats) and
  a README hero leading with the record-once/replay-byte-exact/SSE+MCP/secret-scrubbed wedge.

### Added — feature-complete: governance
- **`eval` scored verdicts (scorecard)** — a judge fixture may now carry `min_score`: the judge reply
  is decoded and a deterministic score extracted (`score: N`, or an `N/100` / `N out of 100` ratio),
  and the assertion passes iff `score >= min_score`. Backward-compatible with the substring
  `pass_marker`. Still zero-dial and byte-reproducible (the judge is a fixture).
- **`cassette serve <dir> --route-header X-Cassette-Test`** — per-test isolation: each request is
  routed by header value to its own cassette (`dir/<name>.yaml`), so concurrent or out-of-order tests
  can't consume each other's interactions; an unknown/missing test name is a miss (recorded by the CI
  gate). Pairs with `--on-miss=fail` for a hermetic, parallel-safe test fixture server.
- **`cassette serve`/`proxy --mode replay` `--on-miss=fail`** — the CI gate: every live request that
  matches no recorded interaction is recorded into a stable, versioned miss-manifest
  (`cassette-misses.json`, `--miss-manifest` to override) and the command exits nonzero on shutdown,
  so an uncovered LLM call fails the build. A non-Go team points one base-url env at the proxy and
  gets hard pass/fail. Library helpers in `cmd/cassette` (missRecorder + recordingOnMiss).
- **`cassette schema-check`** — validates that every emitted tool call's arguments conform to the
  JSON Schema the request advertised for that tool (Anthropic `input_schema` / OpenAI
  `function.parameters` / Gemini `functionDeclarations`; MCP `tools/list` `inputSchema`) — proving the
  model honored its own tool contract. Hand-rolled structural validation of the common subset (type,
  required, properties, additionalProperties:false, enum), no schema dependency, fully offline; nonzero
  exit on any violation. Library: `analysis.SchemaCheck`.

### Added — feature-complete: providers
- **Ollama native NDJSON dialect** (`/api/chat`) — `semequal.DecodeOllamaNDJSON` +
  `wireenc.EncodeOllamaNDJSON` decode/encode Ollama's line-delimited JSON stream (one object per line,
  not SSE): content concatenated across lines, `message.tool_calls[].function{name,arguments}`
  canonicalized, `done_reason` kept native, telemetry (eval_count/durations/created_at) dropped as
  volatile. Routed by `/api/chat`; wired through Encode/Decode, DecodeInteraction, ProviderName, port,
  and `provider: ollama` screenplays. Proves the Transcript model is framing-agnostic (NDJSON, not just
  SSE). (Ollama's OpenAI-compat endpoint is already covered by the OpenAI dialect.)
- **Google Gemini dialect** — `semequal.DecodeGeminiSSE` + `wireenc.EncodeGeminiSSE` decode/encode
  `streamGenerateContent`, accepting BOTH wire framings (the `alt=sse` event-stream and the default
  top-level JSON-array, via a pure-Go splitter tolerant of a mid-stream cut). functionCall args are
  canonicalized, thinking/citation parts dropped, finishReason kept provider-native. Routed by
  `ProviderForURL` (`:streamGenerateContent`/`:generateContent`), wired into `DecodeInteraction` and
  `port` (`--to gemini`, `provider: gemini` screenplays), with a real-wire round-trip test. One codec
  pair lights up port/coverage/author/graft/scenario/conformance/lint/eval for Gemini.

### Changed — maturation (identity, gate, security)
- **Module path is now `github.com/faisalkhan91/cassette`** (was the placeholder
  `github.com/example/cassette`); `go install`, Homebrew, and the GHCR image are real. A CI identity
  guard fails on any residual placeholder path.
- **`go install …@vX.Y.Z` reports the real version** (BuildInfo fallback); release builds also print
  commit/date.
- **Coverage gate now measures shippable logic** (excludes data/example/fixture packages) with a
  per-package floor, so no package hides behind the aggregate; `analysis/` is now `internal/analysis`
  (the public API is the root package only).
- **Security:** AWS/GitHub/JWT/Google credential shapes are now scrubbed from bodies AND rejected by
  the secret scanner; a write-time scan in `saveLocked` refuses to persist a secret that survived
  scrubbing (unless `DisableScrub`); `cassette proxy` binds loopback by default and warns on a
  non-loopback record/branch bind; `wirefmt.Load` caps file size; CI runs `govulncheck`.
- **New:** global `--no-color` flag; `SECURITY.md`, `CONTRIBUTING.md`, `.editorconfig`.

### Added — PLAN6 composition & counterfactuals
- **`cassette stitch` / `cassette split`** — compose several single-agent / tool-server recordings
  into one ordered society replay, and decompose a recording back into per-provider or per-tool
  cassettes. Library: `analysis.Stitch` + `analysis.Split`.
- **`cassette whatif`** — grafts a turn across a list of alternative values and reports, for each, the
  resulting combined semantic digest and how many downstream turns become counterfactual (their
  request still embeds the pre-edit output) — a one-shot what-if matrix (optionally a Markdown report)
  instead of N manual graft+inspect cycles.

### Added — PLAN6 cross-turn safety
- **`cassette taint`** — traces sensitive values that cross turns: a value that first appeared in an
  earlier turn (as user input, or as untrusted model/tool OUTPUT) and is then carried outbound in a
  later request. Where `egress-audit` finds typed PII per request, taint follows the flow and
  highlights output-sourced flows (potential exfiltration / injection relay). A report by default;
  `--fail-on <classes>` makes it a gate; masked samples only. Library: `analysis.Taint`.

### Added — PLAN6 corpus governance
- **`cassette coverage`** — behavioral coverage matrix over a corpus (tools called, finish reasons,
  providers, refusal verdicts, stream/unary), measured on the normalized Transcript axis; `--require`
  gates CI on the presence of named cells (is the corpus actually testing tool X / refusals?).
  Library: `analysis.Coverage`.
- **`cassette shrink`** — minimal behavior-preserving subset of a corpus: a greedy set cover keyed on
  per-turn SEMANTIC digests + tool/finish/provider/refusal coverage (not bytes). Turns "4000
  cassettes" into "these N cover every behavior"; `-o` copies the kept set. Library: `analysis.Shrink`.
- **`cassette lint`** — one-pass corpus health gate over a file or directory (recursive): secret
  patterns and stale match keys are errors; oversized bodies, undecodable or finish-less streams, and
  empty files are warnings. Exits nonzero on any error (or, with `--strict`, any warning); `--json`
  emits machine-readable findings. The CI front door for a fixture library. Library:
  `analysis.LintFile` + `analysis.Finding`.

### Added — PLAN6 MCP parity
- **MCP-aware analysis** — `analysis.DecodeInteraction` now decodes recorded MCP turns into the same
  Transcript model as HTTP (tool call → ToolCall + canonical args; result text → Text), so `doc`,
  `explain`, `dataset`, `diff`, and `cost` see the tool side of the agentic stack too — one decoder
  makes the whole toolkit MCP-aware.
- **`cassette mcp-serve`** — replays a recorded MCP session as a real stdio JSON-RPC server, so any
  MCP client talks to the recording with zero subprocess launch and zero network (the MCP analogue of
  `serve`). Reuses the in-process MCP replay, so matching is identical to record/replay.

### Added — PLAN6 any-language reach
- **`cassette proxy`** — runs cassette as a language-agnostic reverse proxy, so an app in ANY
  language records or replays by pointing its provider base URL at cassette (no Go, no SDK wrapper).
  `--mode replay` (default) serves a cassette/dir as an offline endpoint (404 on miss, zero egress);
  `--mode record` forwards to `--upstream` and tees each response into the cassette frame-exact (auth
  scrubbed on save); `--mode branch` replays the recorded prefix and goes live on the first divergent
  request. Network is opt-in (record/branch only); the replay path never dials. Reuses the recording
  transport's stream tee, the serve handler, and `ModeBranch`.

### Added — PLAN6 authored robustness
- **`cassette fuzz`** — the inverse of `mutate`: from one turn, generates many VALID, byte-different
  streams (text split across deltas at random boundaries, varied envelope ids/usage, shuffled
  independent tool order) that all decode to the SAME transcript, written as replayable cassettes — a
  property-test corpus that stress-tests the consumer's own SSE/agent parser. Deterministic per
  `--seed`. Library: `wireenc.Reframe` + `analysis.Fuzz`.
- **`cassette scenario`** — from one golden turn, emits a family of valid edge/error cassettes (each
  finish reason, truncated and empty text, malformed-but-legal tool args, an injected provider error)
  plus a self-checking `corpus.yaml` of expected semantic digests. Combines the encoder with authored
  faults; every variant replays offline. Library: `analysis.Scenario`.

### Added — PLAN6 authoring wedge (codec integrity first)
- **`cassette author`** — compiles a terse YAML "screenplay" (provider, model, and per-turn user
  prompt + desired assistant text/tool-calls/finish) into a fully valid, replayable multi-turn
  cassette via the `wireenc` encoder — no API key, no network, deterministic bytes. The from-scratch
  generalization of `graft`. Synthesizes a deterministic request body per turn (or use `request:`
  for byte-fidelity), computes match keys, verifies the result replays (conformance, zero dials)
  before writing, and never writes secrets. Library: `analysis.Compile` + `analysis.Screenplay`.
- **`cassette port`** — transpiles a recording's wire dialect (Anthropic Messages ⇄ OpenAI Chat ⇄
  OpenAI Responses): re-encodes each streaming turn into the target dialect and re-shapes the request
  path + body so a client built for that provider can replay the SAME recorded behavior offline. The
  per-turn semantic digest is preserved; unary/error turns are copied unchanged. A wire-dialect
  re-targeting, NOT a claim of cross-provider model equivalence. Library: `analysis.Port`.
- **`cassette merge` + `serve <dir>`** — union several cassettes (or directories of cassettes) into
  one fixture in argument order, dropping exact duplicates and reporting conflicts (same request,
  different response); a shared-prefix cassette first + per-test tails after gives a deterministic
  layered fixture. `cassette serve <dir>` now serves an entire directory as one logical endpoint
  (the match key routes by path+body). Library: `analysis.Merge`.
- **`cassette codec verify`** — proves the `Transcript⇄wire` codec is behavior-preserving and
  deterministic for every recorded STREAMING turn: decode→encode→decode reaches a stable transcript
  fixpoint and re-encoding is byte-identical. The encoder underpins graft/distill/branch and the
  upcoming authoring commands, so this hardens the keystone they all stand on. Library:
  `wireenc.Decode` (inverse of `Encode`) + `wireenc.RoundTrip`. Unary/error streams skipped; offline.
- **`cassette conformance`** — a closed-loop proof that a recording actually replays: every
  interaction's persisted match key must re-derive from the stored request (a stale key from a hand
  edit silently desyncs replay → `cassette rekey`) and must resolve through the in-process replay
  matcher to a response decoding to the stored transcript — all with ZERO outbound dials. Proactive
  complement to `doctor`. Library: `cassette.Conformance`.
- **`cassette canonicalize`** — re-emits every codec-preserving streaming response through the
  encoder with a zeroed envelope, so the volatile ids/usage and incidental SSE chunk boundaries
  collapse to a fixed shape and two re-records of the same behavior serialize byte-identically
  (`git diff` then shows only behavior changes). Preserves semantic digests, idempotent, leaves
  unary/error streams untouched; `--check` is a pre-commit gate, `-o` writes a copy; secrets never
  written. Library: `analysis.CanonicalizeFile`.

### Added — Castiron workflow (forward-branch, determinism, signed bug reports)
- **`ModeBranch` (forward re-run)** — replays a recorded (optionally grafted) prefix, and on the
  first request that diverges goes LIVE via `Options.Live`, records, and appends — so "edit a step
  and re-run forward" (which `graft` alone cannot do) becomes real. `cassette.LiveTransport(baseURL,
  auth)` points a path-only recording at the real provider and re-injects auth (scrubbed on save).
  `cassette branch <in> --from N [--graft …] -o seed.yaml` prepares the deterministic seed (the
  forward re-run itself is a library step — cassette sits on the RoundTripper and can't drive an
  arbitrary agent loop).
- **`cassette seeds`** — per-turn sampling-determinism report (seed / temperature=0), provider-aware
  (OpenAI Responses & Anthropic have no seed param), honest that determinism is best-effort.
- **`cassette report` + `.castiron`** — turn a failing recording into a committable, CI-runnable bug
  report (invariant violation or diff-vs-baseline), optionally a `.castiron/` bundle (cassette +
  signed attestation + `report.md` + `run.sh`). It re-runs the check, so it exits nonzero *while the
  bug is live* and goes green when fixed — "it failed once last Tuesday" becomes a regression test.

### Added — analysis, safety & counterfactual toolkit
- **Shared engine internals** extracted (`DecodeInteraction`, `CollectTurns`, `AxisDiff`) so the
  CLI features share one decoder/differ.
- **Review & debugging:** `cassette explain` (per-turn microscope), `cassette diff` (signal-only
  semantic diff — text word-diff, tool add/remove/args, finish flip, changed input axis, `--md`),
  `cassette doctor` (diagnose a replay miss → nearest record + changed axis + named remedy),
  `cassette dataset` (index recordings into a queryable jsonl/csv/table dataset), `cassette orphans`
  (dead/missing fixtures), `cassette gitconfig --install` (local git textconv transcript rendering).
- **Cost & safety:** `cassette cost` (byte-exact token/cost report + offline CI budget gate;
  usage-unknown is never silent zero), `cassette egress-audit` (typed PII/data-class detector bank
  with Luhn + entropy, policy-gated), `cassette expect` (embed a behavioral contract that `assert`
  runs zero-arg and that survives re-record), `cassette refusal` + `cassette redteam` (refusal/comply
  classifier + verdict-delta gate), `cassette attest`/`verify-attest` (ed25519-signed behavioral SBOM
  that passes benign re-records and fails behavior changes), `cassette eval` (assertion suites with an
  offline **LLM judge that is itself a replayed fixture** — zero-dial, bit-reproducible).
- **Counterfactuals & live:** `internal/wireenc` — a **Transcript→wire encoder** (the inverse of the
  decoders; round-trips `Decode(Encode(t))==t.Normalize()` across all providers) — which unblocks
  `cassette graft` (rewrite one turn's output as a counterfactual, with a downstream-inconsistency
  warning + `--truncate-after`) and `cassette distill` (parameterize one turn over many values into a
  self-checking eval corpus). `cassette watch` is the opt-in inverse of replay: re-issue recorded
  requests against a LIVE provider and compare under a drift `Policy`.
- Per-command `--help` for every subcommand (drift-guarded by a test).

### Added
- **`cassette pack <cassette.yaml> -o demo`** — compile a recording into one
  self-contained, zero-network executable that serves an interactive **steppable
  browser transcript** plus the serve API. No Go toolchain at pack time: the
  cassette is appended to a copy of the running binary behind a fixed trailer and
  detected at startup. Adds `cassette.OpenBytes` (replay a cassette from memory).
  Refuses to embed secrets; re-bases (never nests) on re-pack; same OS/arch only.
- **`cassette serve <cassette.yaml> [--addr][--pace][--log-misses]`** — expose a
  recording as a real local HTTP endpoint speaking each provider's wire protocol
  (Anthropic/OpenAI/Responses, routed automatically), so any client/language gets
  deterministic, key-less, zero-network replay. In Go: `c.Handler(ServeOptions{})`.
- **Chaos / resilience** — `cassette mutate` and the `mutate` package derive seeded,
  hostile-but-plausible SSE variants (truncation, dropped terminals, duplicated/
  reordered frames, corrupt tool args, multibyte splits). `Options.Faults` injects
  synthetic failures (e.g. 429→200 retry) or stream corruptions at replay time
  **without mutating the cassette**.
- **Behavioral equivalence & safety** — `semequal.Policy` (per-field drift tolerance:
  tool calls exact, prose within a budget), `semequal` invariants + `cassette assert`
  (`--no-tool`, `--no-duplicate-tools`, `--finishes-clean`), and `CanaryHits` /
  `AssertNoCanary` (flag planted tripwire tokens that leaked into an outbound request).
- **Drift intelligence** — `cassette bisect <a> <b>` localizes the first divergent
  turn and the changed request-input axis; `semequal.WireShape` fingerprints provider
  wire grammar (volatile-value-insensitive); `Timeline` is an append-only digest ledger.
- **`cassette doc <cassette.yaml> [--check doc.md]`** — render a recording as a
  Markdown transcript; `--check` fails if the committed doc is stale (living docs).
- OpenAI **Responses API** record/replay: `semequal.DecodeOpenAIResponsesSSE` byte
  decoder + `otrans.FromResponse` adapter + record/replay tests (streaming, tool-call
  fragments). No core changes — the transport is provider-agnostic.
- `match.PathTransform` hook and `match.AzureConfig()` preset (collapse the Azure
  deployment path segment so one cassette replays across deployments).
- Colorized, aligned `cassette` CLI output (TTY-gated; plain when piped).

### Fixed
- A 30-agent adversarial audit of the new features confirmed and fixed:
  - `bisect` now compares **unary (non-streaming) JSON** responses on the canonicalized
    body — previously two different answers both decoded to an empty transcript and
    `bisect` reported "no divergence".
  - `Verify` no longer fails forever when an unbounded synthetic fault (`Attempt: 0`)
    intentionally shadows a recording on every attempt.
  - `CanaryHits` also scans header **names** and the request **URL path** (not just
    body + header values); query strings remain out of scope (path-only match key).
  - `cassette doc` truncates on a **rune boundary** (no invalid UTF-8 in summaries).
  - `semequal.WireShape` derives a grammar token from OpenAI **Chat Completions** chunks
    (`delta.role`/`content`/`tool_calls`/`finish`) instead of collapsing them all to `data`.
- Scrub now redacts `X-Goog-Api-Key` (Gemini) and AWS SigV4 token/date headers,
  closing a provider secret-leak gap.
- `canon` volatile-path drop and `redact-field` now handle array-indexed JSON paths.

## [0.2.0] - 2026-06-22

### Added
- **OpenAI provider** (Chat Completions) record/replay via the real `openai-go/v3`:
  streaming, fragmented tool-call argument reassembly, and 429→200 retry — mirroring
  the Anthropic path. Includes an OpenAI north-star demo (`examples/agent-demo-openai`).
- **Public `semequal` package**: standalone byte-level SSE→Transcript decoders
  (Anthropic + OpenAI, no provider SDK), `Digest`/`Diff`, and `AssertSemanticEqual` —
  the SEMANTIC-equivalence yardstick is now a first-class API. `internal/atrans`
  (and new `internal/otrans`) are thin SDK→Transcript adapters.
- **Pluggable matching**: `match.Config.BodyTransform` and `KeyFunc` hooks (wired
  through the HTTP and MCP key paths; `KeyFunc` is HTTP-only).
- **Editing CLI**: `cassette prune` / `rekey` / `redact-field`, plus an exported
  `cassette.RekeyFile` helper that recomputes persisted match keys (HTTP + MCP).
- **Opt-in stream timing**: `Options.CaptureTiming` records per-frame inter-arrival
  deltas; `Options.PaceStreaming` re-emits them on replay (context-cancel aware).
  Default off, so cassettes stay byte-stable.

[0.2.0]: https://github.com/faisalkhan91/cassette/releases/tag/v0.2.0

## [0.1.0] - 2026-06-18

### Added
- Record/replay HTTP transport via a custom `http.RoundTripper` injected through
  `option.WithHTTPClient`; request-body re-read/restore (incl. `GetBody`).
- Verbatim SSE streaming capture (tee, never `ReadAll`) and byte-identical replay;
  full synthetic responses with Content-Type-driven decoder dispatch.
- Dial-blocking replay transport + atomic dial counter (provable zero egress);
  loud cassette misses (no silent passthrough); context cancellation honored.
- Deterministic match key (`MatchKey`) used identically at record and replay:
  canonical JSON (sorted keys, int/float preserved), volatile-field normalization,
  gzip/identity equivalence, multipart boundary exclusion.
- Ordered/duplicate replay via per-key cursor with `error|repeat_last|cycle`
  exhaustion policy; concurrency-safe.
- Deterministic, git-diffable YAML cassette format; byte-exact body round-trip
  (text when UTF-8-safe, base64 otherwise); atomic writes through the scrubber.
- Secret scrubbing (headers + bodies) and volatile response-field stamping; a
  secret scanner reused by the CLI and CI.
- Idiomatic test helper: `New`, `HTTPClient`, `MCP`, `Verify`; modes
  replay/record/auto with `-update`.
- MCP record/replay wrapper over `MCPCaller` (satisfied by `*mcp.ClientSession`),
  with a real-subprocess stdio proof and an in-memory unit test.
- Embedded real-wire SSE fixtures + in-process fake provider (adversarial framing,
  fragmented tool calls, mid-stream error, 429→200 retry).
- `cassette` CLI: `inspect`, `scrub` (idempotent), `verify`, with defined exit codes.
- North-star demo is a realistic **multi-tool agent**: a barista assistant makes
  five sequential tool calls (list_menu, get_price, check_inventory, caffeine_mg,
  brew_minutes) then recommends a drink, replayed from REAL captured Claude
  streams; proves live↔replay semantic equivalence across six turns against a
  committed golden SHA, with volatile-field divergence and zero dials.
- Single offline CI gate (`scripts/ci.sh`) and GitHub Actions workflow; vendored
  dependencies; MIT license.

### Fixed
- **Security:** `Open` now installs `scrub.DefaultConfig()` by default (secrets-by-
  default); previously only the `New` test helper scrubbed, so a direct `Open`
  caller could write a real API key to disk. Opt out with `Options.DisableScrub`.
- Replay matching is now independent of body scrubbing and gzip request encoding:
  the match key is persisted at record time (`request.match_key`) and computed
  over the live, gunzipped, pre-scrub request. Fixes `ErrNoMatch` on a fresh
  replay process when a request-body secret was scrubbed or the body was gzipped.
- Streaming capture uses a mutex-guarded tee so a concurrent `Close()` during a
  `Read()` cannot race the capture buffer.

### Scope
- Anthropic HTTP path and MCP stdio. OpenAI is a documented additive follow-up
  (the SDK is not fetchable offline in this environment); see DECISIONS.md.

[0.1.0]: https://github.com/faisalkhan91/cassette/releases/tag/v0.1.0
