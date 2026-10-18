package reqping

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/ui"
	"github.com/tonklasocute/go-cli-tools/internal/version"
)

type options struct {
	files       []string
	timeout     time.Duration
	status      string
	contains    string
	follow      bool
	noFollow    bool
	followSet   bool
	headers     []string
	method      string
	insecure    bool
	count       int
	watch       time.Duration
	concurrency int

	// now overrides the clock (tests only).
	now func() time.Time
}

const long = `reqping checks that HTTP(S) endpoints are up: it requests every target
concurrently, verifies the status code (and optionally a body substring),
and reports latency and how many days remain on each TLS certificate.

Targets come from the command line and/or a targets file (-f FILE, or -f -
for stdin; stdin is also read when no URLs are given and it is piped). Each
line of a targets file is

    [name] url [expected_status]

separated by whitespace; blank lines and # comments are ignored. The name
defaults to the URL host. expected_status is a code (200), a class (2xx) or
a comma list (200,204,3xx). URLs must start with http:// or https://.

Health rules: a target is unhealthy when the request fails or times out,
the status is not expected, or --contains is not found in the first 1 MiB
of the body. With --count N every one of the N requests must succeed. The
default expectation is any 2xx when redirects are followed (the default),
or any 2xx/3xx with --no-follow. --status sets the default for targets that
do not specify one. A certificate expiring in fewer than 14 days is a
warning (▲): the target stays healthy but is flagged. An expired certificate
fails the TLS handshake; with --insecure the request succeeds but the target
is marked unhealthy.

--watch re-runs the checks every interval until Ctrl+C. On a terminal the
table is redrawn in place; otherwise each round is printed after a
timestamp line, and with --json each round is one JSON document per line
(NDJSON).

Exit codes:
  0    every target is healthy
  1    at least one target is unhealthy
  2    usage error (bad URL, bad flag value, unreadable or malformed targets file)
  130  interrupted (Ctrl+C), including ending --watch`

// NewCommand builds the reqping command bound to the given streams.
func NewCommand(io cliutil.IO) *cobra.Command {
	c := &cliutil.Common{IO: io}
	var o options
	cmd := &cobra.Command{
		Use:   "reqping [flags] [URL...]",
		Short: "HTTP health checker with latency, status and TLS expiry report",
		Long:  long,
		Example: "  reqping https://go.dev https://pkg.go.dev\n" +
			"  reqping -f docs/samples/targets.txt\n" +
			"  reqping --count 5 --contains \"Go\" https://go.dev\n" +
			"  reqping -H \"Authorization: Bearer $TOKEN\" --status 204 https://api.example.com/health\n" +
			"  reqping --watch 5s -f targets.txt\n" +
			"  cat targets.txt | reqping --json > report.json",
		Args: cliutil.WrapArgs(cobra.ArbitraryArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.followSet = cmd.Flags().Changed("follow")
			return run(cmd.Context(), c, &o, args)
		},
	}
	f := cmd.Flags()
	f.StringArrayVarP(&o.files, "file", "f", nil, "read targets from `FILE` (\"-\" for stdin); repeatable")
	f.DurationVar(&o.timeout, "timeout", 10*time.Second, "timeout per request")
	f.StringVar(&o.status, "status", "", "default expected status, e.g. 200, 2xx or 200,204 (default 2xx; 2xx,3xx with --no-follow)")
	f.StringVar(&o.contains, "contains", "", "require the response body (first 1 MiB) to contain `TEXT`")
	f.BoolVar(&o.follow, "follow", true, "follow redirects")
	f.BoolVar(&o.noFollow, "no-follow", false, "do not follow redirects")
	f.StringArrayVarP(&o.headers, "header", "H", nil, "add a request header `\"Key: Value\"`; repeatable")
	f.StringVar(&o.method, "method", http.MethodGet, "HTTP method: GET or HEAD")
	f.BoolVar(&o.insecure, "insecure", false, "skip TLS certificate verification (prints a warning)")
	f.IntVar(&o.count, "count", 1, "requests per target; shows min/avg/p95/max latency when > 1")
	f.DurationVar(&o.watch, "watch", 0, "re-check every `INTERVAL` (e.g. 5s) until Ctrl+C")
	f.IntVar(&o.concurrency, "concurrency", 10, "number of targets checked at once")
	return cliutil.NewRoot(c, cmd)
}

// checkerFromOptions validates the flags and builds a Checker plus the
// default expectation for targets that do not set one.
func checkerFromOptions(o *options) (*Checker, Expect, error) {
	method := strings.ToUpper(strings.TrimSpace(o.method))
	if method != http.MethodGet && method != http.MethodHead {
		return nil, Expect{}, cliutil.UsageErrorf("invalid --method %q: use GET or HEAD", o.method)
	}
	if o.timeout <= 0 {
		return nil, Expect{}, cliutil.UsageErrorf("invalid --timeout %s: must be positive", o.timeout)
	}
	if o.count < 1 {
		return nil, Expect{}, cliutil.UsageErrorf("invalid --count %d: must be at least 1", o.count)
	}
	if o.concurrency < 1 {
		return nil, Expect{}, cliutil.UsageErrorf("invalid --concurrency %d: must be at least 1", o.concurrency)
	}
	if o.watch < 0 {
		return nil, Expect{}, cliutil.UsageErrorf("invalid --watch %s: must be positive", o.watch)
	}
	if o.followSet && o.follow && o.noFollow {
		return nil, Expect{}, cliutil.UsageErrorf("--follow and --no-follow are mutually exclusive")
	}
	if o.contains != "" && method == http.MethodHead {
		return nil, Expect{}, cliutil.UsageErrorf("--contains needs a response body; it cannot be used with --method HEAD")
	}
	follow := o.follow && !o.noFollow
	def := DefaultExpect(follow)
	if o.status != "" {
		e, err := ParseExpect(o.status)
		if err != nil {
			return nil, Expect{}, cliutil.UsageErrorf("invalid --status: %v", err)
		}
		def = e
	}
	hdr, err := parseHeaders(o.headers)
	if err != nil {
		return nil, Expect{}, cliutil.UsageError(err)
	}
	return &Checker{
		Transport:   NewTransport(o.insecure, nil),
		Method:      method,
		Header:      hdr,
		UserAgent:   "reqping/" + version.Get().Version,
		Timeout:     o.timeout,
		Contains:    o.contains,
		NoFollow:    !follow,
		Count:       o.count,
		Concurrency: o.concurrency,
		Now:         o.now,
	}, def, nil
}

