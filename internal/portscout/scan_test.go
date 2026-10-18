package portscout

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScanFakeStates(t *testing.T) {
	fn := &fakeNet{states: map[int]State{22: StateOpen, 5432: StateOpen, 9999: StateFiltered}}
	var seen atomic.Int64
	sc := &Scanner{Dial: fn.dial, Timeout: 20 * time.Millisecond, Workers: 4, OnResult: func(Result) { seen.Add(1) }}
	ports := []int{9999, 22, 80, 5432, 6379}
	got, err := sc.Scan(context.Background(), "192.0.2.1", ports)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		port    int
		state   State
		service string
	}{
		{22, StateOpen, "ssh"}, {80, StateClosed, "http"}, {5432, StateOpen, "postgres"},
		{6379, StateClosed, "redis"}, {9999, StateFiltered, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Port != w.port || g.State != w.state || g.Service != w.service {
			t.Errorf("result %d = %+v, want %+v", i, g, w)
		}
	}
	if got[4].Err != "timeout" || got[0].Err != "" || got[1].Err != "" {
		t.Errorf("Err fields: filtered=%q open=%q closed=%q", got[4].Err, got[0].Err, got[1].Err)
	}
	if got[4].Latency < 20*time.Millisecond {
		t.Errorf("filtered latency %v should be at least the timeout", got[4].Latency)
	}
	if seen.Load() != int64(len(ports)) {
		t.Errorf("OnResult called %d times, want %d", seen.Load(), len(ports))
	}
}

func TestScanEmpty(t *testing.T) {
	got, err := (&Scanner{}).Scan(context.Background(), "127.0.0.1", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestScanDefaults(t *testing.T) {
	var addrs sync.Map
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		addrs.Store(address, network)
		return nil, refusedError()
	}
	got, err := (&Scanner{Dial: dial, Workers: -3, Timeout: -1}).Scan(context.Background(), "::1", []int{1, 2})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if nw, ok := addrs.Load("[::1]:1"); !ok || nw != "tcp" {
		t.Fatalf("IPv6 address not bracketed via JoinHostPort; dialled %v", nw)
	}
}

// TestScanWorkerBound checks that exactly Workers dials run at once: the
// first dials block until that many are in flight, which would deadlock
// (and hit the timeout) if the pool were smaller, and a larger pool would
// exceed the observed maximum.
func TestScanWorkerBound(t *testing.T) {
	const workers = 5
	var inFlight, maxSeen atomic.Int64
	gate := make(chan struct{})
	var once sync.Once
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		if n >= workers {
			once.Do(func() { close(gate) })
		}
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return nil, refusedError()
	}
	ports := make([]int, 50)
	for i := range ports {
		ports[i] = i + 1
	}
	got, err := (&Scanner{Dial: dial, Workers: workers, Timeout: 10 * time.Second}).Scan(context.Background(), "127.0.0.1", ports)
	if err != nil || len(got) != len(ports) {
		t.Fatalf("got %d results, err %v", len(got), err)
	}
	if maxSeen.Load() != workers {
		t.Fatalf("max concurrent dials = %d, want %d", maxSeen.Load(), workers)
	}
	for _, r := range got {
		if r.State != StateClosed {
			t.Fatalf("port %d: %s (gate never opened?)", r.Port, r.State)
		}
	}
}

func TestScanCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fn := &fakeNet{
		states: map[int]State{4: StateFiltered, 2: StateOpen},
		before: func(_ context.Context, port int) {
			if port == 4 {
				cancel() // Ctrl+C while port 4 is being dialled
			}
		},
	}
	ports := []int{1, 2, 3, 4, 5, 6, 7, 8}
	start := time.Now()
	got, err := (&Scanner{Dial: fn.dial, Workers: 1, Timeout: time.Minute}).Scan(ctx, "127.0.0.1", ports)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("cancellation did not abort the in-flight dial")
	}
	if len(got) != 3 || got[0].Port != 1 || got[1].State != StateOpen || got[2].Port != 3 {
		t.Fatalf("partial results = %+v, want ports 1-3", got)
	}
}

func TestScanAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int64
	dial := func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, refusedError()
	}
	got, err := (&Scanner{Dial: dial, Workers: 3}).Scan(ctx, "127.0.0.1", []int{1, 2, 3, 4, 5})
	if !errors.Is(err, context.Canceled) || len(got) != 0 || calls.Load() != 0 {
		t.Fatalf("got %v, err %v, %d dials", got, err, calls.Load())
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want State
	}{
		{"nil", nil, StateOpen},
		{"refused", refusedError(), StateClosed},
		{"reset", &net.OpError{Op: "dial", Err: refusedErrors[1]}, StateClosed},
		{"deadline", context.DeadlineExceeded, StateFiltered},
		{"other", errors.New("network is unreachable"), StateFiltered},
		{"local exhaustion", &net.OpError{Op: "dial", Err: localErrors[0]}, StateError},
	}
	for _, tt := range tests {
		if got := classify(tt.err); got != tt.want {
			t.Errorf("%s: classify = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestScanRealLocal scans real listeners on 127.0.0.1. Closed ports are only
// asserted to be "not open": on some Windows versions a refused localhost
// connection is retried for ~2s and may surface as filtered.
func TestScanRealLocal(t *testing.T) {
	open := listenLocal(t, 3)
	closed := closedLocalPort(t)
	ports := append([]int{closed}, open...)
	got, err := (&Scanner{Timeout: 5 * time.Second, Workers: 10}).Scan(context.Background(), "127.0.0.1", ports)
	if err != nil {
		t.Fatal(err)
	}
	states := map[int]State{}
	for _, r := range got {
		states[r.Port] = r.State
	}
	for _, p := range open {
		if states[p] != StateOpen {
			t.Errorf("listener port %d: %s, want open", p, states[p])
		}
	}
	if s, ok := states[closed]; !ok || s == StateOpen {
		t.Errorf("released port %d: %q, want not open", closed, s)
	}
	if runtime.GOOS != "windows" && states[closed] != StateClosed {
		t.Errorf("released port %d: %s, want closed", closed, states[closed])
	}
}

func TestScanRealIPv6(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	got, err := (&Scanner{Timeout: 5 * time.Second}).Scan(context.Background(), "::1", []int{port})
	if err != nil || len(got) != 1 || got[0].State != StateOpen {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestScanLocalErrorAndUnreachable(t *testing.T) {
	unreachable := errors.New("connect: no route to host")
	fn := &fakeNet{states: map[int]State{7: StateError}}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if strings.HasSuffix(address, ":8") {
			return nil, unreachable
		}
		return fn.dial(ctx, network, address)
	}
	got, err := (&Scanner{Dial: dial, Workers: 2}).Scan(context.Background(), "10.0.0.1", []int{7, 8})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].State != StateError || got[0].Err == "" {
		t.Errorf("port 7 = %+v, want error state with reason", got[0])
	}
	if got[1].State != StateFiltered || got[1].Err != unreachable.Error() {
		t.Errorf("port 8 = %+v, want filtered with reason", got[1])
	}
}
