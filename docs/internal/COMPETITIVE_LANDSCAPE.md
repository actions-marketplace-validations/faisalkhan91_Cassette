# Competitive landscape — where cassette sits (June 2026)

A fact-checked survey of the LLM record/replay + testing/eval tooling space, to
inform cassette's positioning. Produced by a deep-research fan-out (103 agents;
each falsifiable claim adversarially verified, 2-of-3 refutes to kill). Confidence
and sources are noted per finding; honest caveats are at the end.

## Bottom line

**No verified tool occupies cassette's niche** — an offline, byte-exact,
secret-aware, codec-backed, deterministic record/replay VCR purpose-built for LLM
HTTP/SSE *and* MCP. The market splits into (a) **generic HTTP record/replay
libraries** that are runtime-siloed, have no first-class SSE, and know nothing of
LLM dialects or MCP; and (b) **LLM eval/observability platforms** that run **live**
against provider APIs with no recorded-fixture or judge-as-fixture mechanism.
The one thing cassette does *not* uniquely own is plain offline deterministic
replay — that's mature prior art.

## (a) Generic HTTP record/replay libraries

VCR.py (+ pytest-vcr, pytest-recording), go-vcr, nock, PollyJS, WireMock, mitmproxy.

- **Runtime-siloed; only a proxy is language-agnostic.** *(high, 3-0)* VCR.py and
  plugins are Python-only; nock overrides Node's `http.request`/`ClientRequest`
  (Node-only); PollyJS is JS/Node+browser; go-vcr is Go-only. Only an external
  proxy (mitmproxy, WireMock `proxyAllTo`) is cross-language — the model cassette's
  reverse proxy follows, but those proxies are generic HTTP with no LLM/SSE/MCP
  awareness. [pytest-recording], [pytest-vcr], [nock], [pollyjs], [go-vcr]
- **No first-class SSE — the dominant LLM streaming format — anywhere.** *(high,
  3-0; mitmproxy detail 2-1)* VCR.py, pytest-recording, go-vcr, and PollyJS document
  no SSE/streaming; VCR.py issue **#989** (SSE/streamable-http) is **open and
  unanswered**. nock can replay a generic chunked stream but documents no SSE.
  mitmproxy issue **#4469** (open since 2021): the maintainer confirms default
  buffering means a `text/event-stream` "will buffer it until it's closed. But that
  defeats the purpose of SSE" (a non-default `stream=True` workaround exists). This
  is cassette's core competency. [vcrpy-advanced], [go-vcr], [pollyjs], [nock],
  [mitmproxy-4469]
- **Secret handling is opt-in, never semantic scrub-on-save.** *(high, 3-0)* VCR.py
  offers opt-in key-scoped filters (`filter_headers`, `filter_query_parameters`,
  `filter_post_data_parameters`) and its docs **warn** unfiltered cassettes leak API
  keys/passwords; nock simply **drops** request headers by default (not scrub); 
  WireMock has only custom `StubMappingTransformer` code + selective `captureHeaders`;
  PollyJS documents none. None scrubs arbitrary body secrets on save by pattern —
  cassette's model. [vcrpy-advanced], [pytest-vcr], [nock], [wiremock], [pollyjs]
- **Offline deterministic replay is mature — cassette is NOT differentiated here.**
  *(high, 3-0)* VCR.py `none`/`once` modes, PollyJS, WireMock (offline after
  recording stops; deterministic sequences via `repeatsAsScenarios`), nock
  (record/lockdown/dryrun), and go-vcr all replay offline deterministically. This is
  table stakes, not an edge. [vcrpy-advanced], [pollyjs], [wiremock], [nock], [go-vcr]

## (b) LLM eval / prompt-testing / observability platforms

promptfoo, LangSmith, Braintrust, Langfuse, Helicone, OpenLLMetry, DeepEval, Ragas.

