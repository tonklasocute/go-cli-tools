# Renders docs/demo/dirsize.gif — run `make demo` from the repo root.
Output docs/demo/dirsize.gif

Require dirsize

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 680
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms
Set WindowBar Colorful

Hide
Type `export PATH="$PWD/bin:$PATH" PS1="\[\e[1;35m\]❯\[\e[0m\] "`
Enter
Type "cd /usr/share && clear"
Enter
Show

Type "# where does the space go in /usr/share?"
Enter
Type "dirsize --top 8"
Sleep 400ms
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "dirsize --files --top 8 --min-size 1MB"
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "dirsize vim --depth 2 --top 5 --json | head -16"
Enter
Sleep 4s
