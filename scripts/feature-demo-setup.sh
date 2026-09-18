#!/usr/bin/env bash
# Builds the offline fixtures the per-feature demo tapes (scripts/*.tape) record.
# Everything is authored with `cassette author` — no API key, no network.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOFLAGS=-mod=vendor GOPROXY=off

BIN=/tmp/cassette
go build -o "$BIN" ./cmd/cassette

D=/tmp/cass-feat
rm -rf "$D"; mkdir -p "$D"

# --- diff: two recordings of the same prompt that diverge (text + tool args) ---
cat > "$D/a.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "Capital of France, and the weather there?"
    text: "The capital of France is Paris."
    tool_calls: [{name: get_weather, args: '{"city":"Paris"}'}]
    finish: tool_use
EOF
cat > "$D/b.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "Capital of France, and the weather there?"
    text: "The capital of France is Lyon."
    tool_calls: [{name: get_weather, args: '{"city":"Lyon"}'}]
    finish: tool_use
EOF
"$BIN" author "$D/a.spec.yaml" -o "$D/before.yaml" >/dev/null
"$BIN" author "$D/b.spec.yaml" -o "$D/after.yaml"  >/dev/null

# --- author / codec / doctor: a two-turn session ---
cat > "$D/session.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "What's the capital of France?"
    text: "The capital of France is Paris."
    finish: end_turn
  - user: "And its population?"
    text: "About 2.1 million in the city proper."
    finish: end_turn
EOF
"$BIN" author "$D/session.spec.yaml" -o "$D/session.yaml" >/dev/null

# A live request that WON'T match (same prompt, different model) for `doctor`.
# Field/key order mirrors the authored body so only the model axis differs.
cat > "$D/req.json" <<'EOF'
{"method":"POST","url":"/v1/messages","content_type":"application/json",
 "body":{"max_tokens":1024,"messages":[{"content":"What's the capital of France?","role":"user"}],"model":"claude-sonnet-4-6","stream":true}}
EOF

# --- taint: a value from the user, and one from the tool/model output, both
#     carried outbound in a later turn ---
cat > "$D/taint.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "My email is alice@example.com — look up the account owner."
    text: "The account owner is bob@vendor.example."
    finish: end_turn
  - user: "Great, email bob@vendor.example to confirm."
    text: "Done — confirmation drafted."
    finish: end_turn
EOF
"$BIN" author "$D/taint.spec.yaml" -o "$D/taint.yaml" >/dev/null

# --- migrate / dashboard: a small anthropic corpus (chat + a tool call) ---
mkdir -p "$D/corpus"
"$BIN" author "$D/session.spec.yaml" -o "$D/corpus/chat.yaml"    >/dev/null
"$BIN" author "$D/a.spec.yaml"       -o "$D/corpus/weather.yaml" >/dev/null

# --- init: an empty project dir to scaffold into ---
rm -rf "$D/proj"; mkdir -p "$D/proj"

echo "feature-demo fixtures ready in $D"
