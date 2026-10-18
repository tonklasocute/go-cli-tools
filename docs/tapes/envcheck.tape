# Renders docs/demo/envcheck.gif — run `make demo` from the repo root.
Output docs/demo/envcheck.gif

Require envcheck

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set WindowBar Colorful

Hide
Type "cd docs/samples && clear"
Enter
Show

Type "# the schema is just an annotated .env.example"
Sleep 400ms
Enter
Type "grep -E 'PORT|LOG_LEVEL|API_KEY' .env.example"
Enter
Sleep 2s

Type "envcheck"
Sleep 500ms
Enter
Sleep 4s

Type "clear && envcheck --show-values | head -9"
Enter
Sleep 4s

Type "clear && envcheck init | head -12"
Enter
Sleep 4s
