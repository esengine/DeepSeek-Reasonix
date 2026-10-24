package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/hostwrite"
	"reasonix/internal/base/secrets"
)

// UserSkillsDirName is the folder under the Reasonix home that holds installed
// skills. A skill is instructions and scripts the agent later follows, so the
// folder is never granted for writing; the supported way to keep improving a
// personal skill is to install its source folder in register mode and edit there.
const UserSkillsDirName = "skills"

// WorkspaceRootRefusal says why a folder cannot be an extra workspace root.
type WorkspaceRootRefusal string

const (
	RootNotDirectory   WorkspaceRootRefusal = "workspace_roots.not_directory"
	RootCoveredAlready WorkspaceRootRefusal = "workspace_roots.covered_by_primary"
	RootTooBroad       WorkspaceRootRefusal = "workspace_roots.too_broad"
	RootProtected      WorkspaceRootRefusal = "workspace_roots.protected"
	RootSkillStore     WorkspaceRootRefusal = "workspace_roots.skill_store"
	RootNotOwned       WorkspaceRootRefusal = "workspace_roots.not_owned"
	RootOverlapsWrite  WorkspaceRootRefusal = "workspace_roots.overlaps_write_root"
)

// WriteRootsForRoot is the write roots a session may use: the workspace root,
// the user's allow_write entries and the granted extra roots. Desktop tabs pass
// their project root as fallbackRoot.
func (c *Config) WriteRootsForRoot(fallbackRoot string) []string {
	roots := c.writeRootsBeforeExtras(fallbackRoot)
	for _, extra := range c.projectScope.extraRoots {
		roots = appendMissing(roots, extra)
	}
	return roots
}

// WorkspaceRootError carries the refusal as an identity a frontend can map.
type WorkspaceRootError struct {
	Code WorkspaceRootRefusal
	Path string
}

func (e *WorkspaceRootError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Path) }

// ForbiddenRootsForRoot is every path the host denies readers: the configured
// forbid_read roots, the host's credential files, the broad denylist's
// directories when it is on, and Reasonix's own state. The OS sandbox and the
// extra-root vetting read this one composition.
func (c *Config) ForbiddenRootsForRoot(root string) []string {
	base := c.ForbidReadRootsForRoot(root)
	base = appendUniqueResolved(base, secrets.ForbiddenReadPaths(c.Secrets.ProtectSensitiveFiles)...)
	base = appendUniqueResolved(base, append(secrets.HostSecretPaths(), c.Roots().RemoteStateDir(), c.Roots().ScheduleDir())...)
	if peers := c.Roots().PeersDir(); peers != "" {
		base = appendUniqueResolved(base, peers)
	}
	if dir := c.Roots().SandboxDir(); dir != "" {
		base = appendUniqueResolved(base, dir)
	}
	credentialPath := strings.TrimSpace(c.Roots().UserCredentialsPath())
	if credentialPath == "" {
		return base
	}
	info, err := os.Stat(credentialPath)
	if err != nil || info.IsDir() {
		return base
	}
	if real, err := filepath.EvalSymlinks(credentialPath); err == nil {
		credentialPath = real
	}
	return appendUniqueResolved(base, credentialPath)
}

func appendUniqueResolved(base []string, extra ...string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(out))
	for _, p := range out {
		seen[rootKey(p)] = struct{}{}
	}
	for _, p := range extra {
		p = filepath.Clean(p)
		if _, ok := seen[rootKey(p)]; ok {
			continue
		}
		seen[rootKey(p)] = struct{}{}
		out = append(out, p)
	}
	return out
}

// rootKey is a path's identity for comparison: absolute, symlinks resolved,
// and folded where the filesystem ignores case.
func rootKey(path string) string {
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(path)
	}
	return path
}

// keyWithin reports whether path is dir or lies beneath it. The spelling is
// compared first; then, for every ancestor of path, the file's identity
// (os.SameFile: device and inode, or volume and file index), because a macOS
// firmlink, a UNC or device-namespace path and an 8.3 name all spell the same
// directory differently and string comparison cannot know.
func keyWithin(path, dir string) bool {
	if rel, err := filepath.Rel(dir, path); err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return true
	}
	// codeql[go/path-injection] dir is a protected directory the host chose; it is only stat-ed for file identity.
	target, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		// codeql[go/path-injection] p is the candidate path or one of its ancestors, stat-ed for identity only, never read or written.
		if info, err := os.Stat(p); err == nil && os.SameFile(info, target) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}

// ordinaryPath admits only what a drive letter or a Unix root names. On Windows
// the volume must be a letter and a colon: UNC shares, \\?\ and \\.\ and the
// NT \??\ prefix all name a directory the drive-letter checks cannot see.
func ordinaryPath(dir string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	vol := filepath.VolumeName(dir)
	return len(vol) == 2 && vol[1] == ':' && (vol[0]|0x20 >= 'a' && vol[0]|0x20 <= 'z')
}

