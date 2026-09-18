# cassette — the feature-complete plan

The definitive scope to take cassette from "mature, 51 commands" to **feature
complete**. Produced by decomposing the product into 7 capability pillars (one
agent per pillar defined the done-bar + acceptance criteria), then reconciling a
disciplined-1.0 framing and a maximal framing. This plan adopts the **maximal
in-scope set** (you asked for *complete*), and marks the disciplined 1.0 line as a
milestone inside it.

## What "feature complete" means

cassette is feature complete when it credibly does six things, **offline and
byte-deterministically**:

1. **Decodes/re-encodes every text+tool-call wire dialect a real agent stack is
   realistically pointed at** — proving the `Transcript` model is framing-agnostic
   across SSE, line-delimited NDJSON, the streaming JSON-array framing, and binary
   eventstream — plus the embeddings/JSONL shapes the Transcript can't otherwise express.
2. **A complete deterministic eval stack** (scored verdicts, ensembles, a corpus
   regression suite with a baseline gate, A/B, stats) — every verdict reproducible
   from stored judge fixtures (the moat: judging is a fixture, not a live call).
3. **A governance surface that proves three properties offline** — secrets never
   leak, behavior is signed/tamper-evident, and emitted tool args validate against
   the advertised schema — all reachable through one policy-gated `lint` front door.
4. **A real CI citizen for non-Go teams** — `--on-miss=fail` + a stable
   miss-manifest, per-test isolation, an `init` scaffolder, a GitHub Action, and
   tested framework recipes, all via the any-language proxy.
5. **Determinism honesty** — the concurrency it owns is diagnosed and documented;
   sampling reproducibility is a corpus-wide CI gate.
6. **An honest 1.0 library + release** — no internal-type leaks in the public API,
   runnable godoc Examples, a SemVer/stability policy, compiling docs, and a real
   published tag.

All under the standing constraints: pure Go, offline/vendored CI, no new heavy
deps, zero default-path egress, deterministic byte-exact output, secrets never
written, gofmt+vet clean, coverage floors held.

## Pillar status (all currently *partial*)

| Pillar | Today | Gap to complete |
|--------|-------|-----------------|
| Providers & modalities | 3 SSE dialects + MCP | Gemini, Ollama, Bedrock, Cohere, OpenAI-compat family, embeddings/JSONL |
| Eval & scoring | one judge + substring pass | scored verdicts, ensemble, suite+baseline, A/B, stats, embed-assert |
| Contracts & governance | scrub/egress/taint/attest/refusal | `schema-check`, lint policy-gates, attest data-class binding, trust-boundary doc |
| Corpus intelligence | flat `dataset` + `coverage` | `dataset --query` grammar + columns, `stats` |
| CI & ecosystem | proxy/serve replay | `ci-guard` (--on-miss=fail + manifest), per-test routing, `init`, Action, recipes |
| Determinism depth | model is byte-deterministic; `seeds` | concurrent same-key ordering diagnostic, corpus-wide seeds gate |
| DX / 1.0 polish | goreleaser config, internal `analysis` | close API internal-type leaks, godoc Examples, SemVer policy, release |

## Execution status — 7 of 20 autonomous slices shipped (each gate-green, committed)

- ✅ **Slice 1 — Gemini dialect (dual-framing)** — decoder+encoder+routing+port+round-trip.
- ✅ **Slice 2 — OpenAI-compatible host family** — tested+documented routing guarantee.
- ✅ **Slice 3 — schema-check (HTTP + MCP)** — tool-arg contract conformance.
- ✅ **Slice 4 — ci-guard** — `serve`/`proxy --on-miss=fail` + versioned miss-manifest + nonzero exit.
- ✅ **Slice 5 — per-test routing** — `serve <dir> --route-header` isolates a cassette per test.
- ✅ **Slice 6 — scorecard** — scored judge verdict + `min_score` threshold (eval extension).
- ✅ **Slice 12 — Ollama native NDJSON** (`/api/chat`) — line-delimited framing; proves framing-agnostic.
- ⏳ **Remaining (autonomous):** 7 (judge ensemble), 8 (suite), 9 (ab), 10 (dataset --query), 11 (stats),
  13 (Cohere v2), 14 (embeddings/JSONL), 15 (embed-assert), 16 (Bedrock eventstream), 17 (governance
  fold-in), 18 (determinism diagnostics), 19 (public-API leak fix), 20 (1.0 docs/onboarding).
