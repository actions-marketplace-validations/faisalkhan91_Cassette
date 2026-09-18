# cassette — command reference

## CLI

`cassette --help` opens with a curated **Start here** tier — the commands that matter
before the other ~50:

- `cassette up` — point your provider's base URL here: records once, then replays offline (start here)
- `cassette open` — see a recording: offline dashboard + transcript
- `cassette proxy` / `cassette serve` — language-agnostic record/replay endpoints (advanced)
- `cassette verify` / `cassette conformance` — prove it is secret-free (verify) and replayable (conformance)

The fastest path is **`cassette up`**: install once, point your SDK's base URL at the
address it prints, and it records on the first run (auto-detecting the provider) then
replays byte-exact and offline on every run after — no key, no network. A replay miss
is diagnosed (nearest turn + the field that broke the match key + a one-line fix)
rather than returned as a bare 404.

```sh
go install ./cmd/cassette

cassette up                                   # records (first run) or replays offline; auto port + upstream
cassette up ./fixtures --upstream https://my-host  # OpenAI-compatible/custom host (ambiguous /chat/completions)
cassette open  <cassette.yaml|dir> [-o report.html] [--no-open]  # offline dashboard + transcript
cassette inspect <cassette.yaml>              # pretty-print (nonzero exit if malformed)
cassette scrub   <cassette.yaml>              # redact secrets in place (idempotent)
cassette verify  <cassette.yaml>              # exit 0 iff well-formed and free of secret patterns (SAFE only; use `conformance` to prove it REPLAYS)
cassette prune   <cassette.yaml> --index N,M  # drop interactions by index
cassette rekey   <cassette.yaml> [--force]    # recompute match keys after a hand edit (idempotent); REFUSES if a request body was scrubbed (stored key no longer re-derivable) — --force overrides
cassette redact-field <cassette.yaml> --path a.b.c   # redact a JSON field + rekey (idempotent)
cassette serve   <cassette.yaml|dir> [--addr :8080] [--pace] [--log-misses] [--on-miss fail] [--miss-manifest path] [--route-header NAME] [--volatile a.b,c]  # offline endpoint; --on-miss=fail = CI gate (nonzero exit)
cassette proxy   <cassette.yaml|dir> [--mode record|replay|branch] [--upstream URL] [--pace] [--on-miss fail] [--miss-manifest path] [--volatile a.b,c]  # record/replay from ANY language via base_url
cassette mcp-serve <cassette.yaml>                   # replay a recorded MCP session over stdio JSON-RPC (any MCP client)
cassette mcp-proxy -o <cassette.yaml> -- <server-cmd> [args...]  # record a live MCP session via a passthrough proxy (replay with mcp-serve)
cassette mcp-diff <old.yaml> <new.yaml> [--fail-on-breaking] [--json]  # MCP tool-contract diff: removed tools / incompatible schema / success→error
cassette mcp-steps <session.yaml> [--json]           # walk a recorded MCP session step by step (method/tool/args → result/error)
cassette mutate  <cassette.yaml> --op truncate|drop-terminal|duplicate|reorder|corrupt-tool|inject-error|drop-multibyte [--frame N] [--seed N] [-o|--out path]  # derive a hostile stream variant
cassette assert  <cassette.yaml> [--no-tool NAME]... [--finishes-clean]      # behavioral invariants
cassette doc     <cassette.yaml> [--check doc.md]    # render a Markdown transcript (living docs)
cassette bisect  <a.yaml> <b.yaml>                   # localize the first divergent turn + changed input axis
cassette pack    <cassette.yaml> -o demo            # build a single offline binary serving a steppable browser transcript

# review & debugging
cassette explain <cassette.yaml> --turn N            # per-turn microscope: request→key, body, transcript, wire shape
cassette diff    <a.yaml> <b.yaml> [--md]            # signal-only semantic diff (text/tools/finish/axis)
cassette otel    <cassette.yaml|dir> [-o spans.json] # export as OpenTelemetry GenAI spans (OTLP/JSON) for Langfuse/Phoenix/SigNoz
cassette provenance <cassette.yaml> [--json]         # show a derived cassette's lineage (port/migrate/graft/distill + source digests)
cassette doctor  <cassette.yaml> --request req.json  # diagnose a replay miss: nearest record, changed axis, remedy
cassette dataset [dir] [--where k=v]... [--format …] # index recordings into a queryable eval dataset
cassette coverage [dir] [--require tool=X,finish=Y] [--format table|json]  # behavioral coverage matrix + gap gate
cassette dashboard [dir] -o <report.html>            # render a corpus to one self-contained offline HTML report
cassette shrink  [dir] [-o kept/]                    # minimal behavior-preserving subset of a corpus
cassette orphans [dir]                               # flag dead fixtures and tests missing a recording
cassette lint    <file|dir> [--json] [--strict] [--max-bytes N]  # one-pass corpus health gate (secrets, stale keys, ...)
cassette gitconfig --install                         # render cassettes as transcripts in `git diff` (local)

# cost, safety & contracts
cassette cost    <cassette.yaml> [--prices p.json] [--budget tokens=N|usd=X]  # offline token/cost budget gate
cassette egress-audit <cassette.yaml> [--fail-on email,creditcard,…]          # typed PII/data-class scan (per request)
cassette taint   <cassette.yaml> [--fail-on email,ssn,…] [--json]             # trace sensitive values that cross turns (flow)
cassette audit   <cassette.yaml|dir> [-o bundle.json] [--fail-on email,exfil,…] # offline compliance evidence bundle (manifest+coverage+egress+taint)
cassette policy  <cassette.yaml|dir> [--policy cassette.policy.yaml] [--json]   # enforce a declarative guardrail (secrets/egress/exfil/budget/coverage)
cassette policy init [-o cassette.policy.yaml] [--force]                       # scaffold a safe-by-default starter policy
cassette expect  <cassette.yaml> [--no-tool NAME]... [--finishes-clean]        # embed a contract assert runs zero-arg
cassette refusal <cassette.yaml>                     # classify turns refused/complied/unknown
cassette redteam <dir> [--expect manifest.yaml]      # diff refusal verdicts across recordings
cassette attest  <cassette.yaml> --key k -o c.att    # ed25519-sign the behavioral manifest
cassette verify-attest <cassette.yaml> <out.att>     # verify a signed behavioral manifest (public key is read from the .att)
cassette schema-check <cassette.yaml> [--json]       # validate emitted tool-call args vs the advertised JSON Schema
cassette eval    <subject.yaml> --suite s.yaml [--judge j.yaml]               # assertions + an offline LLM judge fixture

# counterfactuals & live
cassette graft   <in.yaml> --turn N --set text=… -o out.yaml                  # rewrite one turn (counterfactual)
cassette distill <in.yaml> --turn N --slot text --values v.txt -o dir         # parameterize a turn → eval corpus
cassette scenario <golden.yaml> [--turn N] -o dir                            # edge/error fixture matrix from one golden turn
cassette fuzz    <cassette.yaml> [--turn N] [--reframings K] [--seed S] -o dir # valid re-framings to test YOUR parser (inverse of mutate)
cassette watch   <golden.yaml> --base-url URL [--policy prose:0.15]           # LIVE drift probe (opt-in, dials out)
cassette seeds   <cassette.yaml> [--strict]                                   # is a forward-branch reproducible?
cassette branch  <in.yaml> --from N [--graft text=…] -o seed.yaml             # edit a step, prep a forward-branch seed
cassette report  <cassette.yaml> [--no-tool …|--diff base] [--bundle name]    # committable, CI-runnable bug report (.castiron)
cassette stitch  <a.yaml> <b.yaml> [more...] -o society.yaml                 # compose cassettes into one society replay
cassette split   <cassette.yaml> --by provider|tool -o dir                   # decompose a recording (inverse of stitch)
cassette whatif  <in.yaml> --turn N --slot text --values alts.txt [--report m.md]  # batch-graft a turn → what-if matrix

# authoring & codec integrity
cassette author <screenplay.yaml> -o <cassette.yaml> # compile a terse conversation spec into a replayable cassette (no key/network)
cassette port   <cassette.yaml> --to openai-chat [-o out.yaml]  # transpile a recording to another provider's wire dialect
cassette migrate <in|dir> --to <dialect> -o <out|dir>  # port a whole corpus to a dialect, verifying every turn's behavior is preserved
cassette merge  <a.yaml|dir> [more...] -o out.yaml [--strict]   # union cassettes (dedup + conflict detection); serve <dir> also works
cassette codec verify <cassette.yaml> [--json]       # prove the Transcript⇄wire codec round-trips every streaming turn
cassette conformance  <cassette.yaml> [--json]       # prove the recording actually replays (key integrity, zero dials)
cassette canonicalize <cassette.yaml> [--check] [-o out.yaml]  # re-emit streams to byte-stable form (clean re-record diffs)

cassette completion bash|zsh|fish                    # print a shell completion script (generated from the registry)
cassette version                                     # print version (+ commit/date on release builds)
cassette init   [dir] [--stack python|node|go|promptfoo]  # scaffold an offline-replay setup + print a proxy base-url snippet
cassette <command> --help                            # detailed help for any command (also: cassette help <command>)
cassette --no-color <command> …                      # disable color (also honors NO_COLOR / CLICOLOR_FORCE)
```

