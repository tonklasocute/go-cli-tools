package portscout

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Default and limit values for the scanner.
const (
	DefaultTimeout = 500 * time.Millisecond
	DefaultWorkers = 200
	// MaxWorkers caps concurrency so a typo cannot exhaust file descriptors.
	MaxWorkers = 5000
)

// State is the outcome of probing one port.
type State string

// Port states, following nmap's vocabulary for a TCP connect scan.
const (
	// StateOpen means the TCP handshake completed.
	StateOpen State = "open"
	// StateClosed means the host actively refused the connection (RST).
	StateClosed State = "closed"
	// StateFiltered means there was no answer before the timeout, or the
	// connection failed in some other way (e.g. host/network unreachable).
	StateFiltered State = "filtered"
	// StateError means the probe could not be made because this machine ran
	// out of resources (ephemeral ports, file descriptors, buffers); it says
	// nothing about the remote port. Lowering --workers usually helps.
	StateError State = "error"
)

// Result is the outcome for a single port.
type Result struct {
	Port    int
	State   State
	Service string
	// Latency is how long the connection attempt took.
	Latency time.Duration
	// Err describes why a filtered or error port failed ("timeout" or the
	// dial error); it is empty for open and closed ports.
	Err string
}

// DialFunc opens a connection; it matches net.Dialer.DialContext so tests
// can substitute a fake.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Scanner runs a TCP connect scan with a bounded worker pool.
type Scanner struct {
	// Dial opens connections. Nil means a real net.Dialer.
	Dial DialFunc
	// Timeout bounds each connection attempt. Zero means DefaultTimeout.
	Timeout time.Duration
	// Workers is the maximum number of concurrent attempts. Values below 1
	// mean DefaultWorkers.
	Workers int
	// OnResult, if set, is called once per finished port, concurrently from
	// worker goroutines (it must be safe for concurrent use).
	OnResult func(Result)
}

// Scan probes every port on ip (an IP literal; IPv6 needs no brackets) and
// returns the results sorted by port. If ctx is cancelled the scan stops
// promptly: in-flight dials are aborted, ports that were not finished are
// omitted and ctx.Err() is returned together with the partial results.
func (s *Scanner) Scan(ctx context.Context, ip string, ports []int) ([]Result, error) {
	dial := s.Dial
	if dial == nil {
		dial = newDialer().DialContext
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	workers := s.Workers
	if workers < 1 {
		workers = DefaultWorkers
	}
	workers = min(workers, len(ports))

	results := make([]Result, len(ports))
	done := make([]bool, len(ports))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				r, ok := probe(ctx, dial, timeout, ip, ports[i])
				if !ok {
					continue
				}
				// Each index is written by exactly one worker.
				results[i], done[i] = r, true
				if s.OnResult != nil {
					s.OnResult(r)
				}
			}
		}()
	}
feed:
	for i := range ports {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	out := make([]Result, 0, len(ports))
	for i, ok := range done {
		if ok {
			out = append(out, results[i])
		}
	}
	slices.SortFunc(out, func(a, b Result) int { return a.Port - b.Port })
	return out, ctx.Err()
}

// probe attempts one connection. ok is false when the attempt was abandoned
// because the parent context was cancelled.
func probe(ctx context.Context, dial DialFunc, timeout time.Duration, ip string, port int) (Result, bool) {
	if ctx.Err() != nil {
		return Result{}, false
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	start := time.Now()
	conn, err := dial(dctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	latency := time.Since(start)
	cancel()
	if conn != nil {
		closeAbortive(conn)
	}
	if err != nil && ctx.Err() != nil {
		return Result{}, false
	}
	r := Result{Port: port, State: classify(err), Service: ServiceName(port), Latency: latency}
	switch {
	case r.State == StateFiltered && isTimeout(err):
		r.Err = "timeout"
	case r.State == StateFiltered || r.State == StateError:
		r.Err = err.Error()
	}
	return r, true
}

// closeAbortive closes conn with SO_LINGER=0 so the OS sends a RST instead
// of a FIN. The port does not then sit in TIME_WAIT, which matters for a
// scanner: thousands of graceful closes can exhaust the ephemeral port
// range (16384 ports on Windows) and make later probes fail.
func closeAbortive(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = conn.Close()
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// classify maps a dial error to a port state.
func classify(err error) State {
	switch {
	case err == nil:
		return StateOpen
	case matchAny(err, refusedErrors):
		return StateClosed
	case matchAny(err, localErrors):
		return StateError
	default:
		return StateFiltered
	}
}

// matchAny reports whether err wraps any of targets.
func matchAny(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
