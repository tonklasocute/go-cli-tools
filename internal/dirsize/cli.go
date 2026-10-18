package dirsize

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// skippedShown is how many skipped paths the text diagnostic lists.
const skippedShown = 5

type options struct {
	depth   int
	top     int
	minSize string
	exclude []string
	files   bool
	workers int
}

// NewCommand builds the dirsize command bound to the given streams.
func NewCommand(io cliutil.IO) *cobra.Command {
	c := &cliutil.Common{IO: io}
	var o options
	cmd := &cobra.Command{
		Use:   "dirsize [flags] [path]",
		Short: "Concurrent directory size analyzer",
		Long: `dirsize walks a directory tree with a bounded pool of concurrent workers and
shows where the space goes: every entry at the chosen --depth with its
recursive size, share of the total and file count, largest first.

Sizes are apparent sizes (the byte length of each file). Symbolic links are
never followed; a link counts as its own small size. On Unix, files with
several hard links are counted once; on Windows every link is counted.
Entries matching --exclude (base names or glob patterns such as '*.log')
are skipped and excluded directories are not walked at all. Entries that
cannot be read (permission denied, vanished files) are skipped, counted and
listed on stderr; --json includes the full list.

With --files, dirsize lists the largest individual files instead.

Exit codes:
  0    scan completed (skipped entries are reported, not treated as failure)
  2    usage error, or the root path is missing or unreadable
  130  interrupted (Ctrl+C)`,
		Example: `  dirsize
  dirsize ~/projects --depth 2 --top 20
  dirsize . --exclude node_modules,.git,'*.log'
  dirsize /var --files --top 15 --min-size 100MB
  dirsize . --min-size 1MiB --json | jq '.entries[].path'
  dirsize C:\Users\me\go\pkg\mod --workers 32`,
		Args: cliutil.WrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), c, &o, args)
		},
	}
	f := cmd.Flags()
	f.IntVarP(&o.depth, "depth", "d", 1, "aggregate sizes at this depth below the root (0 = totals only)")
	f.IntVarP(&o.top, "top", "n", 10, "show the N largest entries (0 = all)")
	f.StringVar(&o.minSize, "min-size", "", "hide entries smaller than this (e.g. 512K, 10MB, 1.5GiB)")
	f.StringSliceVarP(&o.exclude, "exclude", "x", nil, "skip names or glob patterns (comma-separated or repeated)")
	f.BoolVarP(&o.files, "files", "f", false, "list the largest individual files instead of directories")
	f.IntVarP(&o.workers, "workers", "w", DefaultWorkers(), "number of concurrent directory readers")
	return cliutil.NewRoot(c, cmd)
}

func run(ctx context.Context, c *cliutil.Common, o *options, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.depth < 0 {
		return cliutil.UsageErrorf("--depth must be >= 0, got %d", o.depth)
	}
	if o.top < 0 {
		return cliutil.UsageErrorf("--top must be >= 0, got %d", o.top)
	}
	if o.workers < 1 {
		return cliutil.UsageErrorf("--workers must be >= 1, got %d", o.workers)
	}
	var minSize int64
	if o.minSize != "" {
		v, err := ui.ParseSize(o.minSize)
		if err != nil {
			return cliutil.UsageErrorf("--min-size: %v", err)
		}
		minSize = v
	}
	if err := ValidateExcludes(o.exclude); err != nil {
		return cliutil.UsageError(err)
	}
	root := "."
	if len(args) == 1 {
		root = args[0]
	}

	var scanned atomic.Int64
	stopSpinner := startSpinner(c, root, &scanned)
	res, err := Scan(ctx, root, Options{
		Depth:       o.depth,
		Workers:     o.workers,
		Exclude:     o.exclude,
		Files:       o.files,
		FileLimit:   o.top,
		MinFileSize: minSize,
		Scanned:     &scanned,
	})
	stopSpinner()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return fmt.Errorf("cannot scan %s: %s", root, reason(err))
		}
		return err
	}

	rep := BuildReport(res, ReportOptions{
		Root:         root,
		Depth:        o.depth,
		Top:          o.top,
		MinSize:      minSize,
		MinSizeLabel: strings.TrimSpace(o.minSize),
		Exclude:      o.exclude,
		Files:        o.files,
	})
	if err := c.Printer().Emit(rep, func(w io.Writer, th *ui.Theme) error { return RenderText(w, th, rep) }); err != nil {
		return err
	}
	if !c.JSON {
		return WriteSkipped(c.IO.Err, c.ErrTheme(), rep, skippedShown)
	}
	return nil
}

// startSpinner shows a live "scanning … N entries" line on an interactive
// stderr and returns a function that stops it.
func startSpinner(c *cliutil.Common, root string, scanned *atomic.Int64) func() {
	if !c.Interactive() {
		return func() {}
	}
	th := c.ErrTheme()
	label := func() string {
		return "scanning " + displayRoot(root, true) + th.Muted("  "+commas(scanned.Load())+" entries")
	}
	sp := ui.NewSpinner(c.IO.Err, th, label())
	sp.Start()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				sp.SetLabel(label())
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		sp.Stop()
	}
}
