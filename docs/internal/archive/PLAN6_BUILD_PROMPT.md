# PLAN6 build prompt — the authoring + any-language-reach wedge

> Paste this as the task for a multi-hour autonomous build loop. It implements
> the reconciled roadmap in [`FEATURE_RESEARCH.md`](FEATURE_RESEARCH.md), one
> independently-shippable, gate-green slice at a time. Theme: **a recording is
> now editable, authorable, transpilable source — and cassette escapes Go.**

---

## Your mission

Build out the PLAN6 feature wedge for **cassette** (the Go record/replay VCR for
LLM HTTP/SSE + MCP). Work the slices below **in order**. After **each** slice:
commit it separately, keep `scripts/ci.sh` green, and move on. Do not stop
between slices to ask for approval — this is an autonomous loop. Keep going until
all slices are done or you are blocked on something only the user can decide.

Loop discipline, every slice:
1. Implement the smallest correct version that fully satisfies the slice.
2. `gofmt -w` the changed files; `go build ./...`; `go vet ./...`.
3. Add tests (table-driven, offline). Run `bash scripts/ci.sh` — it must exit 0
   with **coverage ≥ 80%**.
4. Update `README.md` / `docs/*` / `CHANGELOG.md` / `DECISIONS.md` as warranted,
   and wire any new command into the registry + `help_test.go`.
5. Commit with a focused message (see "Commit hygiene"). Then start the next slice.

If a slice turns out to be already partly built, or a better factoring emerges,
**update `FEATURE_RESEARCH.md`** to reflect reality and proceed — the plan serves
the code, not the reverse.

## Non-negotiable constraints (these define the product)

- **Pure Go, fully offline CI.** `GOFLAGS=-mod=vendor`, `GOPROXY=off`. **No new
  heavy dependencies.** Stdlib only for new work (net/http, net/http/httputil,
  crypto/ed25519 already vendored). If you think you need a dep, stop and ask.
