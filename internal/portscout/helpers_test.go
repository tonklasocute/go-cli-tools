package portscout

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// fakeNet is a deterministic DialFunc: each port answers according to
// states (default closed), optionally after a simulated network delay.
type fakeNet struct {
	states map[int]State
	delay  time.Duration
	// before, if set, runs at the start of every dial.
	before func(ctx context.Context, port int)
}

func (f *fakeNet) dial(ctx context.Context, _, address string) (net.Conn, error) {
	_, ps, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(ps)
	if err != nil {
		return nil, err
	}
	if f.before != nil {
		f.before(ctx, port)
	}
	if f.delay > 0 {
		t := time.NewTimer(f.delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: ctx.Err()}
		}
	}
	switch f.states[port] {
	case StateOpen:
		c1, c2 := net.Pipe()
		_ = c2.Close()
		return c1, nil
	case StateError:
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", localErrors[0])}
	case StateFiltered:
		<-ctx.Done()
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: ctx.Err()}
	default:
		return nil, refusedError()
	}
}

// refusedError mimics what net.Dialer returns for a RST.
func refusedError() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", refusedErrors[0])}
}

// listenLocal opens n TCP listeners on 127.0.0.1 with OS-assigned ports.
func listenLocal(t testing.TB, n int) []int {
	t.Helper()
	ports := make([]int, n)
	for i := range ports {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() { // accept and drop so the backlog never fills up
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				_ = c.(*net.TCPConn).SetLinger(0)
				_ = c.Close()
			}
		}()
		ports[i] = ln.Addr().(*net.TCPAddr).Port
	}
	return ports
}

// closedLocalPort returns a port that was just listened on and released, so
// nothing should be listening on it.
func closedLocalPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
