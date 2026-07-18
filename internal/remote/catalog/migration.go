package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/store"
)

// syncWorkspaceSessionsLocked discovers legacy project transcripts and gives
// them durable Remote identities. It intentionally scans the authoritative
// per-workspace Session store, not a client-supplied path, and never derives a
// Session ID from a filename.
func (c *Catalog) syncWorkspaceSessionsLocked(ctx context.Context, workspace workspaceRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := c.sessionStoreDirectoryLocked(workspace, false)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	next, err := cloneState(c.state)
	if err != nil {
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if next.Topics[workspace.ID] == nil {
		next.Topics[workspace.ID] = make(map[protocol.TopicID]topicRecord)
	}
	changed := false

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return catalogError(protocol.ErrSessionPersistFailed, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || agent.IsCleanupPending(path) {
			continue
		}
		if err := validateMetaSidecar(path, false); err != nil {
			return err
		}

		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil {
			return catalogError(protocol.ErrSessionPersistFailed, err)
		}
		if !ok {
			meta, err = agent.EnsureBranchMeta(path)
			if err != nil {
				return catalogError(protocol.ErrSessionPersistFailed, err)
			}
		}
		// A misplaced sidecar must never let one workspace adopt another's
		// transcript. Empty is the expected legacy value and is migrated below.
		if root := strings.TrimSpace(meta.WorkspaceRoot); root != "" {
			canonical, canonicalErr := canonicalExistingDirectory(root)
			if canonicalErr != nil || pathKey(canonical) != pathKey(workspace.CanonicalPath) {
				continue
			}
		}

		sessionID, err := c.importSessionIDLocked(next, workspace.ID, path, meta.RemoteSessionID)
		if err != nil {
			return err
		}
		var topicID protocol.TopicID
		var topic topicRecord
		if existing, exists := next.Sessions[sessionID]; exists && existing.WorkspaceID == workspace.ID && existing.TrashPath == "" {
			topicID = existing.TopicID
			topic, exists = next.Topics[workspace.ID][topicID]
			if !exists {
				return migrationErrorf("Session %s references missing Topic %s", sessionID, topicID)
			}
		} else {
			topicID, topic, err = c.importTopicLocked(next, workspace.ID, meta)
			if err != nil {
				return err
			}
		}
		profile, err := c.migrationProfileLocked(ctx, workspace.CanonicalPath, meta)
		if err != nil {
			return err
		}

		want := meta
		want.RemoteSessionID = string(sessionID)
		want.Scope = "project"
		want.WorkspaceRoot = workspace.CanonicalPath
		want.TopicID = string(topicID)
		want.TopicTitle = topic.Title
		want.Model = profile.Model
		want.Effort = profile.Effort
		want.Mode = string(profile.CollaborationMode)
		want.TokenMode = string(profile.TokenMode)
		want.ToolApprovalMode = string(profile.ToolApprovalMode)
		want.RemoteProfileVersion = 1
		if want.AdditionalDirs == nil {
			want.AdditionalDirs = []string{}
		}
		if !reflect.DeepEqual(meta, want) {
			_, err = agent.UpdateBranchMetaPreserveUpdated(path, func(current *agent.BranchMeta) error {
				// Reapply only Remote-owned fields to the latest sidecar revision;
				// autosave may have advanced listing fields since our initial read.
				current.RemoteSessionID = want.RemoteSessionID
				current.Scope = want.Scope
				current.WorkspaceRoot = want.WorkspaceRoot
				current.TopicID = want.TopicID
				current.TopicTitle = want.TopicTitle
				current.Model = want.Model
				current.Effort = want.Effort
				current.Mode = want.Mode
				current.TokenMode = want.TokenMode
				current.ToolApprovalMode = want.ToolApprovalMode
				current.AdditionalDirs = append([]string(nil), want.AdditionalDirs...)
				current.RemoteProfileVersion = want.RemoteProfileVersion
				return nil
			})
			if err != nil {
				return catalogError(protocol.ErrSessionPersistFailed, err)
			}
		}

		record := sessionRecord{
			ID:          sessionID,
			WorkspaceID: workspace.ID,
			Path:        path,
			TopicID:     topicID,
		}
		if existing, ok := next.Sessions[sessionID]; ok && existing.TrashPath == "" {
			record.TrashedAtMs = existing.TrashedAtMs
		}
		if !reflect.DeepEqual(next.Sessions[sessionID], record) {
			next.Sessions[sessionID] = record
			changed = true
		}
	}

	if !reflect.DeepEqual(c.state.Topics[workspace.ID], next.Topics[workspace.ID]) {
		changed = true
	}
	if !changed {
		return nil
	}
	return c.mutateLocked(func() error {
		c.state.Topics[workspace.ID] = next.Topics[workspace.ID]
		c.state.Sessions = next.Sessions
		return nil
	})
}

func (c *Catalog) sessionStoreDirectoryLocked(workspace workspaceRecord, create bool) (string, error) {
	dir := strings.TrimSpace(c.sessionDir(workspace.CanonicalPath))
	if dir == "" {
		return "", catalogError(protocol.ErrSessionPersistFailed, errors.New("session directory is unavailable"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if create {
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return "", catalogError(protocol.ErrSessionPersistFailed, err)
		}
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !create && os.IsNotExist(err) {
			// ReadDir will turn this into an empty store.
			return filepath.Clean(abs), nil
		}
		return "", catalogError(protocol.ErrSessionPersistFailed, err)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("session store is not a directory")
		}
		return "", catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return filepath.Clean(canonical), nil
}

func (c *Catalog) importSessionIDLocked(state diskState, workspaceID protocol.WorkspaceID, path, candidate string) (protocol.SessionID, error) {
	id := protocol.SessionID(strings.TrimSpace(candidate))
	if id != "" && !state.RetiredSessionIDs[id] {
		if existing, ok := state.Sessions[id]; ok {
			if existing.WorkspaceID == workspaceID && existing.TrashPath == "" {
				if pathKey(existing.Path) == pathKey(path) || !regularFileExists(existing.Path) {
					return id, nil
				}
			}
		} else if _, used := c.issued[string(id)]; !used {
			c.issued[string(id)] = struct{}{}
			return id, nil
		}
	}
	raw, err := c.nextIDLocked("session")
	return protocol.SessionID(raw), err
}

func (c *Catalog) importTopicLocked(state diskState, workspaceID protocol.WorkspaceID, meta agent.BranchMeta) (protocol.TopicID, topicRecord, error) {
	topics := state.Topics[workspaceID]
	candidate := protocol.TopicID(strings.TrimSpace(meta.TopicID))
	if candidate != "" {
		if existing, ok := topics[candidate]; ok {
			if !existing.Trashed {
				return candidate, existing, nil
			}
			candidate = ""
		}
		if candidate != "" {
			if _, used := c.issued[string(candidate)]; !used {
				c.issued[string(candidate)] = struct{}{}
				title := strings.TrimSpace(meta.TopicTitle)
				if title == "" {
					title = c.defaultTopicTitle
				}
				created := meta.CreatedAt.UTC().UnixMilli()
				if created < 0 {
					created = 0
				}
				record := topicRecord{ID: candidate, Title: title, CreatedAtMs: created}
				topics[candidate] = record
				return candidate, record, nil
			}
		}
	}
	raw, err := c.nextIDLocked("topic")
	if err != nil {
		return "", topicRecord{}, err
	}
	title := strings.TrimSpace(meta.TopicTitle)
	if title == "" {
		title = c.defaultTopicTitle
	}
	created := meta.CreatedAt.UTC().UnixMilli()
	if created < 0 {
		created = 0
	}
	record := topicRecord{ID: protocol.TopicID(raw), Title: title, CreatedAtMs: created}
	topics[record.ID] = record
	return record.ID, record, nil
}

func (c *Catalog) migrationProfileLocked(ctx context.Context, workspaceRoot string, meta agent.BranchMeta) (protocol.ResolvedProfile, error) {
	profile := profileFromMeta(meta)
	if meta.RemoteProfileVersion >= 1 && validateResolvedProfile(profile) == nil {
		return profile, nil
	}
	return c.resolveCompleteProfileLocked(ctx, workspaceRoot, profileSelectionFromMeta(meta))
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func validateMetaSidecar(sessionPath string, required bool) error {
	info, err := os.Lstat(agent.BranchMetaPath(sessionPath))
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return catalogError(protocol.ErrSessionPersistFailed, errors.New("Session sidecar is not a regular file"))
	}
	return nil
}

func canonicalizeAdditionalDirectories(primary string, paths []string) ([]string, error) {
	seen := map[string]bool{pathKey(primary): true}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(primary, path)
		}
		canonical, err := canonicalExistingDirectory(path)
		if err != nil {
			return nil, err
		}
		key := pathKey(canonical)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, canonical)
	}
	return result, nil
}

func sessionPathInStore(storeDir, path string) bool {
	storeCanonical, err := filepath.EvalSymlinks(storeDir)
	if err != nil {
		return false
	}
	pathCanonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return pathWithin(storeCanonical, pathCanonical)
}

func migrationErrorf(format string, args ...any) error {
	return catalogError(protocol.ErrSessionPersistFailed, fmt.Errorf(format, args...))
}
