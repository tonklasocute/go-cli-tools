package portscout

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// LookupFunc resolves a host name; it matches net.Resolver.LookupIPAddr.
type LookupFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

// Resolve turns host into a single IP to scan. IP literals (including IPv6,
// with or without brackets) are returned as is. For names, the first IPv4
// address is preferred unless preferIPv6 is set, in which case the first
// IPv6 address wins; either way the other family is the fallback.
func Resolve(ctx context.Context, lookup LookupFunc, host string, preferIPv6 bool) (string, error) {
	h := strings.TrimSpace(host)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if h == "" {
		return "", fmt.Errorf("empty host")
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.String(), nil
	}
	// Zoned IPv6 literal such as fe80::1%eth0: keep the zone for dialing.
	if addr, _, ok := strings.Cut(h, "%"); ok && net.ParseIP(addr) != nil {
		return h, nil
	}
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	addrs, err := lookup(ctx, h)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	var first4, first6 string
	for _, a := range addrs {
		if a.IP.To4() != nil {
			if first4 == "" {
				first4 = a.IP.String()
			}
		} else if first6 == "" && a.IP.To16() != nil {
			first6 = a.IP.String()
			if a.Zone != "" {
				first6 += "%" + a.Zone
			}
		}
	}
	if preferIPv6 && first6 != "" {
		return first6, nil
	}
	if first4 != "" {
		return first4, nil
	}
	if first6 != "" {
		return first6, nil
	}
	return "", fmt.Errorf("cannot resolve host %q: no addresses found", host)
}
