#!/usr/bin/env python3
"""Generate assets/demo.gif: a side-by-side RECORD | REPLAY terminal animation.

Fully offline — uses Pillow + a system monospace font, no network, no new Go deps.
Regenerate with:  python3 scripts/gen-demo.py
The content mirrors `go run ./examples/agent-demo` (the coffee multi-tool agent):
the SAME agent runs live (left, recording to a cassette) and offline (right,
replaying with the network blocked), producing an identical transcript + digest
and provably zero outbound dials.
"""
import os
from PIL import Image, ImageDraw, ImageFont

OUT = os.path.join(os.path.dirname(__file__), "..", "assets", "demo.gif")
FONT = "/System/Library/Fonts/Menlo.ttc"

# palette
BG     = (13, 17, 23)
PANEL  = (22, 27, 34)
LINE   = (48, 54, 61)
FG     = (201, 209, 217)
DIM    = (110, 118, 129)
GREEN  = (63, 185, 80)
BLUE   = (88, 166, 255)
CYAN   = (57, 197, 187)
YELLOW = (210, 168, 120)

W, H = 1120, 600
PAD = 26
body = ImageFont.truetype(FONT, 19)
hdr  = ImageFont.truetype(FONT, 18)
big  = ImageFont.truetype(FONT, 22)
LH = 27  # line height

# Each line: (text, color). Revealed in lockstep across both panes.
left = [
    ("$ go test -update", FG),
    ("", FG),
    ("• RECORD   → session.yaml", GREEN),
    ("  live provider, captured", DIM),
    ("", FG),
    ("  1 list_menu()          → 5 drinks", CYAN),
    ("  2 get_price(latte)     → $4.50", CYAN),
    ("  3 check_inventory()    → in stock", CYAN),
    ("  4 caffeine_mg(latte)   → 128 mg", CYAN),
    ("  5 brew_minutes(latte)  → 4 min", CYAN),
    ("", FG),
    ("  “A latte takes 4 minutes.”", FG),
    ("  digest  cd538a2d…", DIM),
    ("  ✓ recorded · 6 turns", GREEN),
]
right = [
    ("$ go test", FG),
    ("", FG),
    ("• REPLAY   network blocked", BLUE),
    ("  served from session.yaml", DIM),
    ("", FG),
    ("  1 list_menu()          → 5 drinks", CYAN),
    ("  2 get_price(latte)     → $4.50", CYAN),
    ("  3 check_inventory()    → in stock", CYAN),
    ("  4 caffeine_mg(latte)   → 128 mg", CYAN),
    ("  5 brew_minutes(latte)  → 4 min", CYAN),
    ("", FG),
    ("  “A latte takes 4 minutes.”", FG),
    ("  digest  cd538a2d…", DIM),
    ("  ✓ replayed · dials: 0", GREEN),
]
N = len(left)
MIDX = W // 2


def frame(reveal, caption=False):
    img = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(img)
    # title
    d.text((PAD, 18), "cassette", font=big, fill=FG)
    d.text((PAD + big.getlength("cassette") + 14, 22), "record once, replay forever",
           font=hdr, fill=DIM)
    top = 64
    # two panels
    d.rectangle([PAD, top, MIDX - 13, H - 58], fill=PANEL, outline=LINE)
    d.rectangle([MIDX + 13, top, W - PAD, H - 58], fill=PANEL, outline=LINE)
    # pane header bars
    d.rectangle([PAD, top, MIDX - 13, top + 30], fill=(28, 33, 40))
    d.rectangle([MIDX + 13, top, W - PAD, top + 30], fill=(28, 33, 40))
    d.text((PAD + 14, top + 7), "RECORD  ·  live", font=hdr, fill=GREEN)
    d.text((MIDX + 27, top + 7), "REPLAY  ·  offline", font=hdr, fill=BLUE)
    # content
    cy = top + 44
    for i in range(min(reveal, N)):
        lt, lc = left[i]
        rt, rc = right[i]
        d.text((PAD + 16, cy + i * LH), lt, font=body, fill=lc)
        d.text((MIDX + 29, cy + i * LH), rt, font=body, fill=rc)
    # caption strip
    cap = "same transcript   ·   same digest   ·   zero network egress"
    col = GREEN if caption else DIM
    d.text((PAD, H - 40), cap, font=hdr, fill=col)
    return img


frames, durs = [], []
# intro: headers only, brief
frames.append(frame(0)); durs.append(500)
# reveal command + status quickly, then tool calls one-by-one, then answer
schedule = [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14]
pace = {5: 170, 6: 170, 7: 170, 8: 170, 9: 170}  # tool lines a touch slower
for r in schedule:
    frames.append(frame(r)); durs.append(pace.get(r, 120))
# final hold with green caption (twice for emphasis / loop breath)
frames.append(frame(N, caption=True)); durs.append(2600)

# encode (256-color GIF; our palette is tiny so quantization is clean)
pal = [f.convert("P", palette=Image.ADAPTIVE, colors=64) for f in frames]
pal[0].save(OUT, save_all=True, append_images=pal[1:], duration=durs, loop=0,
            optimize=True, disposal=2)
print("wrote", os.path.relpath(OUT), "frames=%d" % len(frames))
