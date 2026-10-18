//go:build !windows

package portscout

import (
	"net"
	"syscall"
)

// refusedErrors are the errno values meaning "the peer answered the SYN with
// a RST", i.e. nothing is listening.
var refusedErrors = []error{syscall.ECONNREFUSED}

// resetErrors are the errno values meaning the handshake completed but the
// peer reset the connection before connect(2) returned — a service accepted
// and immediately dropped us, so the port is open.
var resetErrors = []error{syscall.ECONNRESET}

// localErrors are errno values meaning the probe could not be made because
// this machine ran out of resources (sockets, ephemeral ports, buffers).
var localErrors = []error{syscall.EADDRINUSE, syscall.EADDRNOTAVAIL, syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS}

// newDialer returns the dialer used for real scans.
func newDialer() *net.Dialer { return &net.Dialer{} }