- **Zero outbound network in the default path.** Replay/analysis must never dial.
  Anything network-touching (only the proxy's record/branch modes here) must be
  **explicitly opt-in**, mirror the `watch` precedent, and be impossible to
  trigger from the default path.
- **Deterministic + byte-exact is the brand.** Same input → identical bytes. No
  `map`-iteration-order leaks into output, no timestamps/random in serialized form.
- **Secrets are never written.** Run the existing secret scan before any cassette
  is written to disk (record, author, port, canonicalize, scenario, proxy-record).
  A detected secret aborts the write.
- **Coverage floor 80%.** `gofmt` + `go vet` clean. Small curated public API:
  root package stays a tiny VCR surface; analysis/derived logic goes in
  `analysis/` (or a new internal package), not the root.
- **Commits:** no `Co-Authored-By` / AI footer. One slice per commit. Conventional
  commit prefixes (`feat:`, `feat(cli):`, `test:`, `docs:`, `refactor:`).

## What already exists (reuse, do not rebuild)

Engine: provider-agnostic transport / matcher (`match.Key()`) / scrubber /
`canon` / `semequal` (volatile-normalized semantic digests) / `wirefmt`+`wirefix`
/ egress guard. Decoders for Anthropic Messages, OpenAI Chat, OpenAI Responses
(all SSE) + MCP stdio. **`internal/wireenc` is a working, SDK-verified
`Transcript → Anthropic-wire SSE` encoder** (the keystone). `analysis/` exports
`DecodeInteraction`, `CollectTurns`, `JudgePrompt`, `RequestModel`, `AxisDiff`,
plus the cost/refusal/attest/seeds/usage logic. CLI is a declarative `Command`
registry in `cmd/cassette/commands.go` + a shared flag parser in `cli.go`.
`serve` is **replay-only** (404 on miss, holds no client). `ModeBranch` +
`LiveTransport` already do forward-branch graft. ~35 commands already ship — read
`FEATURE_RESEARCH.md` "Headline finding" and do not duplicate them.

---

## The slices

### Slice 1 — Codec conformance (foundation) ✅ DONE
Prove the encoder keystone before building on it.
- `cassette codec verify <cassette.yaml> [--json]`: for every recorded streaming
  turn, assert `decode(encode(decode(bytes)))` reaches a **stable Transcript
  fixpoint** and `encode` is **deterministic** (same Transcript+Envelope →
  identical bytes). Report any non-preserving turn and classify why (lossy field /
  ordering / canon drift).
- `cassette conformance <cassette.yaml> [--json]`: drive every recorded **request**
  through the in-process replay/match core and assert each response decodes to the
  **stored semantic digest** — a closed-loop "this file actually replays" proof
  (catches hand-edits that broke match keys or framing).
- Library: `wireenc.RoundTrip(...)` helper + a property/table test in
  `internal/wireenc` and `semequal`.
- *Done when:* both commands ship, registered, help-tested; conformance passes on
  the repo's existing fixtures.

### Slice 2 — canonicalize ✅ DONE
- `cassette canonicalize <cassette.yaml> [--check] [-o out.yaml]`: re-emit each
  streaming turn through the encoder with a fixed/zeroed `Envelope` so two
  re-records of the same behavior produce **byte-identical** files. Preserve
  semantic digests. `--check` exits nonzero if the file isn't already canonical
  (a pre-commit/CI gate). Refuse to write if the secret scan trips.
- Establishes the **encode-write-back pattern** (envelope handling, secret-refuse,
  rekey) that slices 3–4 reuse. *Done when:* idempotent (`canonicalize` twice ==
  once), digests preserved, `--check` round-trips.

### Slice 3 — author / compile (flagship) ✅ DONE
- A small declarative **screenplay** schema (YAML): provider, model, and ordered
  turns — each turn a user prompt + desired assistant text / tool calls /
  finish_reason, plus the matching request body or a per-turn request template.
- `cassette author <screenplay.yaml> -o fixture.yaml`: compile through `wireenc`
  into a fully valid, replayable **multi-turn** cassette; synthesize request
  bodies that match `match.Key()`; recompute keys (reuse `rekey`); run `conformance`
  internally before writing; secret-scan before write.
- *Done when:* a 2–3 turn fixture authored from scratch (no key, no network)
  replays with **zero dials** and round-trips through the decoders. Add an
  `examples/` screenplay + a golden test.

### Slice 4 — port (cross-provider transpile) ✅ DONE
- `cassette port <in.yaml> --to anthropic|openai-chat|openai-responses [-o out.yaml]`:
  `decode → Transcript → encode` in the target dialect; rewrite request URL /
  content-type / shape; recompute keys; preserve semantic digests.
- Scope it honestly: this re-emits the *same recorded behavior* under another
  client's wire dialect for offline replay — **not** a claim of model equivalence
  across providers (see the rejected list). Note that in `--help` + docs.
- *Done when:* an Anthropic capture ported to OpenAI-chat replays and decodes to
  an identical Transcript digest. (You may need a matching `wireenc` emitter for
  the OpenAI dialects — build it minimally, behind the same encoder interface.)

### Slice 5 — stack / merge + directory serve routing ✅ DONE
- `cassette merge a.yaml b.yaml ... -o out.yaml`: union interactions with
  **exact-key de-dup** and conflict detection; deterministic ordering.
- A shared-prefix layering API (a reusable "login/handshake" prefix under per-test
  tails).
- `serve` (and the proxy in slice 6) gain a **directory mode**: serve a dir of
  cassettes as one logical endpoint with **longest-prefix request routing**.
- *Done when:* a merged fixture serves both sources; dup detection works; routing
  is deterministic. This is the prerequisite for proxy directory replay.

### Slice 6 — proxy (any-language reach; heaviest slice) ✅ DONE
- `cassette proxy --upstream <URL> --mode record|replay|branch -o cassette.yaml [--addr :8080]`:
  a **reverse proxy** (stdlib `net/http/httputil`) so any language records/replays
  by pointing its `base_url` (or `HTTPS_PROXY`) at cassette — **no MITM/CA**
  (there is intentionally no `crypto/tls` in project source; reverse-proxy avoids it).
  - **record:** proxy → upstream, tee the response (SSE **frame-exact**), scrub
    provider auth, write to the cassette. Opt-in network; this is the only
    network-touching path in PLAN6.
  - **replay:** serve from the cassette (reuse the serve handler + slice-5 dir
    routing); **404 on miss, never a passthrough** — preserve the zero-dial invariant.
  - **branch:** replay the matched prefix, go live on divergence (reuse `ModeBranch`).
- *Done when:* an in-process fake upstream proves captured traffic replays
  byte-exact and **no secrets persist** to disk. Add a docs section + a non-Go
  (curl/python) usage snippet.

### Slice 7 — lint ✅ DONE
- `cassette lint <dir> [--json] [--attest manifest]`: one pass that runs every
  health check (orphans, unconsumed expects, secret-scan, oversized bodies,
  undecodable turns, missing finish reasons, attestation drift) across a corpus,
  emitting JSON findings + a **nonzero exit on policy violation**. The CI front
  door. Pure aggregation of shipped analyzers; zero network.

### Slice 8 — taint (deepest non-codec moat) ✅ DONE
- `cassette taint <cassette.yaml> [--fail-on email,ssn,...] [--json]`: trace
  sensitive tokens **across turns** — data that appears in turn N's outbound
  request/tool-arg and **originated** in earlier user input or tool results
  (turn M < N), and untrusted content (tool result / retrieved doc) that later
  steers a tool call. Report masked `source → sink` paths. Reuse `internal/egress`
  typed detectors + `analysis.CollectTurns` + the raw request bodies.

### Slice 9 — coverage + shrink ✅ DONE
- `cassette coverage [dir] [--require tool=...,finish=...] [--format table|json]`:
  behavioral matrix (distinct tools called, finish reasons, refused-vs-complied,
  providers/models, unary-vs-stream) + gap flags; `--require` fails CI on missing
  cells.
- `cassette shrink [dir] [--keep-coverage tools,finish,refusal] -o kept/`: minimal
  subset preserving the full set of distinct **semantic** digests + coverage.
  Shrink consumes coverage. Both reuse `semequal` digests + `refusal`.

### Slice 10 — scenario matrix ✅ DONE
- `cassette scenario <golden.yaml> --turn N -o dir/`: from one recorded turn, emit
  a family of valid cassettes covering the behavior matrix — each finish_reason
  (max_tokens / stop_sequence / tool_use / refusal), empty / last-token-truncated
  text, malformed-but-legal tool args, and injected provider errors (reuse the
  shipped **faults** model + slice-3 authoring primitives). Emit named cassettes +
  a self-checking `corpus.yaml` of expected digests.

### Slice 11 — MCP parity ✅ DONE
- An MCP interaction **decoder** feeding the shipped `analysis` Transcript model so
  `doc`/`explain`/`dataset`/`diff`/`cost` see MCP, not just HTTP.
- `cassette mcp-serve <cassette.yaml>`: expose a recorded MCP session as a real
  **stdio JSON-RPC server** (zero subprocess launch, zero network) — the MCP
  analogue of `serve`. *Done when:* decoded MCP turns render in `doc`, and an MCP
  client round-trips against `mcp-serve` offline.

### Slice 12 — fuzz (consumer-parser robustness) ✅ DONE
- `wireenc.Reframe(...)` (seeded): from a Transcript/recorded turn, emit many
  **valid** re-framings — split text deltas at random valid boundaries, permute
  independent tool-call order, vary envelope ids/usage — all decoding back to the
  **same** Transcript.
- `cassette fuzz <cassette.yaml> --turn N --reframings 50 --seed 7 -o variants/`:
  write the variant corpus + assert each round-trips. Deterministic per `--seed`
  (no `Math.random`-style nondeterminism; seed the PRNG explicitly). This tests
  the *user's* SSE/agent parser, the inverse of `mutate`.

### Slice 13 — polish cluster ✅ DONE (stitch/split + whatif; attest --wire folded in)
- `cassette stitch a.yaml b.yaml ... -o society.yaml` + `cassette split <in.yaml>
  --by tool|host -o dir/`: compose/decompose multi-agent cassettes (rekey;
  combined digest).
- `cassette whatif <in.yaml> --turn N --slot text --values alts.txt --report
  matrix.md`: batch-graft a turn across alternatives, report which downstream
  turns become counterfactual + digest deltas.
- `cassette attest --wire` (+ `verify-attest` checks it): bind the encoder's
  **canonical wire bytes** as well as the Transcript digest — wire-level +
  behavior-level dual tamper evidence (pairs `canonicalize` with `attest`).

---

## Sequencing rationale (why this order)

Encoder-trust first (1–2) so every authoring feature can rely on it; the two
flagships (3 author, 4 port) next; composition (5) before the proxy (6) that
needs directory routing; then governance/safety/parity/robustness (7–12) which
are independent and can be reordered if one is blocked; polish (13) last because
each piece stands on an earlier primitive. Every slice is gate-green alone, so
the loop can stop cleanly after any commit.

## Commit hygiene & stop conditions

- One slice → one commit, `scripts/ci.sh` green before each. Branch off `main`
  first if not already on a feature branch.
- **Stop and surface to the user** only if: a slice genuinely needs a new heavy
  dependency, a constraint above is in real tension with a requirement, or a
  design choice is irreversible and ambiguous. Otherwise pick the
  smallest-correct option, note it in the commit, and keep looping.
- When all slices are done: run the full gate once more, update `CHANGELOG.md`
  with the PLAN6 summary, tick `FEATURE_RESEARCH.md`, and report what shipped +
  what (if anything) was deferred and why.
