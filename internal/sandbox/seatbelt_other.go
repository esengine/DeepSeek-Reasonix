//go:build !darwin

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Command runs the command unwrapped: no OS sandbox is implemented for this
// platform yet (Linux bubblewrap/landlock is the next step). The permission
// layer still gates the call.
//
// When spec.Mode is "enforce" and bubblewrap (bwrap) is available on PATH,
// the command is wrapped in a bubblewrap sandbox with a profile analogous to
// macOS Seatbelt: writes confined to WriteRoots, network denied unless
// spec.Network is true. When bwrap is unavailable the command runs unconfined
// (boot and acp warn about this once at startup).
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.enforce() {
		return sh.argv(command), false
	}
	if bwrap, err := exec.LookPath("bwrap"); err == nil {
		argv := append([]string{bwrap}, bwrapArgs(spec, sh, command)...)
		return argv, true
	}
	// enforce requested but bwrap unavailable — boot/acp already warned at
	// startup; fall back to unconfined (the false result signals "not sandboxed").
	return sh.argv(command), false
}

// Available reports whether an OS sandbox is available on this platform.
// On Linux, this checks for bubblewrap (bwrap) on PATH.
func Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// bwrapArgs builds the bubblewrap command-line arguments that confine the
// shell command to the write roots, deny network unless allowed, and allow
// read access to the whole filesystem (matching the macOS Seatbelt profile's
// read-open policy). Writable toolchain caches and temp dirs are bound too so
// go build/test, pip, npm, cargo, etc. keep working inside the sandbox.
func bwrapArgs(spec Spec, sh Shell, command string) []string {
	args := []string{
		"--unshare-net", // deny network by default
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if spec.Network {
		// Re-allow network by removing the network namespace.
		args = args[1:] // drop --unshare-net
	}
	for _, root := range spec.WriteRoots {
		args = append(args, "--bind", root, root)
	}
	for _, p := range linuxWriteDirs() {
		args = append(args, "--bind", p, p)
	}
	return append(args, sh.argv(command)...)
}

// linuxWriteDirs returns the deduplicated, symlink-resolved set of directories
// the bwrap sandbox permits writes to beyond spec.WriteRoots: the system temp
// (when it isn't /tmp, which is already a tmpfs) plus the common toolchain
// caches under $HOME (.cache, .cargo, .npm, go), mirroring the macOS Seatbelt
// profile's writeAllowDirs.
func linuxWriteDirs() []string {
	dirs := []string{}
	if td := os.TempDir(); td != "" && td != "/tmp" {
		dirs = append(dirs, td)
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{".cache", ".cargo", ".npm", "go"} {
			dirs = append(dirs, filepath.Join(home, sub))
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if abs == "/tmp" {
			continue // handled by --tmpfs /tmp
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}
