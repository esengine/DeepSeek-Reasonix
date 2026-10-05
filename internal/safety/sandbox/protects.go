package sandbox

import (
	"path/filepath"
	"runtime"
	"strings"
)

// WriteProtects reports whether a command confined by spec is unable to write
// anywhere in or under path. It is true only when the spec enforces, this
// platform has a working backend, and no directory the backend would make
// writable — caller roots, temp, toolchain caches — overlaps path in either
// direction. path need not exist yet.
func WriteProtects(spec Spec, path string) bool {
	return IntegrityEnforced(spec) && outsideAll(path, confinedWriteDirs(spec))
}

// IntegrityEnforced reports whether a command confined by spec runs under a
// backend that demonstrably confines its writes on this host. It is the
// Integrity claim alone: it says nothing about reads or host authorities.
func IntegrityEnforced(spec Spec) bool {
	return spec.Enforce() && Available()
}

// outsideAll reports whether path neither lies under nor contains any of dirs.
// Anything it cannot resolve answers false.
func outsideAll(path string, dirs []string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	target, ok := resolveForCompare(path)
	if !ok {
		return false
	}
	for _, dir := range dirs {
		d, ok := resolveForCompare(dir)
		if !ok {
			return false
		}
		if pathWithin(d, target) || pathWithin(target, d) {
			return false
		}
	}
	return true
}

// resolveForCompare makes path absolute and resolves symlinks through its
// deepest existing ancestor, so a path that does not exist yet still compares
// the way the kernel will see it once created.
func resolveForCompare(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return fold(filepath.Join(real, rest)), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return fold(abs), true
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
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
