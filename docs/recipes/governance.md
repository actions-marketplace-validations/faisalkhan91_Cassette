# Governance & compliance in CI — policy, audit, attest

cassette turns a recording into a governance object, not just a test fixture. This
recipe wires three commands into a CI job so every change is **gated**, **evidenced**,
and **attestable** — entirely offline, from bytes you own:

- **`cassette policy`** — the gate: pass/fail against a committed `cassette.policy.yaml`.
- **`cassette audit`** — the evidence: a deterministic bundle (manifest + coverage +
  PII findings + taint flows) to attach to the build.
- **`cassette attest`** — the signature: an ed25519 manifest binding the recording's
  *behavior* (not its volatile bytes), so a benign re-record still verifies.

## 1. One-time setup

```sh
cassette policy init                       # writes a safe-by-default cassette.policy.yaml
cassette attest gen-key ed25519.key        # generate a signing key (store the key as a CI secret)
```

Edit `cassette.policy.yaml` for your risk posture — e.g. forbid the data classes that
must never leave your boundary and cap cost:

```yaml
secrets: forbid
exfil: forbid                       # no model/tool output carried back outbound
egress:
  forbid: [email, creditcard, ssn, phone]
budget:
  max_tokens: 500000
coverage:
  require_tools: [search, fetch]    # the corpus must exercise these (HTTP tool calls)
```

## 2. The CI job

```sh
set -e
DIR=./testdata/cassettes

# Gate: fail the build on any policy violation (secrets, PII egress, exfil, budget, coverage).
cassette policy "$DIR"

# Evidence: a byte-stable bundle to upload as a build artifact (date it via the filename).
cassette audit "$DIR" -o "audit-$(git rev-parse --short HEAD).json"

# Signature: attest behavior and verify it round-trips (re-attest in CI to detect drift).
for c in "$DIR"/*.yaml; do
  cassette attest "$c" --key ed25519.key -o "$c.att"
  cassette verify-attest "$c" "$c.att"
done
```

`policy` exits nonzero on a violation, so the job fails fast. `audit` is pure
evidence generation (add `--fail-on email,exfil` if you want it to gate too).
`verify-attest` reads the public key from the `.att` itself, so verification needs no
secret.

## 3. What each artifact proves

| Artifact | Proves | For |
|----------|--------|-----|
| `cassette policy` exit 0 | no secrets, no forbidden PII egress, no exfil, within budget, required tools covered | the CI gate / PR check |
| `audit-<sha>.json` | the behavioral manifest, coverage matrix, every outbound PII finding, every cross-turn taint flow — at a point in time | SOC 2 / EU AI Act / ISO 42001 evidence |
| `<cassette>.att` | the recording's behavior **and derivation lineage** are unchanged since signing (benign re-records still verify; a behavior change or a rewritten lineage fails) | change attestation / supply-chain trust |

All three are deterministic and offline: the same recordings produce the same gate
result, the same bundle bytes, and the same behavioral manifest on every machine — no
network, no provider key, nothing to leak.

## 4. Pre-commit (optional)

Run the cheap gates locally before a commit:

```sh
cassette lint "$DIR"               # corpus health incl. secret scan (file or dir)
cassette policy "$DIR"             # full guardrail
```

(`cassette verify <file>` is the single-file, zero-config secret gate; `lint` is its
corpus-level superset.)
