package portscout

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func fakeLookup(addrs ...string) LookupFunc {
	return func(_ context.Context, host string) ([]net.IPAddr, error) {
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		out := make([]net.IPAddr, len(addrs))
		for i, a := range addrs {
			ip, zone, _ := strings.Cut(a, "%")
			out[i] = net.IPAddr{IP: net.ParseIP(ip), Zone: zone}
		}
		return out, nil
	}
}

func TestResolve(t *testing.T) {
	both := fakeLookup("::1", "127.0.0.1")
	tests := []struct {
		name    string
		host    string
		lookup  LookupFunc
		v6      bool
		want    string
		wantErr string
	}{
		{name: "ipv4 literal", host: "10.0.0.1", lookup: fakeLookup(), want: "10.0.0.1"},
		{name: "ipv6 literal", host: "::1", lookup: fakeLookup(), want: "::1"},
		{name: "bracketed ipv6", host: "[::1]", lookup: fakeLookup(), want: "::1"},
		{name: "ipv6 canonicalised", host: "0:0:0:0:0:0:0:1", lookup: fakeLookup(), want: "::1"},
		{name: "zoned ipv6", host: "fe80::1%eth0", lookup: fakeLookup(), want: "fe80::1%eth0"},
		{name: "prefers ipv4", host: "localhost", lookup: both, want: "127.0.0.1"},
		{name: "prefers ipv6 with -6", host: "localhost", lookup: both, v6: true, want: "::1"},
		{name: "ipv6 only", host: "h", lookup: fakeLookup("fe80::2%3"), want: "fe80::2%3"},
		{name: "ipv4 only with -6", host: "h", lookup: fakeLookup("192.0.2.7"), v6: true, want: "192.0.2.7"},
		{name: "not found", host: "nope.invalid", lookup: fakeLookup(), wantErr: `cannot resolve host "nope.invalid"`},
		{name: "empty", host: "  ", lookup: fakeLookup(), wantErr: "empty host"},
		{name: "no addresses", host: "h", lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, nil }, wantErr: "no addresses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(context.Background(), tt.lookup, tt.host, tt.v6)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Resolve(%q) = %q, %v; want %q", tt.host, got, err, tt.want)
			}
		})
	}
}

func TestResolveCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookup := func(ctx context.Context, _ string) ([]net.IPAddr, error) { return nil, ctx.Err() }
	if _, err := Resolve(ctx, lookup, "example.com", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestResolveRealLocalhost(t *testing.T) {
	ip, err := Resolve(context.Background(), nil, "localhost", false)
	if err != nil {
		t.Skipf("localhost does not resolve here: %v", err)
	}
	if parsed := net.ParseIP(ip); parsed == nil || !parsed.IsLoopback() {
		t.Fatalf("localhost resolved to %q", ip)
	}
}
