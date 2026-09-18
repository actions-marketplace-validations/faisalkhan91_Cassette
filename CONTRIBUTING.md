# Contributing to cassette

Thanks for your interest! cassette is a Go record/replay VCR for LLM HTTP/SSE +
MCP. A few invariants keep it trustworthy — please preserve them.

## The one command

Everything the CI gate runs is in one script, and it is fully offline:

```sh
bash scripts/ci.sh
```

It must exit 0 before you open a PR. It runs (in order): an identity guard (no
placeholder module path), `gofmt`, `go vet`, `go build`, the test suite with the
race detector, the coverage floor, and the offline demo.

## Invariants (non-negotiable)

- **Offline & vendored.** Dependencies are vendored; CI runs with
  `GOFLAGS=-mod=vendor` and `GOPROXY=off`. Do not add a heavy dependency — open an
  issue first. New providers/features should be pure Go on the standard library
  plus the existing handful of deps.
- **Deterministic & byte-exact.** The same input must produce identical bytes. No
  timestamps, randomness, or map-iteration order in serialized output. Reach for a
  seeded PRNG when you genuinely need variation (see `wireenc.Reframe`).
- **Zero outbound network in the default path.** Replay and analysis must never
  dial. Anything that touches the network must be explicitly opt-in (see `watch`,
  `proxy --mode record`).
- **Secrets are never committed.** Run the secret scan before writing any cassette
  (`saveScrubbed` does this for you); the gate fails on residue.
- **`gofmt` + `go vet` clean; coverage floor enforced.** Land tests alongside the
  code they cover — the gate measures real logic coverage.

## Workflow

1. Branch off `main`.
2. Make the change; add table-driven, offline tests.
3. `gofmt -w` your files; run `bash scripts/ci.sh` until green.
4. Keep commits focused (conventional prefixes: `feat:`, `fix:`, `refactor:`,
   `test:`, `docs:`). One logical change per commit.
5. Open a PR describing the change and how you verified it.

## Architecture

See [`DECISIONS.md`](DECISIONS.md) for the architecture decision records (why the
match key excludes the host, why the codec is a separate package, etc.) and
[`docs/`](docs/) for the guide, CLI reference, and feature docs.
