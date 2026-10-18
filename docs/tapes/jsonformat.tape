# Renders docs/demo/jsonformat.gif — run `make demo` from the repo root.
Output docs/demo/jsonformat.gif

Require jsonformat

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
Type "cd docs/samples && clear"
Enter
Show

Type "jsonformat -q build data.json"
Sleep 400ms
Enter
Sleep 3s

Hide
Type "clear"
Enter
Show
Type "jsonformat broken.json"
Enter
Sleep 4s

Type `echo '{"name": "go", "tags": ["cli", "json",]}' | jsonformat`
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show
Type "jsonformat --sort-keys -q 'maintainers[0]' data.json"
Enter
Sleep 3s

Type "jsonformat --check *.json; echo exit=$?"
Enter
Sleep 5s
