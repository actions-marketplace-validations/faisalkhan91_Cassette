# Maturation build prompt — cassette to a credible launch

> Status: executed (2026-06). Kept as a provenance record of the maturation pass; not a live task.

> Paste this as the task for an autonomous build loop. It executes the plan in
> [`MATURITY_AUDIT.md`](MATURITY_AUDIT.md), one independently-shippable, gate-green
> phase at a time. Goal: take cassette from "functionally complete" to "credibly
> publishable" — real identity, trustworthy install, an honest test gate, the
> security brand, discoverable CLI, onboarding docs, and per-feature visuals.

---

## Before you start — confirm these decisions (do not guess)

These are owner decisions baked into the work. If they are not already answered in
the task, **stop and ask** rather than assume:

1. **`<OWNER>`** — the real GitHub owner/org for the module path
   `github.com/<OWNER>/cassette`, Homebrew tap (`<OWNER>/homebrew-tap`), and GHCR
   image (`ghcr.io/<OWNER>/cassette`). Verify the namespace is obtainable first.
2. **`analysis/` boundary** — public library (promote `internal/wirefmt` +
   `internal/wireenc` signature types to a public `wire/` package, commit to API
   stability) **or** CLI-internal (move `analysis/` under `internal/`, drop the
   "library consumer" framing). Default if unspecified: **internal** (lower risk).
3. **Supply-chain** — keyless cosign signing + SBOM now, or defer? Default: **defer**
   (Phase 4 ships without it; leave a TODO).
4. **Hero GIFs** — replace the two Pillow renders with real `vhs` captures, or label
   them illustrations? Default: **label as illustrations** + add real per-feature clips.
5. **Secret hard-fail scope** — named credential shapes (AWS/GitHub/JWT/Google +
   provider keys) block CI; Luhn/high-entropy stay audit-only. Default: **as stated**.

## Loop discipline (every phase)

1. Implement the smallest correct version that fully satisfies the phase.
2. `gofmt -w` changed files; `go build ./...`; `go vet ./...`.
3. Add/extend tests (table-driven, offline). Run `bash scripts/ci.sh` — it must
   exit 0 and meet the coverage floor.
4. Update `README.md` / `docs/*` / `CHANGELOG.md` / `DECISIONS.md` as warranted;
   wire any new command into the registry + `help_test.go`.
5. Commit each phase separately (conventional prefix; no AI footer). Then continue.

Work phases **in order** (0 → 7); 0 gates the rest. Phases 5–7 are largely
independent and may be reordered. Do not stop between phases to ask for approval
unless you hit one of the decisions above or a hard-constraint conflict.

## Non-negotiable constraints (unchanged)

Pure Go; offline CI (`GOFLAGS=-mod=vendor`, `GOPROXY=off`); **no new heavy deps**
(stdlib + the existing 6 only; cosign/syft/govulncheck are CI-installed *binaries*,
not Go modules — allowed); deterministic/byte-exact default path; **secrets never
committed** (run the secret scan before any write); `gofmt`+`go vet` clean;
coverage floor (see Phase 2); small curated public API; one phase per commit.

---

## Phase 0 — Make the identity real *(blocker; needs decision #1)*

- Rename `module github.com/example/cassette` → `github.com/<OWNER>/cassette` in
  `go.mod`. One mechanical sweep (`grep -rl 'github.com/example/cassette' --exclude-dir=vendor | xargs sed -i ''`)
  across **all** `.go` imports, `.goreleaser.yaml` (`brews.repository.owner`,
  `dockers` image_templates, `homepage`), `.github/workflows/release.yml`,
  `README.md` (badge, `brew`/`docker`/`go install`/`import` lines), `CHANGELOG.md`
  release links, and `docs/*`.
- Add a permanent CI guard in `scripts/ci.sh`: fail if `grep -rn 'github.com/example/' --exclude-dir=vendor .`
  matches anything.
