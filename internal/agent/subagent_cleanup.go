package agent

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type staleSubagentParent struct {
	sessionPath string
	refs        []string
}

// CleanupStaleRunning interrupts crash leftovers while preserving children of
// a parent that is live in this process or still owns its cross-process lease.
func (s *SubagentStore) CleanupStaleRunning() (int, error) {
	if s == nil {
		return 0, nil
	}
	entries, err := s.staleCleanupEntries()
	if err != nil || entries == nil {
		return 0, err
	}
	parents, err := s.staleRunningParents(entries)
	if err != nil {
		return 0, err
	}
	parentIDs := make([]string, 0, len(parents))
	for parentID := range parents {
		parentIDs = append(parentIDs, parentID)
	}
	sort.Strings(parentIDs)

	cleaned := 0
	now := time.Now().UTC()
	for _, parentID := range parentIDs {
		count, err := s.cleanupStaleParent(parentID, parents[parentID], now)
		cleaned += count
		if err != nil {
			return cleaned, err
		}
	}
	return cleaned, nil
}

func (s *SubagentStore) staleCleanupEntries() ([]os.DirEntry, error) {
	info, err := os.Stat(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("subagent store path %q is not a directory", s.dir)
	}
	entries, err := os.ReadDir(s.dir)
	if err == nil {
		return entries, nil
	}
	// Windows can report a non-directory as ENOENT; only accept a missing leaf.
	if os.IsNotExist(err) {
		if _, statErr := os.Lstat(s.dir); statErr != nil {
			return nil, nil
		}
	}
	return nil, err
}

func (s *SubagentStore) staleRunningParents(entries []os.DirEntry) (map[string]*staleSubagentParent, error) {
	parents := map[string]*staleSubagentParent{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		ref := strings.TrimSuffix(entry.Name(), ".meta.json")
		if !validSubagentRef(ref) {
			continue
		}
		meta, err := s.LoadMeta(ref)
		if err != nil {
			if isSubagentMetaDecodeError(err) {
				continue
			}
			return nil, err
		}
		if meta.Status != SubagentRunning {
			continue
		}
		parentID := strings.TrimSpace(meta.ParentSession)
		sessionPath, ok := s.parentSessionPath(parentID)
		if !ok {
			continue
		}
		parent := parents[parentID]
		if parent == nil {
			parent = &staleSubagentParent{sessionPath: sessionPath}
			parents[parentID] = parent
		}
		parent.refs = append(parent.refs, ref)
	}
	return parents, nil
}

func (s *SubagentStore) cleanupStaleParent(parentID string, parent *staleSubagentParent, now time.Time) (int, error) {
	parentLive := func() bool {
		return s.parentSessionProbe != nil && s.parentSessionProbe(parent.sessionPath)
	}
	if parentLive() {
		return 0, nil
	}
	lease, err := TryAcquireSessionMaintenanceLease(parent.sessionPath)
	if errors.Is(err, ErrSessionLeaseHeld) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("acquire parent session lease %q: %w", parentID, err)
	}
	defer lease.Release()
	// Close the pre-probe/acquire window before touching child metadata.
	if parentLive() {
		return 0, nil
	}

	cleaned := 0
	for _, ref := range parent.refs {
		if s.cleanupBeforeReread != nil {
			s.cleanupBeforeReread(parentID, ref)
		}
		if parentLive() {
			break
		}
		meta, err := s.LoadMeta(ref)
		if err != nil {
			if isSubagentMetaDecodeError(err) {
				continue
			}
			return cleaned, err
		}
		if meta.Status != SubagentRunning || strings.TrimSpace(meta.ParentSession) != parentID {
			continue
		}
		if parentLive() {
			break
		}
		meta.Status = SubagentInterrupted
		meta.UpdatedAt = now
		if err := s.saveMeta(meta); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}
