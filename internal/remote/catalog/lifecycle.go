package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/store"
)

// LifecycleCreated is the durable catalog result used by session/new and
// session/fork.  It deliberately contains Host paths only inside the Host
// package boundary; wire results use Target and RuntimeEpoch exclusively.
type LifecycleCreated struct {
	ResolvedSession
	TopicID    protocol.TopicID
	TopicTitle string
}

// ClearTransition records the reversible durable part of session/clear.  The
// old transcript is not physically removed until RuntimeManager has installed
// the replacement and stopped the old Controller.  Rollback is therefore safe
// when replacement construction fails.
type ClearTransition struct {
	catalog     *Catalog
	Previous    ResolvedSession
	Replacement LifecycleCreated
	previousRec sessionRecord
	once        sync.Once
}

// ProfileTransition is an atomic sidecar profile update.  Runtime rebuild
// callers may Rollback if construction of the replacement Controller fails.
type ProfileTransition struct {
	catalog  *Catalog
	Target   protocol.RuntimeTarget
	Previous protocol.ResolvedProfile
	Current  protocol.ResolvedProfile
	once     sync.Once
}

// CreateSiblingSession persists a new empty Session in the source Topic using
// the source's exact already-resolved profile and additional directories.  It
// intentionally does not consult current Host defaults and does not copy Goal
// state or any other per-Session sidecar.
func (c *Catalog) CreateSiblingSession(ctx context.Context, expected protocol.HostEpoch, source protocol.RuntimeTarget) (LifecycleCreated, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(expected); err != nil {
		return LifecycleCreated{}, err
	}
	resolved, record, topic, meta, err := c.lifecycleSourceLocked(ctx, source)
	if err != nil {
		return LifecycleCreated{}, err
	}
	return c.createLifecycleSessionLocked(record.WorkspaceID, record.TopicID, topic.Title, resolved, meta, "")
}

