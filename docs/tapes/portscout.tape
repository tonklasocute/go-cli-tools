# Renders docs/demo/portscout.gif — run `make demo` from the repo root.
Output docs/demo/portscout.gif

Require portscout
Require go

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 760
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set WindowBar Colorful

# Pretend a few dev services are running.
Hide
Type "go run docs/tapes/fakeports.go 5173 5432 6379 8080 & sleep 3 && clear"
Enter
Sleep 4s
Show

Type "portscout"
Sleep 500ms
Enter
Sleep 4s

Type "clear && portscout -p 1-20000 --workers 500"
Enter
Sleep 5s

Type "clear && portscout --expect-open 5432,27017; echo exit=$?"
Enter
Sleep 4s

Hide
Type "kill %1"
Enter
