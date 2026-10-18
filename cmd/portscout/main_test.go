package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/tonklasocute/go-cli-tools/internal/testutil"
)

// listen opens a TCP listener on 127.0.0.1 that accepts and drops
// connections, returning its port.
func listen(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.(*net.TCPConn).SetLinger(0) // RST: no TIME_WAIT pile-up
			_ = c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestBinary(t *testing.T) {
	bin := testutil.BuildBinary(t, ".", "portscout")
	open1, open2, closed := listen(t), listen(t), closedPort(t)
	spec := fmt.Sprintf("%d,%d,%d", open1, open2, closed)

	t.Run("text", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "127.0.0.1", "-p", spec, "--timeout", "5s")
		if r.Code != 0 {
			t.Fatalf("exit %d: %s", r.Code, r.Stderr)
		}
		for _, want := range []string{"Scanning 127.0.0.1 — 3 ports, 3 workers, timeout 5.00s", "PORT", strconv.Itoa(open1) + "  open", "2 open"} {
			if !strings.Contains(r.Stdout, want) {
				t.Errorf("missing %q in:\n%s", want, r.Stdout)
			}
		}
		if strings.Contains(r.Stdout, "\x1b[") || r.Stderr != "" {
			t.Errorf("unexpected ANSI or stderr: %q", r.Stderr)
		}
	})

	t.Run("json", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "127.0.0.1", "-p", spec, "--timeout", "5s", "--json")
		var doc struct {
			IP           string `json:"ip"`
			PortsScanned int    `json:"ports_scanned"`
			Open         int    `json:"open"`
			Results      []struct {
				Port  int    `json:"port"`
				State string `json:"state"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil || r.Code != 0 {
			t.Fatalf("exit %d, %v:\n%s", r.Code, err, r.Stdout)
		}
		if doc.IP != "127.0.0.1" || doc.PortsScanned != 3 || doc.Open != 2 || len(doc.Results) != 3 {
			t.Fatalf("unexpected doc: %+v", doc)
		}
	})

	t.Run("expect-open fails on a closed port", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "127.0.0.1", "--expect-open", fmt.Sprintf("%d,%d", open1, closed), "--timeout", "5s")
		if r.Code != 1 || !strings.Contains(r.Stdout, "expected open but not: "+strconv.Itoa(closed)) {
			t.Fatalf("exit %d:\n%s", r.Code, r.Stdout)
		}
	})

	t.Run("expect-open succeeds", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "127.0.0.1", "--expect-open", strconv.Itoa(open2), "--timeout", "5s")
		if r.Code != 0 || !strings.Contains(r.Stdout, "✔ expected ports open") {
			t.Fatalf("exit %d:\n%s", r.Code, r.Stdout)
		}
	})

	t.Run("bad port spec", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "-p", "70000")
		if r.Code != 2 || !strings.Contains(r.Stderr, "out of range") || r.Stdout != "" {
			t.Fatalf("exit %d, stderr %q", r.Code, r.Stderr)
		}
	})

	t.Run("help has ethics note", func(t *testing.T) {
		r := testutil.RunBinary(t, bin, nil, "--help")
		if r.Code != 0 || !strings.Contains(r.Stdout, "only scan hosts you own") {
			t.Fatalf("exit %d:\n%s", r.Code, r.Stdout)
		}
	})
}