- Fix the **command count**: README says "33", the registry has 50 — replace with
  the real number or drop it; update `scripts/gen-demo-tour.py` and regenerate
  `assets/demo-tour.gif` if the count is baked in.
- **Done when:** `go build ./...` + `bash scripts/ci.sh` pass, the grep guard is
  green, and no `example/` references remain outside `vendor/`.

## Phase 1 — Quick-wins batch

- `cmd/cassette/version.go`: add a `debug.ReadBuildInfo()` fallback so a
  `go install …@latest` build prints the real module version instead of `dev`;
  embed `-X main.commit={{.Commit}} -X main.date={{.CommitDate}}` in
  `.goreleaser.yaml` ldflags and surface them in `cmdVersion`.
- Move `IDEAS.md`, `FEATURE_RESEARCH.md`, `PLAN6_BUILD_PROMPT.md`,
  `MATURITY_AUDIT.md`, `MATURITY_BUILD_PROMPT.md` → `docs/internal/` (keep for
  provenance) — they are build-loop scratch and shouldn't greet a visitor at root.
- Add `SECURITY.md` (disclosure path), `CONTRIBUTING.md` (document offline-CI
  invariants: `-mod=vendor`, `GOPROXY=off`, gofmt+vet, the floor, `scripts/ci.sh`),
  `.editorconfig` (LF, final newline, trim ws; tabs for `.go`, spaces elsewhere),
  `assets/.gitattributes` (`*.gif binary`).
- `ui.go`: add `style.warn()` (yellow `!`) and `style.note()`; replace ad-hoc
  `⚠`/`"warning:"`/`"note:"` call sites (e.g. `graft.go`). Add a global
  `--no-color` flag that ORs into the style "off" decision.
- Complete `docs/cli.md` (add `version`, `verify-attest`, and any other omissions).

## Phase 2 — Honest test gate + targeted refactor *(do early)*

- In `scripts/ci.sh`, compute the coverage gate over **shippable logic only** —
  exclude `internal/wirefix` (data-only `//go:embed`), `examples/*`, and
  `cmd/mcpfixture`. Add **per-package floors** (loop `go tool cover -func` per
  package) so a weak package can't hide behind the aggregate; document the policy
  in `DECISIONS.md`.
- Add direct `analysis/` unit tests for the currently-0%/under-floor pure
  functions: `DiffRecordings`, `Fuzz`, `JudgePrompt`, `RequestModel`, `diffTools`,
  `logicalKey` (they take `*wirefmt.File`, trivially table-testable). Raise the real
  `analysis` package over 80% on its own merits, then raise the floor toward 85%.
- Add `cmd/cassette/helpers_test.go`: one `httpTurn(url, reqBody, respBody, opts…)`
  + a shared `seedFile`, replacing the ~8 near-identical `*Turn` builders.
- Add `readOrErr(path, stderr) ([]byte, bool)` next to `loadOrErr`; adopt it at the
  ~12 hand-rolled `os.ReadFile` + `cassette: %v` sites.
- **Act on decision #2** (analysis boundary). Do **not** split the flat command
  package — the registry makes it navigable.
- **Done when:** the gate reflects real logic coverage, `analysis` ≥ floor on its
  own, and the suite is green with `-race`.

## Phase 3 — Security hardening (the brand)

- Wire the existing `internal/egress` detector shapes (AWS `AKIA…`, GitHub `gh*_`,
  JWT, Google `AIzaSy…`) into **both** `scrub.DefaultConfig()` body patterns **and**
  `secretScanPatterns` (named shapes only; keep Luhn/high-entropy audit-only).
- Add a **write-time `SecretScan` in `saveLocked`** (`cassette.go`): fail (or loudly
  warn) on residue before writing — closes the scrub↔scan loop. Run it over all
  existing `testdata/` first to confirm no false positives.
