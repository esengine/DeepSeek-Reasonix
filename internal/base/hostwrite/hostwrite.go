// Package hostwrite names the host directories a jailed command may write
// besides its own write roots — temporary directories and toolchain caches —
// so the jail that grants them and the checks that must not trust what lies
// there read one list.
package hostwrite

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dirs lists those directories for this host, unresolved. Windows jails no
// command, so nothing there is narrowed and the list is empty.
func Dirs() []string {
	return dirsFor(runtime.GOOS)
}

// Caches lists the toolchain caches a build writes through, on every platform.
// A check that must not trust what a cache holds reads this rather than Dirs:
// Dirs is what a jail grants, and a host that jails nothing grants nothing.
func Caches() []string {
	return cachesFor(runtime.GOOS)
}

// homeRoot is the one place this package reads the process's home directory.
// Both lists derive from it, so a caller that reads the host through here reads
// it once rather than once per list.
func homeRoot() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil
}

func cachesFor(goos string) []string {
	home, ok := homeRoot()
	if !ok {
		return nil
	}
	subs := []string{".cargo", ".npm", "go"}
	if goos == "darwin" {
		subs = append(subs, "Library/Caches")
	}
	dirs := make([]string, 0, len(subs)+1)
	dirs = append(dirs, filepath.Join(home, ".cache"))
	for _, sub := range subs {
		dirs = append(dirs, filepath.Join(home, sub))
	}
	return dirs
}

func dirsFor(goos string) []string {
	home, homeOK := homeRoot()
	switch goos {
	case "windows":
		return nil
	case "darwin":
		dirs := []string{"/tmp", "/private/tmp", "/private/var/folders", os.TempDir()}
		if homeOK {
			// go build/test → Library/Caches + go; pip/etc → .cache; npm/cargo too.
			for _, sub := range []string{"Library/Caches", ".cache", ".npm", ".cargo", "go"} {
				dirs = append(dirs, filepath.Join(home, sub))
			}
		}
		return dirs
	default:
		var dirs []string
		if td := os.TempDir(); td != "" && td != "/tmp" {
			dirs = append(dirs, td)
		}
		if homeOK {
			for _, sub := range []string{".cache", ".cargo", ".npm", "go"} {
				dirs = append(dirs, filepath.Join(home, sub))
			}
		}
		return dirs
	}
}