- 🔒 **Slice 21 — release** is owner-gated. Continue via
  [`FEATURE_COMPLETE_BUILD_PROMPT.md`](FEATURE_COMPLETE_BUILD_PROMPT.md) (`/loop`) or directly.

## The build sequence (dependency-ordered; each slice gate-green)

Providers share one **dialect-adding pattern** — `wireenc.Provider` + `ProviderForURL`
routing → `semequal.Decode<X>` → `wireenc.Encode<X>` → `DecodeInteraction` wiring →
`port.go` cases → a real-wire fixture + a `codec verify` round-trip test. Gemini
establishes it; the rest are cheap.

1. **Gemini dialect** (dual-framing: `alt=sse` + default `streamGenerateContent`
   top-level JSON-array) — establishes the pattern; introduces JSON-array framing. **L**
2. **OpenAI-compatible host routing** — verify+document+test the family (Azure
   deployment paths, vLLM/Together/Groq/OpenRouter/Mistral/DeepSeek) resolves to the
   OpenAI dialects. No new codec. **S**
3. **schema-check** (HTTP + MCP) — validate recorded tool-call args against the
   schema advertised in the request `tools[]`; hand-rolled structural validation, no
   schema dep; `--fail-on`. **M**
4. **ci-guard** — `--on-miss=fail` + a stable, versioned miss-manifest on `serve`
   AND `proxy --mode replay`; nonzero exit. **M**
5. **Per-test routing / isolation header** — a request header (or path prefix)
   selects a per-test cassette in a served directory. **M**
6. **scorecard** — scored-verdict + threshold extension to `eval` (numeric score,
   byte-stable extraction contract, `--min-score`). **M**
7. **Judge ensemble / panel** — N judge fixtures with a fixed reduction
   (majority/mean/min), folded into `eval`/`suite`. **M**
8. **suite** — corpus regression runner with a persisted scored baseline + drift
   gate + per-turn/windowed scope. **L**
9. **ab** — A/B win/loss aggregate over two frozen corpora (beyond pairwise `diff`). **L**
10. **dataset --query** — frozen boolean/relational grammar (fix the silent
    no-match foot-gun) + provider/MCP/system_digest columns. **M**
11. **stats** — group-by aggregation over the projection. **S**
12. **Ollama native NDJSON** (`/api/chat`) — line-delimited framing; telemetry as
    Envelope-volatile. **M**
13. **Cohere v2** (`/v2/chat`) dialect. **M**
14. **Embeddings codec + JSONL/array results decode** — the structured-output
    shapes the Transcript can't express; decode + `verify` (no batch lifecycle). **M**
15. **embed-assert** — in-cassette vector-relation contract (A nearer B than C),
    checked by `assert`; the relation, not raw float compare. **M**
16. **AWS Bedrock eventstream codec** (Claude-on-Bedrock) — pure-Go
    `vnd.amazon.eventstream` binary-frame parser over the Anthropic semantic path;
    SigV4 is out of the match key. The hard slice, deliberately near-last. **L**
17. **Governance fold-in** — lint opt-in policy-as-code gates (allowed tools/egress
    hosts/data classes), bind the egress/taint data-class profile into the `attest`
    manifest, trust-boundary doc. **M**
18. **Determinism honesty** — concurrent same-key replay-ordering diagnostic +
    documented order-by-record contract; corpus-wide `seeds` reproducibility gate. **M**
19. **Public API leak fix** — remove internal-type leaks (`Options.Match`/`Scrub`,
    `RekeyFile`) from the root surface; an external-module compile test + a CI grep
    guard. **L**
