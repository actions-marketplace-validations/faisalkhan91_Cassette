# Decisions

Every nontrivial choice, the VERIFY-BEFORE-CODE outcomes, and the resolved module path.

## Environment (verified in this build environment)
- `go version` → **go1.27.1 darwin/arm64**. go.mod `go` directive set to **1.27.1** (matches installed; no `toolchain` line). Bumped from 1.24 when dependencies were updated across the board — the MCP SDK v1.8.0 and `golang.org/x/sync` require Go 1.25+, and CI/Dockerfile/release/action pins moved to 1.27 to match.
- **No network, no API key** — permanent and expected. All deps resolved from the local module cache with `GOPROXY=off`. The build vendors deps and runs with `GOFLAGS=-mod=vendor` so CI is fully offline.
- Module path: **`github.com/faisalkhan91/cassette`** (stable placeholder, used consistently).

## Dependency scope
- Pinned, confirmed present in module cache:
  - `github.com/anthropics/anthropic-sdk-go v1.26.0` (module path has **no** `/v` suffix at v1.26.0 — confirmed).
  - `github.com/modelcontextprotocol/go-sdk v0.8.0` (official MCP SDK, pinned exactly; not third-party mcp-go).
  - `gopkg.in/yaml.v3 v3.0.1`.
- **OpenAI scoped OUT of v0.1 (follow-up slice).** `github.com/openai/openai-go/v3` is **not** in the module cache and cannot be fetched (no network). Per the build contract, the Anthropic path alone satisfies the north-star; OpenAI is a documented follow-up. The matching/format/transport layers are provider-agnostic, so adding the OpenAI call surface later is additive (no redesign).

## VERIFY-BEFORE-CODE outcomes (confirmed via `go doc` against installed versions)
- `option.WithHTTPClient(client option.HTTPClient)` where `HTTPClient interface { Do(*http.Request)(*http.Response,error) }` — an `*http.Client` satisfies it; docs recommend a custom `http.RoundTripper` transport. Seam confirmed.
- `option.WithBaseURL`, `option.WithAPIKey`, `option.WithMaxRetries` all present.
- `anthropic.NewClient(opts...) Client`; `Client.Messages MessageService`.
- `MessageService.New(ctx, MessageNewParams, ...opt) (*Message, error)`.
- `MessageService.NewStreaming(ctx, MessageNewParams, ...opt) *ssestream.Stream[MessageStreamEventUnion]`.
- `MessageNewParams{ MaxTokens int64 (required), Messages, Model }`.
- Streaming union type is **`MessageStreamEventUnion`** (confirmed exported).
- `(*Message).Accumulate(MessageStreamEventUnion) error` — there is **no** `GetFinalMessage()`; accumulate in a `for stream.Next()` loop then check `stream.Err()`.
- Latest Opus model constant present is **`anthropic.ModelClaudeOpus4_6`** (`"claude-opus-4-6"`). `ModelClaudeOpus4_8` does NOT exist at v1.26.0 — using `ModelClaudeOpus4_6`.
- Helpers: `anthropic.NewUserMessage(...ContentBlockParamUnion) MessageParam`, `anthropic.NewTextBlock(string) ContentBlockParamUnion`, `anthropic.NewAssistantMessage(...)`.
- `ssestream`: `Stream[T any]`, `NewDecoder(*http.Response) Decoder`, `RegisterDecoder(contentType, ...)` — decoder dispatch keyed on Content-Type.
- MCP:
  - `mcp.AddTool[In,Out](*Server, *Tool, ToolHandlerFor[In,Out])`; handler `func(ctx, *CallToolRequest, In)(*CallToolResult, Out, error)` — takes **`*CallToolRequest`** (not `*CallToolParams`), confirmed.
  - `(*ClientSession).ListTools(ctx, *ListToolsParams)(*ListToolsResult, error)` and `.CallTool(ctx, *CallToolParams)(*CallToolResult, error)` — satisfies our `MCPCaller`.
  - `CallToolParams{ Name string; Arguments any }`; `CallToolResult{ Content []Content; StructuredContent ... }`.
  - `Content` interface; `TextContent{ Text string; ... }` (build/read text via this concrete type).
  - `NewServer(*Implementation, *ServerOptions) *Server`; `NewClient(*Implementation, *ClientOptions) *Client`; `Implementation{ Name, Version, Title }`.
  - `(*Client).Connect(ctx, Transport, *ClientSessionOptions)(*ClientSession, error)`; `(*Server).Run(ctx, Transport) error`.
  - Transports: `&mcp.CommandTransport{Command: *exec.Cmd}` (client side), `&mcp.StdioTransport{}` (server child), `mcp.NewInMemoryTransports()` (in-process unit test).

