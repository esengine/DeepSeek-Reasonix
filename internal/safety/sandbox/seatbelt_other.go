//go:build !darwin && !windows

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/hostwrite"
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
	if egressBridged(spec) {
		args = append(args, egressBridgeArgs(spec)...)
	}
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
	if egressBridged(spec) {
		out = append(out, egressBridgeArgs(spec)...)
	}
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
	if spec.Network && !egressBridged(spec) {
		// Re-allow network by removing the network namespace.
		args = args[1:] // drop --unshare-net
	}
	for _, root := range bwrapWriteBinds(spec) {
		args = append(args, "--bind", root, root)
	}
	args = append(args, bwrapGitMetadataArgs(spec)...)
	args = append(args, bwrapForbidReadArgs(spec.ForbidReadRoots)...)
	// Masks last so no later mount re-exposes an endpoint. A pathname socket
	// lives in the mount namespace, so --unshare-net misses it and
	// --unshare-ipc is the wrong tool: that isolates SysV IPC, not sockets.
	for _, p := range deniedAuthorityEndpoints(spec) {
		args = append(args, "--ro-bind", "/dev/null", p)
	}
	return args
}

// bwrapGitMetadataArgs pins existing protected directories as mount points, which
// rename and rmdir refuse, and binds protected files and trees read-only; paths
// beneath a tree are left to it, so an over-budget group costs one mount. A dir
// holding a writable root is skipped (rebinding hides the mounts beneath), and
// only existing non-symlinks are mount targets: binding a link exposes its target.
func bwrapGitMetadataArgs(spec Spec) []string {
	meta := gitMetadataForSpec(spec)
	paths := meta.Paths
	for _, common := range meta.Commons {
		paths = append(paths, gitGroupPaths(common, gitGroupMaxMounts)...)
	}
	var trees []string
	for _, p := range paths {
		if p.Tree {
			trees = append(trees, p.Path)
		}
	}
	writable := writableDirsForSpec(spec)
	var out []string
	for _, p := range paths {
		if slices.ContainsFunc(trees, func(t string) bool { return strings.HasPrefix(p.Path, t+string(filepath.Separator)) }) {
			continue
		}
		info, err := os.Lstat(p.Path)
		if err != nil {
			continue
		}
		switch {
		case (p.Tree && info.IsDir()) || (!p.Tree && !p.Pin && info.Mode().IsRegular()):
			out = append(out, "--ro-bind", p.Path, p.Path)
		case p.Pin && info.IsDir() && !slices.ContainsFunc(writable, func(w string) bool { return pathWithin(p.Path, w) }):
			out = append(out, "--bind", p.Path, p.Path)
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
			if parent.isDir && parent.path != entry.path && fileutil.Under(entry.path, parent.path) {
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
	if !filepath.IsAbs(destination) || !fileutil.Under(destination, "/tmp") {
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
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		out = append(out, "--dir", current)
	}
	return append(out, "--ro-bind", source, destination)
}

// bwrapWriteBinds are the resolved directories bound writable. Only outermost
// ones are bound, and only ones that exist: bwrap refuses a missing source, and
// host /tmp is replaced by the tmp mount rather than re-exposed.
func bwrapWriteBinds(spec Spec) []string {
	var dirs []string
	for _, d := range planWriteRoots(spec, backendWriteDirs(spec)).dirs {
		if d != "/tmp" && dirExists(d) {
			dirs = append(dirs, d)
		}
	}
	return collapseNested(dirs)
}

// collapseNested drops every directory inside another one in dirs, keeping
// the order of the rest. A bind of a nested root adds nothing its ancestor
// does not grant, and its path runs through a directory a confined command
// can rewrite between the check and the mount.
func collapseNested(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for i, d := range dirs {
		nested := false
		for j, other := range dirs {
			if i != j && fold(other) != fold(d) && pathWithin(fold(other), fold(d)) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, d)
		}
	}
	return out
}

// backendWriteDirs are the toolchain caches bound beside the caller's roots.
func backendWriteDirs(spec Spec) []string {
	if spec.MinimalWrites {
		return nil
	}
	return linuxWriteDirs()
}

func linuxWriteDirs() []string {
	var out []string
	for _, d := range hostwrite.Dirs() {
		if dirExists(d) {
			out = append(out, d)
		}
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// EgressSupported reports whether this platform can confine egress to the
// egress proxy: the command keeps its own network namespace, and this binary,
// run inside it as the egress bridge, is its one way out.
func EgressSupported() bool { return egressBridgeExe() != "" }

// EgressNeedsSocket reports whether the proxy must also listen on a Unix
// socket: a separate network namespace cannot reach the host's loopback.
func EgressNeedsSocket() bool { return true }

var egressBridgeExe = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
})

// egressBridged reports whether this launch reaches the network only through
// the bridge. A route with no socket is not bridged and keeps the namespace
// without one: egress stays shut rather than falling back to open.
func egressBridged(spec Spec) bool {
	return spec.Network && spec.Egress != nil
}

// egressBridgeArgs mounts the proxy socket read-only, hands the bridge its
// orders, and puts this binary in front of the command.
func egressBridgeArgs(spec Spec) []string {
	socket, exe := spec.Egress.SocketPath(), egressBridgeExe()
	if socket == "" || exe == "" {
		return nil
	}
	args := []string{"--ro-bind", socket, socket, "--setenv", egressBridgeEnv, strconv.Itoa(spec.Egress.Port()) + " " + socket}
	args = append(args, bwrapExecutableMountArgs([]string{exe})...)
	return append(args, exe)
}
