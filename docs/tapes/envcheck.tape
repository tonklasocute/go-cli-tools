# Renders docs/demo/envcheck.gif — run `make demo` from the repo root.
Output docs/demo/envcheck.gif

Require envcheck

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

Type "# the schema is just an annotated .env.example"
Enter
Type "grep -E 'PORT|LOG_LEVEL|API_KEY' .env.example"
Enter
Sleep 2s

Hide
Type "clear"
Enter
Show
Type "envcheck"
Sleep 400ms
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "envcheck --show-values --strict"
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "envcheck init | head -14"
Enter
Sleep 4s