- **They run LIVE; no offline byte-exact replay or judge-as-fixture surfaced.**
  *(medium — only promptfoo independently verified; 3-0)* promptfoo (open-source,
  MIT, runs orchestration locally) talks **directly to the live LLM**, not
  recorded/replayed traffic, and sends anonymous telemetry by default. "Runs
  locally" is vendor framing: orchestration is local but inference calls go out.
  The other platforms were named but produced no surviving verified claims — see
  caveats. [promptfoo]

## (c) MCP record/replay tooling

- **Not established by this research.** *(open question)* The MCP testing/mocking
  category produced **zero verified claims** — it was effectively un-surfaced in
  this batch, making it cassette's least-contested but least-confirmed differentiator.

## The gap & cassette's differentiation

- **The full combination is unrepresented.** *(medium; absence-of-evidence across
  3-0 verified sources)* Byte-exact recording + deterministic semantic-equivalence
  digests + a round-trippable Transcript⇄wire codec transpilable across
  Anthropic/OpenAI/Gemini/Ollama + MCP record/replay + offline LLM-as-judge-as-fixture
  + ed25519 behavior attestation — no verified tool offers this set.
- **Sharpest concrete wedges:** first-class **SSE streaming** replay (the universal
  gap), **semantic scrub-on-save** (vs opt-in/drop), **any-language reach via a
  proxy that is LLM/SSE/MCP-aware** (vs generic proxies), and **offline
  judge-as-fixture eval** (vs live eval platforms).

## Where incumbents are honestly stronger

*(high, 3-0)* **Ecosystem maturity & language-native ergonomics** — VCR.py/pytest-*
in Python, nock/PollyJS in JS, go-vcr in Go, WireMock in the JVM/integration world
are each the de-facto native choice in their runtime. **Hosted dashboards** — the
SaaS eval/observability platforms offer UI, collaboration, and trace exploration
cassette deliberately doesn't (it ships `pack`'s offline browser instead). cassette
trades breadth-of-ecosystem for depth-on-LLM-replay.

## Caveats (read before quoting this)

- **Source weakness:** most findings rest on each project's own docs/README —
  authoritative for capability *presence*, only suggestive for *absence* (a tool
  could pass streamed bytes through an adapter even if its docs never mention SSE;
  verifiers flagged this hedge).
- **Eval category under-covered:** only **promptfoo** was independently verified.
  LangSmith, Braintrust, Langfuse, Helicone, OpenLLMetry, DeepEval, Ragas were named
  but yielded no surviving verified claims, so category-wide statements are partly
  inferential.
- **MCP not researched** in this batch (zero verified claims).
- **Time-sensitivity:** VCR.py #989 and mitmproxy #4469 were **open** as of the
  latest data but could be resolved, eroding the streaming gap for those tools.
- **A refuted claim worth noting:** the specific assertion that "mitmproxy fails SSE
  with an HTTP/2 error" was **rejected 0-3** — the real failure mode is *buffering*,
  not a hard error.

## Open questions to chase next

1. What MCP record/replay/mocking tooling actually exists in June 2026?
2. Among the unverified eval/observability platforms, does any do offline
   deterministic replay or judge-from-a-fixture, and which are SaaS-only?
3. Have VCR.py #989 / mitmproxy #4469 been resolved (narrowing the SSE gap)?
4. Does any tool implement a cross-dialect Transcript⇄wire codec or ed25519
   behavior attestation? (No prior art surfaced at all.)

## Sources

- [vcrpy-advanced] https://vcrpy.readthedocs.io/en/latest/advanced.html
- [pytest-vcr] https://pytest-vcr.readthedocs.io/en/latest/
- [pytest-recording] https://github.com/kiwicom/pytest-recording
- [nock] https://github.com/nock/nock
- [pollyjs] https://github.com/Netflix/pollyjs/
- [go-vcr] https://github.com/dnaeon/go-vcr
- [wiremock] https://wiremock.org/docs/record-playback/
- [mitmproxy-4469] https://github.com/mitmproxy/mitmproxy/issues/4469
- [vcrpy-989] https://github.com/kevin1024/vcrpy/issues/989
- [promptfoo] https://www.promptfoo.dev/
