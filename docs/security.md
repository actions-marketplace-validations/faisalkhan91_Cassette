# cassette — security & data handling

cassette is designed so that secrets and live traffic stay contained. This page is
the threat model and the tools for keeping recordings safe to commit. For the
disclosure process, see [`SECURITY.md`](../SECURITY.md).

## Guarantees

- **Zero-dial replay.** In replay mode the transport cannot reach the network: a
  dial increments a counter and returns `ErrNetworkBlocked`, and `VerifyError`
  fails the test on any dial. Replay and every analysis command are fully offline.
- **Egress is opt-in.** Only `record`, `branch`, and `cassette proxy --mode
  record|branch` touch the network, each against an explicit upstream. Everything
  else is local.
- **Scrub on save.** Recordings are scrubbed on a deep copy (live replay state is
  untouched) before being written, and a **write-time secret scan refuses to
  persist** a credential that survived scrubbing — so a secret can't reach disk
  even if it landed in an unexpected field.
- **No telemetry.** cassette never phones home.

## What gets scrubbed

`scrub.DefaultConfig()` redacts:

- **Credential headers** — `Authorization`, `X-Api-Key`, `Api-Key`,
  `OpenAI-Organization`, `X-Goog-Api-Key`, cookies, and AWS SigV4 signing fields.
- **Credential shapes in bodies** — `sk-…` keys, `Bearer …` tokens, and the named
  shapes AWS (`AKIA…`), GitHub (`gh*_…`), JWT (`eyJ….….…`), and Google (`AIza…`).

The **secret scanner** (used by `cassette verify`, `cassette lint`, and the
write-time backstop) flags those same named shapes. It deliberately does **not**
hard-fail on Luhn/credit-card or high-entropy heuristics — those cause false
positives on legitimate recorded content; use `egress-audit` for that class.

`DisableScrub` is the explicit "record raw" escape hatch and bypasses both the
scrubber and the write-time scan — use it only for fixtures you control.

## Finding sensitive data

- **`cassette egress-audit`** — typed PII/data-class scan of outbound requests
  (email, SSN, credit card, phone, plus the credential shapes). `--fail-on` gates CI.
- **`cassette taint`** — follows the *flow*: a value that first appeared in an
  earlier turn (user input, or untrusted model/tool output) and is then carried
  outbound in a later request — potential exfiltration / injection relay.

![cassette taint tracing a user-input email and an untrusted model-output email carried outbound in a later turn.](../assets/demo-taint.gif)

- **`cassette scrub` / `redact-field`** — redact in place (and rekey) after the fact.
- **`cassette verify` / `lint`** — fail on any committed secret shape.

## Proxy safety

`cassette proxy` binds **`127.0.0.1` by default**. In `record`/`branch` mode it
forwards credentialed requests to the upstream, so a non-loopback bind would make
the host an open relay for any peer that can reach it — cassette warns when you do
that. The replay path is always zero-egress (a miss is a 404, never a passthrough).

## Behavioral attestation

`cassette attest` ed25519-signs a manifest of a recording's per-turn semantic
digests + wire-shape fingerprints; `cassette verify-attest` checks it. This makes
"this agent refuses X and only calls tools {A,B}" a cryptographically verifiable,
CI-checkable claim — a behavioral complement to a code SBOM.
