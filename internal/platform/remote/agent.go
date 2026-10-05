package remote

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentDialer opens one connection to the running agent. It is the only thing
// the platforms disagree about: which keys are offered, and which are filtered
// out, is the same question wherever the agent lives.
type agentDialer func() (io.ReadWriteCloser, error)

// agentAuthOver offers the agent's keys as a publickey method, and returns the
// close for the connections it opened along the way.
func agentAuthOver(dial agentDialer, identityFiles []string, identitiesOnly bool) (ssh.AuthMethod, func()) {
	var mu sync.Mutex
	var conns []io.Closer
	method := ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		conn, err := dial()
		if err != nil {
			return nil, err
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		signers, err := agent.NewClient(conn).Signers()
		if err != nil {
			return nil, err
		}
		if identitiesOnly {
			signers = filterAgentSigners(signers, identityFiles)
		}
		return signers, nil
	})
	return method, func() {
		mu.Lock()
		owned := conns
		conns = nil
		mu.Unlock()
		for _, conn := range owned {
			_ = conn.Close()
		}
	}
}

// filterAgentSigners implements OpenSSH's IdentitiesOnly behavior: agent keys
// remain available when they correspond to a configured IdentityFile, but
// unrelated agent keys are not offered to the server.
func filterAgentSigners(signers []ssh.Signer, identityFiles []string) []ssh.Signer {
	allowed := make([]ssh.PublicKey, 0, len(identityFiles))
	for _, path := range identityFiles {
		allowed = append(allowed, identityPublicKeys(path)...)
	}
	if len(allowed) == 0 {
		return nil
	}
	filtered := make([]ssh.Signer, 0, len(signers))
	for _, signer := range signers {
		for _, key := range allowed {
			if publicKeysEqual(signer.PublicKey(), key) {
				filtered = append(filtered, signer)
				break
			}
		}
	}
	return filtered
}

func identityPublicKeys(path string) []ssh.PublicKey {
	path = expandHome(path)
	candidates := []string{path}
	if !strings.HasSuffix(strings.ToLower(path), ".pub") {
		candidates = append(candidates, path+".pub")
	}
	seen := map[string]bool{}
	var keys []ssh.PublicKey
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if key, _, _, _, err := ssh.ParseAuthorizedKey(data); err == nil {
			id := string(normalizePublicKey(key).Marshal())
			if !seen[id] {
				seen[id] = true
				keys = append(keys, key)
			}
			continue
		}
		if signer, err := ssh.ParsePrivateKey(data); err == nil {
			key := signer.PublicKey()
			id := string(normalizePublicKey(key).Marshal())
			if !seen[id] {
				seen[id] = true
				keys = append(keys, key)
			}
			continue
		} else {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) && missing.PublicKey != nil {
				key := missing.PublicKey
				id := string(normalizePublicKey(key).Marshal())
				if !seen[id] {
					seen[id] = true
					keys = append(keys, key)
				}
			}
		}
	}
	return keys
}

func publicKeysEqual(a, b ssh.PublicKey) bool {
	return bytes.Equal(normalizePublicKey(a).Marshal(), normalizePublicKey(b).Marshal())
}

func normalizePublicKey(key ssh.PublicKey) ssh.PublicKey {
	if cert, ok := key.(*ssh.Certificate); ok {
		return cert.Key
	}
	return key
}
