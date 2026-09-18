#!/usr/bin/env bash
# A REAL two-pane split-terminal demo of cassette:
#   LEFT  pane  — `cassette serve` exposes a recording as a local LLM endpoint
#                 (no API key, network egress blocked).
#   RIGHT pane  — a plain `curl` hits it and gets the byte-for-byte replayed SSE.
#
# Recorder-agnostic — run it inside any terminal recorder:
#   asciinema:  asciinema rec -c "scripts/split-demo.sh" demo-split.cast
#               agg demo-split.cast assets/demo-split.gif
#   vhs:        vhs scripts/demo.tape          # drives this script, writes the gif
#   plain:      scripts/split-demo.sh          # just watch it live
#
# Needs tmux (brew install tmux). The action is driven by a background "player"
# so it plays out live AFTER we attach, which is what the recorder captures.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
command -v tmux >/dev/null || { echo "this demo needs tmux:  brew install tmux"; exit 1; }

# Build the CLI offline + stage a cassette and a matching request.
GOFLAGS=-mod=vendor GOPROXY=off go build -o /tmp/cassette ./cmd/cassette
cp testdata/cassettes/anthropic_text_helper.yaml /tmp/session.yaml
cat > /tmp/req.json <<'JSON'
{"max_tokens":1024,"messages":[{"content":[{"text":"Hi there","type":"text"}],"role":"user"}],"model":"claude-opus-4-6","stream":true}
JSON

S=cassette_demo
tmux kill-session -t "$S" 2>/dev/null || true
tmux new-session -d -s "$S" "bash --norc"   # bash panes (interactive comments on)
tmux split-window -h -t "$S" "bash --norc"
tmux set -t "$S" status off

# Background player: types into each pane while we're attached + recording, so
# the recorder captures the action live.
(
  p() { tmux send-keys -t "$S:0.$1" "$2" C-m; }
  sleep 1.2
  # Clean, minimal prompt; clear wipes the setup line.
  p 0 'export PATH=/tmp:$PATH PS1="$ " FORCE_COLOR=1; clear'
  p 1 'export PATH=/tmp:$PATH PS1="$ " FORCE_COLOR=1; clear'
  sleep 0.6
  p 0 '# serve — offline LLM endpoint (no key, no network)'
  p 1 '# client — any SDK or curl; byte-for-byte replay'
  sleep 0.9
  p 0 'cassette serve /tmp/session.yaml --addr :8181'
  sleep 2.0
  # Show the replayed SSE event stream (short lines fit a narrow pane).
  p 1 'curl -s localhost:8181/v1/messages -H "Content-Type: application/json" \'
  p 1 '  -d @/tmp/req.json | grep "^event:"'
  sleep 2.6
  p 1 '# ↑ replayed from the cassette — 0 dials, 0 key'
  sleep 2.6
  tmux send-keys -t "$S:0.0" C-c          # stop serve
  sleep 0.7
  tmux kill-session -t "$S" 2>/dev/null || true
) &

tmux attach -t "$S"
