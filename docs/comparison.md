# cassette vs. alternatives

How cassette compares to the tools people reach for when they want LLM/agent tests
to stop hitting the network. Sourced from a fact-checked survey
([internal landscape report](internal/COMPETITIVE_LANDSCAPE.md); each claim
adversarially verified) — **read the caveats at the bottom before quoting this.**

## The short version

No tool we could verify occupies cassette's exact niche: an **offline, byte-exact,
secret-aware, codec-backed** record/replay VCR built for LLM **HTTP + SSE *and*
MCP**. The space splits into (a) generic HTTP record/replay libraries that are
runtime-siloed, have no first-class SSE, and know nothing of LLM dialects or MCP;
and (b) LLM eval/observability platforms that run **live** against provider APIs
with no recorded-fixture mechanism.

The one thing cassette does **not** uniquely own is plain offline deterministic
replay — that is mature prior art (VCR.py, go-vcr, nock, PollyJS, WireMock all do
it). cassette's edge is everything *around* it for the agentic stack.

## Feature matrix

| | cassette | go-vcr | VCR.py | nock | PollyJS | mitmproxy | eval platforms¹ |
|---|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| Offline deterministic replay | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (after record) | ❌ (run live) |
| First-class **SSE** streaming replay² | ✅ | ❌ | ❌ | ⚠️ generic chunks | ❌ | ⚠️ buffers by default | — |
| **Secret scrub-on-save** (by pattern) | ✅ | ❌ | ⚠️ opt-in key filters | ⚠️ drops headers | ❌ | ❌ | — |
| **MCP** (JSON-RPC) record/replay³ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Cross-dialect **Transcript⇄wire codec**⁴ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Offline **LLM-as-judge-as-fixture** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ⚠️ live judges |
| Any-language reach via a proxy | ✅ (LLM/SSE/MCP-aware) | ❌ Go-only | ❌ Python-only | ❌ Node-only | ❌ JS-only | ✅ (generic HTTP) | varies |
| Hosted dashboards / collaboration UI | ❌ (offline static HTML) | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Language-native ergonomics | Go (proxy elsewhere) | ✅ Go | ✅ Python | ✅ JS | ✅ JS | n/a | n/a |

¹ promptfoo, LangSmith, Braintrust, Langfuse, Helicone, et al. Only **promptfoo** was
independently verified in the survey; the rest are partly inferential (see caveats).
² VCR.py issue #989 and mitmproxy issue #4469 (default buffering "defeats the purpose
of SSE") were **open** as of the survey — this gap could narrow if they're fixed.
³ The MCP testing/mocking category produced **zero** verified competing claims — it is
cassette's least-contested but also least-confirmed differentiator.
⁴ No prior art for a round-trippable codec transpilable across
Anthropic/OpenAI/Gemini/Ollama surfaced at all.

## Where the alternatives are honestly stronger

- **Ecosystem maturity & language-native ergonomics.** VCR.py/pytest-* in Python,
  nock/PollyJS in JS, go-vcr in Go, WireMock in the JVM are each the de-facto native
  choice in their runtime. cassette is Go-native; everywhere else it's the proxy.
- **Hosted dashboards & collaboration.** The SaaS eval/observability platforms offer
  UI, trace exploration, and team features cassette deliberately doesn't — it ships
  an offline static report (`cassette dashboard`) and the `pack` browser instead.

cassette trades breadth-of-ecosystem for depth-on-LLM-replay.

## When to pick cassette

- You stream (SSE) and want the **exact bytes**, frame boundaries included, replayed
  offline — the universal gap in generic VCRs.
- You record real provider traffic and refuse to commit secrets — **scrub-on-save**
  plus a write-time secret-scan backstop.
- You test **MCP** tools, or want to **de-risk a provider switch** by migrating a
  recorded corpus across dialects with a no-silent-drift guarantee (`cassette migrate`).
- You want CI that is **fast, free, and green** with no key and provably zero egress.

## When *not* to

- You only need plain offline HTTP replay in one language and want the native idiom —
  reach for the runtime's own VCR (go-vcr, VCR.py, nock, PollyJS).
- You want a hosted dashboard, live evals against production models, or team
  collaboration UI — the eval/observability platforms are built for that.

## Caveats

- **Absence of evidence ≠ evidence of absence.** Most "❌"s rest on each project's own
  docs/README: authoritative for what a tool *advertises*, only suggestive for what it
  *can't* do (a tool might pass streamed bytes through an adapter its docs never mention).
- **The eval category is under-verified** — only promptfoo was independently confirmed.
- **Time-sensitivity** — the open SSE issues (#989, #4469) could be resolved, narrowing
  that gap for those tools.

Full methodology, confidence levels, and sources:
[internal/COMPETITIVE_LANDSCAPE.md](internal/COMPETITIVE_LANDSCAPE.md).
