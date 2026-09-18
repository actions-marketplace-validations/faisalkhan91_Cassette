# Differentiation & adoption build prompt — cassette

> Paste as the task for an autonomous build loop (e.g. `/loop`). Implements
> [`DIFFERENTIATION_PLAN.md`](DIFFERENTIATION_PLAN.md): 6 slices that turn cassette's
> verified competitive edges into shipped capability — own MCP, an offline dashboard,
> provider-migration, proxy onboarding, and positioning. One gate-green slice per commit.

## Loop discipline (every slice)
1. Smallest correct version satisfying the slice's done-criteria.
2. `gofmt -w`; `go build ./...`; `go vet ./...`.
3. Table-driven offline tests; `bash scripts/ci.sh` must exit 0 and hold the
   aggregate + per-package coverage floors.
4. New command → wire into the registry + `commandGroup` + `help_test` + `docs/cli.md`
   (the doc-lint + registry tests enforce this).
5. Update `CHANGELOG.md` / `docs/*` / `DECISIONS.md` as warranted.
6. Commit the slice separately (conventional prefix, no AI footer). Continue.

Work in order; don't stop between slices to ask. Stop only on a hard-constraint
conflict or a need for a new heavy dependency (there should be none).

## Non-negotiable constraints
Pure Go; offline CI (`-mod=vendor`, `GOPROXY=off`); **no new heavy deps**; **no JS
build / external assets** (dashboard HTML is `html/template` + inlined CSS, optional
inlined vanilla JS); **no SaaS / stateful server / accounts**; deterministic /
byte-exact (no timestamps/rand/map-order in serialized output — the dashboard must be
deterministic for a given corpus); secrets never written; gofmt+vet clean; curated
public API (root package only; `analysis` is internal). Network only on opt-in paths.

## Slices

**1 — `cassette mcp-proxy --record -- <cmd> <args…>` (stdio MCP recording proxy).**
Spawn the real MCP server as a subprocess (`os/exec`, stdio pipes). Sit between the
calling MCP client (this process's stdin/stdout) and the server, forwarding
newline-delimited JSON-RPC both ways verbatim, and **tee** each `tools/call` and
`tools/list` request+result into a cassette (reuse the MCP record format + scrub;
write on shutdown). Replay is the existing `cassette mcp-serve` (no new replay code).
No reimplementation of MCP semantics — pure passthrough + record. Factor a testable
`recordMCPProxy(in, out io.Reader/Writer, serverCmd, rec)` so a fake stdio server
exercises it without a real binary. *Done when:* a client driven through
`mcp-proxy --record` yields a cassette that `mcp-serve` replays identically with zero
subprocess launch; a planted secret in a tool result is scrubbed; tests pass offline.

**2 — MCP authoring + conformance corpus.** Extend `analysis.Screenplay`/`Compile`
(and the `author` cmd) to accept `kind: mcp` turns — `{method: tools/call, tool, args,
result}` (and a `tool_error` variant) — compiled into a replayable MCP cassette in the
recorded MCP format (set the match key via the MCP key path). Ship a small committed
**conformance corpus** under `cmd/cassette/testdata/mcp/` (initialize, tools/list,
tools/call, tool-error) and a test that each replays through `mcp-serve` and decodes via
`DecodeInteraction`. *Done when:* an authored MCP screenplay round-trips through
`mcp-serve`; the corpus is gate-checked.

**3 — `cassette dashboard <dir> -o report.html` (offline static dashboard).** One
self-contained HTML file (no external assets, no server) summarizing a corpus:
behavioral coverage matrix (`analysis.Coverage`), token/cost totals (`cost`), refusal
verdicts (`refusal`), tool-usage counts, and a per-cassette list each linking to its
rendered transcript (reuse `pack`'s transcript renderer / `doc`). `html/template` +
inlined CSS; **deterministic** (no timestamps/wallclock in the output). *Done when:*
`report.html` opens offline and shows the corpus at a glance; a test asserts the HTML
contains the expected sections/counts and is byte-stable for a fixed corpus.

**4 — `cassette migrate <in|dir> --to anthropic|openai-chat|openai-responses|gemini|ollama -o <out>`.**
Port each recording to the target dialect (reuse `analysis.Port`) **and verify per-turn
semantic digests are preserved** (compare `CollectTurns` digests pre/post); fail loudly
(nonzero exit + per-file report) on any drift. The first-class "de-risk a provider
switch with your existing tests" command. *Done when:* migrating an anthropic corpus to
openai-chat preserves every digest; an intentionally lossy case fails; tests pass.

**5 — `cassette init` + pytest recipe + GitHub Action.** `cassette init` scaffolds an
offline-replay setup (a `testdata/cassettes/` dir + `.gitignore` for `cassette-misses.json`
+ a printed proxy base-url snippet for the detected/declared stack). Add `docs/recipes/`
with a pytest `conftest.py` pointing `*_BASE_URL` at `cassette proxy` and a reusable
**`action.yml`** GitHub Action wrapping the offline binary (`verify` / `lint` / serve
`--on-miss=fail`). cassette stays pure-Go; recipes are thin shims/docs. (Dedupe against
FEATURE_COMPLETE slice 20 — build once here.) *Done when:* a fresh dir scaffolds and the
documented pytest recipe runs a hermetic replay test.

**6 — Positioning docs.** Add `docs/comparison.md` — a "vs go-vcr / VCR.py / nock /
PollyJS / mitmproxy / eval platforms" table sourced from `COMPETITIVE_LANDSCAPE.md`,
with the honest caveats (offline replay is mature prior art; eval category partly
inferential). Refresh the README hero to lead with the wedge ("record once → replay
byte-exact, offline, any language, forever; SSE + MCP + secret-scrubbed") and add a
tight "vs alternatives" blurb to `docs/faq.md`. *Done when:* every claim matches the
verified report; gate + doc-lint green.

## Stop conditions
Stop and surface only if a slice needs a new heavy dependency (it shouldn't — MCP
proxy is stdio pipes + JSON-RPC lines; dashboard is `html/template`), a constraint
truly conflicts, or slice 5's Action/recipe needs a decision. Otherwise pick the
smallest-correct option, note it in the commit, and continue. When done: full gate,
update CHANGELOG/DECISIONS, tick `DIFFERENTIATION_PLAN.md`, report what shipped.
