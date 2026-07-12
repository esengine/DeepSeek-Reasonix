package config

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"reasonix/internal/command"
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
)

func userConfigPath() string {
	dir := userConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

func userConfigDir() string {
	return reasonixHomeDir()
}

func reasonixHomeDir() string {
	if dir := cleanEnvDir("REASONIX_HOME"); dir != "" {
		return dir
	}
	if runtimeGOOS == "windows" {
		if dir := osUserConfigDir(); dir != "" {
			return filepath.Join(dir, "reasonix")
		}
		if home, err := osUserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "AppData", "Roaming", "reasonix")
		}
		return ""
	}
	if home, err := osUserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".reasonix")
	}
	if dir := osUserConfigDir(); dir != "" {
		return filepath.Join(dir, "reasonix")
	}
	return ""
}

func userConfigLoadPath() string {
	primary := userConfigPath()
	if primary == "" {
		return legacyUserConfigPath()
	}
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	if legacy := legacyUserConfigPath(); legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	for _, legacy := range legacyXDGConfigPaths() {
		if legacy == "" || samePath(legacy, primary) {
			continue
		}
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

func legacyUserConfigPath() string {
	dir := legacyOSSupportDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "config.toml")
	if primary := userConfigPath(); primary != "" && samePath(path, primary) {
		return ""
	}
	return path
}

func userConfigCandidatePaths() []string {
	var paths []string
	if p := userConfigPath(); p != "" {
		paths = append(paths, p)
	}
	if p := legacyUserConfigPath(); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, legacyXDGConfigPaths()...)
	return paths
}

