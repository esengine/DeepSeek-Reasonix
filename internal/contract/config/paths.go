package config

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	runtimeGOOS     = runtime.GOOS
	osUserHomeDir   = os.UserHomeDir
	osUserConfigDir = func() string {
		dir, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		return dir
	}
	osUserCacheDir = func() string {
		dir, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		return dir
	}
)

func (r Roots) userConfigPath() string {
	dir := r.userConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

func (r Roots) userConfigDir() string { return r.Home() }

// UserConfigPath is this binding's user config.toml.
func (r Roots) UserConfigPath() string { return r.userConfigPath() }

// UserConfigLoadPath is the user file a load actually reads: the primary path
// when it exists, else the legacy one. A reader of one table of it must use this
// so it and the full loader agree which file is authoritative.
func (r Roots) UserConfigLoadPath() string { return r.userConfigLoadPath() }

// Home is the Reasonix home this binding resolves to.
func (r Roots) Home() string { return r.Dir(RootHome) }

func (r Roots) userConfigLoadPath() string {
	primary := r.userConfigPath()
	if primary == "" {
		return r.legacyUserConfigPath()
	}
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	if legacy := r.legacyUserConfigPath(); legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	for _, legacy := range r.legacyXDGConfigPaths() {
		if legacy == "" || samePath(legacy, primary) {
			continue
		}
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

func (r Roots) legacyUserConfigPath() string {
	dir := r.legacyOSSupportDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "config.toml")
	if primary := r.userConfigPath(); primary != "" && samePath(path, primary) {
		return ""
	}
	return path
}

func (r Roots) userConfigCandidatePaths() []string {
	var paths []string
	if p := r.userConfigPath(); p != "" {
		paths = append(paths, p)
	}
	if p := r.legacyUserConfigPath(); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, r.legacyXDGConfigPaths()...)
	return paths
}

func (r Roots) legacyXDGConfigPaths() []string {
	if r.pinnedHomeDir() != "" {
		return nil
	}
	if runtimeGOOS == "windows" {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if dir := cleanEnvDir("XDG_CONFIG_HOME"); dir != "" {
		add(filepath.Join(dir, "reasonix", "config.toml"))
	}
	if home, err := osUserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".config", "reasonix", "config.toml"))
	}
	return paths
}

func (r Roots) userSupportDir() string { return r.Dir(RootState) }

func (r Roots) legacyOSSupportDir() string {
	if r.pinnedHomeDir() != "" {
		return ""
	}
	dir := osUserConfigDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "reasonix")
	if current := r.Home(); current != "" && samePath(path, current) {
		return ""
	}
	return path
}

func (r Roots) userCacheDir() string { return r.Dir(RootCache) }

func cleanEnvDir(name string) string {
	return expandDirValue(os.Getenv(name))
}

