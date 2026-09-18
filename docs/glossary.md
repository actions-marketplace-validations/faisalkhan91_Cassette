# cassette — glossary

**Cassette** — a deterministic, git-diffable YAML file holding an ordered list of
recorded interactions. Record once, replay byte-exact and offline forever.

**Interaction** — one recorded request/response pair. `kind: http` (an LLM HTTP/SSE
call) or `kind: mcp` (an MCP tool call).

**Match key** — the deterministic fingerprint used to look up the recorded response
for a live request. Derived from the method, URL *path* (host excluded, so cassettes
are portable across base URLs), and the canonicalized request body. Volatile-but-
non-secret fields (api keys, request ids, gzip vs identity, JSON key order) are
normalized away so they never change the key. Computed identically at record and
replay time. See [`DECISIONS.md`](../DECISIONS.md).

**Transcript** — the normalized, volatile-field-free view of a model turn: role,
assembled text, tool calls (with canonicalized arguments), and finish reason. The
SSE/MCP decoders reduce raw wire bytes to this, so two runs are compared on behavior.

**Semantic digest** — a stable SHA-256 of a normalized transcript. Two runs are
*semantically equivalent* iff their digests match — regardless of chunk boundaries,
ids, or usage counts. A **combined digest** reduces a whole conversation to one hash.

**Codec** — the round-trippable `Transcript ⇄ wire` pair (decode + `wireenc`
encode). Because it exists, a recording is editable/authorable *source*, which is
what `author`, `graft`, `port`, `canonicalize`, `scenario`, and `fuzz` build on.

**Scrub** — redacting secrets (and stamping volatile response fields) before a
cassette is written. Independent of matching: scrubbing changes what is *stored*; it
is never required for replay to work. See [Security](security.md).

**Replay / record / auto / branch** — the four modes. *Replay* serves recordings with
the network blocked (zero dials). *Record* captures live traffic. *Auto* records if the
cassette file is absent, else replays. *Branch* replays a recorded (optionally edited)
prefix and goes live on the first divergent request.

**Castiron** — the forward-branch workflow + the committable `.castiron` bundle: a
CI-runnable bug report that re-runs its own check and goes green when the bug is
fixed. See [Features](features.md).

**Provenance (lineage)** — the chain of derivation operations (`port`, `migrate`,
`graft`, `distill`) carried in a derived cassette's `provenance` block. Each link
records the op and the *behavioral* (semantic) digest of the source, so the chain is
secret-free and stable across benign re-records. Inspect it with `cassette provenance`;
`attest` binds it into the signed manifest.
