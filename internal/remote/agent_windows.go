//go:build windows

package remote

import (
	"fmt"
	"net"
)

// dialAgent connects to the ssh-agent on Windows. The OpenSSH agent listens on
// the named pipe \\.\pipe\openssh-ssh-agent; SSH_AUTH_SOCK may name a pipe
// path. Named-pipe dialing needs a Windows-specific transport that is a V2
// follow-up, so V1 reports agent auth as unavailable on Windows and falls back
// to key/password methods.
func dialAgent(sock string) (net.Conn, error) {
	return nil, fmt.Errorf("remote: ssh-agent support on Windows is not yet implemented (SSH_AUTH_SOCK=%s)", sock)
}