## `go mod tidy` offline (yaml.v3 test-dep trap)
- `gopkg.in/yaml.v3@v3.0.1` ships **no `go` directive**, so the module graph is left *unpruned* and yaml.v3's own **test** suite dependency `gopkg.in/check.v1` is dragged into the graph. The pinned `check.v1` version's module zip is **not** in the local cache and cannot be fetched (no network). This dependency is part of yaml.v3's *test* suite only — it is never built or imported by cassette.
- Plain `go mod tidy` therefore exits non-zero offline (it tries to download `check.v1`). `go mod tidy -e` ("proceed despite package-load errors") exits **0**, is **stable** (running it twice yields no diff), and leaves a correct go.mod/go.sum for our actual build.
- **Decision:** `scripts/ci.sh` runs `go mod tidy -e` (not plain tidy) and asserts no git diff in go.mod/go.sum, PLUS `go mod verify` (exit 0, "all modules verified"). This catches real dependency drift in our module while tolerating the unreachable test-only dependency of a third-party module. Documented here so the deviation from plain `tidy` is explicit.

## Persisted match key (record-time), not recompute-at-load
- The match key is computed at **record time** over the live request — gunzipped and **before** scrubbing — and persisted as `request.match_key`. At replay, `index()` uses the stored key and the live lookup uses the same `match.Key` function over the live (gunzipped, unscrubbed) request. This keeps "one `MatchKey` function used identically on both paths" while making replay matching **independent of body scrubbing and stored-body encoding**.
- Why: an adversarial audit found that recomputing the key from the *stored* body at load time broke replay whenever (a) a body secret was scrubbed (stored body ≠ live body) or (b) the request body was gzip-encoded (the load path didn't gunzip while the live path did). Persisting the key fixes both. Hand-written cassettes that omit `match_key` fall back to recomputation. Regression tests: `TestReplay_RequestBodySecretStillMatches`, `TestReplay_GzipRequestBodyMatches`.
- The streaming-capture tee uses a mutex-guarded manual tee (not `io.TeeReader`) so a `Close()` from another goroutine (permitted by the `http.Response.Body` contract to unblock a `Read`) cannot race the capture buffer.

## Vendoring & offline CI
- Dependencies are **vendored** (`go mod vendor`) and the build/test run with `GOFLAGS=-mod=vendor` + `GOPROXY=off`, so `scripts/ci.sh` is fully offline by construction — no module is ever fetched. The only sockets opened during tests are loopback `httptest` servers on the record legs; every replay leg asserts **zero** dials via the dial counter, and the north-star demo prints `outbound dials: 0`.
- `scripts/ci.sh` is the single gate (the autonomous loop's stop condition) and the GitHub Actions workflow calls it and nothing else, with no secrets configured.
- The `-race` step keeps cgo enabled (the race detector requires it); only the Windows cross-build step sets `CGO_ENABLED=0`.
- Coverage floor is **80%**; the suite currently reports ~85% total. The CLI is covered both via `os/exec` on the built binary (real exit codes) and in-process `run()` calls (coverage attribution).

## Wire fixtures location
- Verbatim real-wire SSE fixtures live in **`internal/wirefix/wire/*.sse`** (embedded via `//go:embed`) rather than `testdata/wire/`, with a provenance `NOTICE`. Rationale: `//go:embed` cannot reach a sibling `testdata/` directory, and the runnable demo binary must read the fixtures offline regardless of working directory. Embedding makes them a single source of truth shared by the fake provider, the tests, and the demo.

## Tooling
- `gofumpt`, `staticcheck`, `golangci-lint` are **not installed** and cannot be `go install`ed offline. `scripts/ci.sh` runs them **only if present** and otherwise skips with a logged note (never blocks). `gofmt` (ships with Go) is always run and enforced.
- **golangci-lint is advisory, not a gate.** The offline gate (`scripts/ci.sh`: `gofmt` + `go vet`, vendored, `GOPROXY=off`) is the authoritative CI check and must stay network-free. golangci-lint needs network to install, so it runs as a **separate, network-allowed `lint` job** in `.github/workflows/ci.yml` marked `continue-on-error: true` — it surfaces findings (misspell/unconvert/bodyclose/staticcheck per `.golangci.yml`, v2 schema) on PRs but can **never** block the build. Contributors can also run it locally. A red advisory job is a hint to clean up, not a failure.

## Format / policy choices
- Cassette format: **YAML** (`gopkg.in/yaml.v3`), one file per test under `testdata/cassettes/`. `schema_version: 1`.
- Compression policy: **capture AFTER transport-level decompression**, store decoded bytes, strip `Content-Encoding`. Replay sets no `Content-Encoding`.
- Exhaustion policy default: **error** (`on_exhausted: error|repeat_last|cycle`).
- Match key default header allowlist: **EMPTY**. Volatile headers + configurable JSON paths dropped from the key only.
- multipart/form-data: keyed on ordered (field-name, content-type, content-digest), boundary token excluded. (In scope for the matcher; LLM JSON bodies are the primary path.)
- Default mode: **Replay** (safe/offline). Precedence: explicit API option > `CASSETTE_MODE` env > `-update` test flag.
- Atomic record writes: temp file + `os.Rename`, body passed through the scrubber before write.

## Pluggable matcher hooks (F1)
- `Options.Match` is the **public** `cassette.MatchConfig`, with two optional hooks, both applied identically at record-index and replay-lookup time (and persisted via `MatchKey`, so they flow to replay automatically):
  - `BodyTransform(path, body, contentType) []byte` — normalizes the body before digesting (key only; stored body untouched). Applies to BOTH the HTTP path and the MCP key path (the `path` is the URL path for HTTP, or `mcp:<method>:<tool>` for MCP).
  - `KeyFunc(cassette.MatchInput) (string, error)` — full key override. **HTTP only**: MCP keys are derived from tool name + canonical arguments, and `MatchInput` is HTTP-shaped, so `KeyFunc` is intentionally not consulted on the MCP path. Documented here per the build contract's "wire into MCP OR document HTTP-only" requirement.
- Default (nil hooks) is byte-identical to prior behavior; committed cassettes still replay.

## Public config types — no internal-type leak (F19)
- `Options.Match`/`Options.Scrub` and `RekeyFile` expose **public root types** (`cassette.MatchConfig`, `cassette.MatchInput`, `cassette.ScrubConfig`), NOT `internal/match`/`internal/scrub` types — an external module must be able to name and construct them (Go's internal-package rule otherwise blocks it). The public types are translated to the internal `match.Config`/`scrub.Config` once at `Open`/`OpenBytes` time (stored on the unexported `Cassette.match`/`.scrub` fields), so internal call sites keep working with the internal types while the API surface stays nameable. `cassette.AzureMatchConfig()`/`DefaultScrubConfig()` expose the ready-made bases. **Not aliases** — a `type X = internal.Y` alias still renders the unusable internal path in `go doc` and leaks `match.Input` through `KeyFunc`'s signature.

## OpenAI provider (F5)
- Added `github.com/openai/openai-go/v3 v3.41.0` (resolved from the now-reachable module proxy; vendored for offline CI). Import path has the **/v3** major suffix; option pkg `.../v3/option`; ssestream `.../v3/packages/ssestream`.
- Call surface implemented: **Chat Completions** — `client.Chat.Completions.New` (unary) and `.NewStreaming` (returns `*ssestream.Stream[ChatCompletionChunk]`); accumulation via `openai.ChatCompletionAccumulator{}.AddChunk(chunk)`; terminal `data: [DONE]`. Message/tool builders: `openai.UserMessage`, `openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{...})`. Model const `openai.ChatModelGPT4o`.
- No core changes were needed: cassette's transport is provider-agnostic, so OpenAI works through the same `HTTPClient()` seam. OpenAI support = real-wire fixtures (`openai_text.sse`, `openai_tooluse.sse`, hand-transcribed from the documented OpenAI streaming format, cited in NOTICE), a `semequal.DecodeOpenAISSE` byte decoder, an `internal/otrans` SDK→Transcript adapter, and record/replay tests mirroring the Anthropic path.

## Inter-chunk stream timing (F3)
- Opt-in only. `Options.CaptureTiming` records per-SSE-frame inter-arrival deltas (milliseconds, keyed to DECODED frame boundaries — gzip is decoded after capture, so raw byte offsets would not map) into an additive `response.stream_timing` field. Default OFF, so cassettes stay byte-stable across re-records (`TestStreamTiming_DefaultByteStable`).
- `Options.PaceStreaming` re-emits frames with the recorded delays via a `pacedReader` that honors `req.Context()` cancellation; falls back to plain delivery if frame/timing counts disagree. Default OFF.
- Clock and sleep are injected (`time.Now`/context-aware sleep by default) for deterministic tests.
- **Honest fidelity caveat:** deltas reflect SDK read boundaries, not server flush events (HTTP/2/buffered transports), so they are approximate arrival timing suitable for timeout/TTFT/cancellation testing — not exact wire reproduction. Documented in the README and the field comment.

## F1-F6 audit fixes
- `canon.dropPath` now handles **array indices** (gjson/sjson path syntax), so `VolatileJSONPaths` and `cassette redact-field` work on array paths like `choices.0.message.content` (previously the key-side drop was a silent no-op on arrays → replay `ErrNoMatch`). Test: `TestCanonicalizeDropping_ArrayPath`.
- Stream-timing capture is **disabled for gzip-encoded streams** (frame boundaries in compressed bytes are meaningless); paced replay tolerates an unterminated final frame (pads a zero delay) instead of silently falling back.
- README documents that matcher config is record-time-authoritative (replay-time changes need re-record or `cassette rekey`).
- OpenAI streaming test now also asserts **byte-identical** capture vs the fixture; OpenAI tests check all `VerifyError` returns.

## OpenAI Responses API + provider hygiene
- Added a `semequal.DecodeOpenAIResponsesSSE` byte decoder + `otrans.FromResponse` adapter for OpenAI's Responses API (typed SSE events: `response.output_text.delta`, `response.function_call_arguments.delta`, `response.output_item.added`, `response.completed`). Streams already record/byte-replay through the provider-agnostic transport with no core changes; only the Transcript decode was added. Fixtures hand-transcribed from the documented Responses event taxonomy.
- `match.PathTransform(method, path) string` normalizes the URL path into the key (record+replay identical), and `match.AzureConfig()` is a preset that collapses the Azure OpenAI `/openai/deployments/{name}/…` segment so one cassette replays across deployments (api-version query and host are already excluded by the path-only key).
- Scrub defaults now redact `X-Goog-Api-Key` (Gemini secret) and AWS SigV4 `X-Amz-Security-Token`/`X-Amz-Date`/`X-Amz-Content-Sha256` (secret + volatile signing fields), closing a real Gemini-key-leak gap and improving byte-stability for AWS-signed requests.

## `cassette serve` (G1)
- A cassette can be served as a real HTTP listener (`Cassette.Handler(ServeOptions)` / `cassette serve`). The lookup core (`matchResponse`) is shared with the replay RoundTripper — one implementation derives the match key, advances the per-key cursor, and returns the recorded response; `synthesize` (transport) and `writeStored` (serve) both reconstruct from that stored response, with serve writing SSE frame-by-frame + Flush.
- Honest scoping: SSE *frame bytes* are faithful but the HTTP envelope is reconstructed (Content-Encoding/Length not replayed); the dial-counter zero-egress proof is inert in serve mode, so instead the handler holds no `http.Client` and a miss is a hard 404 (never a passthrough). Routing is by path (the key already includes it), so one mixed cassette serves Anthropic + OpenAI + Responses.

## Blue-sky features (PLAN3)
- Shipped: `cassette serve` (offline LLM endpoint, shared `matchResponse` core), `mutate` (seeded SSE fuzz corpus + `cassette mutate`), `Options.Faults` chaos overlays (synthetic failures + stream corruptions, cassette never mutated), `semequal.Policy` (per-field drift tolerance) + `Invariant`s + `cassette assert`, `semequal.WireShape` (grammar fingerprint), `CanaryHits` (outbound-exfil scanner), cross-provider contract test, `Timeline` digest ledger, `cassette doc` (Markdown, `--check`), `cassette bisect` (turn+axis localization), `cassette pack` (self-contained offline demo binary).

## G11 `cassette pack` — self-extracting demo binary
`cassette pack <cassette.yaml> -o demo` produces one self-contained executable that serves an
interactive **steppable browser transcript** of the recording plus the G1 serve API, fully offline.
- **Mechanism (no Go toolchain at pack time):** append the cassette to a copy of the *running*
  `cassette` binary with a fixed 24-byte trailer `[payload][uint64 LE len][16-byte magic]`. At
  startup `main()` reads its own trailer; if the magic matches it enters packed serve mode and
  ignores subcommands. Verified empirically that appended bytes sit beyond the Go linker's signed
  code region, so darwin/arm64 + linux still run the binary (`codesign -v` reports invalid; exec
  succeeds). This sidesteps invoking `go build` (which would need a compiler + module resolution
  and was the original reason this was deferred) — packing is pure file I/O, fully offline.
- **Payload** is a stdlib-only, timestamp-free length-prefixed bundle holding just `cassette.yaml`;
  `index.html` is `//go:embed`-baked into the binary (identical everywhere) and `transcript.json`
  is computed at boot via the same `decodeTranscript` path as `cassette doc` (no second copy to drift).
- **Re-base, don't nest:** packing an already-packed binary strips the existing trailer first so the
  output never grows with stale payloads.
- **Caveats (documented in usage):** same OS/arch only (the artifact is a copy of the host binary);
  macOS reports the packed binary's signature invalid (still runs locally, not a notarized
  distributable); don't post-process the output (`strip`/UPX) since the trailer lives at EOF;
  Windows PE is unsupported and `pack` refuses there. `pack` also refuses to embed a cassette that
  trips `scrub.SecretScan`, so credentials never ship inside a binary.
- **Testing:** pure helpers (trailer/bundle round-trips, transcript projection, `servePackedMux`
  via httptest, `OpenBytes`) are in-process and coverage-counted; the headline `os/exec` smoke test
  builds the CLI, packs a fixture, runs the produced binary on `127.0.0.1:0`, asserts the UI +
  `/transcript.json` + a replayed provider response, and confirms graceful SIGTERM shutdown.

## Adversarial audit of the blue-sky features (fixes)
A 30-agent adversarial audit (find → verify) over the PLAN3 code confirmed and fixed:
- **bisect (high):** `decodeResponse` ran an SSE decoder on every response, so two different *unary* (non-streaming) JSON answers both decoded to an empty transcript and `Bisect` reported "no divergence". Now unary turns compare on the canonicalized body digest.
- **faults (medium):** an unbounded synthetic fault (`Status>0`, `Attempt:0` = the default) shadows a key on every attempt and never consumes the recording, which made `Verify` fail forever with "never replayed". `Verify` now excludes interactions fully shadowed by such a fault (`FaultSet.shadowsEveryAttempt`).
- **canary (medium):** `CanaryHits` scanned only body + header values. Now also scans header NAMES and the recorded URL path. Query strings are not in the recording (path-only match key) — documented as out of scope.
- **doc (low):** `oneLine` truncated by byte offset and could split a multibyte rune into invalid UTF-8; now truncates on a rune boundary.
- **wireshape (low):** OpenAI Chat Completions chunks (no top-level `type`) all collapsed to `"data"`. Now derive a grammar token (`delta.role`/`delta.content`/`delta.tool_calls`/`delta.finish:<reason>`) so the shape is sensitive to chat-grammar drift but still volatile-value-insensitive.

## Analysis/safety/counterfactual toolkit (PLAN4)
Built in dependency order behind the single offline gate (coverage ≥ 80%, no new heavy deps —
gjson/yaml.v3/stdlib crypto only). Phase 0 extracted the shared engine internals (`DecodeInteraction`
keeping the unary `"unary:<hex>"` path, `CollectTurns`, `AxisDiff`) and re-pointed bisect/assert/doc.
- **Keystone — `internal/wireenc`:** the Transcript→wire SSE encoder, inverse of `semequal.Decode*`,
  with a deterministic `Envelope` for volatile ids/usage. The gate is the round-trip property
  `Decode(Encode(t)) == t.Normalize()` across Anthropic / OpenAI Chat / Responses (97.5% covered). To
  keep the round-trip exact, finish reasons are emitted faithfully (omitted when empty) rather than
  defaulted. This is what unblocks graft/distill.
- **Honest scoping (graft/distill):** graft is a SINGLE-TURN counterfactual — replay matches on the
  request, so editing a response cannot re-derive later turns; graft warns when a downstream request
  still embeds the pre-edit output and offers `--truncate-after`. To keep within "no new deps" we do
  not edit requests (no sjson), so the match key stays valid and no re-key is needed; changing a
  request means re-recording. distill varies a response slot (text or a tool's args) — it recombines
  recorded behavior and cannot synthesize a shape the model never produced. Both `SecretScan` before
  every write.
- **cost:** usage is decoded straight from recorded bytes (NOT via Transcript, which drops it), and a
  turn with no usage is reported "unknown", never zero (e.g. an OpenAI stream without include_usage).
- **eval:** the LLM judge is itself a replayed cassette; the judge prompt is built deterministically
  (`JudgePrompt`) so it binds to a stored key and the whole eval is zero-dial.
- **watch** is the only network-touching command — opt-in via `--base-url`, loud banner, CI-exercised
  against an httptest fake. **attest** gates on SEMANTIC digests (benign re-records pass; the raw SHA
  is advisory).

## Castiron workflow (PLAN5) — folded into cassette, not a separate project
"Castiron" was pitched as a separate record/replay product; it is ~80% cassette (record/replay +
signed fixtures via `attest` + edit-a-step via `graft`). We folded the net-new wedge in and reuse the
name ONLY for the durable bug-report artifact (`.castiron`) + the workflow framing — not a separate
binary/package.
- **Forward-branch (`ModeBranch`)** is the genuinely new capability `graft` punts on. Design truth:
  cassette is an `http.RoundTripper` and does NOT drive the agent loop, so re-running FORWARD is a
  LIBRARY step (open the seed in `ModeBranch` + `Options.Live`, run YOUR agent, `Save()`); the
  `cassette branch` CLI only prepares the seed. `branch()` reuses the exact shared `matchResponse`:
  HIT → `synthesize` (deterministic prefix, incl. the grafted turn); MISS → `record()` (dial Live +
  append). The appended live suffix is deliberately NOT added to `c.keys`, so it never shadows the
  prefix and a genuinely-new later request misses again. `VerifyError` gets a `ModeBranch` arm that
  saves and tolerates dials + an unconsumed prefix tail (a branch intends to diverge mid-prefix).
- **Honest caveats:** branch DIALS the network (opt-in via `Options.Live`); determinism of the live
  suffix is best-effort (`cassette seeds` reports whether sampling is pinned — and that even
  seed+temp=0 isn't guaranteed bitwise across model/system_fingerprint versions; Anthropic/Responses
  have no seed param). `LiveTransport` re-injects auth at dial time; secrets never enter the saved
  cassette because `saveLocked` scrubs.
- **`.castiron` is a plain directory, NOT a packed binary** — `runPacked` ignores subcommands, so a
  packed binary can't run `verify-attest`/`assert`; the bundle is `{cassette, .att?, baseline?,
  report.md, run.sh}` so CI runs the real CLI offline. `cassette report` re-runs its own check and
  exits nonzero iff the bug is still live (green-when-fixed). All write paths `SecretScan` first.

## Maturation (PLAN-MATURITY)

- **Module identity is `github.com/faisalkhan91/cassette`.** The earlier
  `github.com/example/cassette` was a placeholder that 404'd `go install`, brew, and
  the GHCR image. A CI identity guard (`scripts/ci.sh`) now fails on any residual
  `github.com/example/` in code/config so the placeholder can't regress.
- **`analysis/` is internal (`internal/analysis`).** Its functions traffic in
  `internal/wirefmt`/`internal/wireenc` types an external caller can't construct, so
  exposing it as a public library was an inconsistent middle state. The curated public
  API is the root `cassette` package only; `analysis` is a CLI-implementation detail.
- **The coverage gate measures shippable logic.** Data-only/example/fixture packages
  (`internal/wirefix`, `examples/*`, `cmd/mcpfixture`) are excluded from the
  denominator, and a per-package floor (`PKG_FLOOR`) prevents a weak package from
  hiding behind the aggregate (`COVER_FLOOR`). This replaced a single aggregate floor
  that let `analysis` sit at 74.8% while the total read green.
