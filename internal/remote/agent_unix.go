//go:build !windows

package remote

import "net"

// dialAgent connects to the ssh-agent socket named by SSH_AUTH_SOCK.
func dialAgent(sock string) (net.Conn, error) {
	return net.Dial("unix", sock)
}