### Timing-faithful replay (`--pace`)

By default cassette replays SSE frames as fast as the client reads — best for fast,
deterministic CI. For load/UX testing where inter-token cadence matters, run the
**timing-faithful** round trip: record with `--pace` to capture per-frame
inter-arrival deltas (`stream_timing`), then replay with `--pace` to re-emit them
with those delays.

```sh
cassette up --pace                                   # capture timing on record, pace on replay (one flag)
cassette proxy run.yaml --mode record --pace --upstream https://api.anthropic.com
cassette proxy run.yaml --mode replay --pace         # (serve --pace works too)
```

Timing is off by default and `canonicalize` only strips it from turns it actually
rewrites, so a `--pace` recording stays timing-faithful through the `canonicalize
--check` gate.

### Supported providers

Decoded/encoded dialects: **Anthropic** Messages (SSE), **OpenAI** Chat Completions
& Responses (SSE), **Google Gemini** `streamGenerateContent` (SSE *and* JSON-array
framing), Ollama native NDJSON (`/api/chat`), and **MCP** stdio. The match key is path-only (host excluded), so the
**OpenAI-compatible family — Azure OpenAI, vLLM, Together, Groq, OpenRouter,
Mistral, DeepSeek, Ollama's OpenAI-compat endpoint** — is covered automatically:
point any of them at `cassette proxy`/`serve` and the `/chat/completions` (or
`/responses`) path routes to the OpenAI dialect.

## Beyond testing

cassette is also a small toolkit for the agentic stack built on the same engine:

- **Resilience / chaos** — `mutate` derives seeded hostile-but-plausible streams
  (truncation, dropped terminals, malformed tool args, reorders) and `Options.Faults`
  injects synthetic failures (429→200 retry) or stream corruptions at replay time
  *without mutating the cassette*.
- **Drift intelligence** — `bisect` localizes the first turn (and request-input axis)
  where two recordings diverge; `semequal.WireShape` fingerprints provider wire
  grammar; `Timeline` is an append-only behavioral ledger.
- **Safety** — `assert` checks behavioral invariants over the transcript; `CanaryHits`
  flags planted tripwire tokens that leaked into an outbound request.
- **Equivalence** — `semequal.Policy` allows per-field drift tolerance (tool calls
  exact, prose within a budget) for golden tests against nondeterministic models.
