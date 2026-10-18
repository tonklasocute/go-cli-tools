# Renders docs/demo/hero.gif — run `make demo` from the repo root.
Output docs/demo/hero.gif

Require envcheck
Require jsonformat
Require reqping

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set WindowBar Colorful

Hide
Type `export PATH="$PWD/bin:$PATH" PS1="\[\e[1;35m\]❯\[\e[0m\] "`
Enter
Type "cd docs/samples && clear"
Enter
Show

Type "envcheck"
Sleep 400ms
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show
Type "jsonformat broken.json"
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show
Type "reqping -f targets.txt"
Enter
Sleep 5s