// expandDirValue turns a directory someone wrote — in the environment or in
// config.toml — into an absolute path. Both layers of the root chain share it,
// so a location is spelled the same way wherever it is set.
func expandDirValue(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	dir = ExpandVars(dir)
	if dir == "~" {
		if home, err := osUserHomeDir(); err == nil && home != "" {
			dir = home
		}
	} else if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home, err := osUserHomeDir(); err == nil && home != "" {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Clean(dir)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, aerr := filepath.Abs(a)
	bb, berr := filepath.Abs(b)
	if aerr == nil {
		a = aa
	}
	if berr == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// IsolatedHomeDir returns the REASONIX_HOME directory when it has been
// explicitly set via the environment variable. A non-empty return signals a
// self-contained runtime that must not fall back to legacy OS-default data
// paths or import data from the system-wide production install.
func IsolatedHomeDir() string { return processRoots().pinnedHomeDir() }

// IsolatedStateDir returns the state root when REASONIX_STATE_HOME explicitly
// set it. That root owns sessions, archive, stats and projects, so a run that
// redirected it asked for its own copies of exactly what the legacy importers
// copy in — and importing the production install's would defeat the redirect.
func IsolatedStateDir() string {
	return cleanEnvDir("REASONIX_STATE_HOME")
}

// userConfigDisplayPath is userConfigPath collapsed to a ~-relative form for
// comments rendered into the user's own config.toml, so Windows users see the
// real location instead of a hardcoded ~/.reasonix path.
func (r Roots) userConfigDisplayPath() string {
	p := r.userConfigPath()
	if p == "" {
		return "<os-config-dir>/reasonix/config.toml"
	}
	if home, err := osUserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

// UserConfigPath is the user-global config.toml. It lives under Reasonix home:
// REASONIX_HOME/config.toml, then ~/.reasonix/config.toml on Unix-like systems,
// or %AppData%/reasonix/config.toml on Windows. If %AppData% is unavailable on
// Windows, it falls back to %USERPROFILE%/AppData/Roaming/reasonix/config.toml.
// "" when the user config dir can't be resolved.
func UserConfigPath() string { return processRoots().userConfigPath() }

// LegacyUserConfigPath is the old OS app-support config.toml path when it
// differs from UserConfigPath. It is read as a compatibility fallback when the
// primary user config does not exist.
func LegacyUserConfigPath() string { return processRoots().legacyUserConfigPath() }

// LegacyUserConfigPaths returns every known legacy user config path that differs
// from the current v1.8.1 Reasonix-home config path.
func LegacyUserConfigPaths() []string { return processRoots().legacyUserConfigPaths() }

func (r Roots) legacyUserConfigPaths() []string {
	primary := r.userConfigPath()
	var out []string
	add := func(path string) {
		if path == "" || samePath(path, primary) {
			return
		}
		for _, existing := range out {
			if samePath(existing, path) {
				return
			}
		}
		out = append(out, path)
	}
	add(r.legacyUserConfigPath())
	for _, path := range r.legacyXDGConfigPaths() {
		add(path)
	}
	return out
}

// ReasonixManagedConfigPaths returns the Reasonix-owned user configuration
// FILES that model-driven tools may repair on the user's request, each gated
// by a fresh per-write human approval: the current config.toml, compatibility
// TOML locations, and the legacy v0.x ~/.reasonix/config.json. Individual
// files, never directories — the Reasonix home also holds credentials (.env),
// global hooks (settings.json), skills, and session stores, and none of those
// may ride along on a config repair.
func ReasonixManagedConfigPaths() []string { return processRoots().managedConfigPaths() }

func (r Roots) managedConfigPaths() []string {
	var out []string
	out = appendUniquePath(out, r.userConfigPath())
	for _, path := range r.legacyUserConfigPaths() {
		out = appendUniquePath(out, path)
	}
	out = appendUniquePath(out, r.legacyConfigPath())
	return out
}

func appendUniquePath(paths []string, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return paths
	}
	clean := filepath.Clean(path)
	for _, existing := range paths {
		if samePath(existing, clean) {
			return paths
		}
	}
	return append(paths, clean)
}

// ReasonixHomeDir is the current Reasonix home directory. It honors
// REASONIX_HOME, then uses ~/.reasonix on macOS/Linux or %APPDATA%/reasonix on
// Windows, with a %USERPROFILE%/AppData/Roaming fallback when %APPDATA% is
// unavailable.
func ReasonixHomeDir() string { return processRoots().Home() }

// RemoteStateDir is local state for the remote-SSH module (the managed
// known_hosts file, cached host metadata): <Reasonix home>/remote. Routed
// through the home resolver so REASONIX_HOME isolation holds.
func RemoteStateDir() string { return processRoots().RemoteStateDir() }

// ScheduleDir is where the scheduled-runs store lives: machine-wide, under the
// state root, and never inside a project.
func (r Roots) ScheduleDir() string {
	dir := r.userSupportDir()
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "schedules")
}

// RemoteStateDir is <home>/remote under these roots. A serve launched over SSH
// keeps its launch token there, so runtime sandboxes deny reading it.
func (r Roots) RemoteStateDir() string {
	home := r.Home()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "remote")
}

// RemoteKnownHostsPath is the Reasonix-managed known_hosts file (OpenSSH
// format) that records TOFU-accepted host keys. The user's own
// ~/.ssh/known_hosts is only ever read, never written.
func RemoteKnownHostsPath() string {
	dir := RemoteStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "known_hosts")
}

// MissingReasoningWarnStateDir is the shared directory for the rate-limited
// missing tool-call thinking recovery gate (#7059): <Reasonix home>/state. The
// legacy name preserves callers and the existing state-file contract. Routed
// through the home resolver so REASONIX_HOME isolation holds.
func MissingReasoningWarnStateDir() string {
	home := processRoots().Home()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "state")
}

// WorkspaceLeaseDir stores cross-process Delivery writer locks outside user
// workspaces. It intentionally follows the cache root rather than project or
// session state: taking a lease must never dirty the repository it protects.
func WorkspaceLeaseDir() string {
	return joinRoot(storageRootDir(RootLocks), "workspace-leases")
}

// RepairMutationLockDir stores target-path repair locks in the OS-user cache.
// It deliberately ignores Reasonix home/cache overrides: isolated instances
// can still repair the same project reasonix.toml, so their locks must converge.
func RepairMutationLockDir() string {
	return joinRoot(storageRootDir(RootLocks), "repair-mutation-locks")
}

