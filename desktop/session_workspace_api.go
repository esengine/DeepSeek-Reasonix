package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/agent"
	"reasonix/internal/session"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const localDesktopHostID = "local"

type WorkspaceSummary struct {
	ID         string   `json:"id"`
	Root       string   `json:"root"`
	Title      string   `json:"title"`
	SessionIDs []string `json:"sessionIds"`
	Visible    bool     `json:"visible"`
	CreatedAt  int64    `json:"createdAt"`
	UpdatedAt  int64    `json:"updatedAt"`
}

type WorkspacePendingCreate struct {
	OperationID string `json:"operationId"`
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
	CreatedAt   int64  `json:"createdAt"`
}

type WorkspaceSnapshot struct {
	Generation         uint64                   `json:"generation"`
	Workspaces         []WorkspaceSummary       `json:"workspaces"`
	ArchivedSessionIDs []string                 `json:"archivedSessionIds"`
	PendingCreates     []WorkspacePendingCreate `json:"pendingCreates"`
}

type WorkspaceSessionPage struct {
	Sessions           []WorkspaceSessionSummary `json:"sessions"`
	NextCursor         string                    `json:"nextCursor,omitempty"`
	RegistryGeneration uint64                    `json:"registryGeneration"`
}

type SessionArchitectureDiagnostics struct {
	PendingOperations       int    `json:"pending_operations"`
	MissingMembers          int    `json:"missing_members"`
	IdentityMismatches      int    `json:"identity_mismatches"`
	SourceConflicts         int    `json:"source_conflicts"`
	RecoveryEntries         int    `json:"recovery_entries"`
	SessionHeadersTotal     int    `json:"session_headers_total"`
	WorkspaceMembersTotal   int    `json:"workspace_members_total"`
	UnassignedSessions      int    `json:"unassigned_sessions"`
	MigrationPending        int    `json:"migration_pending"`
	MigrationFailed         int    `json:"migration_failed"`
	MigrationCompleted      int    `json:"migration_completed"`
	ProjectionPending       int    `json:"projection_pending"`
	ProjectionFailed        int    `json:"projection_failed"`
	PendingCreateRecovered  uint64 `json:"pending_create_recovered"`
	PruneBlockedPersistence uint64 `json:"prune_blocked_persistence"`
}

func unixMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func (a *App) GetWorkspaceSnapshot() (WorkspaceSnapshot, error) {
	state, err := a.workspaceRegistry().Load(context.Background())
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	result := WorkspaceSnapshot{
		Generation:         state.Generation,
		Workspaces:         make([]WorkspaceSummary, 0, len(state.WorkspaceIDs)),
		ArchivedSessionIDs: append([]string{}, state.ArchivedSessionIDs...),
		PendingCreates:     make([]WorkspacePendingCreate, 0, len(state.PendingCreates)),
	}
	for _, id := range state.WorkspaceIDs {
		workspace, ok := state.Workspaces[id]
		if !ok {
			continue
		}
		result.Workspaces = append(result.Workspaces, WorkspaceSummary{
			ID: workspace.ID, Root: workspace.Root, Title: workspace.Title,
			SessionIDs: append([]string{}, workspace.SessionIDs...), Visible: workspace.Visible,
			CreatedAt: unixMillis(workspace.CreatedAt), UpdatedAt: unixMillis(workspace.UpdatedAt),
		})
	}
	for _, pending := range state.PendingCreates {
		result.PendingCreates = append(result.PendingCreates, WorkspacePendingCreate{
			OperationID: pending.OperationID, WorkspaceID: pending.WorkspaceID,
			SessionID: pending.SessionID, CreatedAt: unixMillis(pending.CreatedAt),
		})
	}
	return result, nil
}

