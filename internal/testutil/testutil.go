// Package testutil provides helpers for integration tests that build and run
// the real binaries.
package testutil

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// BuildBinary compiles the main package in dir (relative to the test's
// working directory, usually ".") and returns the path to the executable.
// The binary is placed in a temporary directory removed after the test.
func BuildBinary(t testing.TB, dir, name string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping binary build in -short mode")
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", dir, err, b)
	}
	return out
}

// Result is the outcome of running a binary.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// RunBinary executes bin with args, feeding stdin (may be nil). Color is
// disabled via NO_COLOR so output is stable.
func RunBinary(t testing.TB, bin string, stdin io.Reader, args ...string) Result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %s: %v", bin, err)
		}
		code = ee.ExitCode()
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}
