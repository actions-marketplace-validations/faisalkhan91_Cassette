# vhs tape for the Castiron forward-branch demo → assets/demo-branch.gif
# Install once, then run:   vhs scripts/branch.tape   (needs vhs + tmux)
#
# Drives scripts/branch-demo.sh: LEFT shows a run that leaked data; RIGHT grafts
# that one step to a fix and prepares a forward-branch seed (re-run live in ModeBranch).

Output assets/demo-branch.gif

Require tmux

Set Shell "bash"
Set FontSize 16
Set Width 1440
Set Height 420
Set Padding 14
Set Theme "Catppuccin Mocha"
Set Framerate 24

Hide
Type "cd $(git rev-parse --show-toplevel)" Enter
Show

Type "scripts/branch-demo.sh" Enter
Sleep 16s