func (a *App) GetSessionArchitectureDiagnostics() (SessionArchitectureDiagnostics, error) {
	state, err := a.workspaceRegistry().Load(context.Background())
	if err != nil {
		return SessionArchitectureDiagnostics{}, err
	}
	infos, listErr := listAllCanonicalSessionInfo(context.Background(), a.desktopSessionService("").Query())
	result := SessionArchitectureDiagnostics{
		PendingCreateRecovered:  a.desktopSessions.pendingCreateRecovered.Load(),
		PruneBlockedPersistence: a.desktopSessions.pruneBlockedPersistence.Load(),
	}
	members := map[string]bool{}
	for _, op := range state.PendingOperations {
		if op.Phase != "committed" {
			result.PendingOperations++
		}
	}
	for _, entry := range state.RecoveryEntries {
		if entry.Status == "restored" {
			continue
		}
		result.RecoveryEntries++
		if strings.Contains(entry.Reason, "conflict") {
			result.SourceConflicts++
		}
	}
	for _, workspace := range state.Workspaces {
		result.WorkspaceMembersTotal += len(workspace.SessionIDs)
		for _, sessionID := range workspace.SessionIDs {
			members[sessionID] = true
			if info, found := infos[sessionID]; !found {
				result.MissingMembers++
			} else if !sameDesktopPath(info.CWD, workspace.Root) {
				result.IdentityMismatches++
			}
		}
	}
	for sessionID, info := range infos {
		if info.Origin != "" {
			result.SessionHeadersTotal++
		}
		if !members[sessionID] {
			result.UnassignedSessions++
		}
		switch info.MetadataStatus {
		case session.MetadataPending:
			result.ProjectionPending++
		case session.MetadataFailed:
			result.ProjectionFailed++
		}
	}
	desktopMigrationMu.Lock()
	var ledger desktopMigrationLedger
	body, readErr := os.ReadFile(desktopMigrationLedgerPath())
	if readErr == nil {
		readErr = json.Unmarshal(body, &ledger)
	}
	desktopMigrationMu.Unlock()
	if readErr != nil && !os.IsNotExist(readErr) {
		return result, readErr
	}
	for _, record := range ledger.Records {
		switch record.Status {
		case "pending":
			result.MigrationPending++
		case "failed":
			result.MigrationFailed++
		case "completed":
			result.MigrationCompleted++
		}
	}
	return result, listErr
}

func (a *App) ListWorkspaceSessions(workspaceID, queryText, cursor string, limit int, includeArchived bool) (WorkspaceSessionPage, error) {
	state, err := a.workspaceRegistry().Load(context.Background())
	if err != nil {
		return WorkspaceSessionPage{}, err
	}
	workspace, ok := state.Workspaces[strings.TrimSpace(workspaceID)]
	if !ok {
		return WorkspaceSessionPage{}, workspacestate.ErrWorkspaceNotFound
	}
	start, err := decodeWorkspaceSessionCursor(cursor, state.Generation)
	if err != nil {
		return WorkspaceSessionPage{}, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	service := a.desktopSessionService("")
	archived := make(map[string]bool, len(state.ArchivedSessionIDs))
	for _, id := range state.ArchivedSessionIDs {
		archived[id] = true
	}
	ids := make([]string, 0, len(workspace.SessionIDs))
	for _, id := range workspace.SessionIDs {
		if state.SessionStates[id].Lifecycle == workspacestate.Deleted {
			continue
		}
		if includeArchived || !archived[id] {
			ids = append(ids, id)
		}
	}
	infos, listErr := listWorkspaceSessionInfo(context.Background(), service.Query(), ids)
	needle := strings.ToLower(strings.TrimSpace(queryText))
	rows := make([]WorkspaceSessionSummary, 0, len(workspace.SessionIDs))
	for _, sessionID := range workspace.SessionIDs {
		if state.SessionStates[sessionID].Lifecycle == workspacestate.Deleted {
			continue
		}
		isArchived := archived[sessionID]
		if isArchived && !includeArchived {
			continue
		}
		info, found := infos[sessionID]
		row := workspaceSessionRow(workspace.ID, sessionID, info, found, isArchived, service)
		if needle != "" && !strings.Contains(strings.ToLower(row.Title+"\n"+row.Preview+"\n"+sessionID), needle) {
			continue
		}
		rows = append(rows, row)
	}
	if start > len(rows) {
		start = len(rows)
	}
	end := min(start+limit, len(rows))
	page := WorkspaceSessionPage{
		Sessions:           append([]WorkspaceSessionSummary{}, rows[start:end]...),
		RegistryGeneration: state.Generation,
	}
	if end < len(rows) {
		page.NextCursor = fmt.Sprintf("%d:%d", state.Generation, end)
	}
	if listErr != nil && len(page.Sessions) == 0 {
		return page, listErr
	}
	return page, nil
}

type workspaceSessionInfoReader interface {
	Stat(context.Context, session.SessionRef) (session.SessionInfo, error)
}

// The registry already owns membership. Reading each workspace's own headers
// avoids a complete catalog traversal for every workspace in a sidebar refresh.
func listWorkspaceSessionInfo(ctx context.Context, reader workspaceSessionInfoReader, ids []string) (map[string]session.SessionInfo, error) {
	infos := make(map[string]session.SessionInfo, len(ids))
	seen := make(map[string]bool, len(ids))
	var readErr error
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		info, err := reader.Stat(ctx, session.SessionRef{HostID: localDesktopHostID, SessionID: id})
		if errors.Is(err, session.ErrSessionNotFound) {
			continue
		}
		if err != nil {
			readErr = errors.Join(readErr, err)
			info = session.SessionInfo{SessionID: id, Error: err.Error(), MetadataStatus: session.MetadataFailed}
		}
		infos[id] = info
	}
	return infos, readErr
}

