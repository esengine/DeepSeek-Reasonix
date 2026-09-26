package builtin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

// ConfineBash returns the bash built-in bound to an OS-sandbox spec, overriding
// the unconfined instance registered at init. When the spec enforces, bash runs
// each command through the sandbox (see package sandbox). guard appends a
// warning to command output when the command references Reasonix's own session
// stores (see SessionDataGuard).
//
// Session-private temporary directories are bound separately via
// BindSessionTemp (or Workspace.SessionTemp) so the timeout variadic form stays
// stable for existing callers.
func ConfineBash(spec sandbox.Spec, guard SessionDataGuard, timeout ...time.Duration) tool.Tool {
	shell := spec.Shell
	if shell.Path == "" {
		shell = sandbox.ResolveShell("", "", nil)
	}
	b := bash{sb: spec, shell: shell, guard: guard}
	if len(timeout) > 0 {
		b.timeout = timeout[0]
	}
	return b
}

// BindSessionTemp attaches a session-private temporary directory manager to a
// confined bash (and, when present, grep) tool. ok is false when tl is not a
// bash tool (including wrappers that do not unwrap).
func BindSessionTemp(tl tool.Tool, m *sessiontemp.Manager) (tool.Tool, bool) {
	switch t := tl.(type) {
	case bash:
		t.sessionTemp = m
		return t, true
	case grepTool:
		t.sessionTemp = m
		return t, true
	case writeFile:
		t.sessionTemp = m
		return t, true
	case editFile:
		t.sessionTemp = m
		return t, true
	case multiEdit:
		t.sessionTemp = m
		return t, true
	case moveFile:
		t.sessionTemp = m
		return t, true
	case notebookEdit:
		t.sessionTemp = m
		return t, true
	case deleteRange:
		t.sessionTemp = m
		return t, true
	case deleteSymbol:
		t.sessionTemp = m
		return t, true
	default:
		return nil, false
	}
}

// RebindBashWriteRoots returns a copy of bash with its complete write surface
// narrowed to roots. ok is false when tl is not a confined bash tool, when the
// sandbox is not enforcing (cannot honour narrower roots), or when roots is empty.
// Callers that wrap bash (e.g. foreground-only subagent wrappers) must unwrap
// before calling and re-wrap the result.
func RebindBashWriteRoots(tl tool.Tool, roots []string) (tool.Tool, bool) {
	b, ok := tl.(bash)
	if !ok || !b.sb.Enforce() {
		return nil, false
	}
	rs := realRoots(roots)
	if len(rs) == 0 {
		return nil, false
	}
	spec := b.sb
	spec.WriteRoots = rs
	// Sub-agent claims are strict capability boundaries. Do not add the normal
	// build-cache and temporary-directory allowances outside the claimed roots.
	spec.MinimalWrites = true
	// Do not inherit a wider AppContainer write lane from the parent workspace
	// confinement — the claim roots are the only allowed write surface.
	spec.AppContainerWriteRoots = append([]string(nil), rs...)
	b.sb = spec
	// sessionTemp is preserved: sub-agent write-root rebinding must not drop
	// the session-private temporary directory manager.
	return b, true
}

// ConfineWebFetch returns the web_fetch built-in bound to Reasonix proxy
// settings while preserving its SSRF-guarded dialer.
func ConfineWebFetch(proxySpec netclient.ProxySpec) tool.Tool {
	return webFetch{proxySpec: proxySpec}
}

// ConfineWriters returns the file-writing built-ins (write_file, edit_file,
// multi_edit, move_file, notebook_edit) bound to roots — the only directories they may
// modify. The composition root adds these to the per-run registry to override
// the unconfined instances registered at init time, so writes stay inside the
// workspace by default. roots may be relative; they are resolved to absolute,
// symlink-free paths once here. An empty roots slice yields unconfined writers.
// guard additionally rejects writes into Reasonix's own session stores even
// when the roots would allow them (see SessionDataGuard). managed names the
// Reasonix-owned config files writable outside the roots after a fresh human
// approval (see ManagedConfigPaths).
func ConfineWriters(roots []string, guard SessionDataGuard, managed ManagedConfigPaths) []tool.Tool {
	rs := realRoots(roots)
	return []tool.Tool{
		writeFile{roots: rs, guard: guard, managed: managed},
		editFile{roots: rs, guard: guard, managed: managed},
		multiEdit{roots: rs, guard: guard, managed: managed},
		moveFile{roots: rs, guard: guard, managed: managed},
		notebookEdit{roots: rs, guard: guard, managed: managed},
		deleteRange{roots: rs, guard: guard, managed: managed},
		deleteSymbol{roots: rs, guard: guard, managed: managed},
	}
}

