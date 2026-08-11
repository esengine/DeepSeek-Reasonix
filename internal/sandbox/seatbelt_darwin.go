package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Command returns the argv to run `command` through sh, wrapped in sandbox-exec
// when the spec enforces and the tool is available. The second return is whether
// wrapping happened; false means the argv is unwrapped (sandbox off, or
// sandbox-exec missing). Callers decide whether an unwrapped command is allowed.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.Enforce() || !Available() {
		return sh.argv(command), false
	}
	return append([]string{"sandbox-exec", "-p", seatbeltProfile(spec)}, sh.argv(command)...), true
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string. The args are appended directly after the sandbox prefix
// without shell interpretation — suitable for direct binary invocations like
// ripgrep that don't need a shell wrapper.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	if !spec.Enforce() || !Available() {
		return args, false
	}
	return append([]string{"sandbox-exec", "-p", seatbeltProfile(spec)}, args...), true
}

// sandboxExecUsability caches the probe result per resolved binary path, so
// repeated Available() calls stay O(1) after the first check.
var sandboxExecUsability sync.Map // resolved executable path -> bool

const (
	sandboxExecProbeTimeout = 10 * time.Second
	sandboxExecProbeCommand = "/usr/bin/true"
)

// usableSandboxExec distinguishes an installed sandbox-exec from a usable
// Seatbelt backend. On restricted macOS hosts, sandbox-exec can be on PATH
// while sandbox_apply fails with exit 71. Probe that operation directly with a
// minimal profile, mirroring usableBwrap on Linux.
func usableSandboxExec() bool {
	path, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return false
	}
	return usableSandboxExecPath(path)
}

func usableSandboxExecPath(path string) bool {
	if path == "" {
		return false
	}
	if cached, ok := sandboxExecUsability.Load(path); ok {
		return cached.(bool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sandboxExecProbeTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, path, "-p", "(version 1)(allow default)", sandboxExecProbeCommand).Run()
	// A slow host should not permanently poison the process-local cache with a
	// transient timeout. Definitive probe failures (including exit 71) remain
	// cached so every command does not pay the failed probe cost.
	if ctx.Err() != nil {
		return false
	}
	usable := err == nil
	actual, _ := sandboxExecUsability.LoadOrStore(path, usable)
	return actual.(bool)
}

// Available reports whether the OS sandbox backend can actually confine
// processes. macOS probes sandbox-exec; Linux verifies bubblewrap can enter its
// namespace (see seatbelt_other.go).
func Available() bool {
	return usableSandboxExec()
}

// seatbeltProfile builds an SBPL profile that allows everything, then denies
// all file writes and re-allows them only under the write-roots (workspace +
// temp + caches). Network is denied unless allowed. Forbid-read roots get
// individual deny-read rules. Reads elsewhere are left open so the
// toolchain (compilers reading GOROOT, git reading ~/.gitconfig, …) keeps
// working — the boundary this draws is "can't write outside the configured
// writable roots, and optionally can't talk to the network", which is the Phase
// 0 blast-radius made to also cover arbitrary shell commands.
func seatbeltProfile(spec Spec) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	for _, p := range writeAllowDirsForSpec(spec) {
		fmt.Fprintf(&b, "    (subpath %s)\n", sbplString(p))
	}
	b.WriteString(")\n")
	// Deny reads under forbid-read roots so even a permitted shell command
	// cannot peek at them through the OS sandbox. Each path gets its own deny
	// rule; (allow default) above keeps reads working everywhere else.
	for _, p := range forbidReadDirs(spec.ForbidReadRoots) {
		fmt.Fprintf(&b, "(deny file-read* (subpath %s))\n", sbplString(p))
	}
	if !spec.Network {
		b.WriteString("(deny network*)\n")
	}
	return b.String()
}

// writeAllowDirs is the deduplicated, symlink-resolved set of directories the
// sandbox permits writes to: the caller's roots plus temp dirs, /dev, and the
// common toolchain caches under $HOME. Symlinks are resolved because macOS's
// /tmp and $TMPDIR live under /private, which is the path Seatbelt matches.
func writeAllowDirs(roots []string) []string {
	return writeAllowDirsForSpec(Spec{WriteRoots: roots})
}

func writeAllowDirsForSpec(spec Spec) []string {
	roots := spec.WriteRoots
	dirs := append([]string{}, roots...)
	dirs = append(dirs, "/dev")
	if dir := strings.TrimSpace(spec.SessionTemp); dir != "" {
		// Session-private temporary directory must be writable under Seatbelt
		// even when MinimalWrites omits the broad host temp allowances.
		dirs = append(dirs, dir)
	}
	if !spec.MinimalWrites {
		dirs = append(dirs, "/tmp", "/private/tmp", "/private/var/folders", os.TempDir())
	}
	if !spec.MinimalWrites {
		if home, err := os.UserHomeDir(); err == nil {
			// go build/test → Library/Caches + go; pip/etc → .cache; npm/cargo too.
			for _, sub := range []string{"Library/Caches", ".cache", ".npm", ".cargo", "go"} {
				dirs = append(dirs, filepath.Join(home, sub))
			}
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
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}

// sbplString quotes a path as an SBPL string literal, escaping backslash and
// double-quote so a path can't break out of the profile syntax.
func sbplString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// forbidReadDirs resolves forbid-read roots to absolute, symlink-free paths so
// Seatbelt matches the canonical on-disk location (e.g. /private/tmp for /tmp).
func forbidReadDirs(roots []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(roots))
	for _, d := range roots {
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
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}