func legacyXDGConfigPaths() []string {
	if IsolatedHomeDir() != "" {
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

func userSupportDir() string {
	if dir := cleanEnvDir("REASONIX_STATE_HOME"); dir != "" {
		return dir
	}
	return reasonixHomeDir()
}

func legacyOSSupportDir() string {
	if IsolatedHomeDir() != "" {
		return ""
	}
	dir := osUserConfigDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "reasonix")
	if current := reasonixHomeDir(); current != "" && samePath(path, current) {
		return ""
	}
	return path
}

func userCacheDir() string {
	if dir := cleanEnvDir("REASONIX_CACHE_HOME"); dir != "" {
		return dir
	}
	if dir := cleanEnvDir("REASONIX_HOME"); dir != "" {
		return filepath.Join(dir, "cache")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix")
}

func cleanEnvDir(name string) string {
	dir := strings.TrimSpace(os.Getenv(name))
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
func IsolatedHomeDir() string {
	return cleanEnvDir("REASONIX_HOME")
}

// userConfigDisplayPath is userConfigPath collapsed to a ~-relative form for
// comments rendered into the user's own config.toml, so Windows users see the
// real location instead of a hardcoded ~/.reasonix path.
func userConfigDisplayPath() string {
	p := userConfigPath()
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
func UserConfigPath() string { return userConfigPath() }

// LegacyUserConfigPath is the old OS app-support config.toml path when it
// differs from UserConfigPath. It is read as a compatibility fallback when the
// primary user config does not exist.
func LegacyUserConfigPath() string { return legacyUserConfigPath() }

// LegacyUserConfigPaths returns every known legacy user config path that differs
// from the current v1.8.1 Reasonix-home config path.
func LegacyUserConfigPaths() []string {
	primary := userConfigPath()
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
	add(legacyUserConfigPath())
	for _, path := range legacyXDGConfigPaths() {
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
func ReasonixManagedConfigPaths() []string {
	var out []string
	out = appendUniquePath(out, UserConfigPath())
	for _, path := range LegacyUserConfigPaths() {
		out = appendUniquePath(out, path)
	}
	out = appendUniquePath(out, legacyConfigPath())
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
func ReasonixHomeDir() string { return reasonixHomeDir() }

// UserCredentialsPath is the reasonix-owned global .env file under Reasonix
// home. It is the single source for provider credentials saved by Reasonix, so
// stale shell, Windows, project, or home env vars cannot silently override keys
// the user saved through setup or settings. "" when Reasonix home can't be
// resolved.
func UserCredentialsPath() string {
	dir := reasonixHomeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".env")
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user state
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string {
	dir := userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `reasonix --continue` / `--resume` to find the recent ones. Empty
// if the user state dir can't be resolved — sessions then aren't saved.
func SessionDir() string {
	dir := userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sessions")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <state root>/projects/<slug>/sessions. Empty when either the state root
// or workspaceRoot doesn't resolve.
func ProjectSessionDir(workspaceRoot string) string {
	base := MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

// MemoryCompilerDir is the project-scoped state directory for the Memory v5
// execution compiler. Empty means persistent compiler state is unavailable.
func MemoryCompilerDir(workspaceRoot string) string {
	base := MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "memory", "compiler")
}

// WorkspaceSlug flattens an absolute workspace path into the directory name
// used under <config root>/projects. Windows spells the same folder with
// varying case (drive-letter case, Explorer renames), so the slug folds case
// there — matching agent.CanonicalSessionPath's key form — or equivalent
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
func CacheDir() string {
	dir := userCacheDir()
	if dir == "" {
		return ""
	}
	return dir
}

// MemoryUserDir returns the reasonix user state root (…/reasonix), under which
// the user-global REASONIX.md and the per-project auto-memory store live. Empty
// when the user state dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string {
	return userSupportDir()
}

// ConventionDirs are the parent directories scanned for agent assets (skills,
// commands), in canonical-first order. .reasonix is ours; .agents / .agent /
// .claude let users drop in assets authored for other agent tools without moving
// files. Shared so skills (internal/skill) and commands (CommandDirs) discover
// the same set. Note: hooks are NOT scanned across these — a .claude/settings.json
// uses a different hook schema that can't be parsed as ours, so hooks stay in
// .reasonix/settings.json (see internal/hook).
var ConventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// conventionSubdirsAsc joins sub under each ConventionDir of base, in ascending
// priority (reverse of ConventionDirs) so the canonical .reasonix ends up the
// highest-priority entry — command.Load lets a later directory win on a clash.
func conventionSubdirsAsc(base, sub string) []string {
	out := make([]string, 0, len(ConventionDirs))
	for i := len(ConventionDirs) - 1; i >= 0; i-- {
		out = append(out, filepath.Join(base, ConventionDirs[i], sub))
	}
	return out
}

// CommandDirs returns the directories scanned for custom slash commands, lowest
// priority first, so a later (more specific) directory overrides an earlier one
// on a name clash. Order: home-dir convention dirs (~/.claude/commands …
// ~/.reasonix/commands), the Reasonix home commands dir, the legacy OS
// app-support dir if different, then the project's
// convention dirs (.claude/commands … .reasonix/commands). Scanning the .claude /
// .agents / .agent dirs lets commands authored for other agent tools (same .md +
// frontmatter format) work here unchanged.
func CommandDirs() []string {
	return CommandDirsForRoot(".")
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global dirs are
// unchanged — they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	roots := CommandRootsForRoot(root)
	dirs := make([]string, 0, len(roots))
	for _, spec := range roots {
		dirs = append(dirs, spec.Path)
	}
	return dirs
}

// CommandRootsForRoot is the ownership-aware form of CommandDirsForRoot.
// Plugin roots retain their package name so the loader can expose stable,
// package-qualified command names and hidden short-name compatibility aliases.
func CommandRootsForRoot(root string) []command.Root {
	root = resolveRoot(root)
	var roots []command.Root
	add := func(spec command.Root) {
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
	for _, spec := range pluginPackageCommandRoots() {
		add(spec)
	}
	if dir := legacyOSSupportDir(); dir != "" {
		add(command.Root{Path: filepath.Join(dir, "commands")})
	}
	for _, legacy := range legacyXDGConfigPaths() {
		add(command.Root{Path: filepath.Join(filepath.Dir(legacy), "commands")})
	}
	if home, err := osUserHomeDir(); err == nil {
		for _, dir := range conventionSubdirsAsc(home, "commands") {
			add(command.Root{Path: dir})
		}
	}
	if dir := userConfigDir(); dir != "" {
		add(command.Root{Path: filepath.Join(dir, "commands")})
	}
	if dir := userSupportDir(); dir != "" && !samePath(dir, userConfigDir()) {
		add(command.Root{Path: filepath.Join(dir, "commands")})
	}
	for _, dir := range conventionSubdirsAsc(root, "commands") {
		add(command.Root{Path: dir})
	}
	return roots
}

// SourcePath returns the highest-priority config file that exists, or "" if none.
func SourcePath() string {
	return SourcePathForRoot(".")
}

// SourcePathForRoot returns the highest-priority config file that exists under
// root, or "" if none. Equivalent to SourcePath() when root is ".".
func SourcePathForRoot(root string) string {
	root = resolveRoot(root)
	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}
	if _, err := os.Stat(projectTOML); err == nil {
		return projectTOML
	}
	if uc := userConfigLoadPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	return ""
}