// BeginClear atomically creates the replacement catalog identity and retires
// the old identity, while retaining the old artifacts for RuntimeManager's
// still-live Controller.  Call CleanupPrevious only after target migration;
// call Rollback if replacement construction fails.
func (c *Catalog) BeginClear(ctx context.Context, expected protocol.HostEpoch, source protocol.RuntimeTarget) (*ClearTransition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(expected); err != nil {
		return nil, err
	}
	resolved, record, topic, meta, err := c.lifecycleSourceLocked(ctx, source)
	if err != nil {
		return nil, err
	}
	created, err := c.createLifecycleArtifactLocked(record.WorkspaceID, record.TopicID, topic.Title, resolved, meta, "")
	if err != nil {
		return nil, err
	}
	// Hide the retired transcript before removing its registry record. Without
	// this marker a concurrent catalog read could discover the still-open old
	// artifact as a legacy Session and allocate it a second Remote identity.
	if err := agent.MarkCleanupPending(resolved.SessionPath, "remote-clear"); err != nil {
		_ = removeLifecycleArtifacts(created.SessionPath)
		return nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	newRecord := sessionRecord{
		ID: created.Target.SessionID, WorkspaceID: record.WorkspaceID,
		Path: created.SessionPath, TopicID: record.TopicID,
	}
	if err := c.mutateLocked(func() error {
		delete(c.state.Sessions, record.ID)
		c.state.RetiredSessionIDs[record.ID] = true
		c.state.Sessions[newRecord.ID] = newRecord
		return nil
	}); err != nil {
		_ = agent.ClearCleanupPending(resolved.SessionPath)
		_ = removeLifecycleArtifacts(created.SessionPath)
		return nil, err
	}
	return &ClearTransition{
		catalog: c, Previous: resolved, Replacement: created, previousRec: record,
	}, nil
}

// Rollback restores the previous identity and removes the unadmitted empty
// replacement.  It is idempotent and is valid only before CleanupPrevious.
func (t *ClearTransition) Rollback() error {
	if t == nil || t.catalog == nil {
		return nil
	}
	var result error
	t.once.Do(func() {
		c := t.catalog
		c.mu.Lock()
		defer c.mu.Unlock()
		result = c.mutateLocked(func() error {
			current, ok := c.state.Sessions[t.Replacement.Target.SessionID]
			if !ok || current.Path != t.Replacement.SessionPath || !c.state.RetiredSessionIDs[t.Previous.Target.SessionID] {
				return errors.New("catalog: clear rollback lost its durable transition")
			}
			delete(c.state.Sessions, t.Replacement.Target.SessionID)
			delete(c.state.RetiredSessionIDs, t.Previous.Target.SessionID)
			c.state.Sessions[t.previousRec.ID] = t.previousRec
			return nil
		})
		if result == nil {
			result = errors.Join(
				removeLifecycleArtifacts(t.Replacement.SessionPath),
				agent.ClearCleanupPending(t.Previous.SessionPath),
			)
		}
	})
	return result
}

// CleanupPrevious permanently removes the retired transcript and sidecars.
// A durable cleanup marker is written first.  Failure leaves the identity
// retired and returns cleanup_pending to the caller; startup/listing already
// excludes retired identities and the marker supports later reconciliation.
func (t *ClearTransition) CleanupPrevious() (protocol.SessionClearDisposition, error) {
	if t == nil || t.catalog == nil {
		return protocol.SessionCleanupPending, errors.New("catalog: nil clear transition")
	}
	disposition := protocol.SessionCleared
	var cleanupErr error
	t.once.Do(func() {
		path := t.Previous.SessionPath
		if !agent.IsCleanupPending(path) {
			if err := agent.MarkCleanupPending(path, "remote-clear"); err != nil {
				disposition = protocol.SessionCleanupPending
				cleanupErr = err
				return
			}
		}
		c := t.catalog
		for _, artifact := range lifecycleArtifacts(path) {
			if artifact == agent.CleanupPendingPath(path) {
				continue
			}
			if err := c.removeAll(artifact); err != nil {
				disposition = protocol.SessionCleanupPending
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		if disposition == protocol.SessionCleared {
			if err := agent.ClearCleanupPending(path); err != nil {
				disposition = protocol.SessionCleanupPending
				cleanupErr = err
			}
		}
	})
	return disposition, cleanupErr
}

// AdoptFork gives a real Controller.ForkSession transcript a fresh Remote
// identity in the source Topic.  Its local branch metadata is retained while
// Remote ownership fields are overwritten from the source's frozen values.
func (c *Catalog) AdoptFork(
	ctx context.Context,
	expected protocol.HostEpoch,
	source protocol.RuntimeTarget,
	checkpointID protocol.CheckpointID,
	forkPath string,
) (LifecycleCreated, error) {
	if strings.TrimSpace(string(checkpointID)) == "" {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("fork checkpoint identity is empty"))
	}
	return c.adoptControllerBranch(ctx, expected, source, checkpointID, forkPath)
}

// AdoptBranch gives a real Controller.BranchSession tip copy a fresh Remote
// identity. A tip branch has no checkpoint boundary to claim, so it retains
// local ParentID/ForkMessageIndex ancestry but deliberately omits a fabricated
// RemoteParentCheckpointID.
func (c *Catalog) AdoptBranch(
	ctx context.Context,
	expected protocol.HostEpoch,
	source protocol.RuntimeTarget,
	branchPath string,
) (LifecycleCreated, error) {
	return c.adoptControllerBranch(ctx, expected, source, "", branchPath)
}

func (c *Catalog) adoptControllerBranch(
	ctx context.Context,
	expected protocol.HostEpoch,
	source protocol.RuntimeTarget,
	checkpointID protocol.CheckpointID,
	forkPath string,
) (LifecycleCreated, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(expected); err != nil {
		return LifecycleCreated{}, err
	}
	forkPath = filepath.Clean(strings.TrimSpace(forkPath))
	if forkPath == "." || !filepath.IsAbs(forkPath) {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("fork transcript path is invalid"))
	}
	info, err := os.Lstat(forkPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = errors.New("fork transcript is not a regular file")
		}
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	// Controller fork/branch primitives persist the child before the catalog can
	// adopt it. Hide that short-lived unowned transcript while resolving the
	// source, otherwise the legacy discovery pass would import it under one
	// identity and this transaction would allocate a second identity.
	if err := agent.MarkCleanupPending(forkPath, "remote-branch-adopt"); err != nil {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	markerCleared := false
	defer func() {
		if !markerCleared {
			_ = agent.ClearCleanupPending(forkPath)
		}
	}()
	resolved, record, topic, sourceMeta, err := c.lifecycleSourceLocked(ctx, source)
	if err != nil {
		return LifecycleCreated{}, err
	}
	if !sessionPathInStore(resolved.SessionDir, forkPath) || forkPath == resolved.SessionPath {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("fork transcript is outside the source Session store"))
	}
	meta, ok, err := agent.LoadBranchMeta(forkPath)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("fork metadata is missing")
		}
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	idRaw, err := c.nextIDLocked("session")
	if err != nil {
		return LifecycleCreated{}, err
	}
	id := protocol.SessionID(idRaw)
	applyRemoteLifecycleMeta(&meta, id, record.WorkspaceID, record.TopicID, topic.Title, resolved, sourceMeta)
	meta.RemoteParentSessionID = string(source.SessionID)
	if checkpointID == "" {
		meta.RemoteParentCheckpointID = ""
	} else {
		meta.RemoteParentCheckpointID = string(checkpointID)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(forkPath, meta); err != nil {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	// Clear the discovery guard while still holding the catalog lock. No
	// catalog scan can observe the file between this point and registry commit.
	if err := agent.ClearCleanupPending(forkPath); err != nil {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	markerCleared = true
	newRecord := sessionRecord{ID: id, WorkspaceID: record.WorkspaceID, Path: forkPath, TopicID: record.TopicID}
	if err := c.mutateLocked(func() error {
		c.state.Sessions[id] = newRecord
		return nil
	}); err != nil {
		_ = removeLifecycleArtifacts(forkPath)
		return LifecycleCreated{}, err
	}
	return LifecycleCreated{ResolvedSession: ResolvedSession{
		Target:        protocol.RuntimeTarget{WorkspaceID: record.WorkspaceID, SessionID: id},
		WorkspaceRoot: resolved.WorkspaceRoot, AdditionalDirs: append([]string(nil), resolved.AdditionalDirs...),
		SessionDir: resolved.SessionDir, SessionPath: forkPath, ResolvedProfile: resolved.ResolvedProfile,
	}, TopicID: record.TopicID, TopicTitle: topic.Title}, nil
}

// RollbackCreatedSession removes a session/new or session/fork identity and
// its artifacts when a compound lifecycle operation has not been admitted.
// The identity remains retired so it can never be donated to another Session.
func (c *Catalog) RollbackCreatedSession(target protocol.RuntimeTarget) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.state.Sessions[target.SessionID]
	if !ok || record.WorkspaceID != target.WorkspaceID || record.TrashPath != "" {
		return catalogError(protocol.ErrSessionNotFound, errors.New("created Session is not live"))
	}
	if err := c.mutateLocked(func() error {
		delete(c.state.Sessions, target.SessionID)
		c.state.RetiredSessionIDs[target.SessionID] = true
		return nil
	}); err != nil {
		return err
	}
	if err := removeLifecycleArtifacts(record.Path); err != nil {
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return nil
}

// BeginProfilePatch merges a non-empty patch over the Session's complete
// profile, resolves the whole combination against authoritative Host config,
// and persists it as one sidecar transaction.
func (c *Catalog) BeginProfilePatch(ctx context.Context, expected protocol.HostEpoch, target protocol.RuntimeTarget, patch protocol.ProfilePatch) (*ProfileTransition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := patch.Validate(); err != nil {
		return nil, catalogError(protocol.ErrInvalidProfile, err)
	}
	if err := c.validateHostEpochLocked(expected); err != nil {
		return nil, err
	}
	resolved, _, _, meta, err := c.lifecycleSourceLocked(ctx, target)
	if err != nil {
		return nil, err
	}
	selection := profileSelectionFromResolved(resolved.ResolvedProfile)
	if patch.Model != nil {
		selection.Model = patch.Model
	}
	if patch.Effort != nil {
		selection.Effort = patch.Effort
	}
	if patch.CollaborationMode != nil {
		selection.CollaborationMode = patch.CollaborationMode
	}
	if patch.TokenMode != nil {
		selection.TokenMode = patch.TokenMode
	}
	if patch.ToolApprovalMode != nil {
		selection.ToolApprovalMode = patch.ToolApprovalMode
	}
	next, err := c.resolveCompleteProfileLocked(ctx, resolved.WorkspaceRoot, selection)
	if err != nil {
		return nil, err
	}
	previous := resolved.ResolvedProfile
	applyProfileMeta(&meta, next)
	if err := agent.SaveBranchMetaPreserveUpdated(resolved.SessionPath, meta); err != nil {
		return nil, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if err := c.mutateLocked(func() error { return nil }); err != nil {
		_ = c.writeProfileLocked(resolved.SessionPath, previous)
		return nil, err
	}
	return &ProfileTransition{catalog: c, Target: target, Previous: previous, Current: next}, nil
}

func (t *ProfileTransition) Rollback() error {
	if t == nil || t.catalog == nil {
		return nil
	}
	var result error
	t.once.Do(func() {
		c := t.catalog
		c.mu.Lock()
		defer c.mu.Unlock()
		resolved, err := c.resolveRuntimeTargetLocked(context.Background(), t.Target)
		if err != nil {
			result = err
			return
		}
		if resolved.ResolvedProfile != t.Current {
			result = errors.New("catalog: profile rollback lost its current revision")
			return
		}
		if err := c.writeProfileLocked(resolved.SessionPath, t.Previous); err != nil {
			result = err
			return
		}
		result = c.mutateLocked(func() error { return nil })
	})
	return result
}

// Commit prevents a deferred rollback after the corresponding runtime or
// in-place Controller update has succeeded.
func (t *ProfileTransition) Commit() {
	if t != nil {
		t.once.Do(func() {})
	}
}

func (c *Catalog) writeProfileLocked(path string, profile protocol.ResolvedProfile) error {
	_, err := agent.UpdateBranchMetaPreserveUpdated(path, func(meta *agent.BranchMeta) error {
		applyProfileMeta(meta, profile)
		return nil
	})
	if err != nil {
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return nil
}

func (c *Catalog) lifecycleSourceLocked(ctx context.Context, target protocol.RuntimeTarget) (ResolvedSession, sessionRecord, topicRecord, agent.BranchMeta, error) {
	resolved, err := c.resolveRuntimeTargetLocked(ctx, target)
	if err != nil {
		return ResolvedSession{}, sessionRecord{}, topicRecord{}, agent.BranchMeta{}, err
	}
	record := c.state.Sessions[target.SessionID]
	topic, ok := c.state.Topics[target.WorkspaceID][record.TopicID]
	if !ok || topic.Trashed {
		return ResolvedSession{}, sessionRecord{}, topicRecord{}, agent.BranchMeta{}, catalogError(protocol.ErrTopicNotFound, errors.New("source Topic is unavailable"))
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("source Session metadata is missing")
		}
		return ResolvedSession{}, sessionRecord{}, topicRecord{}, agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return resolved, record, topic, meta, nil
}

func (c *Catalog) createLifecycleSessionLocked(workspaceID protocol.WorkspaceID, topicID protocol.TopicID, topicTitle string, source ResolvedSession, sourceMeta agent.BranchMeta, name string) (LifecycleCreated, error) {
	created, err := c.createLifecycleArtifactLocked(workspaceID, topicID, topicTitle, source, sourceMeta, name)
	if err != nil {
		return LifecycleCreated{}, err
	}
	record := sessionRecord{ID: created.Target.SessionID, WorkspaceID: workspaceID, Path: created.SessionPath, TopicID: topicID}
	if err := c.mutateLocked(func() error {
		c.state.Sessions[record.ID] = record
		return nil
	}); err != nil {
		_ = removeLifecycleArtifacts(created.SessionPath)
		return LifecycleCreated{}, err
	}
	return created, nil
}

func (c *Catalog) createLifecycleArtifactLocked(workspaceID protocol.WorkspaceID, topicID protocol.TopicID, topicTitle string, source ResolvedSession, sourceMeta agent.BranchMeta, name string) (LifecycleCreated, error) {
	idRaw, err := c.nextIDLocked("session")
	if err != nil {
		return LifecycleCreated{}, err
	}
	id := protocol.SessionID(idRaw)
	path, err := createEmptySessionFile(source.SessionDir, source.ResolvedProfile.Model)
	if err != nil {
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	meta := agent.BranchMeta{Name: strings.TrimSpace(name), SchemaVersion: agent.BranchMetaCountsVersion}
	applyRemoteLifecycleMeta(&meta, id, workspaceID, topicID, topicTitle, source, sourceMeta)
	// New/Clear must not inherit Goal or recovery/branch ancestry.
	meta.Goal = ""
	meta.RemoteParentSessionID = ""
	meta.RemoteParentCheckpointID = ""
	meta.ParentID = ""
	meta.ForkTurn = 0
	meta.ForkMessageIndex = 0
	if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		_ = removeLifecycleArtifacts(path)
		return LifecycleCreated{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return LifecycleCreated{ResolvedSession: ResolvedSession{
		Target:        protocol.RuntimeTarget{WorkspaceID: workspaceID, SessionID: id},
		WorkspaceRoot: source.WorkspaceRoot, AdditionalDirs: append([]string(nil), source.AdditionalDirs...),
		SessionDir: source.SessionDir, SessionPath: path, ResolvedProfile: source.ResolvedProfile,
	}, TopicID: topicID, TopicTitle: topicTitle}, nil
}

func applyRemoteLifecycleMeta(meta *agent.BranchMeta, id protocol.SessionID, workspaceID protocol.WorkspaceID, topicID protocol.TopicID, topicTitle string, source ResolvedSession, sourceMeta agent.BranchMeta) {
	meta.RemoteSessionID = string(id)
	meta.Scope = "project"
	meta.WorkspaceRoot = source.WorkspaceRoot
	meta.TopicID = string(topicID)
	meta.TopicTitle = topicTitle
	meta.AdditionalDirs = append([]string(nil), source.AdditionalDirs...)
	meta.RemoteProfileVersion = 1
	applyProfileMeta(meta, source.ResolvedProfile)
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = sourceMeta.UpdatedAt
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = sourceMeta.CreatedAt
		}
	}
	_ = workspaceID // retained in the call shape to make cross-workspace copies impossible by construction.
}

func applyProfileMeta(meta *agent.BranchMeta, profile protocol.ResolvedProfile) {
	meta.Model = profile.Model
	meta.Effort = profile.Effort
	meta.Mode = string(profile.CollaborationMode)
	meta.TokenMode = string(profile.TokenMode)
	meta.ToolApprovalMode = string(profile.ToolApprovalMode)
	meta.RemoteProfileVersion = 1
}

func profileSelectionFromResolved(profile protocol.ResolvedProfile) protocol.ProfileSelection {
	model, effort := profile.Model, profile.Effort
	collaboration, token, approval := profile.CollaborationMode, profile.TokenMode, profile.ToolApprovalMode
	return protocol.ProfileSelection{Model: &model, Effort: &effort, CollaborationMode: &collaboration, TokenMode: &token, ToolApprovalMode: &approval}
}

func lifecycleArtifacts(path string) []string {
	result := []string{path}
	result = append(result, store.SessionSidecarFiles(path)...)
	result = append(result,
		store.SessionCheckpointDir(path), store.SessionJobsDir(path), store.SessionCleanupPending(path),
		store.SessionLockFile(path), store.SessionLeaseLock(path), store.SessionLeaseInfo(path),
	)
	return result
}

func removeLifecycleArtifacts(path string) error {
	var combined error
	for _, artifact := range lifecycleArtifacts(path) {
		if artifact == "" {
			continue
		}
		if err := os.RemoveAll(artifact); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (c LifecycleCreated) String() string {
	return fmt.Sprintf("%s/%s", c.Target.WorkspaceID, c.Target.SessionID)
}
