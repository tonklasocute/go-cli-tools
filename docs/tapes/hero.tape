# Renders docs/demo/hero.gif (the README banner) — run `make demo` from the repo root.
Output docs/demo/hero.gif

Require envcheck
Require jsonformat
Require reqping

Set Shell "bash"
Set FontSize 15
Set Width 1200
Set Height 620
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set WindowBar Colorful

Hide
Type "cd docs/samples && clear"
Enter
Show

Type "envcheck | tail -6"
Enter
Sleep 3s

Type "clear && jsonformat broken.json"
Enter
Sleep 3s

Type "clear && reqping -f targets.txt"
Enter
Sleep 4s
