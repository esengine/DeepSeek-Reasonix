package fileutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNetworkPathOutsideScope identifies a network path refused by its spelling:
// it names a machine other than the one a workspace root already lives on.
var ErrNetworkPathOutsideScope = errors.New("network path outside the workspace")

// CodeNetworkPathOutsideScope is the refusal code every caller of NetworkScope
// reports, so the model and the frontends see one identity for one cause.
const CodeNetworkPathOutsideScope = "workspace.network_path_outside_scope"

// HostIsWindows selects the Windows reading of a path spelling for callers that
// have no seam of their own; tests replace it to hold that reading on any host.
var HostIsWindows = runtime.GOOS == "windows"

// NetworkScope answers a path that Windows would send to another machine before
// anything resolves it: resolving a share is itself a connection, so the verdict
// is lexical. A network path passes only below a root that is itself a network
// path. Elsewhere `//x` is an ordinary local path and always passes.
func NetworkScope(path string, roots []string) error {
	return NetworkScopeOn(HostIsWindows, path, roots)
}

// NetworkScopeOn is NetworkScope with the path rules chosen by the caller, so a
// test can hold the Windows reading on any platform.
func NetworkScopeOn(windows bool, path string, roots []string) error {
	if !windows {
		return nil
	}
	if !IsNetworkPath(path) {
		abs, ok := absOnWindows(path)
		if !ok || !IsNetworkPath(abs) {
			return nil
		}
		path = abs
	}
	if target, ok := networkParts(path); ok {
		for _, root := range roots {
			if !IsNetworkPath(root) {
				continue
			}
			if base, ok := networkParts(root); ok && len(base) <= len(target) && equalParts(base, target[:len(base)]) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s", ErrNetworkPathOutsideScope, path)
}

func absOnWindows(path string) (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	abs, err := filepath.Abs(path)
	return abs, err == nil
}

// networkParts reads `\\host\share\rest` (or its `\\?\UNC\` spelling) into
// folded components, host first. Windows drops trailing dots and spaces from a
// component and clamps `..` at the share, so both are applied here. A spelling
// that cannot be placed under a share reports ok=false.
func networkParts(path string) ([]string, bool) {
	p := strings.ReplaceAll(path, "/", `\`)
	switch {
	case hasPrefixFold(p, `\\?\UNC\`), hasPrefixFold(p, `\\.\UNC\`), hasPrefixFold(p, `\??\UNC\`):
		p = p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\`) && !strings.HasPrefix(p, `\\\`) && !isNamespacePrefix(p):
		p = p[2:]
	default:
		return nil, false
	}
	var parts []string
	for c := range strings.SplitSeq(p, `\`) {
		switch c {
		case "", ".":
			continue
		case "..":
			if len(parts) > 2 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		c = strings.TrimRight(c, ". ")
		if c == "" {
			return nil, false
		}
		parts = append(parts, strings.ToLower(c))
	}
	return parts, len(parts) >= 2
}

func isNamespacePrefix(p string) bool {
	return len(p) >= 4 && (p[2] == '?' || p[2] == '.') && p[3] == '\\'
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func equalParts(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
