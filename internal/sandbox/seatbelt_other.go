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
	args := bwrapBaseArgs(spec)
	return append(args, sh.argv(command)...)
}

// bwrapArgsForArgs is like bwrapArgs but accepts raw argv instead of a shell
// command string. It builds the same sandbox prefix and appends the caller's
// argv directly — no shell interpreter wrapping.
func bwrapArgsForArgs(spec Spec, args []string) []string {
	out := bwrapBaseArgs(spec)
	// /tmp is replaced above (tmpfs or session-private bind) so MCP servers
	// cannot inspect unrelated host temporary files. A configured executable
	// may itself live below /tmp, though (for example a downloaded one-shot
	// launcher or a Go test helper). Re-expose only that exact file, read-only,
	// after every masking mount so the process can start without revealing its
	// siblings. Session-private binds already contain the generation's files,
	// so only host-/tmp executables need this re-mount.
	out = append(out, bwrapExecutableMountArgs(args)...)
	return append(out, args...)
}

// bwrapBaseArgs is the shared bubblewrap prefix for shell and raw-argv launches.
// With Spec.SessionTemp set, the private directory is bind-mounted at /tmp so
// consecutive Bash calls in the same logical session share temporary files.
// Without it (MCP and other independent sandboxes), /tmp is a fresh empty
// tmpfs as before.
func bwrapBaseArgs(spec Spec) []string {
	args := []string{
		"--unshare-net", // deny network by default
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
	}
	args = append(args, bwrapTmpMountArgs(spec)...)
	if spec.Network {
		// Re-allow network by removing the network namespace.
		args = args[1:] // drop --unshare-net
	}
	for _, root := range spec.WriteRoots {
		args = append(args, bwrapWriteRootMountArgs(root)...)
	}
	if !spec.MinimalWrites {
		for _, root := range linuxWriteDirs() {
			args = append(args, "--bind", root, root)
		}
	}
	args = append(args, bwrapProtectedWriteArgs(spec, spec.WriteRoots)...)
	return append(args, bwrapForbidReadArgs(spec.ForbidReadRoots)...)
}

func bwrapProtectedWriteArgs(spec Spec, writeRoots []string) []string {
	protected := resolveProtectedWriteRoots(spec.ProtectedWriteRoots)
	protected = overlappingProtectedWriteRoots(protected, writeRoots)
	if len(protected) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, root := range protected {
		if seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, "--ro-bind", root, root)
	}
	stateRoot := singleProtectedStateRoot(protected)
	for _, abs := range writeRoots {
		if stateRoot != "" && IsProtectedWritePath(abs, stateRoot) {
			continue
		}
		for _, prot := range protected {
			if abs != prot && PathWithin(prot, abs) {
				out = append(out, "--bind", abs, abs)
				break
			}
		}
	}
	return out
}

func overlappingProtectedWriteRoots(protected, writeRoots []string) []string {
	var out []string
	for _, prot := range protected {
		for _, root := range writeRoots {
			root = filepath.Clean(strings.TrimSpace(root))
			if root != "" && root != "." && (PathWithin(root, prot) || PathWithin(prot, root)) {
				out = append(out, prot)
				break
			}
		}
	}
	return out
}

func resolveProtectedWriteRoots(roots []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := ResolveAbsPath(root)
		if err == nil && !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

func bwrapTmpMountArgs(spec Spec) []string {
	if dir := strings.TrimSpace(spec.SessionTemp); dir != "" {
		return []string{"--bind", dir, "/tmp"}
	}
	return []string{"--tmpfs", "/tmp"}
}

func bwrapWriteRootMountArgs(root string) []string {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil
	}
	if !filepath.IsAbs(root) || !pathWithin(root, "/tmp") {
		return []string{"--bind", root, root}
	}
	out := bwrapTmpParentDirArgs(root)
	return append(out, "--bind", root, root)
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

	out := bwrapTmpParentDirArgs(destination)
	return append(out, "--ro-bind", source, destination)
}

func bwrapTmpParentDirArgs(destination string) []string {
	parent := filepath.Dir(destination)
	rel, err := filepath.Rel("/tmp", parent)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2*strings.Count(rel, string(filepath.Separator))+4)
	current := "/tmp"
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		out = append(out, "--dir", current)
	}
	return out
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
