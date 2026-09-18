# Auto-rendered: vhs scripts/feat-migrate.tape  (fixtures: scripts/feature-demo-setup.sh)
Output assets/demo-migrate.gif
Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 420
Set Padding 16
Set Theme "Catppuccin Mocha"
Set Framerate 24
Hide
Type "bash scripts/feature-demo-setup.sh >/dev/null 2>&1" Enter
Type "export PATH=/tmp:$PATH FORCE_COLOR=1 PS1='$ '; cd /tmp/cass-feat; clear" Enter
Show
Sleep 700ms
Type "ls corpus" Sleep 400ms Enter
Sleep 1500ms
Type "cassette migrate corpus --to openai-chat -o migrated" Sleep 400ms Enter
Sleep 4s