// FolderGrantRefusal judges a folder nobody should grant wholesale: a drive
// root, a home directory or anything holding one, and Reasonix's storage and
// the key and credential directories in either direction. Workspace trust and
// extra roots share it. It judges where symlinks resolve; a folder that does
// not exist yet is judged by its nearest existing parent.
func (r Roots) FolderGrantRefusal(dir string) (WorkspaceRootRefusal, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return RootNotDirectory, true
	}
	if !ordinaryPath(dir) {
		return RootNotDirectory, true
	}
	key := rootKey(resolveThroughExisting(dir))
	if key == "." || filepath.Dir(key) == key {
		return RootTooBroad, true
	}
	if home, err := osUserHomeDir(); err == nil && home != "" && keyWithin(rootKey(home), key) {
		return RootTooBroad, true
	}
	if keyWithin(key, rootKey(resolveThroughExisting(filepath.Join(r.Dir(RootHome), UserSkillsDirName)))) {
		return RootSkillStore, true
	}
	var guarded []string
	for _, id := range RootIDs() {
		guarded = append(guarded, r.Dir(id))
	}
	guarded = append(guarded, r.legacyUserConfigPaths()...)
	guarded = append(guarded, secrets.CredentialDirs()...)
	guarded = append(guarded, secrets.SensitiveHomeDirs()...)
	for _, g := range guarded {
		if g == "" {
			continue
		}
		gk := rootKey(resolveThroughExisting(g))
		if keyWithin(key, gk) || keyWithin(gk, key) {
			return RootProtected, true
		}
	}
	return "", false
}

func resolveThroughExisting(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	rest := ""
	for dir := p; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		if filepath.Dir(dir) == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// VetExtraRoot resolves dir and refuses it unless it may take writes: an
// existing directory the user owns, clear of the primary and of every write root
// the session has (existing are the extra roots already granted), and not one
// FolderGrantRefusal or the forbid list rules out. The resolved path is stored.
func (c *Config) VetExtraRoot(primary, dir string, existing []string) (string, error) {
	dir = strings.TrimSpace(dir)
	real, key, err := c.vetRoot(primary, dir, true)
	if err != nil {
		return "", err
	}
	refuse := func(code WorkspaceRootRefusal) (string, error) {
		return "", &WorkspaceRootError{Code: code, Path: dir}
	}
	held := append(c.writeRootsBeforeExtras(primary), os.TempDir())
	held = append(held, hostwrite.Dirs()...)
	for _, w := range held {
		if wk := rootKey(w); keyWithin(key, wk) || keyWithin(wk, key) {
			return refuse(RootOverlapsWrite)
		}
	}
	for _, w := range existing {
		wk := rootKey(w)
		if wk == key {
			continue
		}
		if keyWithin(key, wk) || keyWithin(wk, key) {
			return refuse(RootOverlapsWrite)
		}
	}
	return real, nil
}

// VetReadableRoot is the part of VetExtraRoot that decides whether anything may
// be read through a granted folder: an existing directory the user owns, not a
// link, and not one FolderGrantRefusal or the forbid list rules out. It leaves
// out what only matters to writes, so a grant that overlaps a write root still
// reads. It returns the resolved path.
func (c *Config) VetReadableRoot(primary, dir string) (string, error) {
	real, _, err := c.vetRoot(primary, strings.TrimSpace(dir), false)
	return real, err
}

// vetRoot runs the checks both uses share, in the order a refusal is reported;
// forWrite adds the one that only a writer needs, that the folder is not already
// inside the primary.
func (c *Config) vetRoot(primary, dir string, forWrite bool) (real, key string, err error) {
	refuse := func(code WorkspaceRootRefusal) (string, string, error) {
		return "", "", &WorkspaceRootError{Code: code, Path: dir}
	}
	if !filepath.IsAbs(dir) || !ordinaryPath(dir) {
		return refuse(RootNotDirectory)
	}
	real, err = filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return refuse(RootNotDirectory)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return refuse(RootNotDirectory)
	}
	if isReparsePoint(real) {
		return refuse(RootProtected)
	}
	key = rootKey(real)
	if forWrite && primary != "" && keyWithin(key, rootKey(primary)) {
		return refuse(RootCoveredAlready)
	}
	if code, bad := c.Roots().FolderGrantRefusal(real); bad {
		return refuse(code)
	}
	if !ownedByCurrentUser(info) {
		return refuse(RootNotOwned)
	}
	for _, p := range c.ForbiddenRootsForRoot(primary) {
		pk := rootKey(p)
		if keyWithin(key, pk) || keyWithin(pk, key) {
			return refuse(RootProtected)
		}
	}
	return real, key, nil
}
