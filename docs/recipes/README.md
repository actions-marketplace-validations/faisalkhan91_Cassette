# Recipes — adopt cassette in any stack

cassette is a single static Go binary, so any language can record and replay
LLM/MCP traffic by pointing its provider base URL at `cassette proxy`. Scaffold the
layout with `cassette init`, then use one of these:

## pytest (Python)

Copy [`conftest.py`](conftest.py) to your test root. It starts `cassette proxy
--mode replay` for the session and points `OPENAI_BASE_URL` at it — every request
is served from a committed cassette, and a miss is a hard error (no network).

```sh
cassette init --stack python
# record once against the real API:
cassette proxy ./testdata/cassettes --mode record --upstream https://api.openai.com --addr :8080
pytest        # replays offline, no key, no network
```

## GitHub Actions

Use the reusable action at the repo root ([`action.yml`](../../action.yml)):

```yaml
- uses: faisalkhan91/cassette@v1
  with:
    command: lint             # or: verify (a single file)
    path: testdata/cassettes
```

`lint` runs the cassette linter over a file or corpus directory; `verify` checks a
single cassette is well-formed and secret-free. Both terminate, so they make clean
CI gates. (To gate live traffic against a replay
server, run `cassette serve <dir> --on-miss=fail` yourself as a backgrounded step in
your own job and drive it with your test command — the action intentionally only
wraps the one-shot checks.)

## promptfoo (evals on top of replay)

[`promptfoo.md`](promptfoo.md) points the [promptfoo](https://www.promptfoo.dev) eval
runner at a `cassette` replay endpoint, so evals run deterministic, offline, and
key-less — a guaranteed replay layer under promptfoo's best-effort cache.

```sh
cassette init --stack promptfoo
cassette serve ./testdata/cassettes --addr :8080   # then: promptfoo eval
```

## Governance & compliance in CI

[`governance.md`](governance.md) wires `cassette policy` (gate) + `cassette audit`
(evidence bundle) + `cassette attest` (signed behavioral manifest) into one offline
CI job — for SOC 2 / EU AI Act / ISO 42001 evidence and change attestation.

```sh
cassette policy init && cassette attest gen-key ed25519.key
cassette policy ./testdata/cassettes        # the gate
```

## A real coding-agent CLI (Claude Code / Codex)

[`realworld.md`](realworld.md) is the validated setup for putting cassette in front
of a real agent CLI — recording its live LLM traffic and replaying it offline — incl.
the `--volatile` paths each CLI needs (they stamp a per-session id into every request).

## Any other stack

`cassette init` prints the base-url snippet for your detected stack. The pattern is
always the same: record once through `cassette proxy --mode record`, commit the
cassettes, then replay offline in CI with `--mode replay`.
