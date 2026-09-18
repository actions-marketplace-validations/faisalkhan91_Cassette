# Auto-rendered: vhs scripts/feat-doctor.tape  (fixtures: scripts/feature-demo-setup.sh)
Output assets/demo-doctor.gif
Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 380
Set Padding 16
Set Theme "Catppuccin Mocha"
Set Framerate 24
Hide
Type "bash scripts/feature-demo-setup.sh >/dev/null 2>&1" Enter
Type "export PATH=/tmp:$PATH FORCE_COLOR=1 PS1='$ '; cd /tmp/cass-feat; clear" Enter
Show
Sleep 700ms
Type "cassette doctor session.yaml --request req.json" Sleep 500ms Enter
Sleep 4500ms
