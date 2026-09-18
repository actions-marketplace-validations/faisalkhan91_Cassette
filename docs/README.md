# cassette documentation

cassette is a record/replay VCR for LLM HTTP/SSE + MCP: record once against a real
provider, then replay byte-exact and fully offline in your tests — no API key, no
network, deterministic.

## Start here

- **[Guide](guide.md)** — the 2-minute tour, library + `go test` usage, the cassette
  format, and the semantic-equivalence API.
- **[CLI reference](cli.md)** — every `cassette` subcommand, grouped.
- **[Demos](demos.md)** — reproducible GIFs of the headline workflows.

## Go deeper

- **[Features](features.md)** — serve a cassette as an offline endpoint, pack a
  recording into a single binary, the Castiron forward-branch workflow, migrate a
  corpus across providers, author from a screenplay, the offline dashboard, governance
  (policy + audit), OpenTelemetry export, provenance, and `init`/proxy onboarding.
- **[Security](security.md)** — the zero-dial / scrub-on-save / opt-in-egress model,
  the secret scanner, `egress-audit` / `taint`, and `proxy` safety.
- **[MCP](mcp.md)** — recording and replaying MCP tool calls, `mcp-serve`, the
  `mcp-proxy` recorder, and `kind: mcp` authoring.
- **[Recipes](recipes/README.md)** — adopt cassette in any stack: `cassette init`,
  a pytest `conftest.py`, and a reusable GitHub Action.
- **[FAQ & troubleshooting](faq.md)** — replay misses, stale keys, and how cassette
  compares to `go-vcr` / `vcrpy` / `nock`.
- **[Glossary](glossary.md)** — cassette, interaction, match key, transcript, digest,
  Castiron.

## Reference

- **[Cassette format spec (v1)](spec/cassette-format.md)** — the normative,
  language-independent on-disk format and match-key algorithm, so a recording can be
  read and replayed from any language.
- **[Architecture decisions (DECISIONS.md)](../DECISIONS.md)** — why the match key
  excludes the host, why the codec is its own layer, the coverage-gate policy, etc.
- **[Changelog](../CHANGELOG.md)** · **[Contributing](../CONTRIBUTING.md)** ·
  **[Security policy](../SECURITY.md)**

Internal planning and competitive-research notes live under [`internal/`](internal/) (roadmaps, the competitive landscape, and executed build prompts).
