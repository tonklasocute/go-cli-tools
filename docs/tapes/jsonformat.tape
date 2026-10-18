# Renders docs/demo/jsonformat.gif — run `make demo` from the repo root.
Output docs/demo/jsonformat.gif

Require jsonformat

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 680
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set WindowBar Colorful

Hide
Type "cd docs/samples && clear"
Enter
Show

Type "jsonformat data.json | head -20"
Sleep 500ms
Enter
Sleep 3s

Type "clear && jsonformat broken.json"
Enter
Sleep 4s

Type "clear && jsonformat -q 'tools[0].tags' data.json"
Enter
Sleep 2s

Type `echo '{"b":2,"a":[1,2,],}' | jsonformat`
Enter
Sleep 3s

Type "clear && jsonformat --check *.json; echo exit=$?"
Enter
Sleep 4s
