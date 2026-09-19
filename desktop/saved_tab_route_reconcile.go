package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/session"
	"reasonix/internal/store"
)

type savedTabRouteCandidate struct {
	sessionID string
	kind      string
}

func savedTabRouteCandidateForPath(raw string) savedTabRouteCandidate {
	value := strings.TrimSpace(raw)
	if value == "" {
		return savedTabRouteCandidate{}
	}
	if strings.HasPrefix(value, remoteSessionIDRoutePrefix) {
		locator := classifySessionLocator(value)
		if locator.kind != sessionLocatorCanonical {
			return savedTabRouteCandidate{kind: "invalid_route"}
		}
		return savedTabRouteCandidate{sessionID: locator.ref.SessionID, kind: "canonical_route"}
	}
	if !persistedPathLooksAbsolute(value) {
		return savedTabRouteCandidate{}
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	base := normalized[strings.LastIndex(normalized, "/")+1:]
	if !strings.HasPrefix(base, remoteSessionIDRoutePrefix) {
		return savedTabRouteCandidate{}
	}
	// A real legacy transcript or sidecar can legally contain a colon on
	// POSIX. Those names remain paths and are never upgraded by this repair.
	if strings.HasSuffix(strings.ToLower(base), ".jsonl") || strings.HasSuffix(strings.ToLower(base), ".meta") {
		return savedTabRouteCandidate{}
	}
	locator := classifySessionLocator(base)
	if locator.kind != sessionLocatorCanonical {
		return savedTabRouteCandidate{kind: "invalid_route"}
	}
	return savedTabRouteCandidate{sessionID: locator.ref.SessionID, kind: "pseudo_route_path"}
}

func persistedPathLooksAbsolute(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return true
	}
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func (a *App) normalizeSavedTabRoute(ctx context.Context, entry desktopTabEntry, evidence savedTabReconcileEvidence) (desktopTabEntry, bool, *savedTabReconcileDecision) {
	candidate := savedTabRouteCandidateForPath(entry.SessionPath)
	failed := func(reason string, pending, recovery bool) (desktopTabEntry, bool, *savedTabReconcileDecision) {
		return entry, false, &savedTabReconcileDecision{
			outcome: preserveError, reason: reason, identityKind: candidate.kind,
			hadPending: pending, hadRecoveryOwner: recovery,
		}
	}
	if candidate.kind == "" {
		return entry, false, nil
	}
	if candidate.kind == "invalid_route" || candidate.sessionID == "" {
		return failed("invalid_route", false, false)
	}
	if id := strings.TrimSpace(entry.SessionID); id != "" && id != candidate.sessionID {
		return failed("identity_conflict", false, false)
	}
	if evidence.registryErr != nil || evidence.draftErr != nil {
		return failed("persistence_state_unavailable", false, false)
	}

	pendingID, pendingFound, pendingConflict := savedTabPendingSessionIdentity(desktopTabEntry{
		CreateOperationID: entry.CreateOperationID,
	}, evidence)
	if pendingConflict || (pendingFound && pendingID != candidate.sessionID) {
		return failed("pending_identity_conflict", true, false)
	}
	if candidate.kind == "canonical_route" {
		entry.SessionID = candidate.sessionID
		entry.SessionPath = ""
		return entry, true, nil
	}

	present, artifactErr := savedTabPseudoRouteArtifactsPresent(entry.SessionPath)
	if artifactErr != nil {
		return failed("legacy_artifacts_unreadable", pendingFound, false)
	}
	if present {
		return failed("legacy_artifacts_present", pendingFound, false)
	}
	confirmed, conflict, recoveryOwner := a.savedTabPseudoRouteIdentityConfirmed(ctx, entry, candidate.sessionID, evidence, pendingID, pendingFound)
	if conflict {
		return failed("identity_conflict", pendingFound, recoveryOwner)
	}
	if !confirmed {
		return failed("identity_evidence_absent", pendingFound, recoveryOwner)
	}
	entry.SessionID = candidate.sessionID
	entry.SessionPath = ""
	return entry, true, nil
}

func savedTabPseudoRouteArtifactsPresent(path string) (bool, error) {
	return savedTabPseudoRouteArtifactsPresentWith(path, os.Lstat)
}

func savedTabPseudoRouteArtifactsPresentWith(path string, lstat func(string) (os.FileInfo, error)) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if runtime.GOOS == "windows" && savedTabRouteCandidateForPath(path).kind == "pseudo_route_path" {
		// The route colon is illegal in a Windows file name, so this spelling
		// cannot identify a real transcript or sidecar on the native filesystem.
		return false, nil
	}
	// Foreign Windows paths on POSIX cannot name a local artifact. Native
	// paths are probed without cleaning so the persisted spelling is preserved.
	if runtime.GOOS != "windows" && !filepath.IsAbs(path) {
		return false, nil
	}
	artifacts := append([]string{path}, store.SessionSidecarFiles(path)...)
	artifacts = append(artifacts,
		store.SessionLockFile(path), store.SessionLeaseLock(path), store.SessionLeaseInfo(path),
		store.SessionCheckpointDir(path), store.SessionJobsDir(path), store.SessionInboxDir(path),
		store.SessionCleanupPending(path),
	)
	seen := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == "" || seen[artifact] {
			continue
		}
		seen[artifact] = true
		if _, err := lstat(artifact); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func (a *App) savedTabPseudoRouteIdentityConfirmed(ctx context.Context, entry desktopTabEntry, sessionID string, evidence savedTabReconcileEvidence, pendingID string, pendingFound bool) (bool, bool, bool) {
	confirmed := pendingFound && pendingID == sessionID
	owners := savedTabDurableRouteOwnerIDs(entry, sessionID, evidence)
	if len(owners) > 1 || (len(owners) == 1 && !owners[sessionID]) {
		return false, true, len(owners) > 0
	}
	if owners[sessionID] {
		confirmed = true
	}

	info, err := a.desktopSessionService("").Query().Stat(ctx, session.SessionRef{HostID: localDesktopHostID, SessionID: sessionID})
	if err == nil {
		workspace, consistent := savedTabCanonicalWorkspace(evidence.registry, info, sessionID)
		if consistent && savedTabMatchesWorkspace(entry, workspace) {
			confirmed = true
		}
	}
	return confirmed, false, len(owners) > 0
}

func savedTabDurableRouteOwnerIDs(entry desktopTabEntry, candidateID string, evidence savedTabReconcileEvidence) map[string]bool {
	owners := map[string]bool{}
	operationID := strings.TrimSpace(entry.CreateOperationID)
	rawPath := strings.TrimSpace(entry.SessionPath)
	workspaceID := restoredWorkspaceID(entry)
	for sessionID, pending := range evidence.registry.PendingCreates {
		if (operationID != "" && pending.OperationID == operationID) || (sessionID == candidateID && pending.WorkspaceID == workspaceID) {
			owners[strings.TrimSpace(sessionID)] = true
		}
	}
	for _, operation := range evidence.draftOps {
		if operationID != "" && operation.ID == operationID {
			owners[strings.TrimSpace(operation.SessionID)] = true
		}
	}
	for _, operation := range evidence.registry.PendingOperations {
		if operationID != "" && strings.TrimSpace(operation.ID) == operationID {
			for _, id := range operation.SessionIDs {
				owners[strings.TrimSpace(id)] = true
			}
		}
		if operation.Mapping != nil && strings.TrimSpace(operation.Mapping.Path) == rawPath {
			owners[strings.TrimSpace(operation.Mapping.SessionID)] = true
		}
		if operation.WorkspaceID == workspaceID && slices.Contains(operation.SessionIDs, candidateID) {
			owners[candidateID] = true
		}
	}
	for _, mapping := range evidence.registry.SourceMappings {
		if strings.TrimSpace(mapping.Path) == rawPath {
			owners[strings.TrimSpace(mapping.SessionID)] = true
		}
		if mapping.WorkspaceID == workspaceID && mapping.SessionID == candidateID {
			owners[candidateID] = true
		}
	}
	for _, recovery := range evidence.registry.RecoveryEntries {
		if strings.TrimSpace(recovery.Path) == rawPath {
			owners[strings.TrimSpace(recovery.SessionID)] = true
		}
		if recovery.SessionID == candidateID && (recovery.WorkspaceID == workspaceID || recovery.Scope == entry.Scope && sameDesktopPath(recovery.WorkspaceRoot, entry.WorkspaceRoot)) {
			owners[candidateID] = true
		}
	}
	delete(owners, "")
	return owners
}

func blockSavedTabUnsafeRestore(file desktopTabsFile, reason string) desktopTabsFile {
	file.Tabs = append([]desktopTabEntry(nil), file.Tabs...)
	for index := range file.Tabs {
		path := strings.TrimSpace(file.Tabs[index].SessionPath)
		unsafe := savedTabRouteCandidateForPath(path).kind != ""
		if !unsafe && path != "" {
			_, ok, err := legacySessionPathForFileAccess(path)
			unsafe = err != nil || !ok
		}
		if unsafe {
			file.Tabs[index].restoreBlocked = true
			file.Tabs[index].restoreBlockReason = reason
		}
	}
	return file
}
