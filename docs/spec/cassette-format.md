# Cassette file format — v1 (portable spec)

This is the normative, implementation-independent description of the cassette
on-disk format and its match-key algorithm, so a recording can be **read and
replayed from any language**, not just Go. The reference implementation is
`internal/wirefmt` (format) and `internal/match` (keys); where this document and the
code disagree, the code is authoritative and this spec is the bug.

A cassette is a deterministic, readable, git-diffable **YAML** document. Two
recordings of the same session serialize to identical bytes, and body round-trips
are byte-exact, so SSE / JSON-RPC frames replay byte-for-byte on every OS.

## 1. Versioning

The top-level `schema_version` is `1`. A reader MUST reject a cassette whose
`schema_version` it does not understand rather than guess.

## 2. File structure

```yaml
schema_version: 1
notice: "optional free-text human note; ignored by tooling"
expect:                       # optional embedded behavioral contract (§6)
  no_tools: [deploy]
  no_duplicate_tools: true
  finishes_clean: true
match:                        # optional persisted match normalization (§5.4)
  volatile_json_paths: [metadata.timestamp]
  header_allowlist: [X-Tenant]
provenance:                   # optional lineage of a DERIVED cassette (§6.1)
  - { op: port, from: <source behavior digest>, note: "→ openai-chat" }
interactions:                 # ordered list; replay matches by key, not position
  - kind: http                # "http" | "mcp"
    request: { ... }          # §3
    response: { ... }         # §4
```

Only `schema_version` and `interactions` are required. Field order within a mapping
is not significant to readers, but the reference writer emits a fixed order and
sorts headers for byte-stability.

## 3. Request

HTTP requests use `method` / `url` / `headers` / `body`; MCP requests use
`mcp_method` / `mcp_tool` / `body`.

| field | type | notes |
|-------|------|-------|
| `method` | string | HTTP method (HTTP only) |
| `url` | string | **path only** — the host is scrubbed; matching is path + body |
| `headers` | mapping (§7) | request headers (secrets scrubbed) |
| `mcp_method` | string | MCP JSON-RPC method, e.g. `tools/call`, `tools/list` |
| `mcp_tool` | string | tool name for an MCP `tools/call` |
| `body` | Body (§8) | request body / MCP params (canonicalized JSON or raw) |
| `body_sha256` | string | optional digest of the live pre-scrub body (advisory) |
| `match_key` | string | the authoritative replay key (§5). Empty ⇒ recompute from the stored request |

`match_key` is computed at record time over the **live, gunzipped, pre-scrub**
request, so replay is independent of body scrubbing and stored-body encoding. A
reader SHOULD treat a non-empty `match_key` as authoritative and only recompute when
it is absent (hand-written cassettes).

## 4. Response

| field | type | notes |
|-------|------|-------|
| `status` | int | HTTP status (default 200 on replay if absent) |
| `headers` | mapping (§7) | response headers; `Content-Encoding`/`Content-Length` are NOT replayed (bodies are stored decoded) |
| `streaming` | bool | true ⇒ `body` is the verbatim SSE / NDJSON byte stream with original framing |
| `body` | Body (§8) | the response bytes |
| `error` | string | set when the recorded turn was a transport/stream error |
| `stream_timing` | list[int] | optional per-frame inter-arrival deltas in ms (`[i]` = ms after the previous frame; `[0]` = from stream start). Present only when recorded with `--pace`; approximate (SDK read boundaries), omitted otherwise for byte-stable re-record |

## 5. Match key (normative)

Replay resolves a live request to a recorded interaction by computing a **match
key** and looking it up. The key is a newline-joined string (then used verbatim as a
map key; it is not itself hashed).

### 5.1 HTTP

```
KEY = METHOD "\n" PATH "\n" BODYKIND ":" BODYDIGEST [ "\nH" ( "\n" NAME "=" VALUES )* ]
```

- `METHOD` — upper-cased HTTP method.
- `PATH` — the request path (after any configured path transform).
- `BODYKIND` / `BODYDIGEST` — see §5.3.
- The trailing `H` block is present only when `match.header_allowlist` is non-empty:
  each allowlisted header (canonicalized name, sorted; values sorted and
  comma-joined) is appended. The default allowlist is **empty** — no header enters
  the key.

