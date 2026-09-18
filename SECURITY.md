# Security policy

## Reporting a vulnerability

Please report security issues **privately** via GitHub's
[private vulnerability reporting](https://github.com/faisalkhan91/cassette/security/advisories/new)
(Security → Report a vulnerability). Do not open a public issue for a suspected
vulnerability.

We aim to acknowledge a report within a few days and to ship a fix or mitigation
before any public disclosure.

## Scope and design guarantees

cassette is built so that secrets and live traffic stay contained:

- **Zero-dial replay.** In replay mode the transport cannot reach the network — a
  dial increments a counter and returns `ErrNetworkBlocked`, and `VerifyError`
  fails on any dial. Replay and all analysis commands are fully offline.
- **Egress is opt-in.** Only `record`, `branch`, and `cassette proxy --mode
  record|branch` touch the network, and they do so explicitly against a
  caller-supplied upstream.
- **Secrets are scrubbed on write.** Recordings are scrubbed (a deep copy, so live
  replay state is untouched) before being written to disk, and the credential
  shapes are rejected by `cassette verify` and `cassette lint`.
- **No telemetry.** cassette never phones home.

If you find a way to defeat any of these guarantees (e.g. a secret that survives
scrubbing, a replay path that dials out, or a proxy that persists credentials),
that is exactly the kind of report we want.

## Handling cassettes safely

Recorded cassettes can contain prompts, tool arguments, and responses. Treat them
as you would any test fixture derived from real traffic: run `cassette verify` /
`cassette lint` before committing, and use `cassette scrub`, `redact-field`,
`egress-audit`, and `taint` to find and remove sensitive data.
