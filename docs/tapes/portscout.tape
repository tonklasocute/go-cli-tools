# Renders docs/demo/portscout.gif — run `make demo` from the repo root.
Output docs/demo/portscout.gif

Require portscout
Require fakeports

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
# Pretend a few dev services are running.
Type "fakeports 5173 5432 6379 8080 & sleep 1; clear"
Enter
Sleep 5s
Show

Type "portscout -p 3000,3306,5173,5432,6379,8080,9200,27017"
Sleep 400ms
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show
Type "portscout -p 1-20000 --workers 500"
Enter
Sleep 5s

Hide
Type "clear"
Enter
Show
Type "portscout --expect-open 5432,27017; echo exit=$?"
Enter
Sleep 5s

Hide
Type "kill %1"
Enter