20. **1.0 docs & onboarding** — `init` scaffolder, runnable godoc Examples (root +
    semequal), SemVer/stability policy, every docs/README Go snippet compiles,
    framework recipes (pytest/vitest/LangChain/CrewAI), a reusable **GitHub Action**
    wrapping the offline binary. **M**
21. **Release** *(owner-gated)* — wire `HOMEBREW_TAP_TOKEN` + tap repo + GHCR
    namespace, `-rc` dry-run of release.yml, cut the first real tag. **M**

### The disciplined 1.0 milestone (a valid stopping line)

Slices **1–6, 8, 10–12, 17–21** form a self-respecting 1.0 ("nothing important
missing"). The items that push to "maximal complete" but aren't strictly required
for a credible 1.0: **ensemble (7), ab (9), Cohere (13), embeddings/JSONL (14),
embed-assert (15), Bedrock (16)**. They're in-scope and we will build them — this
is just the line you can ship from if priorities change.

## Non-goals (explicitly out — do not build)

- **Realtime/voice WebSocket duplex; vision/image bytes; gRPC/protobuf** — different
  transports/modalities the `{Role,Text,ToolCalls,FinishReason}` Transcript can't
  model; a different product.
- **Batch async submit→poll lifecycle** — only the JSONL results *shape* is net-new
  (slice 14); no lifecycle state machine.
- **Reasoning/thinking traces as first-class fields** — deliberately dropped at decode.
- **Live/online judging, embedding-similarity or perturb-and-rescore scoring,
  statistical-significance/Elo** — need a live model or heavy ML dep, or overclaim a
  sampling distribution from a single frozen draw.
- **Native client SDKs (pip/npm), a plugin system, a hosted dashboard/web UI, a MITM
  CA daemon, an open query engine (SQL/JOINs)** — violate pure-Go/thin-shim/offline
  or duplicate `pack`/`dataset`.
- **App-loop nondeterminism (clock injection, goroutine-interleaving capture, flaky-run
  detection, retry policy)** — lives above the transport; cassette is an
  `http.RoundTripper`, not the agent loop. (We diagnose the concurrency we *own*; we
  don't drive the loop.)
- **Promoting `wireenc`/`wirefmt` to a public package; cosign/SBOM; non-GitHub CI
  shims; `pact`; `sidecar` tags; standalone `dedupe`/`query`/`panel` commands** —
  deferred/folded per prior DECISIONS + this analysis.

## Global done-definition (feature complete when ALL hold)

1. Every targeted dialect (Anthropic, OpenAI Chat/Responses, **Gemini dual-framing,
   Ollama NDJSON, Cohere v2, Bedrock eventstream**, the OpenAI-compatible host
   family) and the **embeddings + JSONL** shapes decode to `Transcript` and round-trip
   green in `codec verify` on a real-wire fixture (text + tool-use).
2. The eval stack is complete and zero-dial: scored verdict + threshold, ensemble,
   `suite` with a persisted baseline-drift gate + per-turn scope, `ab`, `stats`,
   `embed-assert` — all reproducible from stored judge fixtures.
3. Governance proves all three properties offline (no secret leak; signed +
   data-class-bound attestation; `schema-check` HTTP+MCP), reachable through one
   policy-gated `lint`, each guarantee documented.
4. CI citizen: `--on-miss=fail` + versioned miss-manifest on serve & proxy,
   per-test isolation, `init`, a GitHub Action, tested pytest/vitest/LangChain/CrewAI
   recipes — a non-Go team scaffolds, points one base-url env at the proxy, and gets
   hard pass/fail.
5. Determinism honesty: same-key concurrent ordering detected + documented; `seeds`
   enforceable corpus-wide.
6. Honest 1.0 library: root+semequal API self-contained (external-module compile
   test + CI grep guard), pkg.go.dev renders runnable Examples, a committed SemVer
   policy, every docs snippet compiles.
7. Release fired: tap+token+GHCR wired, `-rc` dry-run, a real tag whose `go install`
   / `brew install` / `docker pull` all succeed; CHANGELOG dated and closed.
8. No regression to the hard constraints; per-package + aggregate coverage floors held.
