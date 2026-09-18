# promptfoo on top of cassette — deterministic, offline evals

[promptfoo](https://www.promptfoo.dev) is a local-first eval/red-team runner. By
default it calls providers **live** and caches responses on a best-effort basis (the
cache is not a guarantee, and errors aren't cached). Point it at a `cassette` replay
endpoint instead and every eval run becomes **deterministic, offline, and key-less** —
the same recorded bytes on every machine and every CI run, with zero egress.

cassette serves each provider's wire protocol on an OpenAI-compatible base URL, so no
plugin is needed: promptfoo's built-in `openai:` provider just targets the replay port.

## Setup

```sh
# 1. scaffold + record once against the real API
cassette init --stack promptfoo
cassette proxy ./testdata/cassettes --mode record --upstream https://api.openai.com --addr :8080
#    …run your promptfoo eval once here so the calls are captured…

# 2. from now on, replay offline (no key, no network)
cassette serve ./testdata/cassettes --addr :8080      # or: cassette up
promptfoo eval                                        # deterministic, zero-egress
```

## promptfooconfig.yaml

Point the provider's `apiBaseUrl` at the replay endpoint; the key is unused on replay:

```yaml
providers:
  - id: openai:chat:gpt-4o-mini
    config:
      apiBaseUrl: http://localhost:8080/v1
      apiKey: replay            # any non-empty value; replay never checks it

prompts:
  - "Summarize: {{input}}"
tests:
  - vars: { input: "the quick brown fox" }
    assert:
      - type: contains
        value: fox
```

Anthropic works the same way with promptfoo's `anthropic:` provider and
`apiBaseUrl: http://localhost:8080` (no `/v1` suffix for Anthropic).

## Why this beats promptfoo's own cache

- **Guaranteed**, not best-effort: a replay miss is a hard 404 (use `cassette serve
  --on-miss=fail` to make CI fail loudly), never a silent live call.
- **Committable + shareable**: the cassette is a file in your repo — every teammate
  and CI runner replays identical bytes. promptfoo's cache is local and ephemeral.
- **Secret-free**: cassette scrubs secrets on save, so recorded eval fixtures are safe
  to commit (`cassette verify` gates it).
- **Inspectable**: `cassette open` / `cassette otel` turn the same recording into a
  dashboard or OpenTelemetry spans.

To gate a CI run on the eval replaying cleanly, serve with `--on-miss=fail` and run
`promptfoo eval` against it — any un-recorded call exits the server nonzero.
