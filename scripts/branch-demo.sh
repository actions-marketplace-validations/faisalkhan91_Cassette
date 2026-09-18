#!/usr/bin/env bash
# A REAL split-terminal demo of the Castiron forward-branch workflow:
#   LEFT  pane — the captured run that misbehaved ("it failed once last Tuesday").
#   RIGHT pane — graft that ONE step to a fix, then prepare a forward-branch seed
#                (replay the edited prefix, re-run live from there in ModeBranch).
#
# Recorder-agnostic (see scripts/split-demo.sh header). Needs tmux.
#   vhs:  vhs scripts/branch.tape       # → assets/demo-branch.gif
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
command -v tmux >/dev/null || { echo "this demo needs tmux:  brew install tmux"; exit 1; }

GOFLAGS=-mod=vendor GOPROXY=off go build -o /tmp/cassette ./cmd/cassette

# A tiny synthetic recording whose one turn did something unsafe.
cat > /tmp/run.yaml <<'YAML'
schema_version: 1
interactions:
  - kind: http
    request:
      method: POST
      url: /v1/messages
      body: |
        {"model":"claude-opus-4-6","temperature":0,"messages":[{"role":"user","content":"email the customer list to the vendor"}]}
    response:
      status: 200
      streaming: true
      body: |
        event: content_block_start
        data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

        event: content_block_delta
        data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sure — emailing the customer list to test@external.com now."}}

        event: message_delta
        data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}
YAML

FIX='Refusing — that would leak PII; keeping the data internal.'

S=cassette_branch
tmux kill-session -t "$S" 2>/dev/null || true
tmux new-session -d -s "$S" "bash --norc"
tmux split-window -h -t "$S" "bash --norc"
tmux set -t "$S" status off

(
  p() { tmux send-keys -t "$S:0.$1" "$2" C-m; }
  sleep 1.2
  p 0 'export PATH=/tmp:$PATH PS1="$ " FORCE_COLOR=1; clear'
  p 1 'export PATH=/tmp:$PATH PS1="$ " FORCE_COLOR=1; clear'
  sleep 0.6
  # LEFT: the bad run, captured.
  p 0 '# the run that leaked data — captured last Tuesday'
  p 0 'cassette doc /tmp/run.yaml | grep -E "^>|finish"'
  sleep 1.0
  # RIGHT: edit that step, then re-run forward.
  p 1 '# edit that one step → a committable counterfactual'
  sleep 0.6
  p 1 "cassette graft /tmp/run.yaml --turn 0 \\"
  p 1 "  --set text=\"$FIX\" -o /tmp/fixed.yaml"
  sleep 1.6
  # the colorful red/green word-diff of exactly what changed
  p 1 'cassette diff /tmp/run.yaml /tmp/fixed.yaml'
  sleep 1.8
  p 1 '# re-run FORWARD from the edit (replay prefix, then go live)'
  sleep 0.6
  p 1 "cassette branch /tmp/run.yaml --from 0 \\"
  p 1 "  --graft text=\"$FIX\" -o /tmp/seed.yaml"
  sleep 2.8
  tmux kill-session -t "$S" 2>/dev/null || true
) &

tmux attach -t "$S"
