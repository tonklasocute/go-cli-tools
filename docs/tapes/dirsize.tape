# Renders docs/demo/dirsize.gif — run `make demo` from the repo root.
Output docs/demo/dirsize.gif

Require dirsize
Require go

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 680
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set WindowBar Colorful

Hide
Type `cd "$(go env GOROOT)" && clear`
Enter
Show

Type "# where does the space go in the Go toolchain?"
Enter
Type "dirsize --depth 2 --top 8"
Sleep 500ms
Enter
Sleep 4s

Type "clear && dirsize --files --top 6 --min-size 1MB --exclude '*.exe,testdata'"
Enter
Sleep 4s

Type "clear && dirsize src --json | head -15"
Enter
Sleep 3s
