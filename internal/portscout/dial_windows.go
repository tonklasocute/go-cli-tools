//go:build windows

package portscout

import (
	"net"
	"syscall"
)

// Winsock error codes (winerror.h). The syscall package's ECONNREFUSED is an
// invented value on Windows, so the real WSA codes are matched instead.
const (
	wsaeconnreset    = syscall.Errno(10054)
	wsaeconnrefused  = syscall.Errno(10061)
	wsaemfile        = syscall.Errno(10024)
	wsaeaddrinuse    = syscall.Errno(10048)
	wsaeaddrnotavail = syscall.Errno(10049)
	wsaenobufs       = syscall.Errno(10055)
)

// refusedErrors are the errno values meaning "the peer sent a RST".
var refusedErrors = []error{wsaeconnrefused, wsaeconnreset, syscall.ECONNREFUSED}

// localErrors are errno values meaning the probe could not be made because
// this machine ran out of resources (sockets, ephemeral ports, buffers).
var localErrors = []error{wsaeaddrinuse, wsaeaddrnotavail, wsaenobufs, wsaemfile, syscall.EMFILE}

// sioTCPInitialRTO is SIO_TCP_INITIAL_RTO = _WSAIOW(IOC_VENDOR, 17).
const sioTCPInitialRTO = 0x98000011

// initialRTOParams is a TCP_INITIAL_RTO_PARAMETERS struct in its in-memory
// layout: USHORT Rtt = TCP_INITIAL_RTO_UNSPECIFIED_RTT (0xFFFF), UCHAR
// MaxSynRetransmissions = TCP_INITIAL_RTO_NO_SYN_RETRANSMISSIONS (0xFE),
// one byte of padding.
var initialRTOParams = [4]byte{0xFF, 0xFF, 0xFE, 0x00}

// newDialer returns the dialer used for real scans.
//
// Windows retransmits a SYN that was answered with RST, so connecting to a
// closed port takes ~2s instead of failing immediately and every closed port
// would look "filtered". Disabling SYN retransmissions on the socket
// (SIO_TCP_INITIAL_RTO, Windows 10 1703+) restores the immediate "connection
// refused". The ioctl is best effort: if it fails the dial proceeds normally.
func newDialer() *net.Dialer {
	return &net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			var returned uint32
			_ = syscall.WSAIoctl(syscall.Handle(fd), sioTCPInitialRTO,
				&initialRTOParams[0], uint32(len(initialRTOParams)),
				nil, 0, &returned, nil, 0)
		})
	}}
}