// Double-collect the owner metadata around list materialization. Registry and
// catalog revisions alone do not observe a live session's title/result events.
// Stat reads metadata only; it never synchronously replays cold transcripts.
func workspaceSessionInfoUnchanged(ctx context.Context, reader workspaceSessionInfoReader, ids []string, before map[string]session.SessionInfo) bool {
	after, _ := listWorkspaceSessionInfo(ctx, reader, ids)
	return reflect.DeepEqual(before, after)
}

func listAllCanonicalSessionInfo(ctx context.Context, query *session.Query) (map[string]session.SessionInfo, error) {
	infos := map[string]session.SessionInfo{}
	if query == nil {
		return infos, errors.New("desktop canonical session query is unavailable")
	}
	var cursor string
	for {
		page, err := query.List(ctx, cursor, 100)
		if err != nil {
			return infos, err
		}
		for _, info := range page.Sessions {
			infos[info.SessionID] = info
		}
		if page.NextCursor == "" {
			return infos, nil
		}
		if page.NextCursor == cursor {
			return infos, errors.New("desktop canonical session cursor did not advance")
		}
		cursor = page.NextCursor
	}
}

func workspaceSessionRow(workspaceID, sessionID string, info session.SessionInfo, found, archived bool, service *session.Service) WorkspaceSessionSummary {
	ref := session.SessionRef{HostID: localDesktopHostID, SessionID: sessionID}
	row := WorkspaceSessionSummary{
		Ref: ref, WorkspaceID: workspaceID, Archived: archived,
		MetadataStatus: "indexing", Health: "migrating",
	}
	if found {
		row.Title, row.Preview, row.Turns = info.Title, info.Preview, info.Turns
		row.CreatedAt, row.UpdatedAt = unixMillis(info.CreatedAt), unixMillis(info.UpdatedAt)
		row.ResultSequence = info.ResultSequence
		row.ModelRef, row.ParentSessionID, row.Origin = info.ModelRef, info.ParentSessionID, string(info.Origin)
		row.Blank = info.MetadataStatus == session.MetadataReady && info.Turns == 0 && strings.TrimSpace(info.Title) == "" && strings.TrimSpace(info.Preview) == ""
		row.MetadataStatus = info.MetadataStatus
		row.Health = "healthy"
		if info.Error != "" {
			row.MetadataStatus, row.Health = "failed", "read_only"
		}
	}
	if service != nil {
		_, row.Running = service.Runtime(ref)
	}
	return row
}

func decodeWorkspaceSessionCursor(cursor string, generation uint64) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, errors.New("invalid workspace session cursor")
	}
	wantGeneration, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || wantGeneration != generation {
		return 0, errors.New("workspace session cursor is stale")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset < 0 {
		return 0, errors.New("invalid workspace session cursor")
	}
	return offset, nil
}

func validateLocalSessionRef(ref session.SessionRef) error {
	if ref.HostID != localDesktopHostID || strings.TrimSpace(ref.SessionID) == "" {
		return errors.New("a local canonical session reference is required")
	}
	return nil
}

func (a *App) ArchiveCanonicalSession(ref session.SessionRef) error {
	_, err := a.archiveCanonicalSessionWithOperation(ref, "archive-"+strings.TrimPrefix(newTabID(), "tab_"))
	return err
}

