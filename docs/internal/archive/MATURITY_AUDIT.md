# cassette — maturity audit & maturation plan

> Status: Phases 0–7 shipped (2026-06). Retained as a reference record; open items below may since be resolved — cross-check against the code.

A deep, multi-dimensional audit to take cassette from "functionally complete" to
"credibly publishable." Produced by fanning out **11 specialized auditor agents**
across the real repo (install/distribution, tests/CI, code refactor, repo
hygiene, docs structure, docs/visuals, terminal UX, CLI ergonomics, security/
supply-chain, performance/scale, wildcard) — **78+ evidence-backed findings** —
then reconciling two synthesis framings (release-readiness and adoption/polish).

## Headline

The engineering substance is genuinely strong for a pre-1.0 tool: zero-dial
replay (dial-blocking transport + `VerifyError`), byte-exact recordings, 50
subcommands from one declarative registry, scrub-on-save, ed25519 attestation,
offline vendored CI with the race detector, and a clean single-source color
layer. **What stands between it and a launch is not features — it's identity,
packaging, test-gate honesty, and presentation.** One blocker dominates:

> **The module path is the placeholder `github.com/example/cassette`.** Verified:
> `go install …@latest` 404s, and `brew install example/tap/cassette` +
> `ghcr.io/example/cassette` + every README/docs link/badge are non-functional.
> ~141 import sites + go.mod + `.goreleaser.yaml` + `release.yml` + README +
> CHANGELOG reference it. Nothing real can ship until this is decided and renamed.

Two more truths the audit surfaced: the **80% coverage floor is a mirage** — it's
an *aggregate*, and `analysis/` is actually **74.8%** (below floor), kept green by
high-coverage siblings; and the **README claims "33 commands" when there are 50.**

## Status — IMPLEMENTED

Phases 0–3, 4-config, 5, 6, 7 are **shipped** (each its own gate-green commit):
identity rename to `github.com/faisalkhan91/cassette` + CI guard; version BuildInfo
+ hygiene (SECURITY/CONTRIBUTING/.editorconfig) + `--no-color`; honest coverage gate
(shippable-only denominator + per-package floor 78, aggregate 82; `analysis/`
internalized, 74.8→86.6%); security (named-credential scrub + write-time scan
backstop, loopback proxy default, load size cap, govulncheck); multi-arch image +
tap-token + macOS matrix; grouped `--help` + did-you-mean + shell completion +
ASCII/Windows glyph handling; docs hub + security/mcp/faq/glossary + cli doc-lint;
5 per-feature GIFs (author/diff/doctor/codec/taint) + tour regen.

**Owner-only remainder:** create `faisalkhan91/homebrew-tap` + the
`HOMEBREW_TAP_TOKEN` secret, confirm the GHCR namespace, then cut the first `v0.x`
tag (`-rc`/snapshot dry-run first). Deferred by choice: cosign signing + SBOM.

## Decisions needed from you (these gate the plan)

1. **The real module path / GitHub owner** — e.g. `github.com/<owner>/cassette`.
   Everything in distribution depends on it. ⚠️ Verify the org / Homebrew-tap /
   GHCR namespace are actually obtainable *before* the rename (the name "cassette"
   is widely used).
2. **Is `analysis/` a public library or CLI-internal?** Today its exported funcs
   take/return `internal/wirefmt` + `internal/wireenc` types that external callers
   can't construct — an inconsistent middle state. Either promote those wire types
   to a public `wire/` package (commit to API stability) **or** move `analysis/`
   under `internal/` and drop the "library consumer" framing. (Lower-effort: internal.)
3. **Supply-chain ambition for 1.0** — keyless cosign signing + SBOM now, or defer?
4. **The two Pillow "hero" GIFs** — replace with real terminal captures (stronger
   for the byte-exact brand) or keep as labeled illustrations?
5. **Secret-scan hard-fail scope** — which credential shapes block CI (AWS/GitHub/
   JWT/Google + provider keys) vs. stay audit-only (Luhn/high-entropy, to avoid
   false positives).

