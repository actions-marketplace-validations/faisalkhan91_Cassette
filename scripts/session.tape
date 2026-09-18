# vhs tape for the 5-prompt live-session demo → assets/demo-session.gif
# Run:  vhs scripts/session.tape   (needs vhs)

Output assets/demo-session.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 16
Set Theme "Catppuccin Mocha"
Set Framerate 24

Hide
Type "cd $(git rev-parse --show-toplevel)" Enter
Type "export GOFLAGS=-mod=vendor GOPROXY=off FORCE_COLOR=1 CASSETTE_DEMO_PACE=1 PS1='$ '" Enter
Type "go build -o /tmp/session ./examples/session-demo" Enter
Type "clear" Enter
Show

Type "/tmp/session" Enter
Sleep 8s
