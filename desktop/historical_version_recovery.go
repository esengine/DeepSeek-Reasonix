package main

import (
	"context"
	"path/filepath"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/identitylock"
	"reasonix/internal/session"
)

// Called only for an explicit version import, while the source's import and
// directory ownership guards are held. Startup and ordinary opens never rekey
// an old adoption or turn a failed replacement into a new visible branch.
func (a *App) resumeConflictingHistoricalVersion(ctx context.Context, state workspacestate.State, source historicalSource, workspaceID string) (SessionRestoreResult, bool, error) {
	if source.version == "" || source.format != "canonical" {
		return SessionRestoreResult{}, false, nil
	}
	baseKey := desktopSourceKey(source.path, source.head)
	var candidate *workspacestate.Operation
	for _, op := range state.PendingOperations {
		if op.Kind != "import" || op.Phase != "content_ready" || op.Mapping == nil ||
			op.Mapping.SourceKey != baseKey || op.Mapping.Fingerprint != source.version || op.WorkspaceID != workspaceID ||
			op.Mapping.Format != source.format || op.Mapping.HeadID != source.head || !sameDesktopPath(op.Mapping.Path, source.path) {
			continue
		}
		if candidate != nil {
			return SessionRestoreResult{}, false, nil
		}
		copy := op
		candidate = &copy
	}
	if candidate == nil {
		return SessionRestoreResult{}, false, nil
	}
	if len(candidate.SessionIDs) != 1 {
		return SessionRestoreResult{}, false, nil
	}
	releaseMutation, ok := a.tryLockRuntimeMutation("recover historical version")
	if !ok {
		return SessionRestoreResult{}, true, errTopicArchiveBusy
	}
	defer releaseMutation()
	// As with cold export, the source directory guard plus a shared writer
	// lock freezes the source without opening or modifying its event log.
	releaseSource, err := identitylock.TryAcquireMode(filepath.Join(source.path, "writer.lock"), identitylock.ModeShared)
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	defer releaseSource()
	fingerprint, err := desktopSourceFingerprint(source.path)
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	if fingerprint != source.version {
		return SessionRestoreResult{}, true, workspacestate.ErrMutationConflict
	}
	id := candidate.SessionIDs[0]
	releaseTarget, err := session.NewFilesystemPersistence(a.desktopSessions.root).AcquireMaintenance(id)
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	defer releaseTarget()
	target := session.SessionRef{HostID: localDesktopHostID, SessionID: id}
	if err := a.validateDesktopWorkspaceMembership(ctx, workspaceID, target); err != nil {
		return SessionRestoreResult{}, true, err
	}
	if _, err := a.desktopSessionService("").Query().Snapshot(ctx, target); err != nil {
		return SessionRestoreResult{}, true, err
	}
	old, err := session.NewService("migration-source", session.NewFilesystemPersistence(filepath.Dir(source.path)))
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	defer func() { _ = old.Shutdown(context.Background()) }()
	want, err := canonicalMigrationDigest(ctx, old.Query(), session.SessionRef{HostID: "migration-source", SessionID: filepath.Base(source.path)})
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	got, err := canonicalMigrationDigest(ctx, a.desktopSessionService("").Query(), target)
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	if got != want {
		return SessionRestoreResult{}, true, workspacestate.ErrMutationConflict
	}
	if err := a.workspaceRegistry().CommitHistoricalVersion(ctx, *candidate, baseKey+":review:"+source.version); err != nil {
		return SessionRestoreResult{}, true, err
	}
	updated, err := a.workspaceRegistry().Load(ctx)
	if err != nil {
		return SessionRestoreResult{}, true, err
	}
	return SessionRestoreResult{Session: target, WorkspaceID: workspaceID, Generation: updated.Generation}, true, nil
}