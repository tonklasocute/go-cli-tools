package cliutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newTestCmd(c *Common, run func(cmd *cobra.Command, args []string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "demo [arg]",
		Args: WrapArgs(cobra.MaximumNArgs(1)),
		RunE: run,
	}
	cmd.Flags().Int("n", 0, "a number")
	return NewRoot(c, cmd)
}

func TestRunExitCodes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tests := []struct {
		name       string
		args       []string
		run        func(*cobra.Command, []string) error
		wantCode   int
		wantStderr string
		wantStdout string
	}{
		{name: "ok", run: func(*cobra.Command, []string) error { return nil }, wantCode: ExitOK},
		{
			name: "check failed is silent", wantCode: ExitFail,
			run: func(*cobra.Command, []string) error { return ErrCheckFailed },
		},
		{
			name: "operational error", wantCode: ExitUsage, wantStderr: "demo: error: disk on fire\n",
			run: func(*cobra.Command, []string) error { return errors.New("disk on fire") },
		},
		{
			name: "wrapped exit error keeps code", wantCode: 7, wantStderr: "demo: error: ctx: custom\n",
			run: func(*cobra.Command, []string) error {
				return fmt.Errorf("ctx: %w", &ExitError{Code: 7, Err: errors.New("custom")})
			},
		},
		{
			name: "usage error from RunE", wantCode: ExitUsage, wantStderr: "Run 'demo --help' for usage.",
			run: func(*cobra.Command, []string) error { return UsageErrorf("bad %s", "input") },
		},
		{
			name: "unknown flag", args: []string{"--nope"}, wantCode: ExitUsage,
			wantStderr: "unknown flag: --nope",
			run:        func(*cobra.Command, []string) error { return nil },
		},
		{
			name: "too many args", args: []string{"a", "b"}, wantCode: ExitUsage,
			wantStderr: "accepts at most 1 arg",
			run:        func(*cobra.Command, []string) error { return nil },
		},
		{
			name: "canceled maps to 130", wantCode: ExitInterrupted, wantStderr: "interrupted",
			run: func(*cobra.Command, []string) error { return context.Canceled },
		},
		{
			name: "version flag", args: []string{"--version"}, wantCode: ExitOK, wantStdout: "demo ",
			run: func(*cobra.Command, []string) error { return nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			c := &Common{IO: IO{In: strings.NewReader(""), Out: &out, Err: &errb}}
			code := Run(context.Background(), newTestCmd(c, tt.run), tt.args)
			if code != tt.wantCode {
				t.Fatalf("code = %d, want %d (stderr=%q)", code, tt.wantCode, errb.String())
			}
			if tt.wantStderr == "" && errb.Len() != 0 {
				t.Fatalf("unexpected stderr %q", errb.String())
			}
			if !strings.Contains(errb.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want to contain %q", errb.String(), tt.wantStderr)
			}
			if !strings.Contains(out.String(), tt.wantStdout) {
				t.Fatalf("stdout = %q, want to contain %q", out.String(), tt.wantStdout)
			}
		})
	}
}

func TestRunInterruptedContext(t *testing.T) {
	var out, errb bytes.Buffer
	c := &Common{IO: IO{Out: &out, Err: &errb}}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := newTestCmd(c, func(*cobra.Command, []string) error {
		cancel() // simulate Ctrl+C arriving while the command runs
		return nil
	})
	if code := Run(ctx, cmd, nil); code != ExitInterrupted {
		t.Fatalf("code = %d, want %d", code, ExitInterrupted)
	}
}

func TestCommonFlagsAndHelpers(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	var out, errb bytes.Buffer
	c := &Common{IO: IO{In: strings.NewReader("x"), Out: &out, Err: &errb}}
	var sawJSON bool
	cmd := newTestCmd(c, func(*cobra.Command, []string) error {
		sawJSON = c.JSON
		if c.Theme().Color() {
			t.Error("theme must be colorless under --json")
		}
		if c.Interactive() {
			t.Error("--json must never be interactive")
		}
		return c.Printer().Emit(map[string]int{"a": 1}, nil)
	})
	if code := Run(context.Background(), cmd, []string{"--json"}); code != ExitOK {
		t.Fatalf("code = %d stderr=%q", code, errb.String())
	}
	if !sawJSON || out.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("json output = %q", out.String())
	}

	c2 := &Common{IO: IO{Out: &out, Err: &errb}, NoColor: true}
	if c2.Theme().Color() || c2.ErrTheme().Color() {
		t.Error("--no-color must disable color")
	}
	c3 := &Common{IO: IO{Out: &out, Err: &errb}}
	if !c3.Theme().Color() || !c3.ErrTheme().Color() {
		t.Error("FORCE_COLOR should enable color on buffers")
	}
}

func TestStdIOAndPiped(t *testing.T) {
	s := StdIO()
	if s.In != os.Stdin || s.Out != os.Stdout || s.Err != os.Stderr {
		t.Fatal("StdIO must return the os streams")
	}
	if (IO{}).StdinIsPiped() {
		t.Error("nil stdin is not piped")
	}
	if !(IO{In: strings.NewReader("")}).StdinIsPiped() {
		t.Error("reader stdin should count as piped")
	}
}

func TestExitErrorString(t *testing.T) {
	if got := (&ExitError{Code: 3}).Error(); got != "exit status 3" {
		t.Errorf("Error() = %q", got)
	}
	inner := errors.New("inner")
	e := &ExitError{Code: 1, Err: inner}
	if e.Error() != "inner" || !errors.Is(e, inner) {
		t.Errorf("ExitError should wrap inner")
	}
}
