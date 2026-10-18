package portscout

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tonklasocute/go-cli-tools/internal/ui"
)

func sampleReport() *Report {
	return &Report{
		Host:      "localhost",
		IP:        "127.0.0.1",
		Requested: 4,
		Workers:   4,
		Timeout:   500 * time.Millisecond,
		Elapsed:   512 * time.Millisecond,
		Results: []Result{
			{Port: 22, State: StateClosed, Service: "ssh", Latency: 800 * time.Microsecond},
			{Port: 5432, State: StateOpen, Service: "postgres", Latency: 1234567 * time.Nanosecond},
			{Port: 9999, State: StateFiltered, Latency: 500 * time.Millisecond},
			{Port: 27017, State: StateOpen, Service: "mongodb", Latency: 2 * time.Millisecond},
		},
	}
}

func render(t *testing.T, r *Report, color bool) string {
	t.Helper()
	var b bytes.Buffer
	if err := r.WriteText(&b, ui.NewTheme(color)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWriteText(t *testing.T) {
	got := render(t, sampleReport(), false)
	want := `Scanning localhost (127.0.0.1) — 4 ports, 4 workers, timeout 500ms

 PORT  STATE     SERVICE   LATENCY
─────  ────────  ────────  ───────
   22  closed    ssh         800µs
 5432  open      postgres    1.2ms
 9999  filtered  -               -
27017  open      mongodb     2.0ms

2 open, 1 closed, 1 filtered in 512ms
`
	if got != want {
		t.Fatalf("text output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestWriteTextColor(t *testing.T) {
	r := sampleReport()
	r.Results[1].Latency = 0 // coarse clocks can measure a loopback connect as zero
	got := render(t, r, true)
	if !strings.Contains(got, "\x1b[") {
		t.Fatal("expected ANSI colors")
	}
	if !strings.Contains(got, "<1ms") {
		t.Fatalf("zero latency should render as <1ms:\n%s", got)
	}
}

func TestWriteTextOpenOnlyAndExpect(t *testing.T) {
	r := sampleReport()
	r.OpenOnly = true
	r.Expect = []int{5432, 27017}
	got := render(t, r, false)
	for _, want := range []string{"5432", "27017", "(2 not-open ports not listed", "✔ expected ports open: 5432, 27017"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "  ssh") {
		t.Errorf("closed port listed with OpenOnly:\n%s", got)
	}
}

func TestWriteTextExpectFailInterrupted(t *testing.T) {
	r := sampleReport()
	r.Requested = 6
	r.Interrupted = true
	r.Expect = []int{22, 5432, 6379}
	got := render(t, r, false)
	for _, want := range []string{
		"(interrupted: 4 of 6 ports scanned)",
		"✘ expected open but not: 22 (closed), 6379 (not scanned)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if m := r.Missing(); !slices.Equal(m, []int{22, 6379}) {
		t.Errorf("Missing() = %v", m)
	}
}

func TestWriteTextEmpty(t *testing.T) {
	r := sampleReport()
	r.OpenOnly = true
	r.Results = r.Results[:1]
	if got := render(t, r, false); !strings.Contains(got, "no open ports found") {
		t.Errorf("got:\n%s", got)
	}
	r.Results = nil
	if got := render(t, r, false); !strings.Contains(got, "no ports scanned") {
		t.Errorf("got:\n%s", got)
	}
}

func TestHostLabel(t *testing.T) {
	tests := []struct{ host, ip, want string }{
		{"localhost", "127.0.0.1", "localhost (127.0.0.1)"},
		{"127.0.0.1", "127.0.0.1", "127.0.0.1"},
		{"[::1]", "::1", "::1"},
		{"", "10.0.0.1", "10.0.0.1"},
	}
	for _, tt := range tests {
		if got := (&Report{Host: tt.host, IP: tt.ip}).HostLabel(); got != tt.want {
			t.Errorf("HostLabel(%q, %q) = %q, want %q", tt.host, tt.ip, got, tt.want)
		}
	}
}

func TestJSONReport(t *testing.T) {
	r := sampleReport()
	j := r.JSON()
	if j.Host != "localhost" || j.IP != "127.0.0.1" || j.PortsScanned != 4 || j.PortsTotal != 4 ||
		j.Open != 2 || j.Closed != 1 || j.Filtered != 1 || j.ElapsedMS != 512 || j.TimeoutMS != 500 {
		t.Fatalf("unexpected JSON header: %+v", j)
	}
	if len(j.Results) != 4 || j.Results[1].LatencyMS != 1.235 || j.Results[1].Service != "postgres" {
		t.Fatalf("unexpected results: %+v", j.Results)
	}
	if j.Expect != nil {
		t.Fatal("expect must be omitted when unused")
	}
	r.OpenOnly = true
	r.Expect = []int{6379}
	j = r.JSON()
	if len(j.Results) != 2 || j.Expect == nil || j.Expect.OK || !slices.Equal(j.Expect.Missing, []int{6379}) {
		t.Fatalf("unexpected open-only/expect JSON: %+v %+v", j.Results, j.Expect)
	}
	r.Expect = []int{5432}
	if e := r.JSON().Expect; !e.OK || e.Missing == nil || len(e.Missing) != 0 {
		t.Fatalf("expect = %+v, want ok with empty (non-nil) missing", e)
	}
}

func TestWriteTextLocalErrors(t *testing.T) {
	r := sampleReport()
	r.Results = append(r.Results, Result{Port: 30000, State: StateError, Err: "connect: too many open files"})
	got := render(t, r, false)
	for _, want := range []string{
		"30000  error",
		"2 open, 1 closed, 1 filtered, 1 error in 512ms",
		"▲ 1 port could not be probed because this machine ran out of sockets (connect: too many open files); retry with fewer --workers",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	j := r.JSON()
	if j.Errors != 1 || j.Results[4].Error != "connect: too many open files" || j.Results[0].Error != "" {
		t.Fatalf("JSON errors=%d results=%+v", j.Errors, j.Results)
	}
	if !strings.Contains(render(t, r, true), "\x1b[") {
		t.Fatal("expected color")
	}
}