## Themes

- **Identity & distribution** — the placeholder path; Homebrew cross-repo token;
  arm64/multi-arch Docker; version embedding; (optional) signing/SBOM; completions.
- **An honest test gate** — decouple coverage from the aggregate; raise the real
  floor; add direct `analysis/` unit tests; cross-OS matrix; first benchmarks.
- **Security is the brand** — broaden secret detectors into the *write/verify*
  path; write-time `SecretScan`; loopback-default proxy; input size caps; govulncheck.
- **Refactor for maintenance, not churn** — fix the `analysis` API boundary;
  dedup test helpers; a `readOrErr` helper. (Do **not** split the flat command
  package — the registry makes it navigable.)
- **Discoverability & terminal polish** — grouped `--help`, shell completion,
  did-you-mean, a codified color/glyph/`--json` style guide, ASCII fallback.
- **Docs that onboard** — a docs hub + IA, security/MCP/FAQ/glossary pages,
  positioning vs go-vcr/vcrpy, and **per-feature explainer GIFs** (your ask).
- **Hygiene** — move build-loop artifacts out of root; add SECURITY/CONTRIBUTING/
  .editorconfig/.gitattributes.

## Quick wins (do first — all S-effort, high signal)

- Fix the **"33 commands" → 50** (or drop the number) in README + `gen-demo-tour.py`.
- **`version.go`:** `debug.ReadBuildInfo()` fallback so `go install @latest` prints
  the real semver, not `dev`; embed commit/date via ldflags.
- **Move** `IDEAS.md`, `FEATURE_RESEARCH.md`, `PLAN6_BUILD_PROMPT.md` (and this
  file) out of repo root → `docs/internal/` or delete — they're build-loop scratch.
- Add **`SECURITY.md`** + short **`CONTRIBUTING.md`** (document the offline-CI
  invariants), **`.editorconfig`**, **`assets/.gitattributes`** (`*.gif binary`).
- Add a global **`--no-color`** flag; add `style.warn()` / `style.note()` helpers
  (one spelling per concept, like `check()`/`cross()`).
- Complete `docs/cli.md` (it omits `version`, `verify-attest`).

## The plan — 8 phases (each independently shippable, gate-green)

