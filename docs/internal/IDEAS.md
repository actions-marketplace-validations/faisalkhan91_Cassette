# cassette — greenfield idea backlog

Output of a fan-out ideation pass (48 agents across 5 lenses — landscape, devex,
safety, ops, wildcard — each idea adversarially vetted for novelty + feasibility
against the engine, then deduped and ranked). These are candidates, not commitments.

## Themes

- **Offline eval, judging & datasets** — make the cassette corpus a byte-reproducible eval substrate (incl. the radical "the judge is itself a fixture").
- **Semantic review & diff ergonomics** — make re-recorded cassettes reviewable: signal-only semantic diffs, git textconv rendering, PR-ready changelogs.
- **Cost / token / latency analytics** — decode the byte-exact usage fields cassettes already preserve into deterministic, offline reports + a CI budget gate.
- **Debugging & corpus governance** — diagnose the replay miss (the worst VCR pain), per-turn microscopes, dead-fixture maps, fleet health gates.
- **Safety & egress auditing** — audit the literal bytes that would leave the process: typed PII/data-class detection, cross-turn taint tracing, signed behavior attestations.
- **Live-boundary ops** — the deliberate *inverse* of zero-dial replay (opt-in, network-touching): drift probes, deploy gates, sampled capture, consumer-driven pacts.
- **Distribution & multi-agent** — recordings as content-addressed shareable artifacts (`.cas`/OCI); stitch single-agent cassettes into multi-agent society replay.

## Top 10 (ranked)

