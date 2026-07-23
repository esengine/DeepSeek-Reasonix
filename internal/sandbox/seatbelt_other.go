//go:build !darwin && !windows

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var bwrapUsability sync.Map // resolved executable path -> bool

// usableBwrap distinguishes an installed binary from a usable sandbox backend.
// Hardened Linux hosts (including some CI runners) may expose bwrap on PATH but
// deny the user namespace it needs; treating that as available makes enforce
// fail later with a misleading launch error and overstates MCP isolation.
func usableBwrap() (string, bool) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return "", false
	}
	if cached, ok := bwrapUsability.Load(bwrap); ok {
		return bwrap, cached.(bool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, bwrap, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--", "true").Run()
	usable := err == nil
	actual, _ := bwrapUsability.LoadOrStore(bwrap, usable)
	return bwrap, actual.(bool)
}

// When spec.Mode is "enforce" and bubblewrap (bwrap) is available on PATH,
// the command is wrapped in a bubblewrap sandbox with a profile analogous to
// macOS Seatbelt: writes confined to WriteRoots, network denied unless
// spec.Network is true. When bwrap is unavailable, the argv is returned
// unwrapped with wrapped=false so callers can decide whether to fail closed.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.Enforce() {
		return sh.argv(command), false
	}
	if bwrap, ok := usableBwrap(); ok {
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
	if bwrap, ok := usableBwrap(); ok {
		argv := append([]string{bwrap}, bwrapArgsForArgs(spec, args)...)
		return argv, true
	}
	return args, false
}

// Available reports whether an OS sandbox is available on this platform.
// On Linux, this verifies that bubblewrap can actually enter its namespace;
// binary presence alone is insufficient on hardened hosts.
func Available() bool {
	_, ok := usableBwrap()
	return ok
}

// bwrapArgs builds the bubblewrap command-line arguments that confine the
// shell command to the write roots, deny network unless allowed, and overlay
// forbid-read paths so directories appear empty and files read as empty. The
// rest of the filesystem is mounted read-only (matching macOS Seatbelt).
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
	if !spec.MinimalWrites {
		for _, root := range linuxWriteDirs() {
			args = append(args, "--bind", root, root)
		}
	}
	args = append(args, bwrapForbidReadArgs(spec.ForbidReadRoots)...)
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
	if !spec.MinimalWrites {
		for _, root := range linuxWriteDirs() {
			out = append(out, "--bind", root, root)
		}
	}
	out = append(out, bwrapForbidReadArgs(spec.ForbidReadRoots)...)
	// /tmp is intentionally replaced with an empty filesystem above so MCP
	// servers cannot inspect unrelated host temporary files. A configured
	// executable may itself live below /tmp, though (for example a downloaded
	// one-shot launcher or a Go test helper). Re-expose only that exact file,
	// read-only, after every masking mount so the process can start without
	// revealing its siblings.
	out = append(out, bwrapExecutableMountArgs(args)...)
	return append(out, args...)
}

// bwrapForbidReadArgs returns mounts suitable for both configured directory
// roots and Reasonix-owned credential files. bubblewrap cannot mount tmpfs on a
// file, so an existing file is replaced by a read-only /dev/null bind instead.
// Missing paths are ignored: there are no bytes to protect and passing a
// missing mount destination would make an otherwise valid sandbox fail closed.
func bwrapForbidReadArgs(roots []string) []string {
	type forbiddenPath struct {
		path  string
		isDir bool
	}
	paths := make([]forbiddenPath, 0, len(roots))
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		paths = append(paths, forbiddenPath{path: root, isDir: info.IsDir()})
	}

	var out []string
	seen := map[string]bool{}
	for _, entry := range paths {
		if seen[entry.path] {
			continue
		}
		covered := false
		for _, parent := range paths {
			if parent.isDir && parent.path != entry.path && pathWithin(entry.path, parent.path) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		seen[entry.path] = true
		if entry.isDir {
			out = append(out, "--tmpfs", entry.path)
			continue
		}
		out = append(out, "--ro-bind", "/dev/null", entry.path)
	}
	return out
}

func bwrapExecutableMountArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	destination := filepath.Clean(args[0])
	if !filepath.IsAbs(destination) || !pathWithin(destination, "/tmp") {
		return nil
	}
	source := destination
	if resolved, err := filepath.EvalSymlinks(destination); err == nil {
		source = resolved
	}

	parent := filepath.Dir(destination)
	rel, err := filepath.Rel("/tmp", parent)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2*strings.Count(rel, string(filepath.Separator))+4)
	current := "/tmp"
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		out = append(out, "--dir", current)
	}
	return append(out, "--ro-bind", source, destination)
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
