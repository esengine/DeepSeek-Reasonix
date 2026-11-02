package sandbox

import (
	"path/filepath"
	"runtime"
	"strings"
)

// IntegrityEnforced reports whether a command confined by spec runs under a
// backend that demonstrably confines its writes on this host. It is the
// Integrity claim alone: it says nothing about reads or host authorities.
func IntegrityEnforced(spec Spec) bool {
	return spec.Enforce() && Available()
}

func fold(p string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func pathWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
