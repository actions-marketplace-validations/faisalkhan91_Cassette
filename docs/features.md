# cassette — feature deep-dives

The offline endpoint, the self-contained demo binary, the forward-branch bug-report
workflow, the corpus/onboarding tools, governance (policy + audit), OpenTelemetry
export, and provenance. (MCP — `mcp-proxy`/`mcp-serve`, the `mcp-diff` breaking-change
gate, the `mcp-steps` protocol walk, and `kind: mcp` authoring — lives in [mcp.md](mcp.md).)

## Serve a cassette as an offline LLM endpoint

![Split terminal: left runs `cassette serve` as an offline endpoint, right `curl`s it and gets the replayed SSE stream — 0 dials, 0 key.](../assets/demo-split.gif)

`cassette serve` exposes a recording as a real local HTTP listener speaking each
provider's wire protocol (Anthropic `/v1/messages`, OpenAI `/chat/completions`,
Responses `/responses`, Gemini `:streamGenerateContent`, Ollama `/api/chat` — routed
automatically by request path), so **any client in any language** gets deterministic,
zero-network, zero-key replay:

```sh
cassette serve session.yaml --addr :8080            # then point any SDK's base_url at it
cassette serve session.yaml --pace                   # re-emit recorded inter-frame timing
cassette serve session.yaml --log-misses             # log misses (always 404; never proxies)
```

SSE frames are written and flushed frame-by-frame; a miss is a 404, never a
passthrough (the listener holds no HTTP client). In Go, `c.Handler(ServeOptions{})`
returns the `http.Handler` directly.

## Pack a recording into one offline binary

`cassette pack` turns a recording into a single self-contained executable — *the
recording is the demo*, shippable as one file:

```sh
cassette pack session.yaml -o demo    # produces ./demo
./demo                                 # serves on http://127.0.0.1:7070, zero network
./demo --addr 127.0.0.1:0              # or pick an ephemeral port
```

Running `./demo` serves an **interactive, steppable browser transcript** (step
through each turn, watch the assistant text stream out, tool calls rendered as
chips) at `/`, the decoded turns at `/transcript.json`, and the full serve API at
the real provider paths — so a real SDK can point its base URL at it for key-less,
offline replay.

It works with **no Go toolchain at pack time**: the cassette is appended to a copy
of the running `cassette` binary behind a fixed trailer, and detected at startup.
Caveats: the artifact is a copy of the host binary, so it is **same-OS/arch only**
(no cross-compile); on macOS the appended bytes invalidate the code signature (it
still runs locally but is not a notarized distributable); don't post-process the
output (`strip`/UPX); Windows is unsupported. `pack` refuses to embed a cassette
that trips the secret scanner, so credentials never ship inside a binary.

## The Castiron workflow — "it failed once last Tuesday" → a committed test

![Split terminal: left shows a captured run that leaked data; right grafts that one step to a fix and prepares a forward-branch re-run.](../assets/demo-branch.gif)

When an agent does something wrong on one run, that run is a recording. The
Castiron workflow turns it into a durable, signed, CI-runnable bug report and
lets you **edit a step and re-run forward** to explore the fix:

```sh
# 1. Edit one step and prepare a forward-branch seed (replay turns 0..N, then go live).
cassette branch run.yaml --from 2 --graft text="use the safe tool instead" -o seed.yaml

#    Re-run FORWARD in code: open the seed in ModeBranch and run YOUR agent. Turns
#    0..N replay (turn N returns the edit); the first request that diverges goes
#    live and is recorded, capturing the new branch:
#      c, _ := cassette.Open("seed.yaml", cassette.Options{
#          Mode: cassette.ModeBranch, Live: cassette.LiveTransport(baseURL, injectAuth)})
#      runMyAgent(c.HTTPClient()); c.Save()   // grown branch = edited prefix + live suffix

# 2. Will it even reproduce? Check the sampling determinism.
cassette seeds run.yaml --strict

# 3. Sign the fixture and package the bug as a committable .castiron.
cassette attest run.yaml --key ed25519.key -o run.att
cassette report run.yaml --no-tool delete_account --attest run.att --bundle tuesday
#    → tuesday.castiron/  { run.yaml, run.att, report.md, run.sh }  — commit it.
```

`cassette report` **re-runs the check it documents**, so a committed `.castiron`
becomes a regression test that exits nonzero *while the bug is live* and goes
green exactly when it's fixed. The forward re-run is a library step (cassette sits
on the RoundTripper and can't drive your agent loop); `cassette branch` only
prepares the seed. The live branch **dials the network** and its reproducibility
is best-effort (`cassette seeds` reports whether sampling is pinned).

## Migrate a corpus across providers — de-risk a switch

