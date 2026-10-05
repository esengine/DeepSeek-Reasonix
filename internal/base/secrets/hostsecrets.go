package secrets

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var hostSecrets struct {
	sync.Mutex
	paths []string
}

// RegisterHostSecretPath records a file this process reads a secret from, such
// as serve's --token-file, so runtime sandboxes and read tools deny it.
func RegisterHostSecretPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	hostSecrets.Lock()
	defer hostSecrets.Unlock()
	if slices.Contains(hostSecrets.paths, path) {
		return
	}
	hostSecrets.paths = append(hostSecrets.paths, path)
}

// HostSecretPaths lists every path passed to RegisterHostSecretPath.
func HostSecretPaths() []string {
	hostSecrets.Lock()
	defer hostSecrets.Unlock()
	return append([]string(nil), hostSecrets.paths...)
}
