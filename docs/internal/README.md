# Internal notes — planning & research

Working documents behind cassette's design: competitive research, roadmaps, and the
one-shot build prompts that were pasted into autonomous build loops. Provenance, not
user docs. Kept in git (not deleted) so the reasoning behind shipped work is
recoverable.

## Active

| Doc | What |
|-----|------|
| [VISION_2026H1.md](VISION_2026H1.md) | **Latest** — product vision & strategy: market map, defensible wedge, feature gaps, new areas, cohesion + zero-config setup story, roadmap. Source-verified (incl. the 2026 "VCR for LLM" competitor swarm + MCP record/replay tools). Supersedes the competitive read below where they differ. |
| [COMPETITIVE_LANDSCAPE.md](COMPETITIVE_LANDSCAPE.md) | Fact-checked survey of the record/replay + eval space. **Publicly linked** from [`docs/comparison.md`](../comparison.md). Predates the 2026 competitor swarm — see VISION_2026H1 for the refresh. |
| [IDEAS.md](IDEAS.md) | Greenfield idea backlog — candidates, not commitments. |
| [archive/FEATURE_RESEARCH_R4.md](archive/FEATURE_RESEARCH_R4.md) | **Newest research (unconsumed)** — round-4 idea exploration across the GitHub community + adjacent domains (record/replay debuggers, snapshot testing, MCP spec surface, AI red-team, trace interop). 83 vetted ideas, 8 themes; strongest signal = a snapshot-tool-grade review/pending/promote lifecycle + MCP-2025-06-18 governance surface + deterministic redteam detectors. Candidates, not commitments. |
| [FEATURE_COMPLETE_PLAN.md](FEATURE_COMPLETE_PLAN.md) | The definitive feature-complete scope. **Live roadmap** — slices remain unshipped. |
| [FEATURE_COMPLETE_BUILD_PROMPT.md](FEATURE_COMPLETE_BUILD_PROMPT.md) | The loop-execute prompt for the plan above. **Still actionable.** |

## Archived ([`archive/`](archive/))

Executed: the work these describe has shipped. Build prompts are spent one-shots;
plans/research are consumed records. Cross-check against the code, not these.

| Doc | Status |
|-----|--------|
| archive/DIFFERENTIATION_PLAN.md · DIFFERENTIATION_BUILD_PROMPT.md | All 6 slices shipped (mcp-proxy, dashboard, migrate, init, MCP authoring, positioning). |
| archive/MATURITY_AUDIT.md · MATURITY_BUILD_PROMPT.md | Phases 0–7 shipped (install, tests, distribution, docs, GIFs, CLI UX). |
| archive/PLAN6_BUILD_PROMPT.md · FEATURE_RESEARCH.md | Authoring + any-language-reach wedge — shipped. |
| archive/PLAN7_BUILD_PROMPT.md · FEATURE_RESEARCH_R3.md | Provider breadth + deterministic eval/CI — shipped; remaining ideas folded into FEATURE_COMPLETE_PLAN. |
