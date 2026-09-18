# cassette — feature research (round 2)

A second exhaustive ideation pass, run **after** the entire PLAN4 + Castiron
backlog shipped. This is the "note it before building" record: the methodology,
the headline finding, the reconciled ranked features, the dependency-ordered
build sequence, and the explicitly-rejected ideas (so they are not re-proposed).

These are candidates and a recommended order — not commitments.

## How this was produced

A background workflow fanned out **10 lenses** (protocols, proxy/adoption,
editing, eval, observability, multi-agent, devex, security, format, wildcard).
Each lens agent grounded itself with web search + the actual codebase and
proposed 6–9 concrete features (~**80 ideas total**). Every idea was then
**adversarially vetted** by a skeptical principal-engineer agent told to *kill*
it unless it was novel, feasible in pure offline Go with no heavy deps, not
already shipped, and a natural fit for the engine. Survivors were synthesized
**twice under different framings** — *adoption leverage* (most new users / unlocks
clusters first) and *moat* (most structurally impossible for competitors) — then
the two rankings were reconciled below. 93 agents, ~3.08M tokens.

## Headline finding

The original `IDEAS.md` ranked top-10 is **entirely shipped** (eval, diff,
doctor, cost, egress-audit, gitconfig, watch, refusal/redteam, dataset, attest),
plus graft / distill / branch / seeds / report from Castiron. Critically, the
**`wireenc` encoder** — which the old backlog called *"the gating dependency for
a whole cluster of editing ideas"* — now exists and is SDK-verified. That single
fact reframes the whole roadmap:

> cassette is no longer a *decode-only recorder*. With a round-trippable
> `Transcript ⇄ wire` codec, a recording is **editable, authorable, and
> transpilable source** — not just a read-only artifact. The strongest
> remaining features are the ones that consume that codec, plus the one feature
> that breaks cassette out of the Go-only world: a **record-side proxy**.

Verified against the tree while planning: `serve` is strictly replay-only
(404 on miss, holds no client), and there is **no `crypto/tls`/`x509` in project
source** — so a record-side proxy is genuinely net-new, and the reverse-proxy
(point your `base_url` at cassette) path avoids any MITM/CA machinery.

## Themes (merged across both framings)

- **Codec as authoring substrate** — the round-trippable encoder turns recordings
  into editable source: author a fixture from intent, transpile across providers,
  canonicalize for clean diffs. No decode-only VCR can do this.
- **Codec integrity is the brand** — the encoder is now load-bearing for
  graft/distill/branch; proving round-trip fixpoint + determinism *is* the
  credibility of everything built on it.
- **Any-language reach** — a record/replay reverse proxy converts a Go library
  into a universal sidecar usable from Python/TS/Ruby by changing only `base_url`.
- **Corpus governance at scale** — compose/merge/route many cassettes; one-pass
  lint gate; behavior-keyed coverage + minimization; cross-corpus dedup.
- **Egress-boundary ownership** — cassette holds the literal bytes in *and* out
  across the whole ordered conversation, so cross-turn taint/exfil-flow tracing
  lives only here (egress-audit finds typed PII per-request; taint follows flow).
- **MCP parity** — bring the recorded tool side up to the HTTP side's surface
  (analysis decoder + an stdio `mcp-serve`).

## The best features (reconciled, best-first)

