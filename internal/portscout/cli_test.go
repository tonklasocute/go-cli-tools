package portscout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
)

type cliResult struct {
	code     int
	out, err string
}

func runCLI(t *testing.T, ctx context.Context, d deps, args ...string) cliResult {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := newCommand(cliutil.IO{In: strings.NewReader(""), Out: &out, Err: &errb}, d)
	code := cliutil.Run(ctx, cmd, args)
	return cliResult{code: code, out: out.String(), err: errb.String()}
}

// fakeDeps returns deps whose network answers from states and whose resolver
// maps every name to 127.0.0.1.
func fakeDeps(states map[int]State) deps {
	fn := &fakeNet{states: states}
	return deps{dial: fn.dial, lookup: fakeLookup("127.0.0.1")}
}

func TestCLIDefaultCuratedList(t *testing.T) {
	r := runCLI(t, context.Background(), fakeDeps(map[int]State{5432: StateOpen, 6379: StateOpen}), "--no-color")
	if r.code != cliutil.ExitOK {
		t.Fatalf("exit %d, stderr %s", r.code, r.err)
	}
	n := len(CommonPorts())
	for _, want := range []string{
		fmt.Sprintf("Scanning localhost (127.0.0.1) — %d ports, %d workers, timeout 500ms", n, n),
		"PORT", "STATE", "SERVICE", "LATENCY",
		"postgres", "redis", "mongodb",
		fmt.Sprintf("2 open, %d closed in", n-2),
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("missing %q in output:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out, "\x1b[") {
		t.Error("ANSI codes with --no-color")
	}
	if r.err != "" {
		t.Errorf("unexpected stderr (progress must be off when not a TTY): %q", r.err)
	}
}

func TestCLIJSON(t *testing.T) {
	r := runCLI(t, context.Background(), fakeDeps(map[int]State{22: StateOpen, 81: StateFiltered}),
		"db.local", "-p", "22,80-81", "--timeout", "30ms", "--json")
	if r.code != cliutil.ExitOK {
		t.Fatalf("exit %d, stderr %s", r.code, r.err)
	}
	var j JSONReport
	if err := json.Unmarshal([]byte(r.out), &j); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.out)
	}
	if j.Host != "db.local" || j.IP != "127.0.0.1" || j.PortsScanned != 3 || j.Open != 1 ||
		j.Closed != 1 || j.Filtered != 1 || j.Workers != 3 || j.TimeoutMS != 30 || j.Interrupted {
		t.Fatalf("unexpected JSON: %+v", j)
	}
	want := []struct {
		port  int
		state State
	}{{22, StateOpen}, {80, StateClosed}, {81, StateFiltered}}
	for i, w := range want {
		if j.Results[i].Port != w.port || j.Results[i].State != w.state {
			t.Errorf("results[%d] = %+v, want %+v", i, j.Results[i], w)
		}
	}
	if strings.Contains(r.out, "\x1b[") || strings.Contains(r.out, `"expect"`) {
		t.Errorf("unexpected content in JSON:\n%s", r.out)
	}
}

func TestCLIAutoOpenOnly(t *testing.T) {
	d := fakeDeps(map[int]State{150: StateOpen})
	r := runCLI(t, context.Background(), d, "-p", "1-200", "--no-color")
	if r.code != 0 || !strings.Contains(r.out, "(199 not-open ports not listed") || strings.Contains(r.out, "\n   80  ") {
		t.Fatalf("auto open-only not applied (exit %d):\n%s", r.code, r.out)
	}
	r = runCLI(t, context.Background(), d, "-p", "1-200", "--open-only=false", "--no-color")
	if !strings.Contains(r.out, "http") || strings.Contains(r.out, "not listed") {
		t.Fatalf("--open-only=false should list everything:\n%s", r.out)
	}
	r = runCLI(t, context.Background(), d, "-p", "149-151", "--open-only", "--json")
	var j JSONReport
	if err := json.Unmarshal([]byte(r.out), &j); err != nil || len(j.Results) != 1 || !j.OpenOnly || j.PortsScanned != 3 {
		t.Fatalf("--open-only JSON: %v %+v", err, j)
	}
}