func (a *App) RestoreCanonicalSession(ref session.SessionRef) error {
	_, err := a.restoreCanonicalSessionWithOperation(ref, "restore-"+strings.TrimPrefix(newTabID(), "tab_"))
	return err
}

func (a *App) MoveWorkspaceSession(workspaceID, sessionID, beforeSessionID string) error {
	ref := session.SessionRef{HostID: localDesktopHostID, SessionID: strings.TrimSpace(sessionID)}
	_, err := a.moveWorkspaceSessionWithOperation(
		ref,
		workspaceID,
		beforeSessionID,
		"move-"+strings.TrimPrefix(newTabID(), "tab_"),
	)
	return err
}

func (a *App) archiveCanonicalSessionWithOperation(ref session.SessionRef, operationID string) (SessionTarget, error) {
	if err := validateLocalSessionRef(ref); err != nil {
		return SessionTarget{}, err
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = "archive-" + strings.TrimPrefix(newTabID(), "tab_")
	}
	release, ok := a.tryLockRuntimeMutation("archive session")
	if !ok {
		return SessionTarget{}, errTopicArchiveBusy
	}
	fallback, err := a.archiveSessionRefsWithOperation([]session.SessionRef{ref}, operationID)
	release()
	if err != nil {
		return SessionTarget{}, err
	}
	if fallback.needs {
		_ = a.openFallbackRuntime(fallback)
	}
	a.emitProjectTreeChanged()
	target, err := a.resolveCanonicalSessionTargetState(ref, "", true)
	if err != nil {
		target = SessionTarget{SessionRef: ref}
	}
	a.emitSessionTargetChange("session_archived", SessionTargetChangeEvent{
		TargetKey: target.key(), OperationID: operationID,
		LifecycleGeneration: target.LifecycleGeneration, WorkspaceID: target.WorkspaceID,
	})
	return target, nil
}

func (a *App) restoreCanonicalSessionWithOperation(ref session.SessionRef, operationID string) (SessionTarget, error) {
	if err := validateLocalSessionRef(ref); err != nil {
		return SessionTarget{}, err
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = "restore-" + strings.TrimPrefix(newTabID(), "tab_")
	}
	if _, err := a.restoreCanonicalSession(a.bootContext(), ref, operationID); err != nil {
		return SessionTarget{}, err
	}
	target, err := a.resolveCanonicalSessionTargetState(ref, "", true)
	if err != nil {
		target = SessionTarget{SessionRef: ref}
	}
	a.emitSessionTargetChange("session_restored", SessionTargetChangeEvent{
		TargetKey: target.key(), OperationID: operationID,
		LifecycleGeneration: target.LifecycleGeneration, WorkspaceID: target.WorkspaceID,
	})
	return target, nil
}

