package sessionstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const maxRecoveryArchiveLineageWalk = 16

// SetSessionLineageArchived changes the catalog state of a conversation and
// every recovery sibling that folds into the same sidebar row. Archiving only
// the visible lead lets a sibling surface as a new active row when recovery GC
// later reclaims the lead. Paths in except, normally sessions with a live pane,
// keep their current state.
func SetSessionLineageArchived(sessionPath string, archived bool, except ...string) error {
	paths, err := recoveryArchiveLineage(sessionPath, except)
	if err != nil {
		return err
	}
	if len(paths) == 1 {
		return SetSessionArchived(paths[0], archived)
	}
	return setSessionPathsArchivedWith(paths, archived, saveBranchMeta)
}

type branchMetaSaveFunc func(string, BranchMeta, bool) error

type archivedMetaSnapshot struct {
	path    string
	meta    BranchMeta
	existed bool
}

func setSessionLineageArchivedWith(sessionPath string, archived bool, except []string, save branchMetaSaveFunc) error {
	paths, err := recoveryArchiveLineage(sessionPath, except)
	if err != nil {
		return err
	}
	return setSessionPathsArchivedWith(paths, archived, save)
}

func setSessionPathsArchivedWith(paths []string, archived bool, save branchMetaSaveFunc) error {
	unlocks := make([]func(), 0, len(paths))
	for _, path := range paths {
		unlock, err := LockSessionMetaPath(path)
		if err != nil {
			for _, unlock := range slices.Backward(unlocks) {
				unlock()
			}
			return err
		}
		unlocks = append(unlocks, unlock)
	}
	defer func() {
		for _, unlock := range slices.Backward(unlocks) {
			unlock()
		}
	}()

	snapshots := make([]archivedMetaSnapshot, 0, len(paths))
	for _, path := range paths {
		meta, existed, err := LoadBranchMeta(path)
		if err != nil {
			return err
		}
		if !existed {
			meta, err = ensureBranchMetaUnlocked(path)
			if err != nil {
				return err
			}
		}
		snapshots = append(snapshots, archivedMetaSnapshot{path: path, meta: meta, existed: existed})
	}

	written := make([]archivedMetaSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.meta.Archived == archived {
			continue
		}
		next := snapshot.meta
		next.Archived = archived
		written = append(written, snapshot)
		if err := save(snapshot.path, next, false); err != nil {
			rollbackErr := rollbackArchivedMetaSnapshots(written, save)
			return errors.Join(fmt.Errorf("archive session lineage: %w", err), rollbackErr)
		}
	}
	return nil
}

func rollbackArchivedMetaSnapshots(snapshots []archivedMetaSnapshot, save branchMetaSaveFunc) error {
	var errs []error
	for _, snapshot := range slices.Backward(snapshots) {
		if !snapshot.existed {
			if err := os.Remove(BranchMetaPath(snapshot.path)); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove new sidecar for %s: %w", snapshot.path, err))
			}
			continue
		}
		if err := save(snapshot.path, snapshot.meta, false); err != nil {
			errs = append(errs, fmt.Errorf("restore sidecar for %s: %w", snapshot.path, err))
		}
	}
	return errors.Join(errs...)
}

func recoveryArchiveLineage(sessionPath string, except []string) ([]string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(sessionPath))
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]bool, len(except))
	for _, path := range except {
		if canonical := CanonicalSessionPath(path); canonical != "" {
			excluded[canonical] = true
		}
	}
	if excluded[CanonicalSessionPath(abs)] {
		return nil, nil
	}
	listed, err := ListSessionOrder(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	byID := make(map[string]SessionOrderInfo, len(listed))
	for _, info := range listed {
		byID[BranchID(info.Path)] = info
	}
	target, ok := byID[BranchID(abs)]
	if !ok {
		return []string{abs}, nil
	}
	root := recoveryArchiveRoot(target, byID)
	paths := []string{abs}
	for _, info := range listed {
		if info.Path == abs || excluded[CanonicalSessionPath(info.Path)] || !info.Recovered || recoveryArchiveRoot(info, byID) != root {
			continue
		}
		paths = append(paths, info.Path)
	}
	sort.Strings(paths)
	return paths, nil
}

func recoveryArchiveRoot(info SessionOrderInfo, byID map[string]SessionOrderInfo) string {
	if root := strings.TrimSpace(info.RecoveryRootID); root != "" {
		return root
	}
	id := BranchID(info.Path)
	for range maxRecoveryArchiveLineageWalk {
		current, ok := byID[id]
		if !ok || !current.Recovered {
			return id
		}
		parent := strings.TrimSpace(current.ParentID)
		if parent == "" || parent == id {
			return id
		}
		id = parent
	}
	return id
}
