# cassette — differentiation & adoption plan

The strategic build plan that follows from the [competitive landscape](../COMPETITIVE_LANDSCAPE.md):
cassette sits in an empty niche, but incumbents win on ecosystem reach and hosted
dashboards. So we **don't compete as "a better VCR library"** — we win categories
incumbents structurally can't enter, and use the proxy to sidestep language reach.

This is the **differentiation & adoption track**. It is distinct from
[`FEATURE_COMPLETE_PLAN.md`](../FEATURE_COMPLETE_PLAN.md) (provider breadth + eval depth
+ 1.0 polish); where they overlap (onboarding), this plan pulls the item forward
because it's now strategically primary — build once, dedupe against that plan.

## What we picked, and why (each maps to a verified finding)

| Bet | Competitive rationale |
|-----|------------------------|
| **Own MCP record/replay** | Research found **zero** verified MCP testing/mocking tooling — the least-contested land-grab. cassette already has `mcp-serve` + in-process MCP record; a stdio **record proxy** + authored conformance corpus makes it *the* MCP testing tool. |
| **Offline static-HTML dashboard** | Incumbents win on hosted dashboards; our brand is offline/zero-egress. A committable `report.html` (no server, no account) captures the value on-brand. |
| **Provider-migration testing** | No competitor has a round-trip codec. "Record once, replay your other-provider client" de-risks a vendor switch — a concrete enterprise pain only `port` can address. |
| **Proxy onboarding (trojan horse)** | The one cross-language path is a proxy; ours is LLM/SSE/MCP-aware. `init` + a pytest recipe + a GitHub Action turn "point your base_url here" into 60-second adoption in any stack. |
| **Positioning from the report** | The verified landscape is launch ammo: a public "vs alternatives" page + README hero. |

## Non-goals (stay disciplined / on-brand)
- **No hosted SaaS / stateful web server / accounts** — the dashboard is a static file; `pack` already proves the offline-HTML pattern.
- **No native client SDKs (pip/npm)** — the proxy + thin shims are the language-reach strategy; we don't maintain N libraries.
- **No new heavy deps / no JS build** — dashboard HTML is `html/template` + inline CSS (optional tiny vanilla JS, inlined), pure Go.
- **mcp-proxy doesn't reimplement MCP semantics** — it tees JSON-RPC lines between a client and the real server subprocess; replay reuses `mcp-serve`.

## Build sequence (dependency-ordered; each slice gate-green)

1. **`cassette mcp-proxy --record`** *(L)* — a stdio MCP **recording proxy**: spawn the
   real MCP server (`-- <cmd> <args…>`), pipe the client's stdin/stdout through
   cassette, tee newline-delimited JSON-RPC, and record each `tools/call`/`tools/list`
   request+result into a cassette (reusing the MCP record path + scrub). Replay is the
   existing `cassette mcp-serve`. *Done:* an MCP client driven through `mcp-proxy
   --record` produces a cassette that `mcp-serve` replays identically, zero subprocess
   launch on replay; secrets scrubbed; tests with a fake stdio MCP server.
2. **MCP authoring + conformance corpus** *(M)* — extend `author` screenplays with
   `kind: mcp` turns (tool name + args + result, and an error variant) compiled into a
   replayable MCP cassette via the MCP record format; ship a small committed
   conformance corpus (initialize / tools-list / tools-call / tool-error) under
   `testdata/` and a `cassette conformance`/`mcp-serve`-backed test. *Done:* authored
   MCP cassettes replay through `mcp-serve`; the corpus is gate-checked.
3. **`cassette dashboard <dir> -o report.html`** *(L)* — a self-contained, offline
   static HTML report over a corpus: the behavioral coverage matrix, token/cost totals,
   refusal verdicts, tool-usage, per-cassette links to a rendered transcript. Pure-Go
   `html/template` + inlined CSS (no external assets, no server) — reuses
   `analysis.Coverage`/`cost`/`refusal`/`dataset` + `pack`'s transcript renderer.
   *Done:* `report.html` opens offline in a browser and shows the corpus at a glance;
   deterministic given a corpus (no timestamps in the output); golden-ish test on structure.
4. **`cassette migrate <in|dir> --to <dialect> -o <out>`** *(M)* — port a whole
   recording/corpus to a target dialect (`port` per file) **and verify per-turn semantic
   digests are preserved**, failing loudly if any drift — the first-class "de-risk a
   provider switch with your existing tests" command. *Done:* migrate anthropic→openai
   corpus, digests identical, nonzero exit on any divergence; tests.
5. **`cassette init` + pytest recipe + GitHub Action** *(M)* — `cassette init`
   scaffolds an offline-replay setup (a `testdata/` dir, a `cassette-misses.json`
   gitignore, and a copy-paste proxy base-url snippet); ship `docs/recipes/` with a
   pytest `conftest.py` that points `*_BASE_URL` at `cassette proxy` and a reusable
   GitHub **Action** (`action.yml`) wrapping the offline binary (`verify`/`lint`/serve
   `--on-miss=fail`). Pure docs + thin shims; cassette stays pure-Go. *Done:* a non-Go
   project can scaffold + run a hermetic replay test from the recipe.
6. **Positioning docs** *(S)* — `docs/comparison.md` ("vs go-vcr/VCR.py/nock/PollyJS/
   mitmproxy and the eval platforms", sourced from the verified report, with honest
   caveats); refresh the README hero to lead with the wedge ("record once → replay
   byte-exact, offline, any language, forever"); a tight "vs alternatives" blurb in
   `docs/faq.md`. *Done:* claims match the verified report; doc-lint/gate green.

## Sequencing rationale
MCP first (1–2): the open category, most defensible, and reuses the most existing
machinery. Dashboard (3) and migrate (4) are independent codec/analysis features
that turn unique capabilities into buyer-facing value. Onboarding (5) is the
adoption multiplier — pull forward from the feature-complete plan. Positioning (6)
lands once the new capabilities exist so the claims are true.

## Status — all 6 slices shipped (2026-06)
1. ✅ `cassette mcp-proxy` — stdio recording proxy; replay via `mcp-serve`.
2. ✅ `kind: mcp` authoring + committed conformance corpus.
3. ✅ `cassette dashboard` — offline, deterministic static HTML report.
4. ✅ `cassette migrate` — corpus port + per-turn digest-drift gate.
5. ✅ `cassette init` + pytest recipe + reusable GitHub Action.
6. ✅ Positioning — `docs/comparison.md`, README hero, faq blurb.

## Global done-definition
- `mcp-proxy --record` + `mcp-serve` make a full record→replay MCP loop, offline,
  zero subprocess on replay, secrets scrubbed.
- A corpus renders to a single offline `report.html`; a corpus migrates across
  dialects with digests preserved or fails loudly.
- A non-Go project adopts cassette in minutes via the proxy + scaffold + Action.
- Public positioning matches the verified competitive report.
- Everything pure-Go, offline CI, no heavy deps, deterministic, secrets never
  written, coverage floors held.
