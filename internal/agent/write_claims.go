package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultMaxSubagentConcurrency is the session-wide sub-agent concurrency
// default (task, fleet items, profile skills, nested children).
const DefaultMaxSubagentConcurrency = 6

// DefaultMaxParallelWriters is the default cap on concurrent writer-capable
// sub-agents that declare non-overlapping write_paths.
const DefaultMaxParallelWriters = 3

// MaxSubagentConcurrencyLimit is the upper bound for both concurrency knobs.
const MaxSubagentConcurrencyLimit = 32

// WritePathSet is a normalized claim over workspace paths a sub-agent may write.
// WholeWorkspace is true when a writer-capable task omitted write_paths and
// therefore claims the entire workspace (forcing writer serialization).
type WritePathSet struct {
	// Paths are absolute, cleaned, and symlink-resolved when possible.
	Paths []string
	// WholeWorkspace claims the entire workspace root.
	WholeWorkspace bool
	// WorkspaceRoot is the absolute workspace root used for WholeWorkspace claims.
	WorkspaceRoot string
}

// Empty reports whether the set claims nothing (read-only work).
func (s WritePathSet) Empty() bool {
	return !s.WholeWorkspace && len(s.Paths) == 0
}

// NormalizeConcurrencyLimits clamps total/writer limits into the public range
// 1–32 and ensures writers never exceed total. Zero inputs become defaults so
// old configs stay at 6/3 without migration.
func NormalizeConcurrencyLimits(total, writers int) (int, int) {
	if total <= 0 {
		total = DefaultMaxSubagentConcurrency
	}
	if writers <= 0 {
		writers = DefaultMaxParallelWriters
	}
	if total > MaxSubagentConcurrencyLimit {
		total = MaxSubagentConcurrencyLimit
	}
	if writers > MaxSubagentConcurrencyLimit {
		writers = MaxSubagentConcurrencyLimit
	}
	if writers > total {
		writers = total
	}
	return total, writers
}

// NormalizeWritePaths validates and normalizes declared write_paths against a
// workspace root. It rejects globs, empty entries, workspace-escape paths, and
// symlink escapes. An empty raw list yields an empty set (read-only / no claim).
func NormalizeWritePaths(workspaceRoot string, raw []string) (WritePathSet, error) {
	root, err := normalizeExistingRoot(workspaceRoot)
	if err != nil {
		return WritePathSet{}, err
	}
	if len(raw) == 0 {
		return WritePathSet{}, nil
	}
	out := WritePathSet{WorkspaceRoot: root}
	seen := map[string]bool{}
	for i, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return WritePathSet{}, fmt.Errorf("write_paths[%d]: path is required", i)
		}
		if strings.ContainsAny(entry, "*?[") {
			return WritePathSet{}, fmt.Errorf("write_paths[%d]: globs are not allowed (%q)", i, entry)
		}
		abs, err := resolveWriteClaimPath(root, entry)
		if err != nil {
			return WritePathSet{}, fmt.Errorf("write_paths[%d]: %w", i, err)
		}
		if !pathWithinFold(root, abs) {
			return WritePathSet{}, fmt.Errorf("write_paths[%d]: path %q is outside the workspace", i, entry)
		}
		key := foldPathKey(abs)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Paths = append(out.Paths, abs)
	}
	return out, nil
}

// WholeWorkspaceWriteClaim claims the entire workspace for a writer that did
// not declare write_paths. Such tasks may only run serially among writers.
func WholeWorkspaceWriteClaim(workspaceRoot string) (WritePathSet, error) {
	root, err := normalizeExistingRoot(workspaceRoot)
	if err != nil {
		return WritePathSet{}, err
	}
	return WritePathSet{WholeWorkspace: true, WorkspaceRoot: root}, nil
}

// Overlaps reports whether two write claims conflict (identical, parent/child,
// or case-equivalent on case-insensitive filesystems).
func (s WritePathSet) Overlaps(other WritePathSet) bool {
	if s.Empty() || other.Empty() {
		return false
	}
	if s.WholeWorkspace || other.WholeWorkspace {
		// Whole-workspace claims collide with every other writer claim that
		// shares the same workspace root (or has an empty root).
		if s.WorkspaceRoot == "" || other.WorkspaceRoot == "" {
			return true
		}
		return pathWithinFold(s.WorkspaceRoot, other.WorkspaceRoot) ||
			pathWithinFold(other.WorkspaceRoot, s.WorkspaceRoot)
	}
	for _, a := range s.Paths {
		for _, b := range other.Paths {
			if pathWithinFold(a, b) || pathWithinFold(b, a) {
				return true
			}
		}
	}
	return false
}

// ValidateNonOverlappingWriteClaims fails if any pair of claims overlaps.
// Used by fleet preflight so no task starts when path division is invalid.
func ValidateNonOverlappingWriteClaims(claims []WritePathSet) error {
	for i := 0; i < len(claims); i++ {
		if claims[i].Empty() {
			continue
		}
		for j := i + 1; j < len(claims); j++ {
			if claims[j].Empty() {
				continue
			}
			if claims[i].Overlaps(claims[j]) {
				return fmt.Errorf("write path conflict between task %d and task %d", i+1, j+1)
			}
		}
	}
	return nil
}

// AllowsPath reports whether target is inside this claim (for re-bound writers).
func (s WritePathSet) AllowsPath(target string) bool {
	if s.Empty() {
		return false
	}
	abs, err := realPathForClaim(target)
	if err != nil {
		return false
	}
	if s.WholeWorkspace {
		if s.WorkspaceRoot == "" {
			return true
		}
		return pathWithinFold(s.WorkspaceRoot, abs)
	}
	for _, root := range s.Paths {
		if pathWithinFold(root, abs) {
			return true
		}
	}
	return false
}

// Roots returns the concrete root list used to re-confine built-in writers and
// bash sandbox WriteRoots. Whole-workspace claims return the workspace root.
func (s WritePathSet) Roots() []string {
	if s.WholeWorkspace {
		if s.WorkspaceRoot == "" {
			return nil
		}
		return []string{s.WorkspaceRoot}
	}
	return append([]string(nil), s.Paths...)
}

func normalizeExistingRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("workspace root is required for write_paths")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	abs = filepath.Clean(abs)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Workspace may not exist yet in some tests; keep cleaned abs.
		return abs, nil
	}
	return real, nil
}

func resolveWriteClaimPath(workspaceRoot, raw string) (string, error) {
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceRoot, path)
	}
	return realPathForClaim(path)
}

// realPathForClaim mirrors the write-tool realPath helper: resolve the deepest
// existing ancestor so a not-yet-created file claim still cannot escape via a
// symlinked parent.
func realPathForClaim(path string) (string, error) {
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
		// Reject intermediate symlink escapes when parent exists as a symlink
		// that leaves the tree — EvalSymlinks failed on cur but may succeed on
		// parent; loop continues.
		info, err := os.Lstat(cur)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			// Symlink that does not resolve — treat as escape risk.
			return "", fmt.Errorf("cannot resolve symlink path %q", path)
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

func pathWithinFold(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	if foldPaths() {
		root = strings.ToLower(root)
		path = strings.ToLower(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func foldPathKey(path string) string {
	if foldPaths() {
		return strings.ToLower(path)
	}
	return path
}

func foldPaths() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}
