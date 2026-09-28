package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	for _, p := range forbidWriteDirs(spec.ProtectedWriteRoots) {
		fmt.Fprintf(&b, "(deny file-write* (subpath %s))\n", sbplString(p))
	}
	for _, p := range explicitProtectedAllowDirs(spec) {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplString(p))
	}
	// Last write rules: SBPL takes the final match, so no allowance re-opens them.
	writeGitMetadataRules(&b, gitMetadataForSpec(spec))
	return b.String()
}

// writeGitMetadataRules denies writes to protected Git metadata. A pin denies
// only removing or renaming the entry and planting a symlink there, so the
// entry's own mode and times stay writable. Worktree and submodule gitdirs
// that exist get exact rules up to a limit; patterns cover the rest and any
// created later, so the profile stays bounded however many a repository holds.
func writeGitMetadataRules(b *strings.Builder, meta gitMetadata) {
	paths := meta.Paths
	var overWorktrees []string
	for _, common := range meta.Commons {
		g := gitGroupsOf(common, gitGroupMaxEntries)
		paths = append(paths, g.Paths...)
		if g.WorktreesOver {
			overWorktrees = append(overWorktrees, common)
		}
	}
	for _, p := range paths {
		switch {
		case p.Pin:
			fmt.Fprintf(b, "(deny file-write-unlink (literal %s))\n", sbplString(p.Path))
			fmt.Fprintf(b, "(deny file-write-create (require-all (literal %s) (vnode-type SYMLINK)))\n", sbplString(p.Path))
		case p.Tree:
			fmt.Fprintf(b, "(deny file-write* (subpath %s))\n", sbplString(p.Path))
		default:
			fmt.Fprintf(b, "(deny file-write* (literal %s))\n", sbplString(p.Path))
		}
	}
	for _, common := range meta.Commons {
		c := sbplRegexQuote(common)
		modules := "^" + c + "/modules/(" + gitGroupSegment + "/)*"
		patterns := []string{
			"^" + c + "/worktrees/[^/]+/(config|config[.]worktree)$",
			modules + "(config|config[.]worktree|commondir)$",
			modules + "hooks(/.*)?$",
		}
		if slices.Contains(overWorktrees, common) {
			patterns = append(patterns, "^"+c+"/worktrees/[^/]+/commondir$")
		}
		for _, re := range patterns {
			fmt.Fprintf(b, "(deny file-write* (regex %s))\n", sbplString(re))
		}
		fmt.Fprintf(b, "(deny file-write-create (require-all (regex %s) (vnode-type SYMLINK)))\n", sbplString("^"+c+"/(modules|worktrees)/"))
	}
}

// gitGroupSegment matches one path segment other than refs and logs, so the
// submodule patterns do not catch a branch, tag or reflog named config or hooks.
const gitGroupSegment = `([^/rl][^/]*|r|re|ref|r[^/e][^/]*|re[^/f][^/]*|ref[^/s][^/]*|refs[^/]+|l|lo|log|l[^/o][^/]*|lo[^/g][^/]*|log[^/s][^/]*|logs[^/]+)`

// sbplRegexQuote escapes a path for a Seatbelt regex, whose metacharacters a
// directory name may legally contain.
func sbplRegexQuote(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func writableDirsForSpec(spec Spec) []string { return writeAllowDirsForSpec(spec) }

func forbidWriteDirs(roots []string) []string {
	return forbidReadDirs(roots)
}

func explicitProtectedAllowDirs(spec Spec) []string {
	protected := forbidWriteDirs(spec.ProtectedWriteRoots)
	if len(protected) == 0 {
		return nil
	}
	stateRoot := singleProtectedStateRoot(protected)
	var out []string
	for _, root := range writeAllowDirsForSpec(spec) {
		if stateRoot != "" && IsProtectedWritePath(root, stateRoot) {
			continue
		}
		for _, prot := range protected {
			if root != prot && PathWithin(prot, root) {
				out = append(out, root)
				break
			}
		}
	}
	return out
}

// writeAllowDirs is the deduplicated, symlink-resolved set of directories the
// sandbox permits writes to: the caller's roots plus temp dirs, /dev, and the
// common toolchain caches under $HOME. Symlinks are resolved because macOS's
// /tmp and $TMPDIR live under /private, which is the path Seatbelt matches.
func writeAllowDirs(roots []string) []string {
	return writeAllowDirsForSpec(Spec{WriteRoots: roots})
}

func writeAllowDirsForSpec(spec Spec) []string {
	if spec.ReadOnly {
		// Preserve device compatibility without granting host file writes.
		return []string{"/dev/null"}
	}
	return darwinWritePlan(spec).dirs
}

func gitMetadataRoots(spec Spec) []string {
	if spec.ReadOnly {
		return nil
	}
	return darwinWritePlan(spec).callers
}

// darwinWritePlan resolves the caller's roots and the directories Seatbelt
// allows beside them; the session temp stays writable under MinimalWrites.
func darwinWritePlan(spec Spec) writeRootPlan {
	extras := []string{"/dev", spec.SessionTemp}
	if !spec.MinimalWrites {
		extras = append(extras, hostWriteDirs()...)
	}
	return planWriteRoots(spec.WriteRoots, extras, spec.SessionTemp)
}

// hostWriteDirs are the temp and toolchain cache directories a non-minimal
// launch may write: go build/test use Library/Caches and go, pip and others
// .cache, and npm and cargo their own.
func hostWriteDirs() []string {
	dirs := []string{"/tmp", "/private/tmp", "/private/var/folders", os.TempDir()}
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{"Library/Caches", ".cache", ".npm", ".cargo", "go"} {
			dirs = append(dirs, filepath.Join(home, sub))
		}
	}
	return dirs
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

// HostWritableDirs lists the host directories any jailed command may write
// besides its write roots: temporary directories and toolchain caches.
func HostWritableDirs() []string { return writeAllowDirsForSpec(Spec{}) }
