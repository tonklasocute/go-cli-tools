package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndRun(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/echo\n\ngo 1.22\n")
	write("main.go", `package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	b, _ := io.ReadAll(os.Stdin)
	fmt.Print(string(b))
	fmt.Fprint(os.Stderr, os.Getenv("NO_COLOR"))
	if len(os.Args) > 1 {
		os.Exit(3)
	}
}
`)
	bin := BuildBinary(t, dir, "echo")

	r := RunBinary(t, bin, strings.NewReader("hello"))
	if r.Stdout != "hello" || r.Stderr != "1" || r.Code != 0 {
		t.Fatalf("unexpected result %+v", r)
	}
	if r := RunBinary(t, bin, nil, "fail"); r.Code != 3 {
		t.Fatalf("exit code = %d, want 3", r.Code)
	}
}