![cassette migrate porting an anthropic corpus to the openai-chat dialect — every turn's semantic digest preserved.](../assets/demo-migrate.gif)

```sh
cassette migrate corpus/ --to openai-chat -o migrated/
```

`migrate` ports a whole recording or corpus to a target wire dialect (Anthropic,
OpenAI Chat/Responses, Gemini, Ollama) **and verifies every turn's semantic digest
is preserved**, failing loudly with a per-file report on any drift. So you can
record once against provider A, migrate the corpus to provider B's dialect, and
replay your B-built client offline against the *same recorded behavior* — with a
hard guarantee nothing silently changed in translation. (`cassette port` does a
single file without the corpus-wide drift gate.)

## Author a recording from a screenplay — no key, no network

![cassette author compiling a terse screenplay into a replayable cassette.](../assets/demo-author.gif)

`cassette author <screenplay.yaml> -o <cassette.yaml>` compiles a terse YAML
*screenplay* into a fully valid, replayable cassette via the encoder — no capture,
no API key, no network — and verifies it replays before writing. An HTTP screenplay
turn supports:

```yaml
provider: anthropic            # anthropic | openai-chat | openai-responses | gemini | ollama
model: claude-opus-4-6
turns:
  - user: "What's the weather in Paris?"   # the user prompt for this turn
    text: "Let me check."                  # assistant text (optional)
    tool_calls:                            # optional tool calls
      - {name: get_weather, args: '{"city":"Paris"}'}
    finish: tool_use                       # finish reason (end_turn, tool_use, stop, ...)
    input_tokens: 1540                     # optional — stamps the response usage block
    output_tokens: 320                     #   so cost/dashboard/--budget have real numbers
    # request: '{...}'                     # optional raw request body override (exact match key)
    # url: /v1/messages                    # optional request path override
```

Mark a turn `kind: mcp` to author MCP `tools/call`/`tools/list` turns instead — see
[mcp.md](mcp.md). (`input_tokens`/`output_tokens` are emitted for Anthropic, OpenAI,
and Gemini; Ollama's native format carries no usage telemetry.)

## A static dashboard over a corpus — offline, committable

![cassette dashboard's self-contained HTML report: coverage matrix, per-model tokens, refusal verdicts, and per-cassette transcripts.](../assets/demo-dashboard.png)

```sh
cassette dashboard testdata/cassettes -o report.html
```

`dashboard` renders a corpus to **one self-contained HTML file** — no server, no
external assets, no accounts: the behavioral coverage matrix, total and per-model
token counts, refusal verdicts, tool usage, and a per-cassette transcript. The
output is deterministic for a given corpus (no timestamps), so it can be committed
or attached to a CI run — the dashboard value of a hosted platform, on-brand with
the zero-egress model.

## Onboard any language in 60 seconds — init + proxy

![cassette init scaffolding an offline-replay setup and printing the proxy base-url snippet for the detected stack.](../assets/demo-init.gif)

```sh
cassette init --stack python      # scaffold + print the base-url snippet
```

`init` scaffolds an offline-replay setup (a `testdata/cassettes/` dir, a
`cassette-misses.json` gitignore entry) and prints a copy-paste snippet for pointing
your provider base URL at `cassette proxy` — the cross-language adoption path. Pair
it with the ready-made [pytest `conftest.py` recipe and reusable GitHub Action](recipes/README.md):
record once through `cassette proxy --mode record`, commit the cassettes, then
replay offline in CI with `--mode replay`. No key, no network, any stack.

## Governance & compliance — one gate, one evidence bundle

`cassette policy <dir>` enforces a committed `cassette.policy.yaml` (no secrets, no
forbidden PII egress, no exfil, within a token budget, required tools covered) as a
single CI exit code; `cassette policy init` scaffolds a safe-by-default starter.
`cassette audit <dir>` emits a deterministic, offline evidence bundle — the behavioral
manifest + coverage matrix + typed PII findings (tagged by field) + cross-turn taint
flows — the artifact a SOC 2 / EU AI Act / ISO 42001 review asks for. See the
[governance CI recipe](recipes/governance.md).

```sh
cassette policy ./testdata/cassettes              # the gate
cassette audit  ./testdata/cassettes -o audit.json
```

## Export to OpenTelemetry — bridge into your observability stack

`cassette otel <dir>` maps a recording to OpenTelemetry GenAI spans (OTLP/JSON) —
`gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.usage.*`, plus the MCP
convention — so a cassette imports into Langfuse / Phoenix / SigNoz instead of
needing a second tool. Deterministic IDs; `--pace`-captured timing yields real span
durations.

```sh
cassette otel session.yaml | curl -X POST localhost:4318/v1/traces -d @-
```

## Provenance — verifiable lineage

A derived cassette carries a `provenance` chain: each `port` / `migrate` / `graft` /
`distill` records the source's *behavioral* digest, so you can prove what a recording
was derived from. `cassette provenance` reads it; `attest` binds it into the signed
manifest. The chain is secret-free and stable across benign re-records.

```sh
cassette provenance ported.yaml      # → port from <source behavior digest>
```

## MCP tooling

Beyond record/replay (`mcp-proxy` / `mcp-serve`), `cassette mcp-diff <old> <new>
--fail-on-breaking` gates CI on MCP tool-contract breaks (removed tool, stricter input
schema, success→error), and `cassette mcp-steps <session>` walks a recorded session at
the protocol level (method sequence, tools/list roster, each call's args → result). See
[MCP](mcp.md).
