package portscout

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

// Report is the outcome of a whole scan, ready to be rendered.
type Report struct {
	// Host is the host as the user typed it; IP is what was scanned.
	Host string
	IP   string
	// Requested is the number of ports that were going to be scanned.
	Requested int
	Workers   int
	Timeout   time.Duration
	Elapsed   time.Duration
	// Results holds every finished port, sorted by port.
	Results []Result
	// Interrupted is set when the scan was cancelled before finishing.
	Interrupted bool
	// OpenOnly limits the listed results to open ports.
	OpenOnly bool
	// Expect lists the ports given with --expect-open (nil when unused).
	Expect []int
}

// Counts returns how many finished ports are in each state.
func (r *Report) Counts() (open, closed, filtered, errored int) {
	for _, res := range r.Results {
		switch res.State {
		case StateOpen:
			open++
		case StateClosed:
			closed++
		case StateError:
			errored++
		default:
			filtered++
		}
	}
	return open, closed, filtered, errored
}

// Missing returns the expected ports that are not open, including any that
// were never scanned because the scan was interrupted. It is never nil.
func (r *Report) Missing() []int {
	open := make(map[int]bool, len(r.Results))
	for _, res := range r.Results {
		if res.State == StateOpen {
			open[res.Port] = true
		}
	}
	missing := []int{}
	for _, p := range r.Expect {
		if !open[p] {
			missing = append(missing, p)
		}
	}
	return missing
}

// Shown returns the results that should be listed, honoring OpenOnly.
func (r *Report) Shown() []Result {
	if !r.OpenOnly {
		return r.Results
	}
	out := make([]Result, 0, len(r.Results))
	for _, res := range r.Results {
		if res.State == StateOpen {
			out = append(out, res)
		}
	}
	return out
}

// HostLabel renders "localhost (127.0.0.1)", or just the IP when the user
// typed an IP literal.
func (r *Report) HostLabel() string {
	h := strings.Trim(r.Host, "[]")
	if h == "" || h == r.IP {
		return r.IP
	}
	return fmt.Sprintf("%s (%s)", h, r.IP)
}

// JSONReport is the stable --json document.
type JSONReport struct {
	Host         string       `json:"host"`
	IP           string       `json:"ip"`
	PortsScanned int          `json:"ports_scanned"`
	PortsTotal   int          `json:"ports_total"`
	Open         int          `json:"open"`
	Closed       int          `json:"closed"`
	Filtered     int          `json:"filtered"`
	Errors       int          `json:"errors"`
	Workers      int          `json:"workers"`
	TimeoutMS    int64        `json:"timeout_ms"`
	ElapsedMS    int64        `json:"elapsed_ms"`
	Interrupted  bool         `json:"interrupted"`
	OpenOnly     bool         `json:"open_only"`
	Results      []JSONResult `json:"results"`
	Expect       *JSONExpect  `json:"expect,omitempty"`
}

// JSONResult is one port in the --json document.
type JSONResult struct {
	Port      int     `json:"port"`
	State     State   `json:"state"`
	Service   string  `json:"service"`
	LatencyMS float64 `json:"latency_ms"`
	// Error explains filtered and error states ("timeout" or the OS error).
	Error string `json:"error,omitempty"`
}

// JSONExpect reports the --expect-open check.
type JSONExpect struct {
	Ports   []int `json:"ports"`
	Missing []int `json:"missing"`
	OK      bool  `json:"ok"`
}

// JSON converts the report into its --json document.
func (r *Report) JSON() JSONReport {
	open, closed, filtered, errored := r.Counts()
	shown := r.Shown()
	j := JSONReport{
		Host:         r.Host,
		IP:           r.IP,
		PortsScanned: len(r.Results),
		PortsTotal:   r.Requested,
		Open:         open,
		Closed:       closed,
		Filtered:     filtered,
		Errors:       errored,
		Workers:      r.Workers,
		TimeoutMS:    r.Timeout.Milliseconds(),
		ElapsedMS:    r.Elapsed.Milliseconds(),
		Interrupted:  r.Interrupted,
		OpenOnly:     r.OpenOnly,
		Results:      make([]JSONResult, 0, len(shown)),
	}
	for _, res := range shown {
		j.Results = append(j.Results, JSONResult{
			Port:      res.Port,
			State:     res.State,
			Service:   res.Service,
			LatencyMS: millis(res.Latency),
			Error:     res.Err,
		})
	}
	if r.Expect != nil {
		missing := r.Missing()
		j.Expect = &JSONExpect{Ports: r.Expect, Missing: missing, OK: len(missing) == 0}
	}
	return j
}

