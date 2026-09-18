#!/usr/bin/env bash
# Runs the north-star demo: the SAME agent runs live (recording against an
# in-process fake provider) and again in replay with network egress impossible,
# proving a semantically identical transcript and ZERO outbound dials. Offline.
#
# Every command below is reproduced verbatim in the README Quickstart.
set -euo pipefail

cd "$(dirname "$0")/.."

export GOFLAGS=-mod=vendor
export GOPROXY=off

go run ./examples/agent-demo
go run ./examples/agent-demo-openai
go run ./examples/session-demo

# Build the offline fixtures the per-feature demo GIFs record (no key, no network).
# This exercises `cassette author` across several screenplays; the GIF rendering
# itself (vhs) stays a manual, local step.
bash scripts/feature-demo-setup.sh