- Proxy: default bind to **`127.0.0.1`**; require an explicit `--addr` for a
  non-loopback bind and warn in record mode (don't route credentialed traffic off-box).
- Bound `wirefmt.Load` with `io.LimitReader` + a generous size cap (hostile-input
  safety).
- Add **govulncheck** to CI (`go run golang.org/x/vuln/cmd/govulncheck@… ./...`,
  network-gated step or vendored binary). Decision #3: cosign keyless + `sboms:` +
  `id-token: write` if signing now; else leave a documented TODO.

## Phase 4 — Distribution completion + first release

- Homebrew: add `token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"` to the brews entry and
  `HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}` in `release.yml`.
- Docker: add a `linux/arm64` dockers entry with arch-suffixed tags + a
  `docker_manifests:` block combining amd64+arm64 into `:{{.Version}}` and `:latest`.
- CI: add `macos-latest` to the test matrix (evaluate `windows-latest`; keep heavy
  demo/secret steps linux-only). Treat the first matrix run as discovery.
- Dry-run `goreleaser release --snapshot --clean` (and/or a `vX.Y.Z-rc1` tag), then
  cut the first real **`v0.x.0`** tag.

## Phase 5 — CLI discoverability + terminal-UX consistency

- Add a `Group` field to the `Command` struct; tag all 50 with the ~5 groups from
  `docs/cli.md` (authoring / analysis & review / safety / serving & proxy /
  maintenance); render **grouped `--help`** with bold section headers in fixed order.
- **Did-you-mean** on unknown command (pure-Go Levenshtein over registry names).
- `cassette completion {bash,zsh,fish}` — generated from the registry (no cobra);
  wire `bash_completion.install` into the brews `install:` block.
- Codify the **terminal style guide** from `MATURITY_AUDIT.md`: ASCII glyph fallback
  via `CASSETTE_ASCII`/non-UTF-8 locale; Windows color default-off unless
  `WT_SESSION`/`FORCE_COLOR`; a grep-guard test for raw `\x1b` outside `ui.go`;
  migrate multi-column tables (`dataset`/`seeds`/`coverage`/`inspect`) to
  `text/tabwriter` with an ANSI-aware `padVisible`.
- Standardize **`--json`** on reporting commands that lack it (e.g. `cost`,
  `egress-audit`, `diff`, `refusal`); reserve `--format` for genuinely multi-format
  output; add a registry test asserting reporting commands accept `--json`.

## Phase 6 — Docs information architecture

- Add `docs/README.md` hub cross-linking Quickstart → Guide → CLI → Features →
  Security → MCP → Architecture(DECISIONS) → Demos (sibling + back links).
- New pages: `docs/security.md` (scrubbing/egress/taint/attestation/proxy threat
  model), `docs/mcp.md`, `docs/faq.md` (replay-miss→doctor, stale-key→rekey, "vs
  go-vcr/vcrpy/nock" positioning), `docs/glossary.md`.
- Make `docs/cli.md` **generated from the registry** (a small `go generate` or
  test-checked doc) so the command list/count can never drift again.

## Phase 7 — Per-feature visual docs *(the GIF ask)*

- Honesty pass on the two Pillow hero GIFs (decision #4). Regenerate the tour with
  no stale count/URL.
- Produce short (≤8 s), reproducible (`vhs` tape in `scripts/`), size-bounded clips
  and embed each next to its feature (with alt-text), per the GIF plan in
  `MATURITY_AUDIT.md`: `diff` (red/green), `doctor`, `author`, `proxy` (a curl/python
  client), `taint`, `codec verify`, `scenario`.
- CI guardrail: assert generator scripts exit 0 and a doc-lint that claimed facts
  (command count, demo digest) still match the live binary. **Do not** byte-compare
  GIFs (non-deterministic across Pillow/vhs versions).

## Stop conditions

Stop and surface to the owner if: a phase needs a new heavy Go dependency; a
constraint genuinely conflicts with a requirement; or one of the five decisions
above is unresolved. Otherwise pick the smallest-correct option, note it in the
commit, and keep going. When all phases are done: run the full gate once more,
update `CHANGELOG.md` + `DECISIONS.md`, tick `MATURITY_AUDIT.md`, and report what
shipped + what was deferred and why.