// ConfineReaders returns the read/list/search built-ins (read_file, glob,
// ls, code_index) bound to forbidRoots — directories the agent may not read or list.
// grep is handled separately by ConfineSearch so it can carry the
// sandbox spec for its ripgrep subprocess.
// An empty forbidRoots slice yields unconfined readers.
func ConfineReaders(forbidRoots []string) []tool.Tool {
	rs := realRoots(forbidRoots)
	return []tool.Tool{
		readFile{forbidRoots: rs},
		listDir{forbidRoots: rs},
		globTool{forbidRoots: rs},
		codeIndex{forbidRoots: rs},
	}
}

// confineRead reports whether target is inside any forbidRoot, names a
// credential file, or — with [secrets] protect_sensitive_files on — matches the
// broader sensitive path denylist. An empty forbidRoots slice with the
// denylist off is unconfined (returns false). Callers should return a result
// that mimics the directory appearing empty, matching the tmpfs semantics the
// bubblewrap sandbox provides. Deny-side, so the check folds case on
// case-insensitive platforms (see withinFold): a case-variant of a forbidden
// path reaches the same bytes there.
func confineRead(forbidRoots []string, target string) bool {
	protect := secrets.ProtectSensitiveFiles()
	credentials := secrets.ProtectCredentialFiles()
	if len(forbidRoots) == 0 && !protect && !credentials {
		return false
	}
	abs, err := realPath(target)
	if err != nil {
		return false // can't resolve -> let the caller's normal error path handle it
	}
	return confineResolved(forbidRoots, abs, protect)
}

// confineResolved is confineRead for a path already free of symlinks.
func confineResolved(forbidRoots []string, abs string, protect bool) bool {
	if secrets.CredentialReadPath(abs) {
		return true
	}
	if protect && sensitiveReadPath(abs) {
		return true
	}
	for _, r := range forbidRoots {
		if withinFold(r, abs) {
			return true
		}
	}
	return false
}

func sensitiveReadPath(abs string) bool {
	clean := filepath.Clean(abs)
	name := strings.ToLower(filepath.Base(clean))
	switch name {
	case ".env", ".git-credentials", ".netrc":
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	for _, dir := range secrets.SensitiveHomeDirs() {
		if withinFold(dir, clean) {
			return true
		}
	}
	return false
}

// realRoots resolves each root to an absolute, symlink-free path, dropping any
// that cannot be made absolute. Resolving here (once) means the per-call check
// only has to resolve the target.
func realRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if real, err := realPath(r); err == nil {
			out = append(out, real)
		}
	}
	return out
}

// CodeWriteOutsideScope identifies a file-tool write refused for landing outside
// workspace_root and allow_write. The file tools enforce it on every platform,
// whether or not an OS sandbox exists, so it is never a sandbox refusal.
const CodeWriteOutsideScope = "workspace.write_outside_scope"

// confine reports an error when target resolves outside every root. An empty
// roots slice is unconfined (returns nil) — the safe default for the built-in
// templates before a run configures the workspace. The refusal carries
// CodeWriteOutsideScope; its text names the path, the roots and the setting.
func confine(roots []string, target string) error {
	if len(roots) == 0 {
		return nil
	}
	abs, err := realPath(target)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", target, err)
	}
	for _, r := range roots {
		if within(r, abs) {
			return nil
		}
	}
	return tool.Refusal{Code: CodeWriteOutsideScope, Message: fmt.Sprintf(
		"write refused: %q is outside the workspace write scope; file tools may write only under %s. "+
			"This scope is enforced by Reasonix's file tools on every platform: it is not an OS sandbox, and it does not depend on whether shell commands are sandboxed. "+
			"Write inside those directories, or ask the user to add the target's directory to the extra writable directories "+
			"(Settings > Sandbox > Also writable, stored as allow_write under [sandbox] in reasonix.toml) or to move workspace_root",
		target, strings.Join(roots, ", "))}
}

