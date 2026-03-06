# Requires https://github.com/rumpl/vhs/tree/screen-settled
# and brew install ffmpeg ttyd

Output docs/demo.gif

Require cagent

Set Shell "zsh"
Set FontSize 24
Set Width 1800
Set Height 1000
Set PlaybackSpeed 2
Set Padding 0
Set WindowBar Colorful
Set BorderRadius 10

Type "cagent run ./examples/gopher.yaml" Sleep 3s Enter
Sleep 1s

Type "Run my non e2e tests and show me a table of the top 10 slowest." Sleep 1s Enter

Wait+Screen     /allow this tool call/
Sleep 1s
Type "A"

Sleep 1s WaitScreenSettled 1s Sleep 1s

Type "Now, look at the slowest test and optimize it." Sleep 1s Enter

Sleep 1s WaitScreenSettled 1s Sleep 1s

Sleep 20s