| # | Idea | Effort | Why it's uniquely a cassette feature |
|---|------|--------|--------------------------------------|
| 1 | **`cassette eval`** — offline LLM-as-judge where the judge is *itself* a recorded fixture | M | promptfoo/DeepEval run *live* judges (flaky, costly, nondeterministic). cassette owns the bytes and can replay the judge offline → a provably zero-network, byte-reproducible LLM-judge eval. Structurally impossible elsewhere. |
| 2 | **`cassette diff`** — signal-only, review-grade semantic diff of two recordings | M | `git diff` on a re-recorded YAML cassette is a wall of id/usage/SSE-boundary noise. Only cassette's semequal volatile-normalization can surface the one behavioral line that changed. (`bisect` stops at the *first* divergence; review needs *all*.) |
| 3 | **`cassette doctor`** — diagnose a replay miss: nearest record, divergent axis, named fix | M | A miss today is an opaque `ErrNoMatch`. Nothing diagnoses *why* a live request failed to match the closest stored key. Turns the worst day-to-day VCR pain into a one-line fix, fully offline. |
| 4 | **`cassette cost`** — offline token/cost regression gate over a corpus | S | Cost tracking exists only as live SaaS proxies. Because cassette captured real `usage` byte-exact, "this agent's token footprint did not grow" becomes a deterministic unit test with no provider call. |
| 5 | **`egress-audit`** — typed PII/data-class detector bank over recorded outbound requests | M | `CanaryHits` needs you to *know* the secret string; `scrub` only redacts credential *shapes*. Neither finds *unknown* sensitive data the agent emitted (e.g. a user's SSN pasted into a search query) at the egress boundary cassette uniquely owns. |
| 6 | **`cassette gitconfig --install`** — git textconv driver rendering cassettes as transcripts | S | `git diff`/`log`/`show` render the human `doc` transcript instead of raw YAML SSE. `doc` already *is* the filter. No LLM VCR does this. |
| 7 | **`cassette watch`** — turn a golden cassette into a live provider-drift SLO probe | M | The sharp inverse of replay: re-issue recorded requests against *today's* live API and compare semantic digests under a `Policy` budget. The golden cassette is request fixture *and* baseline. (opt-in, network-touching) |
| 8 | **refusal oracle + `redteam` verdict-delta** — classify recorded final turns REFUSED/COMPLIED | M | A pure-function refusal classifier over `semequal.Transcript` is a fresh semantic layer; the `redteam` delta catches when a model upgrade silently starts complying where it used to refuse. |
| 9 | **`cassette dataset`** — queryable eval corpus derived from a directory of cassettes | M | No tool makes the recorded traffic *itself* the dataset with byte-exact provenance. The dataset rows and the deterministic replay fixtures are the *same* object. |
| 10 | **Behavior Attestation** — ed25519-signed manifest binding a cassette's semantic digests | M | SBOMs cover code, never model *behavior*. Stable per-turn digests make behavior content-addressable → "this agent refuses X and only calls tools {A,B}" becomes a cryptographically attestable, CI-verifiable claim. |

## Quick wins (S effort)

- **`cassette cost`** — decode captured `usage` + pluggable price table → offline token/cost CI gate (cost always present; latency only when `CaptureTiming` was on).
- **`cassette gitconfig --install`** — one command to wire the textconv diff driver (`doc` is already the filter).
- **`cassette explain --turn N`** — per-turn microscope: raw SSE frames + decoded `Transcript` + match key + canonicalized body + wire-shape tokens (pure assembly of existing engine outputs).
- **`cassette expect`** — co-locate the behavioral contract in the cassette (additive `omitempty` field) so `assert` runs zero-arg in CI and survives re-record (recorder merges prior `Expect`).
- **`cassette orphans`** — resolve cassette files against `Test*` functions to flag dead fixtures (and tests missing one).

## Deferred cleanups (from the new-implementation architectural review)

Verified-real but intentionally not done in the review pass (bigger or design-dependent;
the code is correct and gate-green as-is):
- **Split `internal/analysis`** — 27 files in one flat package (grew by 6 this session: otel,
  audit, policy, provenance, mcpdiff, mcpsteps). Consider subpackages once the shared primitives
  (Merge, Coverage, DecodeInteraction) are factored.
- **One corpus loader** — `loadCassettes` (single-level) vs `lintTargets`+`loadAll` (recursive) are
  used inconsistently; `up` adds a third path (`filepath.Glob`). A deliberate "one loader for
  path-aligned commands" pass. (otel was aligned to the recursive form in the review.)
- **`buildUp` parses the replay corpus twice** — `openForReplay(dir)` then `loadCassettes`+`Merge`
  for doctor-on-miss. Cleanest fix: have `openForReplay` also return the merged `*wirefmt.File`
  (touches the shared helper used by `serve`).
- **`mcp-diff` schema depth** — only top-level input properties are walked; recurse into nested
  object schemas and resolve `$ref` for a complete breaking-change check.
- **`mcpIsError` JSON-RPC `error` branch is effectively dead** — the MCP recorder stores only the
  `result` object, so a top-level `error` never appears and a now-protocol-error call has no recorded
  interaction for `callRegressions`. Either capture error frames in the recorder, or drop the branch
  and scope to tool-level `isError`.

## Deferred cleanups (from the round-2 cleanup/deadcode/refactor audit)

The DO-NOW items from this audit are committed (a `record()` data race fixed by writing the
response envelope before `appendInteraction`; strict fuzz-arg parsing; `decodeMCP` sharing
`mcpIsError`; dead `helpCommands`/`redteamUsage`/`style.note`/redundant `sort`+`if len==0` guards
removed; a tautological canon test replaced with a golden SHA-256 vector; helper dedup —
`canonDigestHex`→`canon.Digest`, `oneLine`→`truncate`, `containsString`→`slices.Contains`,
`stripStoredEncodingHeaders`, `Inv.strOr`, `newCassette`, `runPacked`→`serveUntilSignal`; and the
three bespoke arg parsers — bisect/orphans loops and `parseEditArgs` — folded into the shared
`parseOrUsage`/`FlagSet`). `deadcode -test` is clean (only the two intentional `semequal` public
asserts) and staticcheck is 0. These are the larger, design-dependent items left for a focused pass:
- **Split `semequal` into sub-areas** — the public package mixes the wire→Transcript decode, the
  volatile-normalization/canonicalization, and the equality/diff surface. Any split is an API-visible
  move (external consumers import `semequal`) so it needs a compatibility shim or a major bump — not a
  drive-by. Related to the `internal/analysis` split already noted above.
- **`CanonicalOrRaw` helper** — several call sites do the same "canonicalize JSON, fall back to raw
  bytes on invalid" dance inline before hashing/comparing. A single helper would centralize the
  invariant, but the call sites differ subtly (some already hold a `gjson.Result`); confirm they truly
  unify before extracting, or it becomes a leaky abstraction.
- **`digestJSON` dedup** — the per-turn digest and the diff path compute a canonical-JSON digest via
  near-identical inline sequences. Worth one shared `digestJSON([]byte) string`, but it sits on the
  hot replay-keying path — measure that the extra call/alloc is free before consolidating.
- **`reframe` head/tail split** — the reframing transform hand-slices a head and tail around the edited
  span with duplicated bounds arithmetic; a small `splitAround(idx, n)` would remove the off-by-one
  risk. Low value, low risk — bundle it with the next edit to that file rather than on its own.

## Notes from vetting (rejected, and why)

The vetters killed several plausible-sounding ideas as out-of-scope or incoherent against the engine — worth recording so they aren't re-proposed:
- **"Graft" / counterfactual replay** (edit one turn, re-run the agent forward) and **`distill`** (mine recordings into synthetic evals) both depend on a `Transcript → wire` *encoder* that does not exist (cassette only decodes wire → Transcript). Adding that encoder is its own large project and is the gating dependency for a whole cluster of editing ideas.
- **Adversarial-robustness / perturb-and-rescore** ideas contradict the core guarantee: replay has no live model in the loop, so perturbing a recorded *response* doesn't test model behavior. Any "score stability under perturbation" feature must run against a *live* endpoint (the `watch` lens), not replay.