// DeliveryWorktreeDir is durable storage for user-visible isolated Delivery
// workspaces. Explicit state/home overrides remain authoritative. Windows uses
// LocalAppData by default so large Git worktrees do not roam with the user's
// profile; other platforms keep using Reasonix state storage.
func DeliveryWorktreeDir() string { return processRoots().Dir(RootWorktrees) }

// UserCredentialsPath is the reasonix-owned global .env file under Reasonix
// home. It is the single source for provider credentials saved by Reasonix, so
// stale shell, Windows, project, or home env vars cannot silently override keys
// the user saved through setup or settings. "" when Reasonix home can't be
// resolved.
func UserCredentialsPath() string { return processRoots().UserCredentialsPath() }

// UserCredentialsPath is the credentials .env this binding resolves to.
func (r Roots) UserCredentialsPath() string {
	dir := r.Home()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".env")
}

// credentialsLocationForError names the file a missing key is missing from.
// Resolution reads only this file, never the process environment, so an error
// naming the env var alone sends a caller who exported it hunting for a
// mechanism that was never consulted.
func (r Roots) credentialsLocationForError() string {
	if p := r.UserCredentialsPath(); p != "" {
		return p
	}
	return "Reasonix's credentials file (.env under the state home)"
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user state
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string { return processRoots().ArchiveDir() }

// ArchiveDir is the compaction archive this binding resolves to.
func (r Roots) ArchiveDir() string {
	dir := r.userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `reasonix --continue` / `--resume` to find the recent ones. Empty
// if the user state dir can't be resolved — sessions then aren't saved.
func SessionDir() string { return processRoots().SessionDir() }

// SessionDir is the transcript store this binding resolves to.
func (r Roots) SessionDir() string {
	dir := r.userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sessions")
}

// StatsDir is where usage statistics are persisted (one .jsonl per day, e.g.
// stats/2026-08-02.jsonl). It lives under the user state root — not the install
// directory, which is typically read-only and replaced on upgrade — so usage
// records survive app updates. Empty if the user state dir can't be resolved,
// in which case usage accounting is skipped.
func StatsDir() string { return processRoots().StatsDir() }

// StatsDir is the usage store this binding resolves to.
func (r Roots) StatsDir() string {
	dir := r.userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "stats")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <state root>/projects/<slug>/sessions. Empty when either the state root
// or workspaceRoot doesn't resolve.
func ProjectSessionDir(workspaceRoot string) string {
	base := processRoots().MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

// WorkspaceSlug flattens an absolute workspace path into the directory name
// used under <config root>/projects. Windows spells the same folder with
// varying case (drive-letter case, Explorer renames), so the slug folds case
// there — matching sessionstore.CanonicalSessionPath's key form — or equivalent
// spellings of one workspace would produce distinct slug strings. Existing
// mixed-case slug directories need no migration: NTFS resolves names
// case-insensitively, so the folded slug opens the same directory.
func WorkspaceSlug(absPath string) string {
	if runtimeGOOS == "windows" {
		absPath = strings.ToLower(absPath)
	}
	slug := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-").Replace(absPath)
	return boundFilenameComponent(slug, 255)
}

// boundFilenameComponent caps a derived filename component at the common
// per-component filesystem limit (255 bytes on ext4/APFS/NTFS). maxLen is the
// byte budget for this component (path segments pass 255; names that gain an
// extension pass 255 minus the extension length). Inputs at or under the
// budget pass through byte-identical — every component that ever existed on
// disk is under the budget, or it could not have been created — so existing
// directories and files keep resolving. Only inputs that would previously
// have failed with ENAMETOOLONG are truncated, with an FNV-1a hash of the
// full input appended so distinct deep paths cannot collapse to one name.
func boundFilenameComponent(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	budget := maxLen - 17 // room for "-" + 16 hex digits
	prefix := s[:budget]
	// Back off to a rune boundary so a multi-byte character is never split.
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return fmt.Sprintf("%s-%016x", prefix, h.Sum64())
}

// BoundFilenameComponent is the exported form for sibling packages deriving
// filename components from unbounded input. maxLen is the byte budget for the
// component (pass 255 for a bare path segment; subtract the extension length
// when one will be appended).
func BoundFilenameComponent(s string, maxLen int) string {
	return boundFilenameComponent(s, maxLen)
}

// CacheDir is the per-user cache root for derived/regenerable artefacts: MCP
// handshake snapshots, plugin startup-latency telemetry. Empty when the OS dir is
// unavailable — callers must tolerate that (caching is best-effort).
func CacheDir() string { return processRoots().CacheDir() }

// CacheDir is the derived-artefact root this binding resolves to.
func (r Roots) CacheDir() string { return r.userCacheDir() }

// MemoryUserDir returns the reasonix user state root (…/reasonix), under which
// the user-global REASONIX.md and the per-project auto-memory store live. Empty
// when the user state dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string { return processRoots().MemoryUserDir() }

// MemoryUserDir is the user state root this binding resolves to.
func (r Roots) MemoryUserDir() string { return r.userSupportDir() }

// ConventionDirs are the parent directories scanned for agent assets (skills,
// commands), in canonical-first order. .reasonix is ours; .agents / .agent /
// .claude let users drop in assets authored for other agent tools without moving
// files. Shared so skills (internal/ext/skill) and commands (CommandDirs) discover
// the same set. Note: hooks are NOT scanned across these — a .claude/settings.json
// uses a different hook schema that can't be parsed as ours, so hooks stay in
// .reasonix/settings.json (see internal/ext/hook).
var ConventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// conventionSubdirsAsc joins sub under each ConventionDir of base, in ascending
// priority (reverse of ConventionDirs) so the canonical .reasonix ends up the
// highest-priority entry — command.Load lets a later directory win on a clash.
func conventionSubdirsAsc(base, sub string) []string {
	out := make([]string, 0, len(ConventionDirs))
	for _, v := range slices.Backward(ConventionDirs) {
		out = append(out, filepath.Join(base, v, sub))
	}
	return out
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global dirs are
// unchanged — they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	roots := processRoots().CommandRootsForRoot(root)
	dirs := make([]string, 0, len(roots))
	for _, spec := range roots {
		dirs = append(dirs, spec.Path)
	}
	return dirs
}

// CommandRootsForRoot is the ownership-aware form of CommandDirsForRoot.
// Plugin roots retain their package name so the loader can expose stable,
// package-qualified command names and hidden short-name compatibility aliases.
// CommandDir is one directory custom slash commands are read from, and the
// plugin package that contributed it ("" for the person's own).
type CommandDir struct {
	Path   string
	Plugin string
}

func CommandRootsForRoot(root string) []CommandDir { return processRoots().CommandRootsForRoot(root) }

// CommandRootsForRoot resolves the command roots against this binding.
func (r Roots) CommandRootsForRoot(root string) []CommandDir {
	root = resolveRoot(root)
	var roots []CommandDir
	add := func(spec CommandDir) {
		if spec.Path == "" {
			return
		}
		for _, existing := range roots {
			if samePath(existing.Path, spec.Path) && existing.Plugin == spec.Plugin {
				return
			}
		}
		roots = append(roots, spec)
	}
	// Enabled plugin packages contribute command dirs before user/project dirs,
	// so explicit commands still win exact canonical-name clashes.
	for _, spec := range r.pluginPackageCommandRoots() {
		add(spec)
	}
	if dir := r.legacyOSSupportDir(); dir != "" {
		add(CommandDir{Path: filepath.Join(dir, "commands")})
	}
	for _, legacy := range r.legacyXDGConfigPaths() {
		add(CommandDir{Path: filepath.Join(filepath.Dir(legacy), "commands")})
	}
	if home, err := osUserHomeDir(); err == nil {
		for _, dir := range conventionSubdirsAsc(home, "commands") {
			add(CommandDir{Path: dir})
		}
	}
	if dir := r.userConfigDir(); dir != "" {
		add(CommandDir{Path: filepath.Join(dir, "commands")})
	}
	if dir := r.userSupportDir(); dir != "" && !samePath(dir, r.userConfigDir()) {
		add(CommandDir{Path: filepath.Join(dir, "commands")})
	}
	for _, dir := range conventionSubdirsAsc(root, "commands") {
		add(CommandDir{Path: dir})
	}
	return roots
}

// SourcePath returns the highest-priority config file that exists, or "" if none.
func SourcePath() string {
	return SourcePathForRoot(".")
}

// SourcePathForRoot returns the highest-priority config file that exists under
// root, or "" if none. Equivalent to SourcePath() when root is ".".
func SourcePathForRoot(root string) string { return processRoots().SourcePathForRoot(root) }

// SourcePathForRoot resolves the highest-priority config file against this
// binding.
func (r Roots) SourcePathForRoot(root string) string {
	root = resolveRoot(root)
	projectTOML := ProjectConfigPath(root)
	if _, err := os.Stat(projectTOML); err == nil {
		return projectTOML
	}
	if uc := r.userConfigLoadPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	return ""
}

// projectConfigFile is the project config filename, resolved against the
// project root.
const projectConfigFile = "reasonix.toml"

// ProjectConfigPath returns the project config file for root: the bare
// filename when root resolves to ".", otherwise reasonix.toml under root.
func ProjectConfigPath(root string) string {
	root = resolveRoot(root)
	if root == "." {
		return projectConfigFile
	}
	return filepath.Join(root, projectConfigFile)
}
