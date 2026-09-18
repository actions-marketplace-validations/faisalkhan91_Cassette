#!/usr/bin/env python3
"""Generate assets/demo-tour.gif: a longer single-terminal walkthrough of the
headline commands. Offline; Pillow + system Menlo font, no new deps.

Regenerate:  python3 scripts/gen-demo-tour.py
"""
import os
from PIL import Image, ImageDraw, ImageFont

OUT = os.path.join(os.path.dirname(__file__), "..", "assets", "demo-tour.gif")
FONT = "/System/Library/Fonts/Menlo.ttc"

BG, CHROME, LINE = (13, 17, 23), (22, 27, 34), (48, 54, 61)
FG, DIM = (201, 209, 217), (120, 128, 140)
GREEN, BLUE, CYAN, YELLOW, RED = (63, 185, 80), (88, 166, 255), (57, 197, 187), (210, 168, 120), (248, 113, 93)

W, H = 1180, 460
body = ImageFont.truetype(FONT, 18)
chrome = ImageFont.truetype(FONT, 15)
LH, X0, Y0 = 28, 30, 78

# Scenes: each is a list of (text, color); line 0 is the typed command.
scenes = [
    [("$ go run ./examples/agent-demo", FG),
     ("• live    recording 5 tool calls  → session.yaml", GREEN),
     ("• replay  network blocked, served from the cassette", BLUE),
     ("  liveSHA cd538a2d…   replaySHA cd538a2d…", DIM),
     ("  ✓ identical transcript · outbound dials: 0", GREEN)],
    [("$ cassette doc session.yaml", FG),
     ("# Cassette: session.yaml", FG),
     ("## 0. POST /v1/messages → 200", DIM),
     ("> Hello, world!", FG),
     ("_finish: end_turn_", DIM)],
    [("$ cassette serve session.yaml --addr :8080", FG),
     ("✓ serving on http://127.0.0.1:8080  — no key, no network", GREEN),
     ("$ curl -s :8080/v1/messages -d @req.json", FG),
     ("event: content_block_delta", DIM),
     ("data: …\"text\":\"Hello, world!\"   ← replayed byte-for-byte", CYAN)],
    [("$ cassette report run.yaml --no-tool delete_account --bundle tuesday", FG),
     ("• BUG LIVE: forbidden tool delete_account called in turn 3", RED),
     ("✓ wrote tuesday.castiron/  — cassette + signed attest + run.sh", GREEN),
     ("  commit it; CI re-runs it and turns green when fixed", DIM)],
    [("$ cassette branch run.yaml --from 2 --graft text=\"use the safe tool\" -o seed.yaml", FG),
     ("✓ branch seed seed.yaml (turns 0..2)", GREEN),
     ("  determinism: pinned (seed=42) — re-run should reproduce", YELLOW),
     ("  open in ModeBranch, run your agent, Save() the live branch", DIM)],
    [("$ cassette --help", FG),
     ("Start here →  init · proxy/serve · inspect/doc · verify/conformance", CYAN),
     ("…then the full toolkit: diff · doctor · cost · attest · graft · …", GREEN)],
]


def render(lines, cursor=False):
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)
    d.rectangle([0, 0, W, 40], fill=CHROME)
    for i, c in enumerate(((237, 106, 94), (245, 191, 79), (98, 197, 84))):
        d.ellipse([20 + i * 22, 14, 32 + i * 22, 26], fill=c)
    d.text((W // 2 - 60, 12), "cassette — tour", font=chrome, fill=DIM)
    d.line([0, 40, W, 40], fill=LINE)
    for i, (t, col) in enumerate(lines):
        d.text((X0, Y0 + i * LH), t, font=body, fill=col)
    if cursor and lines:
        cx = X0 + int(body.getlength(lines[-1][0])) + 4
        cy = Y0 + (len(lines) - 1) * LH
        d.rectangle([cx, cy + 2, cx + 9, cy + 20], fill=FG)
    d.text((X0, H - 32), "record once, replay forever  ·  github.com/faisalkhan91/cassette",
           font=chrome, fill=DIM)
    return img


frames, durs = [], []
for sc in scenes:
    frames.append(render([sc[0]], cursor=True)); durs.append(420)        # typed command
    for n in range(2, len(sc) + 1):
        frames.append(render(sc[:n])); durs.append(150)                  # reveal output
    frames.append(render(sc, cursor=True)); durs.append(1700)            # hold
    frames.append(render([])); durs.append(160)                         # clear → next

pal = [f.convert("P", palette=Image.ADAPTIVE, colors=64) for f in frames]
pal[0].save(OUT, save_all=True, append_images=pal[1:], duration=durs, loop=0,
            optimize=True, disposal=2)
print("wrote", os.path.relpath(OUT), "frames=%d  ~%.1fs" % (len(frames), sum(durs) / 1000))