// millis converts d to milliseconds with microsecond precision.
func millis(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Microsecond)) / 1000
}

// Header returns the one-line description printed before the results.
func (r *Report) Header(th *ui.Theme) string {
	return "Scanning " + th.Bold(r.HostLabel()) + th.Muted(fmt.Sprintf(" — %s, %s, timeout %s",
		ui.Plural(r.Requested, "port", "ports"),
		ui.Plural(r.Workers, "worker", "workers"),
		ui.HumanDuration(r.Timeout)))
}

// WriteText renders the human-readable report.
func (r *Report) WriteText(w io.Writer, th *ui.Theme) error {
	var b strings.Builder
	b.WriteString(r.Header(th) + "\n\n")

	shown := r.Shown()
	if len(shown) == 0 {
		msg := "no ports scanned"
		if len(r.Results) > 0 {
			msg = "no open ports found"
		}
		b.WriteString(th.Muted(msg) + "\n")
	} else {
		t := ui.NewTable("PORT", "STATE", "SERVICE", "LATENCY")
		t.RightAlign[0] = true
		t.RightAlign[3] = true
		for _, res := range shown {
			t.AddRow(strconv.Itoa(res.Port), paintState(th, res.State), serviceCell(th, res.Service), latencyCell(th, res))
		}
		if err := t.Render(&b, th); err != nil {
			return err
		}
	}

	b.WriteString("\n" + r.summary(th) + "\n")
	if hidden := len(r.Results) - len(shown); hidden > 0 {
		b.WriteString(th.Muted(fmt.Sprintf("(%d not-open ports not listed; use --open-only=false to list all)", hidden)) + "\n")
	}
	if warn := r.errorWarning(th); warn != "" {
		b.WriteString(warn + "\n")
	}
	if r.Expect != nil {
		b.WriteString(r.expectLine(th) + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func (r *Report) summary(th *ui.Theme) string {
	open, closed, filtered, errored := r.Counts()
	parts := []string{th.Success(fmt.Sprintf("%d open", open)), fmt.Sprintf("%d closed", closed)}
	if filtered > 0 {
		parts = append(parts, th.Warn(fmt.Sprintf("%d filtered", filtered)))
	}
	if errored > 0 {
		parts = append(parts, th.Error(ui.Plural(errored, "error", "errors")))
	}
	s := strings.Join(parts, ", ") + " in " + ui.HumanDuration(r.Elapsed)
	if r.Interrupted {
		s += th.Warn(fmt.Sprintf(" (interrupted: %d of %d ports scanned)", len(r.Results), r.Requested))
	}
	return s
}

// errorWarning explains local probe failures, or returns "" when none.
func (r *Report) errorWarning(th *ui.Theme) string {
	for _, res := range r.Results {
		if res.State == StateError {
			_, _, _, n := r.Counts()
			return th.Badge(ui.RoleWarn, fmt.Sprintf(
				"%s could not be probed because this machine ran out of sockets (%s); retry with fewer --workers, or wait for sockets in TIME_WAIT to expire",
				ui.Plural(n, "port", "ports"), res.Err))
		}
	}
	return ""
}

func (r *Report) expectLine(th *ui.Theme) string {
	missing := r.Missing()
	if len(missing) == 0 {
		return th.Badge(ui.RoleSuccess, "expected ports open: "+joinPorts(r.Expect))
	}
	state := make(map[int]State, len(r.Results))
	for _, res := range r.Results {
		state[res.Port] = res.State
	}
	items := make([]string, len(missing))
	for i, p := range missing {
		st := string(state[p])
		if st == "" {
			st = "not scanned"
		}
		items[i] = fmt.Sprintf("%d (%s)", p, st)
	}
	return th.Badge(ui.RoleError, "expected open but not: "+strings.Join(items, ", "))
}

func joinPorts(ports []int) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ", ")
}

func paintState(th *ui.Theme, s State) string {
	switch s {
	case StateOpen:
		return th.Success(string(s))
	case StateClosed:
		return th.Muted(string(s))
	case StateError:
		return th.Error(string(s))
	default:
		return th.Warn(string(s))
	}
}

func serviceCell(th *ui.Theme, svc string) string {
	if svc == "" {
		return th.Muted("-")
	}
	return svc
}

func latencyCell(th *ui.Theme, res Result) string {
	if res.State == StateFiltered || res.State == StateError {
		return th.Muted("-")
	}
	if res.Latency <= 0 {
		// Some platforms (notably Windows) have a ~0.5ms clock granularity,
		// so a very fast loopback connect can measure as exactly zero.
		return "<1ms"
	}
	return ui.HumanDuration(res.Latency)
}
