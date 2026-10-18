# Renders docs/demo/reqping.gif — run `make demo` from the repo root.
# Needs internet access (the targets are public URLs).
Output docs/demo/reqping.gif

Require reqping

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set WindowBar Colorful

Hide
Type "clear"
Enter
Show

Type "grep -v '^#' docs/samples/targets.txt | grep ."
Enter
Sleep 2s

Type "reqping -f docs/samples/targets.txt; echo exit=$?"
Sleep 500ms
Enter
Sleep 4s

Type "clear && reqping -f docs/samples/targets.txt --count 5"
Enter
Sleep 7s

Type "clear && reqping --watch 2s https://go.dev https://github.com"
Enter
Sleep 7s
Ctrl+C
Sleep 1s
