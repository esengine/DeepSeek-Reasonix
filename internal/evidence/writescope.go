package evidence

import (
	"os"
	"path/filepath"
	"strings"
)

// WriteScope is where a write lands relative to the project, not how risky
// the path name looks. PathClass still owns auth/schema/docs risk.
type WriteScope uint8

const (
	WriteScopeWorkspace WriteScope = iota
	WriteScopeScratch
	WriteScopeOutside
)

func (s WriteScope) String() string {
	switch s {
	case WriteScopeScratch:
		return "scratch"
	case WriteScopeOutside:
		return "outside"
	default:
		return "workspace"
	}
}

// ClassifyWriteScope reports whether path is inside the workspace, a scratch
// root (session temp or OS temp), or somewhere else. Relative paths without a
// workspace root stay workspace so existing receipts keep their meaning.
func ClassifyWriteScope(path, workspaceRoot string, scratchRoots []string) WriteScope {
	path = strings.TrimSpace(path)
	if path == "" {
		return WriteScopeWorkspace
	}
	abs := scopeAbs(path, workspaceRoot)
	if workspaceRoot != "" && pathInside(workspaceRoot, abs) {
		return WriteScopeWorkspace
	}
	if !filepath.IsAbs(path) && strings.TrimSpace(workspaceRoot) == "" {
		return WriteScopeWorkspace
	}
	roots := scratchRootList(scratchRoots)
	for _, root := range roots {
		if pathInside(root, abs) {
			return resolvedScratchScope(abs, workspaceRoot, roots)
		}
	}
	if filepath.IsAbs(abs) {
		return WriteScopeOutside
	}
	return WriteScopeWorkspace
}

// IsDeliveryMutation keeps every successful persistent mutation except writes
// proven to be scratch-only. Outside approved roots remain delivery work: they
// are durable user-visible changes even though they are not project files.
func IsDeliveryMutation(r Receipt, workspaceRoot string, scratchRoots []string) bool {
	if !r.Success || !(r.Mutation || r.Write) || r.DeliveryScope == WriteScopeScratch {
		return false
	}
	if len(r.Paths) == 0 {
		return true
	}
	for _, path := range r.Paths {
		if ClassifyWriteScope(path, workspaceRoot, scratchRoots) != WriteScopeScratch {
			return true
		}
	}
	return false
}

// resolvedScratchScope prevents a lexical temp path from hiding a symlink
// back into the workspace (or another persistent location). Scratch roots are
// resolved too, so the normal /tmp -> /private/tmp alias on macOS stays scratch.
func resolvedScratchScope(path, workspaceRoot string, scratchRoots []string) WriteScope {
	resolved, err := resolveScopePath(path)
	if err != nil {
		return WriteScopeOutside
	}
	if workspaceRoot != "" {
		if root, rootErr := resolveScopePath(workspaceRoot); rootErr == nil && pathInside(root, resolved) {
			return WriteScopeWorkspace
		}
	}
	for _, root := range scratchRoots {
		resolvedRoot, rootErr := resolveScopePath(root)
		if rootErr == nil && pathInside(resolvedRoot, resolved) {
			return WriteScopeScratch
		}
	}
	return WriteScopeOutside
}

// resolveScopePath resolves the deepest existing ancestor and appends the
// missing tail. Writers commonly create new scratch files, so EvalSymlinks on
// the complete path alone is insufficient.
func resolveScopePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	tail := ""
	cur := abs
	for {
		if real, evalErr := filepath.EvalSymlinks(cur); evalErr == nil {
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

// DefaultScratchRoots includes the OS temp directory and Unix public aliases.
// A supplied workspace root always wins for checkouts located under temp.
func DefaultScratchRoots() []string {
	roots := []string{os.TempDir()}
	if filepath.Separator == '/' {
		roots = append(roots, "/tmp", "/private/tmp")
	}
	return uniqueCleanRoots(roots)
}

func scratchRootList(extra []string) []string {
	return uniqueCleanRoots(append(DefaultScratchRoots(), extra...))
}

func uniqueCleanRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	var out []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleaned := filepath.Clean(root)
		key := strings.ToLower(cleaned)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cleaned)
	}
	return out
}

func scopeAbs(path, workspaceRoot string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(workspaceRoot, path))
}

func pathInside(root, target string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	target = filepath.Clean(strings.TrimSpace(target))
	if root == "" || target == "" {
		return false
	}
	if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(target)) {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
