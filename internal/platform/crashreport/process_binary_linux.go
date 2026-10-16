//go:build linux

package crashreport

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sameBinary reports whether pid is running this same executable.
//
// The liveness probe alone cannot answer the question the caller is really
// asking. Pids are recycled, and on Android they are recycled quickly and from
// a small pool — so a dump file named after a kernel that died days ago can
// carry the pid of some unrelated process that is alive right now. Reading that
// as "its writer is still running" left the file in place on every start,
// forever, which is the bug this exists to settle.
//
// `/proc/<pid>/cmdline` is the identity that survives reuse: the kernel is
// started as the one binary this process is, so its argv[0] names that binary
// and nothing else on the device does. Android carries the `linux` build tag,
// so the same code answers there; the app's own pid files do not exist beside
// it, and this is where it does.
//
// Every uncertain answer is `true` — unreadable /proc, an unresolvable path, an
// empty argv. Being wrong that way keeps a file that could have been removed;
// being wrong the other way discards a crash report somebody may need.
func sameBinary(pid int) bool {
	if pid <= 0 {
		return false
	}
	self, err := os.Executable()
	if err != nil || strings.TrimSpace(self) == "" {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return true
	}
	argv0 := strings.SplitN(string(raw), "\x00", 2)[0]
	if argv0 == "" {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(argv0); err == nil {
		return resolved == self
	}
	return argv0 == self
}
