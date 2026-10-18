# Renders docs/demo/reqping.gif — run `make demo` from the repo root.
# Requires internet access (the targets are public URLs).
Output docs/demo/reqping.gif

Require reqping

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 600
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

Type "grep -v '^#' targets.txt | grep ."
Enter
Sleep 2s

Hide
Type "clear"
Enter
Show
Type "reqping -f targets.txt; echo exit=$?"
Sleep 400ms
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "reqping -f targets.txt --count 5"
Enter
Sleep 8s

Hide
Type "clear"
Enter
Show
Type "reqping --watch 2s https://go.dev https://github.com"
Enter
Sleep 7s
Ctrl+C
Sleep 1s