// parseHeaders validates "Key: Value" header flags.
func parseHeaders(raw []string) (http.Header, error) {
	h := http.Header{}
	for _, s := range raw {
		k, v, ok := strings.Cut(s, ":")
		k = strings.TrimSpace(k)
		if !ok || !validHeaderName(k) {
			return nil, fmt.Errorf("invalid header %q: want \"Key: Value\"", s)
		}
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("invalid header %q: value must not contain newlines", s)
		}
		h.Add(k, strings.TrimSpace(v))
	}
	return h, nil
}

// validHeaderName reports whether s is a valid RFC 9110 field name (token).
func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

// loadTargets collects targets from args, then from each -f source in order.
func loadTargets(c *cliutil.Common, o *options, args []string, def Expect) ([]Target, error) {
	var targets []Target
	for _, a := range args {
		t, err := NewTarget("", a, def)
		if err != nil {
			return nil, cliutil.UsageError(err)
		}
		targets = append(targets, t)
	}
	files := o.files
	if len(args) == 0 && len(files) == 0 {
		if !c.IO.StdinIsPiped() {
			return nil, cliutil.UsageErrorf("no targets: pass URLs or -f FILE")
		}
		files = []string{"-"}
	}
	stdinUsed := false
	for _, name := range files {
		var r io.Reader
		source := name
		if name == "-" {
			if stdinUsed {
				return nil, cliutil.UsageErrorf("stdin (-f -) can only be read once")
			}
			stdinUsed = true
			r, source = c.IO.In, "<stdin>"
			if r == nil {
				r = strings.NewReader("")
			}
		}
		ts, err := readTargets(r, name, source, def)
		if err != nil {
			return nil, err
		}
		if len(ts) == 0 {
			return nil, cliutil.UsageErrorf("no targets found in %s", source)
		}
		targets = append(targets, ts...)
	}
	return targets, nil
}

// readTargets parses r, or opens the file name when r is nil.
func readTargets(r io.Reader, name, source string, def Expect) ([]Target, error) {
	if r == nil {
		f, err := os.Open(name)
		if err != nil {
			return nil, cliutil.UsageErrorf("cannot read targets file: %v", err)
		}
		defer f.Close()
		r = f
	}
	ts, err := ParseTargets(r, source, def)
	if err != nil {
		return nil, cliutil.UsageError(err)
	}
	return ts, nil
}

func run(ctx context.Context, c *cliutil.Common, o *options, args []string) error {
	checker, def, err := checkerFromOptions(o)
	if err != nil {
		return err
	}
	targets, err := loadTargets(c, o, args, def)
	if err != nil {
		return err
	}
	if o.insecure {
		fmt.Fprintf(c.IO.Err, "reqping: %s %s\n", c.ErrTheme().Warn("warning:"),
			"--insecure: TLS certificate verification is disabled")
	}
	th := c.Theme()
	if o.watch > 0 {
		return runWatch(ctx, c, o, checker, targets)
	}

	var spin *ui.Live
	if c.Interactive() {
		spin = ui.NewSpinner(c.IO.Err, c.ErrTheme(), "checking "+ui.Plural(len(targets), "target", "targets")+"…")
		spin.Start()
	}
	rep := checker.CheckAll(ctx, targets)
	if spin != nil {
		spin.Stop()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Printer().Emit(rep.ToJSON(), func(w io.Writer, _ *ui.Theme) error {
		return RenderText(w, th, &rep, o.count > 1)
	}); err != nil {
		return err
	}
	if !rep.Healthy() {
		return cliutil.ErrCheckFailed
	}
	return nil
}

// runWatch re-checks the targets every o.watch until ctx is cancelled.
func runWatch(ctx context.Context, c *cliutil.Common, o *options, checker *Checker, targets []Target) error {
	th := c.Theme()
	tty := ui.IsTerminal(c.IO.Out)
	enc := json.NewEncoder(c.IO.Out)
	enc.SetEscapeHTML(false)
	rounds := 0
	return Watch(ctx, o.watch, func(ctx context.Context) error {
		rep := checker.CheckAll(ctx, targets)
		if err := ctx.Err(); err != nil {
			return err
		}
		rounds++
		if c.JSON {
			return enc.Encode(rep.ToJSON())
		}
		ts := rep.CheckedAt.Local()
		var b strings.Builder
		if tty {
			b.WriteString("\x1b[H\x1b[2J")
			b.WriteString(th.Muted(fmt.Sprintf("Every %s · last check %s · Ctrl+C to quit",
				formatInterval(o.watch), ts.Format(time.TimeOnly))) + "\n\n")
		} else {
			if rounds > 1 {
				b.WriteString("\n")
			}
			b.WriteString(th.Muted("── "+ts.Format(time.DateTime)+" ──") + "\n")
		}
		if err := RenderText(&b, th, &rep, o.count > 1); err != nil {
			return err
		}
		_, err := io.WriteString(c.IO.Out, b.String())
		return err
	})
}
