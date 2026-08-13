package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
)

// DismissTodoBatchForTab records a closed completed todo list on the session
// sidecar (and its parent/covering leaf) so the shelf stays hidden after an
// upgrade remounts the same conversation on a different path.
func (a *App) DismissTodoBatchForTab(tabID, batchKey string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		return a.workspaceNotReadyErr(nil)
	}
	sessionPath := strings.TrimSpace(tab.currentSessionPath())
	batchKey = strings.TrimSpace(batchKey)
	if sessionPath == "" || batchKey == "" {
		return nil
	}
	var firstErr error
	for _, path := range a.todoDismissalPersistPaths(sessionPath) {
		if err := agent.RecordDismissedTodoBatch(path, batchKey); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *App) todoDismissalReadPaths(sessionPath string) []string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}
	paths := []string{sessionPath}
	if parent := parentSessionPathForTodoDismissal(sessionPath); parent != "" {
		paths = append(paths, parent)
	}
	return uniqueExistingSessionPaths(paths)
}

func (a *App) todoDismissalPersistPaths(sessionPath string) []string {
	paths := a.todoDismissalReadPaths(sessionPath)
	if a != nil {
		if leaf := strings.TrimSpace(a.continuePathForOpen(sessionPath)); leaf != "" {
			paths = append(paths, leaf)
		}
	}
	return uniqueExistingSessionPaths(paths)
}

func (a *App) dismissedTodoBatchesForSession(sessionPath string) []string {
	var sets [][]string
	for _, path := range a.todoDismissalReadPaths(sessionPath) {
		sets = append(sets, agent.DismissedTodoBatches(path))
	}
	out := agent.MergeDismissedTodoBatches(sets...)
	if out == nil {
		return []string{}
	}
	return out
}

func parentSessionPathForTodoDismissal(sessionPath string) string {
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return ""
	}
	parentID := strings.TrimSpace(meta.ParentID)
	if !safeTodoDismissalParentID(parentID) || parentID == agent.BranchID(sessionPath) {
		return ""
	}
	dir := filepath.Clean(filepath.Dir(sessionPath))
	candidate := filepath.Join(dir, parentID+".jsonl")
	if filepath.Dir(filepath.Clean(candidate)) != dir {
		return ""
	}
	return candidate
}

func safeTodoDismissalParentID(parentID string) bool {
	if parentID == "" || parentID == "." || parentID == ".." {
		return false
	}
	return filepath.Base(parentID) == parentID && !strings.ContainsAny(parentID, `/\`)
}

func uniqueExistingSessionPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := agent.CanonicalSessionPath(path)
		if key == "" {
			key = path
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if _, metaErr := os.Stat(agent.BranchMetaPath(path)); metaErr != nil {
				continue
			}
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}
