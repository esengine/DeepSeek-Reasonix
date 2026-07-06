//go:build !darwin && !windows

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
)

// When spec.Mode is "enforce" and bubblewrap (bwrap) is available on PATH,
// the command is wrapped in a bubblewrap sandbox with a profile analogous to
// macOS Seatbelt: writes confined to WriteRoots, network denied unless
// spec.Network is true. When bwrap is unavailable, the argv is returned
// unwrapped with wrapped=false so callers can decide whether to fail closed.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.Enforce() {
		return sh.argv(command), false
	}
	if bwrap, err := exec.LookPath("bwrap"); err == nil {
		argv := append([]string{bwrap}, bwrapArgs(spec, sh, command)...)
		return argv, true
	}
	// enforce requested but bwrap unavailable — return the unwrapped argv and let
	// callers decide whether a non-sandboxed command is acceptable.
	return sh.argv(command), false
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string. The args are appended directly after the bwrap sandbox
// prefix without shell interpretation — suitable for direct binary invocations
// like ripgrep that don't need a shell wrapper.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	if !spec.Enforce() {
		return args, false
	}
	if bwrap, err := exec.LookPath("bwrap"); err == nil {
		argv := append([]string{bwrap}, bwrapArgsForArgs(spec, args)...)
		return argv, true
	}
	return args, false
}

// Available reports whether an OS sandbox is available on this platform.
// On Linux, this checks for bubblewrap (bwrap) on PATH.
func Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// bwrapArgs builds the bubblewrap command-line arguments that confine the
// shell command to the write roots, deny network unless allowed, and overlay
// forbid-read directories with tmpfs so they appear empty. The rest of the
// filesystem is mounted read-only (matching the macOS Seatbelt profile).
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
	for _, root := range linuxWriteDirs() {
		args = append(args, "--bind", root, root)
	}
	for _, root := range spec.ForbidReadRoots {
		args = append(args, "--tmpfs", root)
	}
	return append(args, sh.argv(command)...)
}

// bwrapArgsForArgs is like bwrapArgs but accepts raw argv instead of a shell
// command string. It builds the same sandbox prefix and appends the caller's
// argv directly — no shell interpreter wrapping.
func bwrapArgsForArgs(spec Spec, args []string) []string {
	out := []string{
		"--unshare-net", // deny network by default
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if spec.Network {
		// Re-allow network by removing the network namespace.
		out = out[1:] // drop --unshare-net
	}
	for _, root := range spec.WriteRoots {
		out = append(out, "--bind", root, root)
	}
	for _, root := range linuxWriteDirs() {
		out = append(out, "--bind", root, root)
	}
	for _, root := range spec.ForbidReadRoots {
		out = append(out, "--tmpfs", root)
	}
	return append(out, args...)
}

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
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if abs == "/tmp" || seen[abs] || !dirExists(abs) {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
