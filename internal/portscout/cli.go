// Package portscout implements a fast concurrent TCP connect-scan port
// scanner aimed at development machines: "what is listening on localhost?"
// and "is the database up yet?".
package portscout

import (
	"context"
	"io"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// autoOpenOnlyThreshold is the port count above which only open ports are
// listed unless --open-only is given explicitly.
const autoOpenOnlyThreshold = 100

// options holds the parsed command-line flags.
type options struct {
	ports      string
	timeout    time.Duration
	workers    int
	openOnly   bool
	expectOpen string
	ipv6       bool
}

// deps are the network seams swapped out by tests.
type deps struct {
	dial   DialFunc
	lookup LookupFunc
}

// NewCommand builds the portscout command bound to the given streams.
func NewCommand(io cliutil.IO) *cobra.Command {
	return newCommand(io, deps{})
}

func newCommand(io cliutil.IO, d deps) *cobra.Command {
	c := &cliutil.Common{IO: io}
	var o options
	cmd := &cobra.Command{
		Use:   "portscout [flags] [host]",
		Short: "Fast concurrent TCP port scanner for dev machines",
		Long: `portscout runs a TCP connect scan against one host (default: localhost) using a
bounded pool of concurrent workers, and reports every port as open, closed
(connection refused) or filtered (no answer within --timeout, unreachable, or
any other connection failure), together with the usual service name and the
time the connection took. A port is reported as "error" only when this machine
itself ran out of sockets; lower --workers if that happens.

Without -p it scans a curated list of ~30 ports that development stacks
commonly use (ssh, http, postgres, mysql, redis, mongodb, vite, jupyter, ...).
-p accepts single ports and ranges separated by commas ("22,80,8000-8100"),
plus the aliases "common" (the curated list) and "all" (1-65535).

The host name is resolved once and its IPv4 address scanned (IPv6 with -6, or
when it has no IPv4 address). IPv6 literals such as ::1 are accepted.

By default every scanned port is listed when scanning at most 100 ports, and
only open ports are listed for larger scans; --open-only / --open-only=false
force either behaviour (it applies to --json results too). Ctrl+C stops the
scan immediately and prints the partial results.

--expect-open turns portscout into a readiness check for scripts and CI: the
exit code is 1 unless every listed port is open. Without -p, only the
expected ports are scanned.

Ethical use: only scan hosts you own or are explicitly authorized to test.
Scanning other people's networks may be illegal and is often treated as an
attack.

Exit codes:
  0    scan completed (and every --expect-open port is open)
  1    --expect-open was given and at least one of those ports is not open
  2    usage error (bad port list, flag value, extra arguments) or the host
       could not be resolved
  130  interrupted (Ctrl+C); partial results are printed first`,
		Example: `  portscout
  portscout db.internal -p 5432,6379
  portscout -p 1-1024,3306,8000-9000 --timeout 300ms --workers 500
  portscout 192.168.1.10 -p all --open-only
  portscout ::1 -p common --json
  portscout localhost --expect-open 5432 --timeout 2s && npm run migrate`,
		Args: cliutil.WrapArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), c, &o, d, args, cmd.Flags().Changed("open-only"))
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.ports, "ports", "p", "", `ports to scan, e.g. "22,80,8000-8100", "common" or "all" (default: curated dev ports)`)
	f.DurationVarP(&o.timeout, "timeout", "t", DefaultTimeout, "per-port connection timeout")
	f.IntVarP(&o.workers, "workers", "w", DefaultWorkers, "maximum concurrent connection attempts (1-5000)")
	f.BoolVar(&o.openOnly, "open-only", false, "list only open ports (default: on when scanning more than 100 ports)")
	f.StringVar(&o.expectOpen, "expect-open", "", "exit 1 unless all these ports are open, e.g. 5432,6379")
	f.BoolVarP(&o.ipv6, "ipv6", "6", false, "prefer the host's IPv6 address over IPv4")
	return cliutil.NewRoot(c, cmd)
}

func run(ctx context.Context, c *cliutil.Common, o *options, d deps, args []string, openOnlySet bool) error {
	if o.timeout <= 0 {
		return cliutil.UsageErrorf("--timeout must be positive, got %s", o.timeout)
	}
	if o.workers < 1 || o.workers > MaxWorkers {
		return cliutil.UsageErrorf("--workers must be between 1 and %d, got %d", MaxWorkers, o.workers)
	}

	var expect []int
	if o.expectOpen != "" {
		var err error
		if expect, err = ParsePorts(o.expectOpen); err != nil {
			return cliutil.UsageErrorf("--expect-open: %v", err)
		}
	}
	ports, err := portsToScan(o.ports, expect)
	if err != nil {
		return err
	}

	host := "localhost"
	if len(args) == 1 {
		host = args[0]
	}
	ip, err := Resolve(ctx, d.lookup, host, o.ipv6)
	if err != nil {
		return err
	}

	openOnly := o.openOnly
	if !openOnlySet {
		openOnly = len(ports) > autoOpenOnlyThreshold
	}
	rep := &Report{
		Host:      host,
		IP:        ip,
		Requested: len(ports),
		Workers:   min(o.workers, len(ports)),
		Timeout:   o.timeout,
		OpenOnly:  openOnly,
		Expect:    expect,
	}

	sc := &Scanner{Dial: d.dial, Timeout: o.timeout, Workers: o.workers}
	var progress *ui.Live
	if c.Interactive() {
		progress = ui.NewProgress(c.IO.Err, c.ErrTheme(), "scanning "+rep.HostLabel(), int64(len(ports)))
		sc.OnResult = func(Result) { progress.Add(1) }
		progress.Start()
	}
	start := time.Now()
	results, scanErr := sc.Scan(ctx, ip, ports)
	rep.Elapsed = time.Since(start)
	if progress != nil {
		progress.Stop()
	}
	rep.Results = results
	rep.Interrupted = scanErr != nil

	if err := c.Printer().Emit(rep.JSON(), func(w io.Writer, th *ui.Theme) error {
		return rep.WriteText(w, th)
	}); err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	if len(rep.Missing()) > 0 {
		return cliutil.ErrCheckFailed
	}
	return nil
}

// portsToScan works out the port list from -p and --expect-open: -p (plus
// any expected ports), else only the expected ports, else the curated list.
func portsToScan(spec string, expect []int) ([]int, error) {
	if spec == "" {
		if len(expect) > 0 {
			return expect, nil
		}
		return CommonPorts(), nil
	}
	ports, err := ParsePorts(spec)
	if err != nil {
		return nil, cliutil.UsageErrorf("-p: %v", err)
	}
	if len(expect) == 0 {
		return ports, nil
	}
	ports = append(ports, expect...)
	slices.Sort(ports)
	return slices.Compact(ports), nil
}
