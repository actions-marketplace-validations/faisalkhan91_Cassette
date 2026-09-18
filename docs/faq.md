# cassette — FAQ & troubleshooting

## A replay "miss" — the request didn't match any recording

A miss means the live request's [match key](glossary.md) differs from every stored
one. Diagnose it:

```sh
cassette doctor <cassette.yaml> --request req.json   # nearest record + which axis changed + the fix
```

![cassette doctor naming the nearest recorded turn, the differing request axis, and the fix.](../assets/demo-doctor.gif)

Common causes: the request body changed (a new field, different model, reordered
content that isn't canonicalized away), a different URL path, or a header that's in
the match config's allowlist. `doctor` names the divergent axis and the remedy.

## "Stale match key" from `lint` / `conformance`

You hand-edited a recording's request body but not its `match_key`, so the stored
key no longer derives from the body. Fix:

```sh
cassette rekey <cassette.yaml>
```

`rekey` **refuses** to recompute a key when an interaction's request body was
scrubbed and the new key would differ from the stored one — re-deriving from
redacted bytes would overwrite the replay-authoritative key. If you hit this, mark
the secret field volatile so it is excluded from keying (`cassette redact-field
<cassette.yaml> --path a.b.c`), or pass `cassette rekey <cassette.yaml> --force` if
you know the body is intact.

`cassette conformance` proves a whole file still replays (key integrity + every
request resolves to its stored response, zero dials) — run it after manual edits.

## Re-recording produces a noisy `git diff`

Volatile ids/usage and incidental SSE chunk boundaries differ between recordings.
Canonicalize so two recordings of the same behavior serialize identically:

```sh
cassette canonicalize <cassette.yaml>     # --check to gate in CI
```

Then `git diff` shows only what the model actually did differently. `cassette diff`
gives a signal-only semantic diff between two recordings directly.

## How is this different from go-vcr / vcrpy / nock / Polly.js?

Those are **generic HTTP** record/replay libraries. cassette is **LLM-native**:

- It understands **SSE streaming** and reassembles Anthropic/OpenAI/Responses
  streams into a normalized **transcript**, so equivalence is judged on *behavior*
  (text, tool calls, finish reason) — not byte-identical chunk boundaries.
- It speaks **MCP** (tool calls), not just HTTP.
- It has a round-trippable **codec**, so a recording is editable/authorable source
  (`author`, `graft`, `port`, `scenario`, `fuzz`), not a read-only artifact.
- It is **secret-aware** by default (scrub-on-save + a refuse-to-write scanner) and
  **provably offline** on replay (zero dials, enforced).

If you only need to stub a plain REST call, a generic VCR is fine. Reach for
cassette when the thing under test is an LLM agent or an MCP client. A fact-checked,
feature-by-feature breakdown (with honest caveats) lives in
[vs. alternatives](comparison.md) — including how cassette differs from the **live**
eval/observability platforms, which run against provider APIs rather than replaying
a committed fixture.

## Does replay ever hit the network?

No. Replay uses a transport that cannot dial; a miss is a 404 (serve/proxy) or
`ErrNoMatch` (library), never a passthrough. Only `record`, `branch`, and
`proxy --mode record|branch` touch the network, explicitly. See [Security](security.md).

## How do I disable color?

`--no-color`, or set `NO_COLOR`. Color auto-disables when output isn't a terminal.
Set `CASSETTE_ASCII=1` (or use a non-UTF-8 locale) to downgrade `✓`/`✗` to ASCII.