### 5.2 MCP

```
tools/call : "mcp:tools/call:" TOOL ":" DIGEST(arguments)
tools/list : "mcp:tools/list:" DIGEST(params)
other      : "mcp:" METHOD ":" DIGEST(body)
```

### 5.3 Body digest

`BODYDIGEST` is the lowercase hex SHA-256 of the **canonical** body; `BODYKIND` is one
of:

- `json` — when the content type contains `json` (or the body looks like JSON):
  canonicalize first (parse, recursively **sort object keys**, drop any
  `volatile_json_paths`, re-serialize compactly), then SHA-256. JSON that is declared
  but unparseable falls back to `raw`.
- `multipart` — `multipart/*`: a digest over the parts (see reference impl).
- `raw` — everything else: SHA-256 of the bytes as-is.

Gzip vs identity is normalized away (the live body is gunzipped before digesting), so
encoding never changes the key.

### 5.4 Persisted normalization (`match`)

`volatile_json_paths` (dropped from the body before digesting) and `header_allowlist`
are persisted in the file's `match` block so **every** reader reproduces the same keys
without the caller re-supplying them. Function-valued hooks (path/body/key transforms)
are intentionally NOT persistable.

## 6. Embedded contract (`expect`)

`expect` is an optional behavioral contract co-located in the recording so a checker
can run zero-arg in CI and the contract survives re-record: `no_tools` (these tools
must not be called), `no_duplicate_tools`, `finishes_clean` (terminal finish reason).

## 6.1 Provenance (lineage)

`provenance` is an optional ordered chain recording how a **derived** cassette was
produced. Each entry is `{op, from, note?}` where `op` is the transformation
(`port` / `migrate` / `graft` / `distill`) and `from` is the **behavioral**
(combined semantic) digest of the source it was derived from. Because it keys off
semantics, not volatile bytes, the chain is stable across benign re-records. The
derivation commands append a link automatically; an original recording has none.
Readers MAY ignore it; it never carries secrets.

## 7. Headers

Headers serialize as a readable YAML mapping of `Name: [v1, v2]`, kept **sorted by
name** for byte-stability. Multiple values per name are preserved in order.

## 8. Body encoding

A body is stored as a **scalar string** when its bytes are valid, safe UTF-8
(readable and git-diffable), otherwise as a mapping:

```yaml
body: '{"hello":"world"}'              # text form
body: { encoding: base64, data: "..." }  # binary / non-UTF-8 form
```

Readers MUST accept both forms and reconstruct the exact original bytes.

## 9. Replay semantics

- A replayer computes the key (§5) for the incoming request and serves the matched
  interaction's response. **Zero egress**: a miss is an error, never a passthrough.
- Streaming responses are written **frame-by-frame, verbatim**; the HTTP envelope is
  reconstructed (drop `Content-Encoding`/`Content-Length`). With timing-faithful
  replay, `stream_timing[i]` is the delay before frame `i`.
- Matching is by key, not list position, so interaction order is informational.

## 10. Determinism & secrets

- Output is byte-stable: sorted headers, canonical JSON in keys, no timestamps in the
  document. The same session records to identical bytes.
- Secrets are redacted on save (provider-aware patterns + volatile-field stamping).
  Because `match_key` is computed pre-scrub, redaction never breaks replay.

## 11. Conformance

A file is well-formed and replayable iff `cassette conformance <file>` passes (every
key re-derives, every request resolves to its response, zero dials) and
`cassette codec verify <file>` shows the streaming turns round-trip. A non-Go
implementation can validate itself against these two gates.

## 12. Minimal example

```yaml
schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      headers:
        Content-Type: [application/json]
      body: '{"model":"claude","messages":[{"role":"user","content":"hi"}]}'
      match_key: "POST\n/v1/messages\njson:5f2e…"
    response:
      status: 200
      streaming: true
      headers:
        Content-Type: [text/event-stream]
      body: "event: message_start\ndata: {...}\n\n…"
```
