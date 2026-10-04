//go:build windows

package serve

import (
	"errors"
	"syscall"
)

// wsaeAddrInUse is WSAEADDRINUSE; syscall.EADDRINUSE on Windows is a
// placeholder value the socket layer never returns.
const wsaeAddrInUse syscall.Errno = 10048

func addrInUse(err error) bool { return errors.Is(err, wsaeAddrInUse) }
