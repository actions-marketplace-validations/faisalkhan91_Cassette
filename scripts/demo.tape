# vhs tape for the REAL split-terminal demo → assets/demo-split.gif
# Install once, then run:   vhs scripts/demo.tape
#   brew install vhs tmux          (vhs bundles ttyd + ffmpeg)
#
# It drives scripts/split-demo.sh, which opens a tmux split: left serves a
# recording as an offline LLM endpoint, right curls it for a byte-for-byte replay.

Output assets/demo-split.gif

Require tmux

Set Shell "bash"
Set FontSize 16
Set Width 1440
Set Height 460
Set Padding 14
Set Theme "Catppuccin Mocha"
Set Framerate 24
Set PlaybackSpeed 1.0

Hide
Type "cd $(git rev-parse --show-toplevel)" Enter
Show

Type "scripts/split-demo.sh" Enter
Sleep 18s