**Phase 0 — Make the identity real** *(blocks distribution; needs decision #1)*
Rename the module to `github.com/<owner>/cassette`; one mechanical sweep across
all ~141 Go imports, `.goreleaser.yaml` (brew owner, GHCR image, homepage),
`release.yml`, README (badge/brew/docker/go-install/import line), CHANGELOG links,
docs; regenerate any GIF with a baked path. Add a permanent **CI grep guard** that
fails on any residual `github.com/example/`. Fix the "33 → 50" count.

**Phase 1 — Quick-wins batch** *(the list above)* — version BuildInfo, artifact
move, community-health files, `--no-color`, `warn()/note()`, docs count fix.

**Phase 2 — Honest test gate + targeted refactor** *(do early — derisks later phases)*
Exclude data-only/example packages (`internal/wirefix`, `examples/*`,
`cmd/mcpfixture`) from the coverage denominator; add **per-package floors**; add
direct `analysis/` unit tests (`DiffRecordings`, `Fuzz`, `JudgePrompt`,
`RequestModel`, `diffTools` — currently 0%/74.8%) and raise the real floor toward
85%. Add `cmd/cassette/helpers_test.go` (one `httpTurn` + shared `seedFile`,
replacing ~8 near-identical builders). Add a `readOrErr` helper (~12 sites).
**Resolve decision #2** (analysis boundary) and act on it.

**Phase 3 — Security hardening (the brand)**
Wire the existing egress detector shapes (AWS `AKIA…`, GitHub `gh*_`, JWT,
Google `AIzaSy…`) into `scrub.DefaultConfig().BodyPatterns` **and**
`secretScanPatterns` (zero new deps — regexes already exist/tested in
`internal/egress`). Add a **write-time `SecretScan` in `saveLocked`** (fail/loudly
warn on residue) — closes the scrub↔scan loop at the point that matters. Default
proxy bind to **`127.0.0.1`**; require explicit `--addr` for non-loopback + warn in
record mode. Bound `wirefmt.Load` with `io.LimitReader` + size cap. Add
**govulncheck** to CI. SECURITY.md disclosure path. (Decision #3: cosign/SBOM.)

**Phase 4 — Distribution completion + first release**
Wire **`HOMEBREW_TAP_TOKEN`** (`token:` in brews + env in release.yml). Add an
**arm64 dockers entry + `docker_manifests`** for a multi-arch image. Add
**macos-latest** (and evaluate windows) to the CI matrix. Snapshot/`-rc` dry-run
the release pipeline, then cut the first real **`v0.x`** tag.

**Phase 5 — CLI discoverability + terminal-UX consistency**
Add a `Group` field to the `Command` struct; tag all 50 with the ~5 groups already
in `docs/cli.md`; render **grouped `--help`** with section headers. **Did-you-mean**
(edit-distance) on unknown commands. **`cassette completion {bash,zsh,fish}`**
generated from the registry (pure-Go, no cobra). Codify the **terminal style
guide** (below): ASCII glyph fallback via `CASSETTE_ASCII`/locale; Windows color
default-off unless `WT_SESSION`/`FORCE_COLOR`; standardize **`--json`** on reporting
commands + a registry test; `text/tabwriter` for multi-column tables.

**Phase 6 — Docs information architecture**
`docs/README.md` hub linking Quickstart → Guide → CLI → Features → Security → MCP →
Architecture(DECISIONS) → Demos, with sibling/back links. New pages:
`docs/security.md` (scrubbing/egress/attestation/proxy safety), `docs/mcp.md`,
`docs/faq.md` (troubleshooting), `docs/glossary.md`, a "vs go-vcr / vcrpy / nock"
positioning section. Complete `docs/cli.md` (ideally **generated from the registry**
so the count can't drift again).

**Phase 7 — Per-feature visual docs** *(your ask)*
Honesty pass on the hero GIFs (decision #4). Regenerate the tour with no stale
count/URL. Produce short, reproducible, size-bounded per-feature clips and embed
each next to its feature (see GIF plan). CI guardrail: generators exit-0 +
doc-lint asserting claimed facts (digest, command count) still match the binary —
**do not** byte-compare GIFs (non-deterministic across Pillow/vhs versions).

## Docs information architecture (target)

| Doc | Purpose |
|-----|---------|
| `README.md` | Crisp pitch + hero gif + install + 5-line quickstart + links out (slim) |
| `docs/README.md` | Hub / table of contents cross-linking every page |
| `docs/guide.md` | Library + `go test` usage, record/replay, the equivalence guarantee |
| `docs/cli.md` | Full 50-command reference (generated from the registry) |
| `docs/features.md` | Conceptual feature tour (with per-feature gifs) |
| `docs/security.md` | Scrubbing, egress-audit/taint, attestation, proxy safety, threat model |
| `docs/mcp.md` | Recording/replaying MCP + `mcp-serve` |
| `docs/faq.md` | Troubleshooting (replay miss → doctor, stale key → rekey), positioning vs go-vcr/vcrpy |
| `docs/glossary.md` | Cassette / interaction / match key / transcript / digest / cstiron |
| `docs/demos.md` | The reproducible gif gallery (exists) |
| `DECISIONS.md` | ADRs — surfaced from the docs hub |
| `docs/internal/` | Build-loop artifacts moved out of root (IDEAS, FEATURE_RESEARCH, prompts) |

## Per-feature GIF plan (your ask)

Today's 5 GIFs cover the hero loop + serve + branch + a 5-prompt session. Add
short (≤8 s), reproducible (`vhs` tape in `scripts/`), size-bounded clips for the
features that *sell themselves visually*, embedded next to the feature:

| Asset | Feature shown | Placement |
|-------|---------------|-----------|
| `demo-diff.gif` | `cassette diff` red/green semantic divergence | features.md (review), README "diff" |
| `demo-doctor.gif` | `doctor` diagnosing a replay miss → named fix | features.md, faq.md |
| `demo-author.gif` | `author` screenplay → replayable cassette (no key) | features.md (authoring), README |
| `demo-proxy.gif` | `proxy --mode record` capturing a non-Go (curl/python) client | features.md, README "any language" |
| `demo-taint.gif` | `taint` cross-turn exfil flow (masked) | docs/security.md |
| `demo-codec.gif` | `codec verify` / `conformance` green proof | features.md (integrity) |
| `demo-scenario.gif` | `scenario` fanning one turn into an edge/error matrix | features.md (robustness) |

Keep generation offline + deterministic-script (not byte-deterministic output);
add alt-text on every embed.

## Terminal style guide (codify these as rules + tests)

- **Single source of color:** all color flows through `style` in `ui.go`; no raw
  ANSI anywhere else (add a grep guard test for `\x1b` outside `ui.go`).
- **Precedence:** `NO_COLOR` (disables) > `CLICOLOR_FORCE`/`FORCE_COLOR` (forces) >
  TTY detection on a concrete `*os.File`. Add a `--no-color` flag that ORs into off.
- **Fixed semantic palette:** green=ok/2xx, red=fail/4xx-5xx, yellow=warn/note,
  dim=neutral, cyan=labels/headings, bold=section headers only. No new meanings.
- **One spelling per concept:** add `style.warn()`/`note()`; remove ad-hoc `⚠`/
  `"warning:"`/`"note:"` at call sites.
- **Glyphs via helpers with ASCII fallback** (`[ok]`/`[x]`/`->`/`-`) triggered by
  non-UTF-8 locale or `CASSETTE_ASCII`; no raw `✓`/`✗`/`→`/`·` in format strings.
- **Windows:** color off unless `WT_SESSION`/`FORCE_COLOR` (no `x/sys` dep).
- **Tables:** `text/tabwriter` + an ANSI-aware `padVisible()`; or keep colored
  cells in the last column.
- **No animation** on `serve`/`proxy`/`watch` — one startup banner, then quiet,
  grep-friendly, pipe-safe; per-request logging only behind `--verbose` to stderr.
- **Error contract:** `cassette: ` (stderr) for failures, `usage:` for usage; exit
  codes 0 ok / 1 fail / 2 usage. No new codes.
- **Machine output:** `--json` for single-shape reports; `--format` only where
  genuinely multi-format (dataset csv/jsonl); document + registry-test it.

## Key risks (and mitigations)

- **Partial rename** leaves a half-broken state → one mechanical sweep + permanent
  CI grep guard + re-run `scripts/ci.sh`.
- **Name availability** — verify the org/tap/GHCR namespace is obtainable *before*
  the sweep, or the work is redone.
- **Coverage floor at ~80.1%** — any added untested branch fails CI before the gate
  is decoupled → land Phase 2 (gate honesty) early; ship tests with each change.
- **First-ever tag** fires `release.yml` for the first time → wire the Homebrew
  token + multi-arch manifest in Phase 4 and do a `-rc`/snapshot dry-run first.
- **Tightening secret detectors** can false-positive on legit fixtures → named
  shapes only in the hard-fail gate; run the new write-time scan over all
  `testdata/` before enabling; keep Luhn/high-entropy audit-only.
- **Cross-OS matrix** may surface latent path/TTY/glyph divergences → treat the
  first matrix run as discovery, keep heavy demo/secret steps linux-only.

## What's already good (leave alone)

Zero-dial replay invariant; scrub-on-save; the declarative command registry +
flat package (navigable *because* of the registry — do **not** subpackage it);
exit-code discipline (3 of ~630 returns bypass the constants, all legit); the
single `style` color layer with correct `NO_COLOR` precedence; no raw-ANSI
leakage; race detector + `-update` golden flow in CI; all 6 deps MIT/Apache-2.0;
package doc comments everywhere (pkg.go.dev will render cleanly). Performance is
well within budget for KB-sized cassettes — add benchmarks as a regression guard,
but resist premature optimization.
