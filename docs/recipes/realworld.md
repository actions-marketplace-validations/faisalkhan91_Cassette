# Recipe — record & replay a real coding-agent CLI (Claude Code / Codex)

cassette can sit transparently in front of a real agent CLI, record its live LLM
traffic, then replay it **offline with zero network**. This recipe is the validated
setup for **Claude Code** and **OpenAI Codex** talking to an OpenAI/Anthropic-style
HTTP gateway. It generalizes to any CLI that lets you override its base URL.

```
RECORD:  CLI ──base_url→ cassette proxy :8788 ──upstream→ gateway → model
                              └ tees SSE → session.yaml (auth headers scrubbed)
REPLAY:  CLI ──base_url→ cassette proxy :8788 (--mode replay, network blocked) → 0 dials
```

## Two rules that make replay actually match

Recording real agent traffic surfaces two things a synthetic fixture won't:

1. **Keep the record and replay URL paths identical.** cassette keys on the URL
   *path*. If your gateway lives under a base path (e.g. `https://host/llm-proxy`),
   put that base path on the **client** side and make `--upstream` host-only — so
   both legs key on `/llm-proxy/v1/...`:

   ```sh
   # client base URL carries the gateway path; --upstream is scheme://host only
   cassette proxy session.yaml --mode record --upstream https://host --addr :8788
   #   CLI base URL → http://127.0.0.1:8788/llm-proxy
   ```

2. **Drop the volatile request fields with `--volatile`.** Real agents stamp a
   fresh per-session id (and sometimes an env-stamped system prompt) into every
   request, so the same prompt produces a different body each run. Pass the exact
   JSON paths to ignore — **identically at record and replay**. Find them by
   recording the same prompt twice and diffing the request bodies.

   | CLI | volatile request paths |
   |-----|------------------------|
   | **Claude Code** | `metadata.user_id,system` |
   | **OpenAI Codex** | `client_metadata,prompt_cache_key` |

   `--volatile` is **persisted into the cassette** (a `match:` block), so you only
   need it on the **record** leg — replay, `conformance`, and `lint` reproduce the
   keys from the file itself. (`cassette conformance` also correctly trusts the
   stored key when scrub altered a key-relevant body region — it won't false-fail or
   tell you to `rekey`.)

## Claude Code

Claude Code honors a base URL only via settings (its `settings.json` `env` beats a
process env var), so override with `--settings`. Auth flows through whatever your
`apiKeyHelper` / key already provides — cassette forwards it and **scrubs it on
save**.

```sh
VOL="metadata.user_id,system"

# RECORD (live, through the gateway)
cassette proxy session.yaml --mode record --upstream https://host --volatile "$VOL" --addr :8788 &
claude -p "Reply with exactly: hello from cassette" \
  --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8788/llm-proxy"}}'
kill %1            # the record proxy writes the cassette on shutdown

# REPLAY (offline; a miss is a hard error)
cassette proxy session.yaml --mode replay --volatile "$VOL" --on-miss fail --addr :8788 &
claude -p "Reply with exactly: hello from cassette" \
  --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8788/llm-proxy"}}'
# → "hello from cassette", served from the cassette, zero outbound dials
```

## OpenAI Codex

Codex takes config overrides with `-c` and runs non-interactively with `codex exec`.
Point its provider `base_url` at cassette; auth via its configured `env_key`.

```sh
VOL="client_metadata,prompt_cache_key"

# RECORD
cassette proxy session.yaml --mode record --upstream https://host --volatile "$VOL" --addr :8790 &
codex exec --skip-git-repo-check \
  -c model_providers.<name>.base_url="http://127.0.0.1:8790/llm-proxy/v1" \
  "Reply with exactly: hello from cassette" </dev/null
kill %1

# REPLAY (offline)
cassette proxy session.yaml --mode replay --volatile "$VOL" --on-miss fail --addr :8790 &
codex exec --skip-git-repo-check \
  -c model_providers.<name>.base_url="http://127.0.0.1:8790/llm-proxy/v1" \
  "Reply with exactly: hello from cassette" </dev/null
```

## Notes

- **Recordings can contain more than the model call.** A real CLI also makes
  connectivity/login preflights, and the request body carries the agent's full
  system prompt + per-install identifiers. cassette scrubs secret *headers* on save,
  but treat raw captures of a corporate gateway as sensitive — review (and prune to
  the model call with `cassette prune`) before committing one as a fixture.
- The volatile paths above were derived empirically (record-twice-and-diff) against
  Claude Code 2.x and Codex 0.14x; re-derive if a CLI changes its request shape.
- **Multi-turn / tool-using sessions replay too.** A Claude Code run that uses a tool
  (e.g. Read → a second model request carrying the tool result) replays cleanly with
  the same `--volatile` paths: the recorded `tool_use_id` flows deterministically from
  the replayed first response into the second request, so it still matches.