// confineWrite is the write-tool boundary check: workspace confinement first,
// then the session-data guard, so a write can be inside the roots (e.g. a
// home-directory workspace covering the state root) and still be refused when
// it targets Reasonix's own session stores. A target outside every root that
// matches a Reasonix-managed config file (see ManagedConfigPaths) may proceed
// after a fresh per-write human approval carried on ctx; without an approver it
// fails closed with the original confinement error semantics.
func confineWrite(ctx context.Context, roots []string, guard SessionDataGuard, managed ManagedConfigPaths, temp *sessiontemp.Manager, target string) error {
	confineErr := confine(roots, target)
	if confineErr == nil || underSessionTemp(temp, target) {
		return guard.Check(target)
	}
	if !managed.Match(target) {
		return withSessionTempAlternative(temp, confineErr)
	}
	if err := guard.Check(target); err != nil {
		return err
	}
	return managed.approve(ctx, target)
}

// withSessionTempAlternative names the one writable place outside the roots, so
// a refused scratch write has somewhere to go that is not the repository.
func withSessionTempAlternative(temp *sessiontemp.Manager, err error) error {
	dir := temp.Dir()
	if dir == "" {
		return err
	}
	suffix := fmt.Sprintf("; scratch files may also go under $TMPDIR (%s)", dir)
	var refusal tool.Refusal
	if errors.As(err, &refusal) {
		refusal.Message += suffix
		return refusal
	}
	return fmt.Errorf("%w%s", err, suffix)
}

// underSessionTemp reports whether target sits in the session's own temporary
// directory. Bash already writes there — it is where $TMPDIR points — so a
// writer refusing it sends a run that was told to keep scratch out of the
// repository back into the repository, which one observed run did.
func underSessionTemp(temp *sessiontemp.Manager, target string) bool {
	dir := temp.Dir()
	if dir == "" {
		return false
	}
	roots := realRoots([]string{dir})
	return len(roots) > 0 && confine(roots, target) == nil
}

// confinePreview mirrors confineWrite for ctx-less diff previews: they read the
// target to render a diff but never write, so a managed config file passes
// without the per-write approval — Execute still gates the actual write.
func confinePreview(roots []string, guard SessionDataGuard, managed ManagedConfigPaths, temp *sessiontemp.Manager, target string) error {
	confineErr := confine(roots, target)
	if confineErr == nil || underSessionTemp(temp, target) {
		return guard.Check(target)
	}
	if !managed.Match(target) {
		return confineErr
	}
	return guard.Check(target)
}

// realPath resolves path to an absolute, symlink-free form. Because a write
// target need not exist yet (write_file creates it), it resolves the deepest
// existing ancestor with EvalSymlinks and re-appends the not-yet-existing tail.
// This stops a symlinked directory from smuggling a write outside a root.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	tail := ""
	cur := abs
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, tail), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil // nothing along the path exists; use the cleaned abs
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// within reports whether path is at or below root. Both must be absolute,
// cleaned, symlink-free. It uses filepath.Rel so it is correct across volumes
// and is not fooled by a prefix that only matches a partial path component
// (e.g. /work-other is not within /work).
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// foldPaths reports whether deny-side path checks on this platform must ignore
// case: the default filesystems on Windows (NTFS) and macOS (APFS/HFS+) are
// case-insensitive, so /X/SESSIONS and /x/sessions reach the same bytes and a
// case-variant must not slip past a deny rule. EvalSymlinks does NOT normalize
// case, so realPath alone cannot be relied on for this.
var foldPaths = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// withinFold is within with platform case folding, for DENY-side checks only
// (forbid-read roots, the session-data guard). Allow-side checks (confine)
// keep the exact within: folding an allow rule on a case-sensitive filesystem
// would wave a genuinely different directory through, whereas folding a deny
// rule only ever refuses more. On a case-sensitive macOS volume this can
// refuse a legitimate same-letters-different-case path; the error text points
// at allow_write / forbid_read config as the way out.
func withinFold(root, path string) bool {
	if foldPaths {
		return within(strings.ToLower(root), strings.ToLower(path))
	}
	return within(root, path)
}