| # | Feature | Effort | Nov | Imp | Why uniquely cassette |
|---|---------|--------|-----|-----|------------------------|
| 1 | **codec verify / conformance** — round-trip fixpoint + determinism proof; closed-loop "does this file actually replay?" | S | 4 | 5 | The encoder is the keystone every authoring feature stands on; only cassette has a `Transcript⇄wire` codec to verify. Hardens the byte-exact brand at the codec layer. |
| 2 | **author / compile** — build a valid multi-turn cassette from a terse YAML "screenplay" (no key, no network) | M | 5 | 5 | Removes the single biggest onboarding barrier (today you need a real key + real traffic first). No VCR can synthesize a byte-valid streaming fixture from intent — none owns an encoder. |
| 3 | **proxy** — record/replay/branch reverse proxy; any language captures by setting `base_url`/`HTTPS_PROXY` | L | 4 | 5 | `serve` is replay-only; nothing captures for non-Go clients. Converts the library into a universal sidecar. Naive proxies can't tee SSE frame-exact or scrub provider auth — cassette's transport already does both. |
| 4 | **port** — cross-provider transpile (decode→Transcript→encode into another dialect) | M | 5 | 4 | Take an Anthropic capture and hand an OpenAI client a byte-valid offline replay of the same behavior. Structurally impossible without the round-trip codec + provider-agnostic Transcript. |
| 5 | **canonicalize** — re-emit streams through the encoder with a zeroed envelope so re-records diff cleanly (`--check` gate) | S | 4 | 3 | Only the encoder can *re-emit* a canonical stream; `scrub` stamps fields in place but can't recanonicalize SSE framing. Kills re-record diff noise at the source. |
| 6 | **taint** — cross-turn exfil/injection flow trace (data in turn N's outbound that originated in earlier input) | M | 5 | 4 | Needs the decoded Transcript *and* byte-exact request bodies across the whole ordered conversation — exactly what cassette holds and a live scanner does not retain coherently. Deepest non-codec moat. |
| 7 | **stack / merge + directory routing** — compose cassettes (shared prefix + per-test tail), serve a directory with longest-prefix match | M | 3 | 4 | A cassette is one file today; suites need a reusable shared prefix + a fixture library. Stable match keys make union/route exact, not heuristic. Prereq for proxy directory replay. |
| 8 | **lint** — one-pass corpus health gate (orphans + expects + secret-scan + oversized + undecodable + attest-drift) → JSON + exit codes | S | 2 | 4 | Each check exists per-file today; teams need one CI front door over a whole corpus. Pure aggregation of shipped analyzers, zero network. |
| 9 | **coverage + shrink** — behavioral coverage matrix (tools/finish/refusal seen + gaps); minimal behavior-preserving corpus subset | S/M | 4 | 4 | Minimization keyed on *semantic* digests (not bytes) is unique to cassette's normalized Transcript. Turns "we have 4000 cassettes" into "these 120 cover every behavior." |
| 10 | **scenario matrix** — from one golden run, emit a family of valid edge/error fixtures (each finish_reason, truncation, malformed-but-legal args, injected errors) | M | 4 | 4 | Merges the shipped faults model with encoder authoring — only cassette has both a fault injector and a round-trip codec. |
| 11 | **MCP parity** — analysis decoder so doc/explain/diff/cost see MCP; `mcp-serve` exposes a recorded MCP server over stdio | M | 4 | 4 | `serve` is HTTP-only; MCP is invisible to the analysis subpackage. One decoder + one server makes the whole toolkit MCP-aware and hands non-Go clients a deterministic MCP server. |
| 12 | **fuzz** — generate many *valid, diverse* re-framings of a stream (split deltas, permute tool order) that all decode to one Transcript; test the consumer's parser | M | 4 | 3 | The inverse of `mutate` (which corrupts). Stress-tests the user's chunk-assembly with framings a single recording never produced. Needs the encoder. |
| 13 | **stitch/split · whatif · wire-attest** (polish cluster) — compose/decompose multi-agent cassettes; batch-graft what-if matrix; sign canonical wire bytes | S each | 3 | 2–3 | Smaller extensions that all stand on earlier primitives (encoder, canonicalize, attest, graft). |

## Recommended build sequence (the multi-hour plan)

Dependency-ordered; **each slice is independently shippable and gate-green**
(gofmt + vet clean, tests pass, coverage ≥ 80%, fully offline, no new heavy deps,
secrets never written). The encoder-trust slices come first because everything
authoring-related depends on them.

1. **Codec conformance** — `codec verify` (round-trip fixpoint + encode-determinism property test) and `conformance` (drive every recorded request through the replay core, assert stored digest). Foundational.
2. **canonicalize** (+ `--check`) — smallest encoder consumer; establishes the encode-write-back pattern (envelope handling, secret-refuse, rekey) reused downstream.
3. **author / compile** — screenplay YAML → multi-turn cassette; runs conformance internally before write. Flagship.
4. **port** — cross-provider transpile; reuses the encode-write-back + rekey + conformance plumbing.
5. **stack / merge + directory serve routing** — composition + longest-prefix routing; prereq for proxy directory replay.
6. **proxy** — record/replay/branch reverse proxy; reuses transport (record), serve handler (replay), ModeBranch (divergence), slice-5 routing. Network strictly opt-in; default replay stays zero-egress. Heaviest slice.
7. **lint** — one-pass corpus health gate with JSON findings + policy exit codes.
8. **taint** — cross-turn exfil/injection flow trace; reuses `internal/egress` detectors + `analysis.CollectTurns`.
9. **coverage + shrink** — behavior-keyed governance (coverage first, shrink consumes it).
10. **scenario matrix** — edge/error fixture families from one golden run (encoder + faults model).
11. **MCP parity** — analysis MCP decoder + `mcp-serve` stdio server.
12. **fuzz** — seeded valid-reframing generator (`wireenc.Reframe`) + `cassette fuzz`.
13. **Polish cluster** — `stitch`/`split`, `whatif`, `attest --wire`; ship piecemeal as time allows.

## Build status (PLAN6) — COMPLETE

All 13 slices shipped, each gate-green (offline, coverage ≥80%): codec verify +
conformance, canonicalize, author, port, merge + serve-dir, proxy, lint, taint,
coverage + shrink, scenario, MCP parity (decoder + mcp-serve), fuzz, and the
composition/counterfactual cluster (stitch/split + whatif). One scope adjustment:
**`attest --wire` was folded in rather than added** — the attestation manifest
already binds a per-turn `WireShapes` fingerprint, so wire-level tamper-evidence
beyond the semantic digest already exists; a separate canonical-byte signature
would have been redundant. `canonicalize` (slice 2) provides the canonical byte
form if a stricter wire attestation is wanted later.

## Rejected — do not re-propose (and why)

- **Generic `Transcript→wire` encoder as a feature** — already shipped as `internal/wireenc`. Propose *consumers* of it instead.
- **Live MITM/always-on capture daemon** — record mode + LiveTransport + branch already cover capture; a standalone always-on proxy adds a network-touching default path against the zero-dial brand. (The reverse-proxy in #3 is opt-in record only; default path stays replay.)
- **Cross-provider *request-shape* translation as a correctness claim** — re-emitting across dialects is fine for replay fixtures, but advertising "model equivalence across providers" is a misread of the byte-exact brand. Keep `port` scoped to "replay the same recorded behavior under another client."
- **Built-in LLM-based fixture generator / auto-redact via a classifier** — needs outbound network + nondeterminism in the default path. `author`/`scenario` synthesize from an explicit deterministic spec; `egress-audit`/`taint` use deterministic typed detection.
- **Web UI / dashboard** — `pack` already ships a self-contained offline browser transcript; a stateful server UI pulls away from the small CLI + curated API and adds non-Go assets.
- **Auto self-healing re-record on drift** — silently mutates the golden baseline and destroys reproducibility. `watch` (detect) + `graft`/`branch` (intentional edit) already cover it.
- **Plugin system for third-party decoders (dynamic loading)** — no safe offline Go plugin story without cgo/heavy deps; providers stay additive in-tree per DECISIONS.md.
- **Generic non-LLM HTTP VCR** — undifferentiated; `go-vcr` et al. own that. The moat is the LLM SSE/MCP decoders + the Transcript codec.
- **Embedding/vector similarity diff** — needs a model/heavy ML dep + nondeterminism; `semequal` digests + Policy text-distance already give a deterministic knob.
- **Binary fast-path format / mmap / shared body store / OCI push-pull / corpus index** — either premature optimization, or duplicate `dataset`/`pack`/`.castiron`; reconsider only when a real large-corpus pain shows up. Keep YAML-as-source-of-truth + git-diffability.
- **Perturb-and-rescore robustness over replay** — replay has no live model; perturbing a recorded response tests nothing. Must run live (the `watch` lens).
