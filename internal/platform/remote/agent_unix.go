//go:build !windows

package remote

import (
	"io"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

// agentAuth offers whatever the agent named by SSH_AUTH_SOCK is holding.
func agentAuth(identityFiles []string, identitiesOnly bool) (ssh.AuthMethod, func()) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, func() {}
	}
	dial := func() (io.ReadWriteCloser, error) { return net.Dial("unix", sock) }
	return agentAuthOver(dial, identityFiles, identitiesOnly)
}
