//go:build !windows

package portscout

import (
	"net"
	"syscall"
)

// refusedErrors are the errno values meaning "the peer sent a RST".
var refusedErrors = []error{syscall.ECONNREFUSED, syscall.ECONNRESET}

// localErrors are errno values meaning the probe could not be made because
// this machine ran out of resources (sockets, ephemeral ports, buffers).
var localErrors = []error{syscall.EADDRINUSE, syscall.EADDRNOTAVAIL, syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS}

// newDialer returns the dialer used for real scans.
func newDialer() *net.Dialer { return &net.Dialer{} }
