package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// NormalizeWriteDir expands a user- or model-supplied write directory into an
// absolute, symlink-resolved path plus a short display form. raw may be
// workspace-relative, start with ~, or contain ${HOME}. Globs are rejected.
func NormalizeWriteDir(raw, workDir, home string) (abs, display string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("write directory is empty")
	}
	if writePathHasGlob(raw) {
		return "", "", fmt.Errorf("write directory %q must be a concrete directory, not a glob", raw)
	}
	expanded, err := expandWritePath(raw, home)
	if err != nil {
		return "", "", err
	}
	if !filepath.IsAbs(expanded) {
		base := strings.TrimSpace(workDir)
		if base == "" {
			base, err = os.Getwd()
			if err != nil {
				return "", "", fmt.Errorf("resolve write directory %q: %w", raw, err)
			}
		}
		expanded = filepath.Join(base, expanded)
	}
	abs, err = ResolveAbsPath(expanded)
	if err != nil {
		return "", "", fmt.Errorf("resolve write directory %q: %w", raw, err)
	}
	return abs, DisplayWritePath(abs, home), nil
}

func expandWritePath(raw, home string) (string, error) {
	if strings.Contains(raw, "${HOME}") {
		if strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("write directory %q uses ${HOME} but the home directory is unknown", raw)
		}
		raw = strings.ReplaceAll(raw, "${HOME}", home)
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(raw, `~\`)) {
		if strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("write directory %q uses ~ but the home directory is unknown", raw)
		}
		if raw == "~" {
			return home, nil
		}
		return filepath.Join(home, raw[2:]), nil
	}
	return raw, nil
}

func writePathHasGlob(raw string) bool {
	return strings.ContainsAny(raw, "*?[")
}

// ResolveAbsPath resolves path to an absolute, cleaned form. Because a write
// target need not exist yet, it resolves the deepest existing ancestor with
// EvalSymlinks and re-appends the not-yet-existing tail.
func ResolveAbsPath(path string) (string, error) {
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
			return abs, nil
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// DisplayWritePath returns a user-facing form such as ~/.local when abs sits
// under home. Other paths stay absolute.
func DisplayWritePath(abs, home string) string {
	abs = canonicalDir(abs)
	home = canonicalDir(home)
	if abs == "" {
		return ""
	}
	if home != "" && PathWithin(home, abs) {
		rel, err := filepath.Rel(home, abs)
		if err == nil {
			if rel == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return abs
}

// FormatConfigWritePath stores home-relative paths as ${HOME}/... and other
// paths as cleaned absolute paths.
func FormatConfigWritePath(abs, home string) string {
	abs = canonicalDir(abs)
	home = canonicalDir(home)
	if abs == "" {
		return ""
	}
	if home != "" && PathWithin(home, abs) {
		rel, err := filepath.Rel(home, abs)
		if err == nil {
			if rel == "." {
				return "${HOME}"
			}
			return "${HOME}/" + filepath.ToSlash(rel)
		}
	}
	return abs
}

// PathWithin reports whether path is at or below root. Both should be
// absolute and cleaned.
func PathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// CollapseWriteRoots deduplicates directories and drops children already
// covered by an ancestor.
func CollapseWriteRoots(dirs []string) []string {
	cleaned := uniqueCleanDirs(dirs)
	if len(cleaned) < 2 {
		return cleaned
	}
	out := make([]string, 0, len(cleaned))
	for _, dir := range cleaned {
		covered := false
		for _, other := range cleaned {
			if other == dir {
				continue
			}
			if PathWithin(other, dir) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, dir)
		}
	}
	return out
}

func uniqueCleanDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" || dir == "." || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// IsFilesystemRoot reports POSIX /, a Windows drive root, or a UNC share root.
func IsFilesystemRoot(abs string) bool {
	raw := strings.TrimSpace(abs)
	if raw == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		// filepath.VolumeName(`\\`) is empty even though the current-drive root
		// is just as broad as an explicit C:\\ root.
		if strings.Trim(raw, `\/`) == "" {
			return true
		}
	}
	abs = filepath.Clean(raw)
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(abs)
		if vol == "" {
			return false
		}
		rest := strings.TrimPrefix(abs, vol)
		return strings.Trim(rest, `\/`) == ""
	}
	return abs == string(filepath.Separator)
}

// IsHomeDir reports whether abs is the user's home directory.
func IsHomeDir(abs, home string) bool {
	abs = canonicalDir(abs)
	home = canonicalDir(home)
	if abs == "" || home == "" {
		return false
	}
	if abs == home {
		return true
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(abs, home)
	}
	return false
}

func canonicalDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if resolved, err := ResolveAbsPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// ProtectedWriteRoots returns the Reasonix state boundary that must stay
// read-only after a broad ancestor grant. Protecting the parent also covers
// state files that do not exist when the sandbox starts.
func ProtectedWriteRoots(stateRoot string) []string {
	stateRoot = canonicalDir(stateRoot)
	if stateRoot == "" {
		return nil
	}
	return []string{stateRoot}
}

// IsProtectedWritePath reports whether abs is a Reasonix session store,
// runtime ledger, or security-boundary file.
func IsProtectedWritePath(abs, stateRoot string) bool {
	abs = canonicalDir(abs)
	stateRoot = canonicalDir(stateRoot)
	if abs == "" || stateRoot == "" {
		return false
	}
	if sameWritePath(abs, stateRoot) {
		return true
	}
	if protectedPathWithin(filepath.Join(stateRoot, "sessions"), abs) {
		return true
	}
	relRoot, relPath := stateRoot, abs
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		relRoot, relPath = strings.ToLower(relRoot), strings.ToLower(relPath)
	}
	if rel, err := filepath.Rel(relRoot, relPath); err == nil && rel != "." && !strings.Contains(rel, string(filepath.Separator)) {
		if isProtectedStateFile(rel) {
			return true
		}
	}
	return protectedPathWithin(filepath.Join(stateRoot, "projects"), abs)
}

func protectedPathWithin(root, path string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	return PathWithin(root, path)
}

func isProtectedStateFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "desktop-") {
		return true
	}
	switch lower {
	case "settings.json", "metrics-pending.json", "crash-pending.json":
		return true
	default:
		return false
	}
}

// NormalizeWriteDirs validates and collapses a list of requested write
// directories. broadHome is true when the request includes the user's home.
func NormalizeWriteDirs(raw []string, workDir, home, stateRoot string) (abs, display []string, broadHome bool, err error) {
	seen := map[string]bool{}
	for _, dir := range raw {
		resolved, _, nerr := NormalizeWriteDir(dir, workDir, home)
		if nerr != nil {
			return nil, nil, false, nerr
		}
		if verr := ValidateWriteDir(resolved, stateRoot); verr != nil {
			return nil, nil, false, verr
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		abs = append(abs, resolved)
		if IsHomeDir(resolved, home) {
			broadHome = true
		}
	}
	abs = CollapseWriteRoots(abs)
	display = make([]string, 0, len(abs))
	for _, dir := range abs {
		display = append(display, DisplayWritePath(dir, home))
	}
	if abs == nil {
		abs = []string{}
	}
	return abs, display, broadHome, nil
}

// ValidateWriteDir rejects filesystem roots and Reasonix-protected paths.
// The user's home directory is allowed; callers should flag it as high risk.
func ValidateWriteDir(abs, stateRoot string) error {
	if IsFilesystemRoot(abs) {
		return fmt.Errorf("write directory %q is a filesystem root and cannot be granted", abs)
	}
	if IsProtectedWritePath(abs, stateRoot) {
		return fmt.Errorf("write directory %q is a Reasonix session or runtime-state path and cannot be granted", abs)
	}
	return nil
}

// EnsureWriteDir creates approved with 0o755 when it does not exist and
// returns the verified identity that callers must grant. It rejects a path
// whose symlink-resolved identity changed after the approval prompt.
func EnsureWriteDir(approved, stateRoot string) (string, error) {
	approved = filepath.Clean(strings.TrimSpace(approved))
	if approved == "" || approved == "." {
		return "", fmt.Errorf("write directory is empty")
	}
	if !filepath.IsAbs(approved) {
		return "", fmt.Errorf("write directory %q is not absolute", approved)
	}
	if err := ValidateWriteDir(approved, stateRoot); err != nil {
		return "", err
	}
	resolved, err := ResolveAbsPath(approved)
	if err != nil {
		return "", fmt.Errorf("resolve approved write directory %q: %w", approved, err)
	}
	if !sameWritePath(approved, resolved) {
		return "", fmt.Errorf("approved write directory %q changed identity to %q", approved, resolved)
	}
	if err := ValidateWriteDir(resolved, stateRoot); err != nil {
		return "", err
	}

	info, err := os.Stat(resolved)
	switch {
	case err == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("write path %q is not a directory", approved)
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(approved, 0o755); err != nil {
			return "", fmt.Errorf("create write directory %q: %w", approved, err)
		}
	default:
		return "", fmt.Errorf("stat write directory %q: %w", approved, err)
	}

	resolved, err = ResolveAbsPath(approved)
	if err != nil {
		return "", fmt.Errorf("re-resolve write directory %q: %w", approved, err)
	}
	if !sameWritePath(approved, resolved) {
		return "", fmt.Errorf("approved write directory %q changed identity to %q", approved, resolved)
	}
	if err := ValidateWriteDir(resolved, stateRoot); err != nil {
		return "", err
	}
	info, err = os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat created write directory %q: %w", approved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("created write path %q is not a directory", approved)
	}
	return resolved, nil
}

func sameWritePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	// Approval identity must remain exact: macOS and Windows can host
	// case-sensitive paths. A casing change must re-prompt, not reuse a grant.
	return left == right
}