func (a *App) moveWorkspaceSessionWithOperation(ref session.SessionRef, workspaceID, beforeSessionID, operationID string) (SessionTarget, error) {
	if err := validateLocalSessionRef(ref); err != nil {
		return SessionTarget{}, err
	}
	target, err := a.resolveCanonicalSessionTarget(ref, "")
	if err != nil {
		return SessionTarget{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || target.WorkspaceID != workspaceID {
		return SessionTarget{}, workspacestate.ErrMutationConflict
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = "move-" + strings.TrimPrefix(newTabID(), "tab_")
	}
	a.cancelAISessionTitle(target.key())
	if err := a.workspaceRegistry().MoveSessionIfUnchanged(
		context.Background(),
		workspaceID,
		ref.SessionID,
		beforeSessionID,
		target.LifecycleGeneration,
	); err != nil {
		return SessionTarget{}, err
	}
	a.emitProjectTreeChanged()
	target, err = a.resolveCanonicalSessionTargetState(ref, "", true)
	if err != nil {
		target = SessionTarget{SessionRef: ref, WorkspaceID: workspaceID}
	}
	a.emitSessionTargetChange("session_moved", SessionTargetChangeEvent{
		TargetKey: target.key(), OperationID: operationID,
		LifecycleGeneration: target.LifecycleGeneration, WorkspaceID: workspaceID,
	})
	return target, nil
}

func (a *App) RenameWorkspace(workspaceID, title string) error {
	if err := a.workspaceRegistry().RenameWorkspace(context.Background(), workspaceID, title); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) SetWorkspaceVisible(workspaceID string, visible bool) error {
	if strings.TrimSpace(workspaceID) == workspacestate.GlobalWorkspaceID && !visible {
		return errors.New("the global workspace cannot be hidden")
	}
	if err := a.workspaceRegistry().SetWorkspaceVisible(context.Background(), workspaceID, visible); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) MoveWorkspace(workspaceID, beforeWorkspaceID string) error {
	if err := a.workspaceRegistry().MoveWorkspace(context.Background(), workspaceID, beforeWorkspaceID); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// CreateSession is the SessionID-only creation facade used by the Workspace
// browser. The existing controller creation transaction still owns prompt/model
// seeding; this method only resolves a durable Workspace identity to that flow.
func (a *App) CreateSession(workspaceID string) (session.SessionRef, error) {
	state, err := a.workspaceRegistry().Load(context.Background())
	if err != nil {
		return session.SessionRef{}, err
	}
	workspace, ok := state.Workspaces[strings.TrimSpace(workspaceID)]
	if !ok {
		return session.SessionRef{}, workspacestate.ErrWorkspaceNotFound
	}
	scope, root := "project", workspace.Root
	if workspace.ID == workspacestate.GlobalWorkspaceID {
		scope, root = "global", ""
	}
	meta, err := a.EnsureBlankSurface(scope, root)
	if err != nil {
		return session.SessionRef{}, err
	}
	if meta.Session != nil {
		return *meta.Session, nil
	}
	ref := session.SessionRef{HostID: localDesktopHostID, SessionID: meta.SessionID}
	return ref, validateLocalSessionRef(ref)
}

// ForkSession creates an independently routed canonical child and publishes it
// immediately after its parent in the same Workspace. An empty boundary means
// the latest completed turn; no message-count inference is used.
// CopySessionTarget creates a full-history copy under a new durable identity.
// The caller-supplied operation id makes retries idempotent across storage
// publication and workspace attachment. The copy is never opened or selected.
func (a *App) CopySessionTarget(selector SessionSelector, operationID string) (SessionCreationResult, error) {
	target, err := a.resolveSessionMutationTarget(selector)
	if err != nil {
		return SessionCreationResult{}, err
	}
	key := target.key()
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = "copy-" + strings.TrimPrefix(newTabID(), "tab_")
	}
	sum := sha256.Sum256([]byte(key + "\x00" + operationID))
	childID := fmt.Sprintf("desktop-copy-%x", sum[:12])
	childRef := session.SessionRef{HostID: localDesktopHostID, SessionID: childID}

	workspaceID := strings.TrimSpace(target.WorkspaceID)
	if workspaceID == "" {
		workspaceID, err = a.ensureDesktopWorkspace(a.bootContext(), target.Scope, target.WorkspaceRoot)
		if err != nil {
			return SessionCreationResult{}, sessionOperationErrorForTarget(err, key, operationID)
		}
	}
	beforeID := ""
	if target.SessionRef.SessionID != "" {
		if state, loadErr := a.workspaceRegistry().Load(a.bootContext()); loadErr == nil {
			if workspace, ok := state.Workspaces[workspaceID]; ok {
				for index, id := range workspace.SessionIDs {
					if id == target.SessionRef.SessionID && index+1 < len(workspace.SessionIDs) {
						beforeID = workspace.SessionIDs[index+1]
						break
					}
				}
			}
		}
	}
	if err := a.workspaceRegistry().BeginCreate(a.bootContext(), workspacestate.PendingCreate{
		OperationID: operationID, WorkspaceID: workspaceID, SessionID: childID,
	}); err != nil {
		return SessionCreationResult{}, sessionOperationErrorForTarget(err, key, operationID)
	}

	service := a.desktopSessionService("")
	cwd := desktopWorkspaceRoot(target.Scope, target.WorkspaceRoot)
	if target.SessionRef.SessionID != "" {
		_, err = service.CopySession(a.bootContext(), session.CopyRequest{
			Source: target.SessionRef, ChildID: childID, OperationID: operationID, CWD: cwd,
		})
	} else if strings.TrimSpace(target.SessionPath) == "" {
		err = newSessionOperationError(sessionOperationNoMessages, "This session has no conversation history to copy.")
	} else {
		err = a.copyLegacySessionTarget(target, childRef, operationID, cwd)
	}
	if err != nil {
		_ = a.workspaceRegistry().AbortCreate(context.Background(), childID)
		return SessionCreationResult{}, sessionOperationErrorForTarget(err, key, operationID)
	}
	var attachErr error
	if target.SessionRef.SessionID != "" {
		attachErr = a.workspaceRegistry().AttachSessionFromSourceIfUnchanged(
			a.bootContext(),
			operationID,
			workspaceID,
			childID,
			beforeID,
			target.SessionRef.SessionID,
			target.LifecycleGeneration,
		)
	} else {
		attachErr = a.workspaceRegistry().AttachSession(a.bootContext(), operationID, workspaceID, childID, beforeID)
	}
	if attachErr != nil {
		if target.SessionRef.SessionID != "" && errors.Is(attachErr, workspacestate.ErrMutationConflict) {
			_ = service.Delete(context.Background(), childRef)
			_ = a.workspaceRegistry().AbortCreate(context.Background(), childID)
			return SessionCreationResult{}, sessionOperationErrorForTarget(attachErr, key, operationID)
		}
		// Storage already committed. Preserve the pending-create journal so a
		// retry with the same operation can finish attachment.
		return SessionCreationResult{
			Ref: childRef, OperationID: operationID, Committed: true, ProjectionPending: true,
		}, nil
	}
	a.emitProjectTreeChanged()
	a.emitSessionTargetChange("session_metadata_changed", SessionTargetChangeEvent{
		TargetKey:   "ref:" + childRef.HostID + ":" + childRef.SessionID,
		OperationID: operationID, WorkspaceID: workspaceID,
	})
	return SessionCreationResult{Ref: childRef, OperationID: operationID, Committed: true}, nil
}

func (a *App) copyLegacySessionTarget(target SessionTarget, child session.SessionRef, operationID, cwd string) error {
	container, err := os.MkdirTemp("", "reasonix-legacy-copy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(container)
	migrationRoot := filepath.Join(container, "migrated")
	migrated, err := session.MigrateLegacy(a.bootContext(), target.SessionPath, migrationRoot)
	if err != nil {
		return err
	}
	service := a.desktopSessionService("")
	if matched, matchErr := service.CopyOperationMatches(a.bootContext(), child, migrated.TargetID, operationID); matchErr == nil && matched {
		return nil
	}
	staging, err := session.NewService(localDesktopHostID, session.NewFilesystemPersistence(migrationRoot))
	if err != nil {
		return err
	}
	defer func() { _ = staging.CloseAll(context.Background()) }()
	source := session.SessionRef{HostID: localDesktopHostID, SessionID: migrated.TargetID}
	copied, err := staging.CopySession(a.bootContext(), session.CopyRequest{
		Source: source, ChildID: child.SessionID, OperationID: operationID, CWD: cwd,
	})
	if err != nil {
		return err
	}
	bundle := filepath.Join(container, "bundle")
	if err := staging.Export(a.bootContext(), copied.Child, bundle); err != nil {
		return err
	}
	_, err = service.ImportWithHeader(a.bootContext(), bundle, session.CreateOptions{
		SessionID: child.SessionID, CWD: cwd, Origin: session.SessionOriginLegacyImport,
	})
	if err == nil {
		return nil
	}
	if matched, matchErr := service.CopyOperationMatches(a.bootContext(), child, migrated.TargetID, operationID); matchErr == nil && matched {
		return nil
	}
	return err
}

func (a *App) ReadSessionHistory(ref session.SessionRef, cursor string, limit int) (HistoryPage, error) {
	if err := validateLocalSessionRef(ref); err != nil {
		return HistoryPage{}, err
	}
	beforeTurn := 0
	if strings.TrimSpace(cursor) != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return HistoryPage{}, errors.New("invalid session history cursor")
		}
		beforeTurn = parsed
	}
	messages, err := a.desktopSessionService("").Query().History(a.bootContext(), ref)
	if err != nil {
		return HistoryPage{}, err
	}
	page := historyPageFromProviderMessages(messages, func(content string) string { return content }, nil, nil, beforeTurn, limit)
	digest, _ := agent.ContentDigestForMessages(messages)
	return historyPageWithFingerprint(page, sessionRoute(ref.SessionID), digest), nil
}

// OpenSession installs exactly ref into the current local surface. It first
// proves the target identity and workspace exist; a missing or damaged identity
// never creates an empty replacement and never clears the currently visible log.
// History bodies are loaded after the runtime commits so a live writer is not
// snapshotted on the navigation goroutine.
func (a *App) OpenSession(ref session.SessionRef) (HistoryPage, error) {
	return a.openSessionWithNavigation(ref, a.desktopSessions.navigationSeq.Add(1))
}

func (a *App) openSessionWithNavigation(ref session.SessionRef, navigationSequence uint64) (HistoryPage, error) {
	if a.desktopSessions.navigationSeq.Load() != navigationSequence {
		return HistoryPage{}, errSessionNavigationSuperseded
	}
	if err := validateLocalSessionRef(ref); err != nil {
		return HistoryPage{}, err
	}
	if _, err := a.desktopSessionService("").Query().Stat(a.bootContext(), ref); err != nil {
		return HistoryPage{}, err
	}
	if a.desktopSessions.navigationSeq.Load() != navigationSequence {
		return HistoryPage{}, errSessionNavigationSuperseded
	}
	tab, ctrl := a.activeOrSingleLocalTab()
	if tab == nil {
		return HistoryPage{}, errors.New("workspace is not ready")
	}
	if _, err := a.resumeCanonicalSessionForTranscript(tab, ctrl, sessionRoute(ref.SessionID), defaultHistoryPageTurns, false, navigationSequence); err != nil {
		return HistoryPage{}, err
	}
	// runtime:rebuilt intentionally has no reload semantics. SessionRef opening
	// is navigation, so publish ready only after the exact target commits and
	// let every frontend owner re-read its metadata and history.
	a.emitReady(a.bootContext(), tab.ID)
	return HistoryPage{Messages: []HistoryMessage{}}, nil
}

func (a *App) RenameCanonicalSession(ref session.SessionRef, title string) error {
	if err := validateLocalSessionRef(ref); err != nil {
		return err
	}
	target, err := a.resolveCanonicalSessionTarget(ref, "")
	if err != nil {
		return err
	}
	return a.renameCanonicalSessionTarget(target, title)
}

// SetSessionPinned updates one durable session, never all members of its topic.
// An older path is adopted through the existing journal before storing the
// session-specific preference.
func (a *App) SetSessionPinned(selector SessionSelector, pinned bool) error {
	target, err := a.resolveSessionMutationTarget(selector)
	if err != nil {
		return err
	}
	if target.SessionRef.SessionID == "" && target.SessionPath != "" {
		workspaceID, ensureErr := a.ensureDesktopWorkspace(a.bootContext(), target.Scope, target.WorkspaceRoot)
		if ensureErr != nil {
			return ensureErr
		}
		if err = a.migrateLegacySession(a.bootContext(), target.SessionPath,
			desktopMigrationSource{scope: target.Scope, workspaceRoot: target.WorkspaceRoot}, workspaceID); err != nil {
			return err
		}
		target, err = a.resolveSessionTarget(SessionSelector{SessionPath: target.SessionPath})
		if err != nil {
			return err
		}
	}
	if target.SessionRef.SessionID == "" {
		return newSessionOperationError(sessionOperationNoMessages, "This empty session has no durable preference yet.")
	}
	if err := a.workspaceRegistry().UpdatePresentation(a.bootContext(), []string{target.SessionRef.SessionID}, nil, &pinned); err != nil {
		return err
	}
	a.emitProjectTreeMetadataChanged()
	return nil
}

func (a *App) renameCanonicalSessionTarget(target SessionTarget, title string) error {
	ref := target.SessionRef
	if err := validateLocalSessionRef(ref); err != nil {
		return err
	}
	a.cancelAISessionTitle(target.key())
	a.topicTitleMutationMu.Lock()
	defer a.topicTitleMutationMu.Unlock()
	err := a.workspaceRegistry().WithSessionUnchanged(
		a.bootContext(),
		ref.SessionID,
		target.WorkspaceID,
		target.LifecycleGeneration,
		func() error {
			return a.desktopSessionService("").SetTitle(a.bootContext(), ref, strings.TrimSpace(title))
		},
	)
	if err != nil {
		return err
	}
	a.updateCanonicalSessionTitle(ref, strings.TrimSpace(title))
	a.emitProjectTreeChanged()
	return nil
}