func TestCLIExpectOpen(t *testing.T) {
	d := fakeDeps(map[int]State{5432: StateOpen})
	r := runCLI(t, context.Background(), d, "--expect-open", "5432", "--no-color")
	if r.code != cliutil.ExitOK || !strings.Contains(r.out, "1 port,") || !strings.Contains(r.out, "✔ expected ports open: 5432") {
		t.Fatalf("exit %d:\n%s%s", r.code, r.out, r.err)
	}
	r = runCLI(t, context.Background(), d, "--expect-open", "5432,6379", "-p", "22", "--no-color")
	if r.code != cliutil.ExitFail || !strings.Contains(r.out, "3 ports,") || !strings.Contains(r.out, "✘ expected open but not: 6379 (closed)") {
		t.Fatalf("exit %d:\n%s%s", r.code, r.out, r.err)
	}
	if r.err != "" {
		t.Errorf("check failure should not print an error: %q", r.err)
	}
	r = runCLI(t, context.Background(), d, "--expect-open", "6379", "--json")
	var j JSONReport
	if err := json.Unmarshal([]byte(r.out), &j); err != nil || r.code != 1 || j.Expect == nil || j.Expect.OK {
		t.Fatalf("exit %d, %v, %+v", r.code, err, j.Expect)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-p", "80-70"}, "start 80 is greater than end 70"},
		{[]string{"-p", "0"}, "out of range"},
		{[]string{"-p", "http"}, "not a number"},
		{[]string{"--expect-open", "x"}, "--expect-open"},
		{[]string{"--workers", "0"}, "--workers must be between 1 and 5000"},
		{[]string{"--workers", "100000"}, "--workers must be between"},
		{[]string{"--timeout", "0s"}, "--timeout must be positive"},
		{[]string{"--timeout", "fast"}, "invalid argument"},
		{[]string{"a", "b"}, "accepts at most 1 arg"},
		{[]string{"--bogus"}, "unknown flag"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := runCLI(t, context.Background(), fakeDeps(nil), tt.args...)
			if r.code != cliutil.ExitUsage || !strings.Contains(r.err, tt.want) || !strings.Contains(r.err, "--help") {
				t.Fatalf("exit %d, stderr %q; want 2 and %q", r.code, r.err, tt.want)
			}
		})
	}
}

func TestCLIResolveError(t *testing.T) {
	d := deps{dial: (&fakeNet{}).dial, lookup: fakeLookup()}
	r := runCLI(t, context.Background(), d, "nope.invalid")
	if r.code != cliutil.ExitUsage || !strings.Contains(r.err, `cannot resolve host "nope.invalid"`) || r.out != "" {
		t.Fatalf("exit %d, out %q, err %q", r.code, r.out, r.err)
	}
}

func TestCLIInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fn := &fakeNet{
		states: map[int]State{2: StateOpen, 3: StateFiltered},
		before: func(_ context.Context, port int) {
			if port == 3 {
				cancel()
			}
		},
	}
	d := deps{dial: fn.dial, lookup: fakeLookup("127.0.0.1")}
	r := runCLI(t, ctx, d, "-p", "1-10", "-w", "1", "--no-color")
	if r.code != cliutil.ExitInterrupted {
		t.Fatalf("exit %d, want 130", r.code)
	}
	if !strings.Contains(r.out, "1 open, 1 closed in") || !strings.Contains(r.out, "(interrupted: 2 of 10 ports scanned)") {
		t.Fatalf("partial results not printed:\n%s", r.out)
	}
	if !strings.Contains(r.err, "interrupted") {
		t.Fatalf("stderr %q", r.err)
	}
}

func TestCLIHelpAndVersion(t *testing.T) {
	r := runCLI(t, context.Background(), fakeDeps(nil), "--help")
	if r.code != 0 {
		t.Fatalf("exit %d", r.code)
	}
	for _, want := range []string{"Examples:", "portscout db.internal -p 5432,6379", "authorized", "Exit codes:", "130", "--expect-open", "--open-only"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
	r = runCLI(t, context.Background(), fakeDeps(nil), "--version")
	if r.code != 0 || !strings.Contains(r.out, "portscout") {
		t.Fatalf("--version: exit %d, %q", r.code, r.out)
	}
}

func TestCLIRealListeners(t *testing.T) {
	open := listenLocal(t, 2)
	closed := closedLocalPort(t)
	spec := fmt.Sprintf("%d,%d,%d", open[0], open[1], closed)
	r := runCLI(t, context.Background(), deps{}, "127.0.0.1", "-p", spec, "--timeout", "5s", "--json")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	var j JSONReport
	if err := json.Unmarshal([]byte(r.out), &j); err != nil {
		t.Fatal(err)
	}
	if j.Open != 2 || j.PortsScanned != 3 || j.Host != "127.0.0.1" {
		t.Fatalf("unexpected report: %+v", j)
	}
	for _, res := range j.Results {
		if (res.State == StateOpen) != (res.Port != closed) {
			t.Errorf("port %d: %s", res.Port, res.State)
		}
	}
	r = runCLI(t, context.Background(), deps{}, "127.0.0.1", "--expect-open", strconv.Itoa(open[0]), "--timeout", "5s")
	if r.code != 0 {
		t.Fatalf("expect-open on a listener: exit %d\n%s", r.code, r.out)
	}
}

func TestNewCommand(t *testing.T) {
	cmd := NewCommand(cliutil.IO{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	if cmd.Name() != "portscout" || cmd.Flags().Lookup("ports").Shorthand != "p" {
		t.Fatalf("unexpected command %q", cmd.Name())
	}
}
