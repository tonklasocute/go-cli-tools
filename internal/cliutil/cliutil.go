// Package cliutil wires the conventions shared by every go-cli-tools binary:
// common flags (--json, --no-color, --version), exit codes, error reporting
// and Ctrl+C handling. Each tool's cobra command is built against IO so it can
// be executed in-process by tests.
package cliutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/output"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
	"github.com/tonklasocute/go-cli-tools/internal/version"
)

// Exit codes shared by every tool.
const (
	// ExitOK means the command ran and every check passed.
	ExitOK = 0
	// ExitFail means the command ran but a check failed
	// (invalid JSON, unhealthy endpoint, schema violation, ...).
	ExitFail = 1
	// ExitUsage means a usage error or an operational error
	// (bad flags, unreadable file, network setup failure, ...).
	ExitUsage = 2
	// ExitInterrupted means the user pressed Ctrl+C.
	ExitInterrupted = 130
)

// IO bundles the standard streams so commands never touch os.Std* directly.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// StdIO returns the process's real standard streams.
func StdIO() IO { return IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr} }

// StdinIsPiped reports whether In is something other than an interactive
// terminal (a pipe, a file or an in-memory reader), i.e. it is worth reading.
func (s IO) StdinIsPiped() bool {
	if s.In == nil {
		return false
	}
	return !ui.IsTerminal(s.In)
}

// ExitError carries a specific exit code out of a command. A nil Err means
// the failure has already been reported and nothing more should be printed.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ExitError) Unwrap() error { return e.Err }

// ErrCheckFailed signals exit code 1 after the command has printed its own
// report. Return it from RunE when a check fails.
var ErrCheckFailed = &ExitError{Code: ExitFail}

// UsageError wraps err so that it maps to exit code 2 and prints a usage hint.
func UsageError(err error) error { return &ExitError{Code: ExitUsage, Err: &usageErr{err}} }

// UsageErrorf formats a usage error.
func UsageErrorf(format string, args ...any) error { return UsageError(fmt.Errorf(format, args...)) }

type usageErr struct{ error }

func (u *usageErr) Unwrap() error { return u.error }

// Common holds the flags every tool supports.
type Common struct {
	JSON    bool
	NoColor bool
	IO      IO
}

// Theme returns the theme for stdout, honoring --no-color, NO_COLOR, --json
// and TTY detection.
func (c *Common) Theme() *ui.Theme {
	return ui.NewTheme(!c.JSON && ui.ColorEnabled(c.IO.Out, c.NoColor))
}

// ErrTheme returns the theme for stderr.
func (c *Common) ErrTheme() *ui.Theme {
	return ui.NewTheme(ui.ColorEnabled(c.IO.Err, c.NoColor))
}

// Printer returns an output printer bound to stdout.
func (c *Common) Printer() output.Printer {
	return output.Printer{W: c.IO.Out, JSON: c.JSON, Theme: c.Theme()}
}

// Interactive reports whether live indicators (spinners, progress bars) may
// be drawn on stderr.
func (c *Common) Interactive() bool { return !c.JSON && ui.IsTerminal(c.IO.Err) }

// NewRoot applies the shared conventions to a tool's root command: version
// flag, silenced cobra errors (we print our own), usage errors mapped to exit
// code 2 and the --json / --no-color persistent flags bound to c.
func NewRoot(c *Common, cmd *cobra.Command) *cobra.Command {
	cmd.Version = version.Get().Version
	cmd.SetVersionTemplate(version.String(cmd.Name()))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetIn(c.IO.In)
	cmd.SetOut(c.IO.Out)
	cmd.SetErr(c.IO.Err)
	cmd.CompletionOptions.HiddenDefaultCmd = true
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return UsageError(err) })
	pf := cmd.PersistentFlags()
	pf.BoolVar(&c.JSON, "json", false, "print machine-readable JSON instead of text")
	pf.BoolVar(&c.NoColor, "no-color", false, "disable colored output (also honors NO_COLOR)")
	return cmd
}

// WrapArgs converts a cobra positional-args validator so that its errors map
// to exit code 2.
func WrapArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return UsageError(err)
		}
		return nil
	}
}

// Run executes cmd with args and returns the process exit code. Errors are
// printed to the command's error stream as "<tool>: error: ...".
func Run(ctx context.Context, cmd *cobra.Command, args []string) int {
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(ctx)
	return HandleError(cmd, err, ctx.Err() != nil)
}

// HandleError reports err (if any) on cmd's error stream and maps it to an
// exit code.
func HandleError(cmd *cobra.Command, err error, interrupted bool) int {
	errOut := cmd.ErrOrStderr()
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	th := ui.NewTheme(ui.ColorEnabled(errOut, noColor))
	if interrupted || errors.Is(err, context.Canceled) {
		fmt.Fprintln(errOut, th.Warn("interrupted"))
		return ExitInterrupted
	}
	if err == nil {
		return ExitOK
	}
	code := ExitUsage
	var ee *ExitError
	if errors.As(err, &ee) {
		code = ee.Code
		if ee.Err == nil {
			return code
		}
	}
	fmt.Fprintf(errOut, "%s: %s %v\n", cmd.Root().Name(), th.Error("error:"), err)
	var ue *usageErr
	if errors.As(err, &ue) {
		fmt.Fprintf(errOut, "Run '%s --help' for usage.\n", cmd.Root().Name())
	}
	return code
}

// Main is the entire body of each cmd/<tool>/main.go: it wires real stdio,
// enables ANSI on Windows consoles, cancels on Ctrl+C and returns the exit
// code for os.Exit.
func Main(newCmd func(IO) *cobra.Command) int {
	if restore, err := termenv.EnableVirtualTerminalProcessing(termenv.NewOutput(os.Stdout)); err == nil {
		defer restore() //nolint:errcheck // best effort on exit
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return Run(ctx, newCmd(StdIO()), os.Args[1:])
}
