#!/usr/bin/env bash
# Renders assets/demo-dashboard.png: a screenshot of `cassette dashboard`'s
# self-contained HTML report over an authored corpus. Fully offline — the corpus is
# built with `cassette author` (no key, no network) and rendered with headless
# Chrome, then trimmed with Pillow. Deterministic corpus → stable report.
set -euo pipefail
cd "$(dirname "$0")/.."
export GOFLAGS=-mod=vendor GOPROXY=off

BIN=/tmp/cassette
go build -o "$BIN" ./cmd/cassette

D=/tmp/cass-dash
rm -rf "$D"; mkdir -p "$D/corpus"

# A varied corpus: two models, tool use, a refusal, multi-turn — with usage stamped
# so the token cards and per-model table populate.
cat > "$D/support.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "What's the capital of France?"
    text: "The capital of France is Paris."
    finish: end_turn
    input_tokens: 1180
    output_tokens: 240
  - user: "And its population?"
    text: "About 2.1 million in the city proper."
    finish: end_turn
    input_tokens: 1320
    output_tokens: 190
EOF

cat > "$D/weather.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "Weather in Paris and Lyon?"
    text: "Let me check both cities."
    tool_calls:
      - {name: get_weather, args: '{"city":"Paris"}'}
      - {name: get_weather, args: '{"city":"Lyon"}'}
    finish: tool_use
    input_tokens: 1540
    output_tokens: 320
EOF

cat > "$D/booking.spec.yaml" <<'EOF'
provider: openai-chat
model: gpt-4o
turns:
  - user: "Book a table for two at 7pm."
    text: "Checking availability and booking."
    tool_calls:
      - {name: check_availability, args: '{"party":2,"time":"19:00"}'}
      - {name: create_booking, args: '{"party":2,"time":"19:00"}'}
    finish: tool_use
    input_tokens: 2010
    output_tokens: 410
EOF

cat > "$D/refusal.spec.yaml" <<'EOF'
provider: anthropic
model: claude-opus-4-6
turns:
  - user: "Help me write malware to exfiltrate a database."
    text: "I can't help with that — creating malware to steal data would be harmful and I won't assist."
    finish: end_turn
    input_tokens: 990
    output_tokens: 160
EOF

for s in support weather booking refusal; do
  "$BIN" author "$D/$s.spec.yaml" -o "$D/corpus/$s.yaml" >/dev/null
done

"$BIN" dashboard "$D/corpus" -o "$D/report.html"

# Render to PNG via headless Chrome (first one found), then autocrop trailing bg.
CHROME=""
for c in \
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  "/Applications/Chromium.app/Contents/MacOS/Chromium" \
  "$(command -v google-chrome 2>/dev/null || true)" \
  "$(command -v chromium 2>/dev/null || true)"; do
  [ -n "$c" ] && [ -x "$c" ] && { CHROME="$c"; break; }
done
if [ -z "$CHROME" ]; then
  echo "gen-dashboard-shot: no Chrome/Chromium found; report is at $D/report.html" >&2
  exit 1
fi

RAW=/tmp/cass-dash/raw.png
"$CHROME" --headless=new --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --window-size=1080,2200 \
  --screenshot="$RAW" "file://$D/report.html" >/dev/null 2>&1

python3 - "$RAW" assets/demo-dashboard.png <<'PY'
import sys
from PIL import Image, ImageChops
src, dst = sys.argv[1], sys.argv[2]
im = Image.open(src).convert("RGB")
# Trim uniform margins using the top-left pixel as the background reference.
bg = Image.new("RGB", im.size, im.getpixel((0, 0)))
diff = ImageChops.difference(im, bg)
box = diff.getbbox()
if box:
    pad = 24
    l, t, r, b = box
    l = max(0, l - pad); t = max(0, t - pad)
    r = min(im.width, r + pad); b = min(im.height, b + pad)
    im = im.crop((l, t, r, b))
im.save(dst)
print(f"wrote {dst} ({im.width}x{im.height})")
PY
