package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/desktop/internal/draftstate"
	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/session"
	"reasonix/internal/skill"
)

type SessionDraftSettings struct {
	Model             string                `json:"model"`
	Effort            string                `json:"effort,omitempty"`
	QualityFloor      string                `json:"qualityFloor,omitempty"`
	Mode              string                `json:"mode"`
	CollaborationMode string                `json:"collaborationMode,omitempty"`
	ToolApprovalMode  string                `json:"toolApprovalMode"`
	Goal              string                `json:"goal,omitempty"`
	DisabledMCP       map[string]ServerView `json:"disabledMcp"`
	MCPOrder          []string              `json:"mcpOrder"`
}

type SessionDraftView struct {
	SnapshotDigest string               `json:"snapshotDigest,omitempty"`
	ID             string               `json:"id"`
	WorkspaceID    string               `json:"workspaceId"`
	Scope          string               `json:"scope"`
	WorkspaceRoot  string               `json:"workspaceRoot"`
	Revision       uint64               `json:"revision"`
	ContentJSON    string               `json:"contentJson"`
	Settings       SessionDraftSettings `json:"settings"`
	Status         string               `json:"status"`
	UpdatedAt      int64                `json:"updatedAt"`
}

type SessionDraftSummary struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	Revision      uint64 `json:"revision"`
	HasContent    bool   `json:"hasContent"`
	State         string `json:"state,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
}

type SessionDraftSaveRequest struct {
	DraftID     string               `json:"draftId"`
	Revision    uint64               `json:"revision"`
	ContentJSON string               `json:"contentJson"`
	Settings    SessionDraftSettings `json:"settings"`
	Force       bool                 `json:"force,omitempty"`
}

type SessionDraftSaveResult struct {
	Draft    SessionDraftView `json:"draft"`
	Conflict bool             `json:"conflict"`
	Outcome  string           `json:"outcome"`
}

type SessionDraftSubmissionRequest struct {
	SourceContentJSON string               `json:"sourceContentJson,omitempty"`
	RequestID         string               `json:"requestId,omitempty"`
	SourceDigest      string               `json:"sourceDigest,omitempty"`
	SnapshotVersion   int                  `json:"snapshotVersion,omitempty"`
	DraftID           string               `json:"draftId"`
	Revision          uint64               `json:"revision"`
	Kind              string               `json:"kind,omitempty"`
	Display           string               `json:"display"`
	Input             string               `json:"input"`
	Invocations       []InvocationRequest  `json:"invocations"`
	Goal              string               `json:"goal,omitempty"`
	CollaborationMode string               `json:"collaborationMode,omitempty"`
	ToolApprovalMode  string               `json:"toolApprovalMode,omitempty"`
	WorkspaceRefs     []DraftWorkspaceRef  `json:"workspaceRefs"`
	Settings          SessionDraftSettings `json:"settings"`
}

type DraftWorkspaceRef struct {
	Path        string `json:"path"`
	IsDir       bool   `json:"isDir,omitempty"`
	DisplayPath string `json:"displayPath,omitempty"`
}

type SessionDraftSubmissionView struct {
	RequestID    string              `json:"requestId,omitempty"`
	Revision     uint64              `json:"revision"`
	CanResume    bool                `json:"canResume"`
	CanEdit      bool                `json:"canEdit"`
	CanCancel    bool                `json:"canCancel"`
	CanDiscard   bool                `json:"canDiscard"`
	OperationID  string              `json:"operationId"`
	DraftID      string              `json:"draftId"`
	Phase        string              `json:"phase"`
	Error        string              `json:"error,omitempty"`
	Session      *session.SessionRef `json:"session,omitempty"`
	SubmissionID string              `json:"submissionId"`
	Tab          *TabMeta            `json:"tab,omitempty"`
	UpdatedAt    int64               `json:"updatedAt"`
}

type SessionDraftContextView struct {
	Operation *SessionDraftSubmissionView `json:"operation,omitempty"`
	Draft     SessionDraftView            `json:"draft"`
	Commands  []CommandInfo               `json:"commands"`
	Servers   []ServerView                `json:"servers"`
	Models    []ModelInfo                 `json:"models,omitempty"`
}

type SessionDraftState struct {
	Draft     SessionDraftView            `json:"draft"`
	Operation *SessionDraftSubmissionView `json:"operation,omitempty"`
}

func (a *App) GetSessionDraftState(draftID string) (SessionDraftState, error) {
	record, op, err := a.draftStore().State(a.bootContext(), strings.TrimSpace(draftID))
	if err != nil {
		return SessionDraftState{}, err
	}
	if op != nil && (op.Phase == "dispatching" || op.Phase == "dispatching_shell" || op.Phase == "dispatch_unknown") {
		// Reconcile a receipt without creating a Controller; reread both sides
		// together so callers never observe a converted operation with an old slot.
		if _, checkErr := a.GetDraftSubmission(op.ID); checkErr == nil {
			record, op, err = a.draftStore().State(a.bootContext(), strings.TrimSpace(draftID))
			if err != nil {
				return SessionDraftState{}, err
			}
		}
	}
	view, err := draftView(record)
	if err != nil {
		return SessionDraftState{}, err
	}
	state := SessionDraftState{Draft: view}
	if op != nil {
		projection := draftOperationView(*op, a.metaForDraftSession(op.SessionID))
		state.Operation = &projection
	}
	return state, nil
}

func (a *App) ResumeDraftSubmission(operationID string, expectedRevision uint64) (SessionDraftSubmissionView, error) {
	op, err := a.draftStore().Operation(a.bootContext(), operationID)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if op.Revision != expectedRevision {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), draftstate.ErrConflict
	}
	release, err := a.draftStore().WorkerLease(op.SessionID)
	if err != nil {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
	}
	op, err = a.draftStore().ResumeOperation(a.bootContext(), operationID, expectedRevision)
	if err != nil {
		release()
		return SessionDraftSubmissionView{}, err
	}
	a.goSafe("resumeDraftSubmission", func() { _, _ = a.resumeDraftSubmissionOwned(op, release) })
	return draftOperationView(op, a.metaForDraftSession(op.SessionID)), nil
}

func (a *App) draftStore() *draftstate.Store {
	if a.desktopDrafts == nil {
		a.desktopDrafts = draftstate.New(config.DesktopDraftStatePath())
	}
	return a.desktopDrafts
}

func draftView(record draftstate.Draft) (SessionDraftView, error) {
	settings := SessionDraftSettings{DisabledMCP: map[string]ServerView{}, MCPOrder: []string{}}
	if strings.TrimSpace(record.SettingsJSON) != "" {
		if err := json.Unmarshal([]byte(record.SettingsJSON), &settings); err != nil {
			return SessionDraftView{}, err
		}
	}
	if settings.DisabledMCP == nil {
		settings.DisabledMCP = map[string]ServerView{}
	}
	if settings.MCPOrder == nil {
		settings.MCPOrder = []string{}
	}
	digest, err := draftstate.SnapshotDigest(record.ContentJSON, record.SettingsJSON)
	if err != nil {
		return SessionDraftView{}, err
	}
	return SessionDraftView{SnapshotDigest: digest, ID: record.ID, WorkspaceID: record.WorkspaceID, Scope: record.Scope,
		WorkspaceRoot: record.WorkspaceRoot, Revision: record.Revision, ContentJSON: record.ContentJSON,
		Settings: settings, Status: record.Status, UpdatedAt: record.UpdatedAt.UnixMilli()}, nil
}

func (a *App) defaultDraftSettings(scope, workspaceRoot string) SessionDraftSettings {
	actualRoot := workspaceRoot
	if scope != "project" {
		scope, workspaceRoot, actualRoot = "global", "", globalWorkspaceRoot()
	}
	model, approval := desktopNewSessionDefaults(scope, actualRoot)
	settings := SessionDraftSettings{Model: model, QualityFloor: tabQualityFloor(workspaceRoot, "standard"),
		Mode: tabModeFromAxes(false, approval == control.ToolApprovalDangerFullAccess), ToolApprovalMode: approval,
		DisabledMCP: map[string]ServerView{}, MCPOrder: []string{}}
	a.mu.RLock()
	if active := a.activeTabLocked(); active != nil {
		if effort := config.RebindSessionEffort(nil, active.model, settings.Model, active.effort); effort != nil {
			settings.Effort = *effort
		}
		settings.QualityFloor = tabQualityFloor(workspaceRoot, active.qualityFloorSafe())
		settings.DisabledMCP = cloneServerViewMap(active.disabledMCP)
		settings.MCPOrder = append([]string(nil), active.mcpOrder...)
	}
	a.mu.RUnlock()
	return settings
}

// OpenSessionDraft creates no Topic, Session, Controller, runtime, or lease.
func (a *App) OpenSessionDraft(workspaceID string) (SessionDraftView, error) {
	started := time.Now()
	state, err := a.workspaceRegistry().Load(a.bootContext())
	if err != nil {
		return SessionDraftView{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	workspace, ok := state.Workspaces[workspaceID]
	if !ok {
		return SessionDraftView{}, workspacestate.ErrWorkspaceNotFound
	}
	scope, root := "project", workspace.Root
	if workspaceID == workspacestate.GlobalWorkspaceID {
		scope, root = "global", ""
	}
	settings, err := json.Marshal(a.defaultDraftSettings(scope, root))
	if err != nil {
		return SessionDraftView{}, err
	}
	record, created, err := a.draftStore().Open(a.bootContext(), workspaceID, scope, root,
		"draft-"+strings.TrimPrefix(newTabID(), "tab_"), string(settings))
	if err != nil {
		return SessionDraftView{}, err
	}
	slog.Debug("desktop: session draft opened", "draft", record.ID, "workspace", workspaceID,
		"created", created, "duration_ms", time.Since(started).Milliseconds())
	return draftView(record)
}

func (a *App) OpenSessionDraftForTarget(scope, workspaceRoot string) (SessionDraftView, error) {
	workspaceID, err := a.ensureDesktopWorkspace(a.bootContext(), scope, workspaceRoot)
	if err != nil {
		return SessionDraftView{}, err
	}
	return a.OpenSessionDraft(workspaceID)
}

func (a *App) RestoreSessionDraft() (*SessionDraftView, error) {
	select {
	case <-a.tabsRestoredSignal():
	case <-a.bootContext().Done():
		return nil, a.bootContext().Err()
	}
	record, err := a.draftStore().Restore(a.bootContext())
	if errors.Is(err, draftstate.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	view, err := draftView(record)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// DismissSessionDraft clears only the page-restore target. The draft remains
// active and continues to appear beside its Workspace.
func (a *App) DismissSessionDraft(draftID string) error {
	return a.draftStore().ClearRestore(a.bootContext(), strings.TrimSpace(draftID))
}

func (a *App) SetSessionDraftRestoreTarget(draftID string) error {
	return a.draftStore().SetRestore(a.bootContext(), strings.TrimSpace(draftID))
}

func (a *App) GetSessionDraft(draftID string) (SessionDraftView, error) {
	record, err := a.draftStore().Get(a.bootContext(), strings.TrimSpace(draftID))
	if err != nil {
		return SessionDraftView{}, err
	}
	return draftView(record)
}

func (a *App) SaveSessionDraft(request SessionDraftSaveRequest) (SessionDraftSaveResult, error) {
	content := strings.TrimSpace(request.ContentJSON)
	if content == "" {
		content = "{}"
	}
	if !json.Valid([]byte(content)) {
		return SessionDraftSaveResult{}, errors.New("session draft content is invalid")
	}
	settings, err := json.Marshal(request.Settings)
	if err != nil {
		return SessionDraftSaveResult{}, err
	}
	record, err := a.draftStore().Save(a.bootContext(), strings.TrimSpace(request.DraftID), request.Revision, content, string(settings), request.Force)
	if errors.Is(err, draftstate.ErrConflict) {
		view, viewErr := draftView(record)
		return SessionDraftSaveResult{Draft: view, Conflict: true, Outcome: "conflict"}, viewErr
	}
	if errors.Is(err, draftstate.ErrConverted) {
		record, getErr := a.draftStore().Get(a.bootContext(), strings.TrimSpace(request.DraftID))
		if getErr != nil {
			return SessionDraftSaveResult{}, err
		}
		view, viewErr := draftView(record)
		outcome := "converted"
		if record.Status == "discarded" {
			outcome = "discarded"
		}
		return SessionDraftSaveResult{Draft: view, Outcome: outcome}, viewErr
	}
	if errors.Is(err, draftstate.ErrOperationConflict) {
		record, getErr := a.draftStore().Get(a.bootContext(), strings.TrimSpace(request.DraftID))
		if getErr != nil {
			return SessionDraftSaveResult{}, err
		}
		view, viewErr := draftView(record)
		return SessionDraftSaveResult{Draft: view, Outcome: "operation_locked"}, viewErr
	}
	if err != nil {
		return SessionDraftSaveResult{}, err
	}
	view, err := draftView(record)
	return SessionDraftSaveResult{Draft: view, Outcome: "saved"}, err
}

func (a *App) ListSessionDraftSummaries() ([]SessionDraftSummary, error) {
	records, err := a.draftStore().ListActive(a.bootContext())
	if err != nil {
		return []SessionDraftSummary{}, err
	}
	out := make([]SessionDraftSummary, 0, len(records))
	for _, record := range records {
		content := strings.TrimSpace(record.ContentJSON)
		out = append(out, SessionDraftSummary{ID: record.ID, WorkspaceID: record.WorkspaceID, Scope: record.Scope,
			WorkspaceRoot: record.WorkspaceRoot, Revision: record.Revision,
			HasContent: content != "" && content != "{}", State: "saved", UpdatedAt: record.UpdatedAt.UnixMilli()})
	}
	return out, nil
}

func (a *App) DiscardSessionDraft(draftID string, revision uint64) error {
	draftID = strings.TrimSpace(draftID)
	if err := a.draftStore().Discard(a.bootContext(), draftID, revision); err != nil {
		return err
	}
	a.releaseAttachmentStageOperations("draft:" + draftID + ":")
	return nil
}

func (a *App) GetDraftContext(draftID string) (SessionDraftContextView, error) {
	state, err := a.GetSessionDraftState(draftID)
	if err != nil {
		return SessionDraftContextView{Commands: []CommandInfo{}, Servers: []ServerView{}}, err
	}
	record, err := a.draftStore().Get(a.bootContext(), strings.TrimSpace(draftID))
	if err != nil {
		return SessionDraftContextView{Commands: []CommandInfo{}, Servers: []ServerView{}}, err
	}
	result := SessionDraftContextView{Draft: state.Draft, Operation: state.Operation, Commands: draftCommandInfos(record), Servers: draftMCPServerViews(record, state.Draft.Settings), Models: a.ModelsForDraft(draftID)}
	return result, nil
}

// draftMCPServerViews projects configured capabilities without constructing a
// Controller or starting an MCP process. Runtime readiness is revalidated when
// the reserved Session is started.
func draftMCPServerViews(record draftstate.Draft, settings SessionDraftSettings) []ServerView {
	root := record.WorkspaceRoot
	if record.Scope != "project" {
		root = globalWorkspaceRoot()
	}
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		return []ServerView{}
	}
	servers := make([]ServerView, 0, len(cfg.Plugins))
	for _, entry := range cfg.Plugins {
		status, intent := "disabled", "off"
		if mcpEntryEnabled(entry, root) {
			status, intent = "deferred", "automatic"
		}
		view := withPluginConfigInWorkspace(ServerView{
			Name: entry.Name, Status: status, StartIntent: intent, RuntimeState: "idle",
		}, entry, root)
		if owner, ok := cfg.PluginPackageOwner(entry.Name); ok {
			view.ManagedByPlugin = owner
		}
		servers = append(servers, finalizeServerView(view))
	}
	return orderServerViews(servers, settings.MCPOrder)
}

func draftCommandInfos(record draftstate.Draft) []CommandInfo {
	root := record.WorkspaceRoot
	if record.Scope != "project" {
		root = globalWorkspaceRoot()
	}
	out := builtinCommandInfos()
	commands, _ := command.LoadRoots(config.CommandRootsForRoot(root)...)
	for _, item := range commands {
		if item.Hidden {
			continue
		}
		out = append(out, CommandInfo{Name: item.Name, Description: item.Description, Hint: item.ArgHint, Kind: "custom", Group: "actions", Plugin: item.Plugin})
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if projectCfg, err := config.LoadForRootReadOnly(root); err == nil {
		cfg = projectCfg
	}
	store := skill.New(skill.Options{
		ProjectRoot: root, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(),
		PluginAgentPaths: cfg.PluginPackageAgentOwners(), ExcludedPaths: cfg.SkillExcludedPaths(),
		DisabledNames: cfg.DisabledSkillNames(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard,
	})
	defer store.Close()
	for _, item := range store.SlashList() {
		kind, group := "skill", "skills"
		if item.RunAs == skill.RunSubagent {
			kind, group = "subagent", "subagents"
		}
		out = append(out, CommandInfo{Name: item.SlashName(), Description: item.Description, Kind: kind, Group: group, Plugin: item.Plugin, Color: item.Color})
	}
	return resolveDocsCommand(out)
}

func draftSubmissionFingerprint(request SessionDraftSubmissionRequest) (string, string, error) {
	request.DraftID = strings.TrimSpace(request.DraftID)
	request.Display = strings.TrimSpace(request.Display)
	request.Input = strings.TrimSpace(request.Input)
	if request.Kind == "" {
		request.Kind = "turn"
	}
	data, err := json.Marshal(request)
	if err != nil {
		return "", "", err
	}
	// Request identity is transport metadata; equivalent concurrent requests
	// must still converge on the same operation.
	request.RequestID = ""
	request.SourceContentJSON = ""
	semantic, err := json.Marshal(request)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(semantic)
	return hex.EncodeToString(digest[:]), string(data), nil
}

func draftOperationView(op draftstate.Operation, tab *TabMeta) SessionDraftSubmissionView {
	view := SessionDraftSubmissionView{OperationID: op.ID, DraftID: op.DraftID, Phase: op.Phase,
		RequestID: op.RequestID, Revision: op.Revision,
		CanResume:  op.Phase == "resume_required" || op.Phase == "runtime_failed",
		CanEdit:    op.Phase == "terminal_failed" || op.Phase == "cancelled",
		CanDiscard: op.Phase == "terminal_failed" || op.Phase == "cancelled",
		CanCancel:  op.Phase != "terminal_failed" && op.Phase != "cancelled" && op.Phase != "cancel_requested",
		Error:      op.Error, SubmissionID: op.SubmissionID, UpdatedAt: op.UpdatedAt.UnixMilli(), Tab: tab}
	if op.SessionID != "" {
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: op.SessionID}
		view.Session = &ref
	}
	return view
}

func (a *App) BeginDraftSubmission(request SessionDraftSubmissionRequest) (result SessionDraftSubmissionView, resultErr error) {
	// A retry of a lost response must wait for the original request's durable
	// decision before absence can mean rejection, including across processes.
	if request.RequestID != "" {
		release, err := a.draftStore().PublicationLease(a.bootContext(), "request-"+request.DraftID+"-"+request.RequestID)
		if err != nil {
			return result, err
		}
		defer release()
	}
	durableAttempt := false
	defer func() {
		if resultErr != nil && !durableAttempt {
			resultErr = draftAdmissionError(resultErr)
		}
	}()
	if request.SnapshotVersion > draftstate.SnapshotVersion {
		return SessionDraftSubmissionView{}, errors.New("unsupported draft execution snapshot version")
	}
	request.DraftID = strings.TrimSpace(request.DraftID)
	if request.RequestID != "" {
		if prior, err := a.draftStore().RequestOperation(a.bootContext(), request.DraftID, request.RequestID); err == nil {
			fingerprint, _, hashErr := draftSubmissionFingerprint(request)
			if hashErr != nil {
				return SessionDraftSubmissionView{}, hashErr
			}
			if fingerprint != prior.Fingerprint {
				return SessionDraftSubmissionView{}, draftstate.ErrOperationConflict
			}
			return draftOperationView(prior, a.metaForDraftSession(prior.SessionID)), nil
		} else if !errors.Is(err, draftstate.ErrOperationNotFound) {
			return SessionDraftSubmissionView{}, err
		}
	}
	if request.Kind == "shell" {
		if strings.TrimSpace(request.Input) == "" {
			return SessionDraftSubmissionView{}, errors.New("shell command is required")
		}
	} else if strings.TrimSpace(request.Input) == "" && len(request.Invocations) == 0 {
		return SessionDraftSubmissionView{}, errors.New("session draft input is required")
	}
	if behavior, name := draftBuiltinBehavior(request.Display); behavior != "" && behavior != "submit" {
		return SessionDraftSubmissionView{}, fmt.Errorf("/%s must be handled on the draft surface before session creation", name)
	}
	record, err := a.draftStore().Get(a.bootContext(), request.DraftID)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if record.Revision != request.Revision {
		return SessionDraftSubmissionView{}, draftstate.ErrConflict
	}
	view, err := draftView(record)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if request.SnapshotVersion >= 4 {
		profile, _ := json.Marshal(request.Settings)
		digest, digestErr := draftstate.SnapshotDigest(record.ContentJSON, string(profile))
		if digestErr != nil || request.SourceDigest == "" || request.SourceDigest != view.SnapshotDigest || digest != view.SnapshotDigest {
			return SessionDraftSubmissionView{}, draftstate.ErrConflict
		}
	}
	root := record.WorkspaceRoot
	if record.Scope != "project" {
		root = globalWorkspaceRoot()
	}
	if providerext.PluginRefOwner(view.Settings.Model) == "" {
		cfg, loadErr := config.LoadForRootReadOnly(root)
		if loadErr != nil {
			return SessionDraftSubmissionView{}, loadErr
		}
		resolved, resolveErr := resolveDraftCreateModelStrict(cfg, view.Settings.Model)
		if resolveErr != nil {
			return SessionDraftSubmissionView{}, resolveErr
		}
		view.Settings.Model = resolved
	}
	if request.SnapshotVersion < 3 {
		request.Settings = view.Settings
		request.SnapshotVersion = 3
	}
	request.SourceContentJSON = record.ContentJSON
	fingerprint, payload, err := draftSubmissionFingerprint(request)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if err := validateDraftAttachments(record); err != nil {
		return SessionDraftSubmissionView{}, err
	}
	durableAttempt = true
	op, created, err := a.draftStore().BeginOperation(a.bootContext(), draftstate.Operation{
		RequestID: request.RequestID, SourceDigest: request.SourceDigest,
		ID: "draft-op-" + strings.TrimPrefix(newTabID(), "tab_"), DraftID: record.ID, WorkspaceID: record.WorkspaceID,
		DraftRevision: record.Revision, SessionID: "desktop-" + strings.TrimPrefix(newTabID(), "tab_"),
		TopicID:      newTopicID(),
		SubmissionID: "draft-submit-" + strings.TrimPrefix(newTabID(), "tab_"), Fingerprint: fingerprint, RequestJSON: payload,
	})
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	slog.Debug("desktop: draft submission reserved", "draft", op.DraftID, "operation", op.ID,
		"session", op.SessionID, "phase", op.Phase, "reused", !created)
	if op.Phase == "accepted" || op.Phase == "cancelled" || op.Phase == "dispatching_shell" || op.Phase == "dispatch_unknown" || op.Phase == "dispatching" {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), nil
	}
	if created {
		release, leaseErr := a.draftStore().WorkerLease(op.SessionID)
		if leaseErr != nil {
			// A prior failed worker may still be unwinding. Only this newly
			// created operation is paused; never reset another process's worker.
			op, _, err = a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, []string{"reserved"}, "resume_required", "Previous session worker is finishing. Continue to retry.")
			return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
		}
		a.goSafe("resumeDraftSubmission", func() { _, _ = a.resumeDraftSubmissionOwned(op, release) })
	} else if op.Phase == "reserved" {
		a.goSafe("resumeDraftSubmission", func() {
			if _, resumeErr := a.resumeDraftSubmission(op); resumeErr != nil {
				// Runtime preparation errors may retain provider configuration.
				// Keep diagnostics value-free and surface the actionable error through
				// the durable operation state instead of copying it into logs.
				slog.Warn("desktop: draft submission paused", "operation", op.ID, "phase", op.Phase)
			}
		})
	}
	return draftOperationView(op, a.metaForDraftSession(op.SessionID)), nil
}

func draftBuiltinBehavior(input string) (behavior, name string) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", ""
	}
	name = strings.TrimPrefix(fields[0], "/")
	for _, command := range builtinCommandInfos() {
		if command.Name == name {
			if command.DraftBehavior == "" {
				return "submit", name
			}
			return command.DraftBehavior, name
		}
	}
	return "", name
}

func (a *App) GetDraftSubmission(operationID string) (SessionDraftSubmissionView, error) {
	op, err := a.draftStore().Operation(a.bootContext(), strings.TrimSpace(operationID))
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if op.Phase == "dispatch_unknown" || op.Phase == "dispatching" || op.Phase == "dispatching_shell" {
		{
			var request SessionDraftSubmissionRequest
			payload := op.ExecutionJSON
			if payload == "" {
				payload = op.RequestJSON
			}
			if json.Unmarshal([]byte(payload), &request) == nil {
				found, lookupErr := a.lookupDraftReceipt(op, request)
				if lookupErr == nil && found {
					op, err = a.draftStore().AcceptAndConvert(a.bootContext(), op.DraftID, op.ID)
					if err == nil {
						a.completeDraftSessionTabOperation(op)
					}
				}
			}
		}
	}
	return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
}

func (a *App) lookupDraftReceipt(op draftstate.Operation, request SessionDraftSubmissionRequest) (bool, error) {
	req := draftControlSubmissionRequest(op.SubmissionID, request)
	if tab := a.metaForDraftSession(op.SessionID); tab != nil {
		if found, err := a.knownSubmission(tab.ID, req); found || err != nil {
			return found, err
		}
	}
	snapshot, err := a.desktopSessionService("").Query().Snapshot(a.bootContext(), session.SessionRef{HostID: localDesktopHostID, SessionID: op.SessionID})
	if err != nil {
		return false, err
	}
	if snapshot.DurableSequence < snapshot.EventSequence {
		return false, errors.New("submission durability remains unknown")
	}
	receipt, found := snapshot.Projection.Submissions.Lookup(op.SessionID, op.SubmissionID)
	if found && !control.MatchesSubmissionReceipt(req, receipt) {
		return false, errors.New("submission receipt does not match frozen request")
	}
	return found, nil
}

func (a *App) resumeDraftSubmission(op draftstate.Operation) (SessionDraftSubmissionView, error) {
	release, err := a.draftStore().WorkerLease(op.SessionID)
	if err != nil {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
	}
	return a.resumeDraftSubmissionOwned(op, release)
}

// Ownership is transferred into the goroutine without an unlock/reacquire gap.
func (a *App) resumeDraftSubmissionOwned(op draftstate.Operation, release func()) (SessionDraftSubmissionView, error) {
	defer func() {
		defer release()
		a.finishDraftCancellation(op)
	}()
	var err error
	started := time.Now()
	initialPhase := op.Phase
	defer func() {
		slog.Debug("desktop: draft submission resume finished", "operation", op.ID, "session", op.SessionID,
			"initial_phase", initialPhase, "final_phase", op.Phase, "duration_ms", time.Since(started).Milliseconds())
	}()
	if op.Phase == "accepted" || op.Phase == "cancelled" {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), nil
	}
	if op.Phase == "dispatching_shell" {
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), errors.New("shell execution result is unknown; it will not be replayed automatically")
	}
	claimed, ok, err := a.draftStore().ClaimOperationPhase(a.bootContext(), op.ID, []string{"reserved", "runtime_failed", "resume_required"}, "starting")
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if !ok {
		return draftOperationView(claimed, a.metaForDraftSession(claimed.SessionID)), nil
	}
	op = claimed
	var request SessionDraftSubmissionRequest
	if err := json.Unmarshal([]byte(op.RequestJSON), &request); err != nil {
		op, _, _ = a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, []string{"starting"}, "terminal_failed", err.Error())
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
	}
	request.Settings, err = a.draftOperationSettings(op)
	if err != nil {
		op, _, _ = a.setDraftOperationFailure(op, []string{"starting"}, "terminal_failed", err)
		return draftOperationView(op, nil), err
	}
	tab, err := a.ensureDraftSessionTab(op)
	if err != nil {
		op, _, _ = a.setDraftOperationFailure(op, []string{"starting"}, "terminal_failed", err)
		return draftOperationView(op, nil), err
	}
	if err := a.resolveDraftExternalRefs(tab.ID, &request); err != nil {
		op, _, _ = a.setDraftOperationFailure(op, []string{"starting"}, "terminal_failed", err)
		return draftOperationView(op, &tab), err
	}
	resolvedPayload, err := json.Marshal(request)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	op, ok, err = a.draftStore().UpdateOperationRequest(a.bootContext(), op.ID, "starting", string(resolvedPayload))
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if !ok {
		return draftOperationView(op, &tab), nil
	}
	dispatchPhase := map[bool]string{true: "dispatching_shell", false: "dispatching"}[request.Kind == "shell"]
	// Serialize cancellation with the admission boundary. Cancellation either
	// prevents this transition, or observes its durable receipt afterwards.
	releaseDispatch, err := a.draftStore().PublicationLease(a.bootContext(), op.ID)
	if err != nil {
		return draftOperationView(op, &tab), err
	}
	defer releaseDispatch()
	op, ok, err = a.draftStore().ClaimOperationPhase(a.bootContext(), op.ID, []string{"starting"}, dispatchPhase)
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if !ok {
		return draftOperationView(op, &tab), nil
	}
	a.mu.Lock()
	if target := a.tabs[tab.ID]; target != nil && target.SessionID == op.SessionID {
		target.draftAdmission = &draftAdmissionProfile{submissionID: op.SubmissionID, settings: request.Settings, controller: target.Ctrl}
	}
	a.mu.Unlock()
	if request.Kind == "shell" {
		err = a.runShellForTabWithID(tab.ID, request.Input, op.SubmissionID)
	} else if request.Goal != "" {
		_, err = a.SubmitInitialGoalToTabWithID(tab.ID, request.Goal, request.Display, request.Input, request.Invocations, request.CollaborationMode, request.ToolApprovalMode, op.SubmissionID)
	} else if len(request.Invocations) > 0 {
		err = a.SubmitInvocationsToTabWithID(tab.ID, request.Display, request.Input, request.Invocations, op.SubmissionID)
	} else if request.Display != request.Input {
		err = a.SubmitDisplayToTabWithID(tab.ID, request.Display, request.Input, op.SubmissionID)
	} else {
		_, err = a.StartTurnForTab(tab.ID, request.Input, op.SubmissionID)
	}
	if err != nil {
		phase := "dispatch_unknown"
		if errors.Is(err, control.ErrSubmissionNotAccepted) {
			phase = "terminal_failed"
		}
		op, _, _ = a.setDraftOperationFailure(op, []string{dispatchPhase}, phase, err)
		return draftOperationView(op, &tab), err
	}
	op, err = a.draftStore().AcceptAndConvert(a.bootContext(), op.DraftID, op.ID)
	if err != nil {
		return draftOperationView(op, &tab), err
	}
	a.completeDraftSessionTabOperation(op)
	a.emitProjectTreeChanged()
	return draftOperationView(op, &tab), nil
}

func (a *App) completeDraftSessionTabOperation(op draftstate.Operation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && tab.SessionID == op.SessionID && tab.PendingCreateOperationID == op.ID {
			tab.PendingCreateOperationID = ""
			tab.draftAdmission = nil
			a.saveTabsLocked()
			return
		}
	}
}

func (a *App) resolveDraftExternalRefs(tabID string, request *SessionDraftSubmissionRequest) error {
	if request == nil || len(request.WorkspaceRefs) == 0 {
		return nil
	}
	_, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return errors.New("session runtime is not ready")
	}
	for _, ref := range request.WorkspaceRefs {
		if !ref.IsDir || !filepath.IsAbs(ref.Path) {
			continue
		}
		token, _, err := ctrl.RegisterExternalFolderRef(ref.Path)
		if err != nil {
			return err
		}
		request.Input = rewriteDraftExternalFolderRef(request.Input, ref.Path, token)
	}
	return nil
}

func rewriteDraftExternalFolderRef(input, path, token string) string {
	old := "@" + strings.TrimSuffix(filepath.Clean(path), string(filepath.Separator)) + "/"
	return strings.ReplaceAll(input, old, "@"+strings.Trim(token, "/")+"/")
}

func draftControlSubmissionRequest(submissionID string, request SessionDraftSubmissionRequest) control.SubmissionRequest {
	result := control.SubmissionRequest{ID: submissionID, Input: request.Input, Display: request.Display}
	if request.Kind == "shell" {
		result.Action = "shell"
		result.Display = request.Input
		return result
	}
	if request.Goal != "" {
		result.Goal = strings.TrimSpace(request.Goal)
		result.ToolApprovalMode = normalizeToolApprovalMode(request.ToolApprovalMode)
		result.Invocations = controlInvocationRequests(request.Invocations)
	} else if len(request.Invocations) > 0 {
		result.Invocations = controlInvocationRequests(request.Invocations)
	}
	return result
}

func (a *App) setDraftOperationFailure(op draftstate.Operation, from []string, phase string, cause error) (draftstate.Operation, bool, error) {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	result, changed, err := a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, from, phase, message)
	if err == nil && changed && (phase == "terminal_failed" || phase == "cancelled") {
		if cleanupErr := a.workspaceRegistry().AbortCreateIfOperation(a.bootContext(), op.SessionID, op.ID); cleanupErr != nil {
			slog.Warn("desktop: clear failed draft create reservation", "operation", op.ID, "session", op.SessionID, "err", cleanupErr)
		}
	}
	return result, changed, err
}

func (a *App) beginDraftWorkspaceCreate(op draftstate.Operation, workspaceID string) error {
	store := a.workspaceRegistry()
	state, err := store.Load(a.bootContext())
	if err != nil {
		return err
	}
	if lifecycle := state.SessionStates[op.SessionID].Lifecycle; lifecycle == workspacestate.Archived || lifecycle == workspacestate.Deleted {
		return fmt.Errorf("session %q is %s and cannot accept the draft", op.SessionID, lifecycle)
	}
	pending := workspacestate.PendingCreate{OperationID: op.ID, WorkspaceID: workspaceID, SessionID: op.SessionID}
	err = store.BeginCreate(a.bootContext(), pending)
	if !errors.Is(err, workspacestate.ErrMutationConflict) {
		return err
	}
	stale, ok := state.PendingCreates[op.SessionID]
	if !ok || stale.OperationID == op.ID || stale.WorkspaceID != workspaceID {
		return err
	}
	prior, lookupErr := a.draftStore().Operation(a.bootContext(), stale.OperationID)
	if lookupErr != nil || prior.DraftID != op.DraftID || (prior.Phase != "terminal_failed" && prior.Phase != "cancelled") {
		return err
	}
	if cleanupErr := store.AbortCreateIfOperation(a.bootContext(), op.SessionID, stale.OperationID); cleanupErr != nil {
		return cleanupErr
	}
	return store.BeginCreate(a.bootContext(), pending)
}

func (a *App) ensureDraftSessionTab(op draftstate.Operation) (TabMeta, error) {
	if op.TopicID == "" {
		var err error
		op, err = a.draftStore().EnsureOperationTopic(a.bootContext(), op.ID, newTopicID())
		if err != nil {
			return TabMeta{}, err
		}
	}
	settings, err := a.draftOperationSettings(op)
	if err != nil {
		return TabMeta{}, err
	}
	if meta := a.metaForDraftSession(op.SessionID); meta != nil {
		if desktopWorkspaceID(meta.Scope, meta.WorkspaceRoot) != op.WorkspaceID {
			return *meta, workspacestate.ErrMutationConflict
		}
		state, stateErr := a.workspaceRegistry().Load(a.bootContext())
		if stateErr != nil {
			return *meta, stateErr
		}
		if lifecycle := state.SessionStates[op.SessionID].Lifecycle; lifecycle == workspacestate.Archived || lifecycle == workspacestate.Deleted {
			return *meta, fmt.Errorf("session %q is %s and cannot accept the draft", op.SessionID, lifecycle)
		}
		if err := a.applyDraftOperationSettings(op, settings); err != nil {
			return *meta, err
		}
		meta = a.metaForDraftSession(op.SessionID)
		if meta != nil && meta.Ready {
			return *meta, nil
		}
		if meta != nil && meta.StartupErr != "" {
			tab, _ := a.tabAndCtrlByID(meta.ID)
			if tab == nil {
				return *meta, errors.New(meta.StartupErr)
			}
			a.mu.Lock()
			if a.tabs[tab.ID] == tab {
				clearTabStartupError(tab)
				tab.PendingCreateOperationID = op.ID
			}
			a.mu.Unlock()
			return a.startCreatedSessionTab(tab, desktopWorkspaceRoot(tab.Scope, tab.WorkspaceRoot))
		}
		return *meta, errors.New("session runtime is still starting")
	}
	record, err := a.draftStore().Get(a.bootContext(), op.DraftID)
	if err != nil {
		return TabMeta{}, err
	}
	mode := tabModeFromAxes(
		normalizeCollaborationMode(settings.CollaborationMode) == "plan" || (settings.CollaborationMode == "" && tabModeHasPlan(settings.Mode)),
		normalizeToolApprovalMode(settings.ToolApprovalMode) == control.ToolApprovalDangerFullAccess,
	)
	workspaceID, err := a.ensureDesktopWorkspace(a.bootContext(), record.Scope, record.WorkspaceRoot)
	if err != nil {
		return TabMeta{}, err
	}
	if workspaceID != op.WorkspaceID {
		return TabMeta{}, workspacestate.ErrMutationConflict
	}
	if err := a.beginDraftWorkspaceCreate(op, workspaceID); err != nil {
		return TabMeta{}, err
	}
	actualRoot := record.WorkspaceRoot
	if record.Scope != "project" {
		actualRoot = globalWorkspaceRoot()
		if err := os.MkdirAll(actualRoot, 0o755); err != nil {
			return TabMeta{}, err
		}
	}
	topicID := op.TopicID
	if err := createTopicState(record.WorkspaceRoot, topicID, defaultTopicTitle, topicTitleSourceAuto, time.Now().UnixMilli()); err != nil {
		return TabMeta{}, err
	}
	_ = prependTopicInProjectsFile(record.WorkspaceRoot, topicID, false)
	tab := &WorkspaceTab{Scope: record.Scope, WorkspaceRoot: actualRoot,
		TopicID: topicID, TopicTitle: topicTitleForTab(record.Scope, record.WorkspaceRoot, topicID), topicTitleSource: topicTitleSourceAuto,
		SessionID: op.SessionID, PendingCreateOperationID: op.ID, model: settings.Model, qualityFloor: settings.QualityFloor,
		mode: mode, toolApprovalMode: settings.ToolApprovalMode, disabledMCP: cloneServerViewMap(settings.DisabledMCP), mcpOrder: append([]string(nil), settings.MCPOrder...)}
	if settings.Effort != "" {
		effort := settings.Effort
		tab.effort = &effort
	}
	a.mu.Lock()
	tab.ID = a.newUniqueTabIDLocked()
	tab.sink = &tabEventSink{tabID: tab.ID, app: a}
	a.tabs[tab.ID] = tab
	a.tabOrder = append(a.tabOrder, tab.ID)
	a.saveTabsLocked()
	a.mu.Unlock()
	if _, err := a.startCreatedSessionTab(tab, actualRoot); err != nil {
		return TabMeta{}, err
	}
	a.mu.RLock()
	meta := enrichTabMeta(a.tabMeta(tab, a.activeTabID == tab.ID))
	a.mu.RUnlock()
	return meta, nil
}

func (a *App) draftOperationSettings(op draftstate.Operation) (SessionDraftSettings, error) {
	var request SessionDraftSubmissionRequest
	if err := json.Unmarshal([]byte(op.RequestJSON), &request); err != nil {
		return SessionDraftSettings{}, err
	}
	if request.SnapshotVersion > draftstate.SnapshotVersion {
		return SessionDraftSettings{}, errors.New("unsupported draft execution snapshot version")
	}
	if (request.SnapshotVersion == 3 || request.SnapshotVersion == draftstate.SnapshotVersion) && strings.TrimSpace(request.Settings.Model) != "" {
		return request.Settings, nil
	}
	// A pre-v3 operation may only inherit the current draft settings when the
	// revision still proves that they are the settings it froze.
	record, err := a.draftStore().Get(a.bootContext(), op.DraftID)
	if err != nil {
		return SessionDraftSettings{}, err
	}
	if record.Revision != op.DraftRevision {
		return SessionDraftSettings{}, errors.New("draft operation predates frozen settings and cannot be resumed safely")
	}
	view, err := draftView(record)
	if err != nil {
		return SessionDraftSettings{}, err
	}
	return view.Settings, nil
}

func (a *App) applyDraftOperationSettings(op draftstate.Operation, settings SessionDraftSettings) error {
	return a.prepareDraftRuntime(op, settings)
}

// Caller holds App.mu; this publishes metadata only after runtime preparation.
func (a *App) publishDraftSettingsLocked(op draftstate.Operation, settings SessionDraftSettings) {
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || tab.SessionID != op.SessionID {
			continue
		}
		tab.PendingCreateOperationID = op.ID
		tab.model = settings.Model
		tab.Label = settings.Model
		tab.qualityFloor = settings.QualityFloor
		tab.mode = tabModeFromAxes(
			normalizeCollaborationMode(settings.CollaborationMode) == "plan" || (settings.CollaborationMode == "" && tabModeHasPlan(settings.Mode)),
			normalizeToolApprovalMode(settings.ToolApprovalMode) == control.ToolApprovalDangerFullAccess,
		)
		tab.toolApprovalMode = settings.ToolApprovalMode
		// The source editor owns the initial objective until atomic Goal
		// submission accepts it. Runtime preparation must not start a Goal.
		tab.goal = ""
		tab.disabledMCP = cloneServerViewMap(settings.DisabledMCP)
		tab.mcpOrder = append([]string(nil), settings.MCPOrder...)
		if settings.Effort == "" {
			tab.effort = nil
		} else {
			effort := settings.Effort
			tab.effort = &effort
		}
		a.saveTabsLocked()
		return
	}
}

func (a *App) metaForDraftSession(sessionID string) *TabMeta {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && tab.SessionID == sessionID {
			meta := enrichTabMeta(a.tabMeta(tab, tab.ID == a.activeTabID))
			return &meta
		}
	}
	return nil
}

func (a *App) CancelDraftSubmission(operationID string) (SessionDraftSubmissionView, error) {
	releasePublication, err := a.draftStore().PublicationLease(a.bootContext(), strings.TrimSpace(operationID))
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	defer releasePublication()
	op, err := a.draftStore().Operation(a.bootContext(), strings.TrimSpace(operationID))
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if op.Phase == "dispatching" || op.Phase == "dispatch_unknown" || op.Phase == "dispatching_shell" {
		if checked, checkErr := a.GetDraftSubmission(op.ID); checkErr == nil {
			if checked.Phase == "accepted" {
				if checked.Tab != nil {
					_, err = a.CancelSessionForTab(checked.Tab.ID)
				}
				return checked, err
			}
			if refreshed, refreshErr := a.draftStore().Operation(a.bootContext(), op.ID); refreshErr == nil {
				op = refreshed
			}
		}
	}
	if op.Phase == "accepted" {
		if meta := a.metaForDraftSession(op.SessionID); meta != nil {
			_, err = a.CancelSessionForTab(meta.ID)
		}
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
	}
	if op.Phase == "dispatching" || op.Phase == "dispatch_unknown" || op.Phase == "dispatching_shell" {
		if meta := a.metaForDraftSession(op.SessionID); meta != nil {
			_, err = a.CancelSessionForTab(meta.ID)
		}
		return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
	}
	op, cancelled, err := a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID,
		[]string{"reserved", "starting", "runtime_failed", "resume_required"}, "cancel_requested", "")
	if err != nil {
		return SessionDraftSubmissionView{}, err
	}
	if cancelled {
		if release, leaseErr := a.draftStore().WorkerLease(op.SessionID); leaseErr == nil {
			a.finishDraftCancellation(op)
			release()
			op, err = a.draftStore().Operation(a.bootContext(), op.ID)
		}
	} else if op.Phase == "accepted" || op.Phase == "dispatching" || op.Phase == "dispatch_unknown" || op.Phase == "dispatching_shell" {
		if meta := a.metaForDraftSession(op.SessionID); meta != nil {
			_, err = a.CancelSessionForTab(meta.ID)
		}
	}
	return draftOperationView(op, a.metaForDraftSession(op.SessionID)), err
}

// Called only while holding the creation-worker lease, after its work ends.
func (a *App) finishDraftCancellation(op draftstate.Operation) {
	current, err := a.draftStore().Operation(a.bootContext(), op.ID)
	if err != nil || current.Phase != "cancel_requested" {
		return
	}
	if err := a.workspaceRegistry().AbortCreateIfOperation(a.bootContext(), op.SessionID, op.ID); err != nil {
		return
	}
	_, _, _ = a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, []string{"cancel_requested"}, "cancelled", "")
}

// reconcileDraftSubmissionOperations never replays work. It only completes a
// conversion backed by a durable receipt or moves interrupted work into an
// explicit user-resume/unknown state.
func (a *App) reconcileDraftSubmissionOperations() {
	select {
	case <-a.tabsRestoredSignal():
	case <-a.bootContext().Done():
		return
	}
	ops, err := a.draftStore().PendingOperations(a.bootContext())
	if err != nil {
		slog.Warn("desktop: reconcile draft submissions", "err", err)
		return
	}
	if len(ops) > 0 {
		slog.Info("desktop: reconciling draft submissions", "count", len(ops))
	}
	for _, op := range ops {
		release, leaseErr := a.draftStore().WorkerLease(op.SessionID)
		if leaseErr != nil {
			continue
		}
		switch op.Phase {
		case "cancel_requested":
			a.finishDraftCancellation(op)
		case "accepted":
			accepted, err := a.draftStore().AcceptAndConvert(a.bootContext(), op.DraftID, op.ID)
			if err != nil {
				slog.Warn("desktop: finish accepted draft conversion", "operation", op.ID, "err", err)
			} else {
				a.completeDraftSessionTabOperation(accepted)
			}
		case "reserved", "starting":
			if _, _, err := a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, []string{op.Phase}, "resume_required", "Continue to resume this session creation."); err != nil {
				slog.Warn("desktop: pause interrupted draft creation", "operation", op.ID, "err", err)
			}
		case "dispatching", "dispatching_shell", "failed":
			checked, checkErr := a.GetDraftSubmission(op.ID)
			if checkErr == nil && checked.Phase == "accepted" {
				release()
				continue
			}
			if _, _, err := a.draftStore().TransitionOperationPhase(a.bootContext(), op.ID, []string{op.Phase}, "dispatch_unknown", "Submission acceptance is unknown; it will not be replayed automatically."); err != nil {
				slog.Warn("desktop: mark interrupted draft dispatch unknown", "operation", op.ID, "err", err)
			}
		}
		release()
	}
}

func (a *App) composerTargetWorkspace(target ComposerTarget) (string, control.SessionAPI, error) {
	if target.Kind == "draft" {
		record, err := a.draftStore().Get(a.bootContext(), strings.TrimSpace(target.DraftID))
		if err != nil {
			return "", nil, err
		}
		root := record.WorkspaceRoot
		if record.Scope != "project" {
			root = globalWorkspaceRoot()
		}
		base, err := workspaceBaseFromRoot(root)
		return base, nil, err
	}
	root, ctrl, ok := a.workspaceTargetForTab(target.TabID)
	if !ok {
		return "", nil, errors.New("composer target is unavailable")
	}
	base, err := workspaceBaseFromRoot(root)
	return base, ctrl, err
}

func (a *App) SavePastedImageForComposerTarget(target ComposerTarget, dataURL string) (string, error) {
	root, _, err := a.composerTargetWorkspace(target)
	if err != nil {
		return "", err
	}
	const marker = ";base64,"
	before, after, ok := strings.Cut(dataURL, marker)
	if !ok || !strings.HasPrefix(before, "data:") {
		return "", errors.New("unsupported pasted image")
	}
	raw, err := decodeBase64(after)
	if err != nil {
		return "", err
	}
	return control.SaveImageBytesInRoot(root, strings.TrimPrefix(before, "data:"), raw)
}

func (a *App) SavePastedFileForComposerTarget(target ComposerTarget, name, dataURL string) (string, error) {
	root, _, err := a.composerTargetWorkspace(target)
	if err != nil {
		return "", err
	}
	_, after, ok := strings.Cut(dataURL, ";base64,")
	if !ok {
		return "", errors.New("unsupported pasted file")
	}
	raw, err := decodeBase64(after)
	if err != nil {
		return "", err
	}
	return control.SaveAttachmentBytesInRoot(root, name, raw)
}

func (a *App) SaveClipboardImageForComposerTarget(target ComposerTarget) (string, error) {
	root, _, err := a.composerTargetWorkspace(target)
	if err != nil {
		return "", err
	}
	return control.SaveClipboardImageInRoot(root)
}

func decodeBase64(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode attachment: %w", err)
	}
	return decoded, nil
}

func (a *App) ListDirForTarget(target ComposerTarget, rel string) []DirEntry {
	root, ctrl, err := a.composerTargetWorkspace(target)
	if err != nil {
		return []DirEntry{}
	}
	return listDirForWorkspaceTarget(root, ctrl, rel)
}

func (a *App) SearchFileRefsForTarget(target ComposerTarget, query string) []DirEntry {
	root, ctrl, err := a.composerTargetWorkspace(target)
	if err != nil {
		return []DirEntry{}
	}
	return searchFileRefsForWorkspaceTarget(root, ctrl, query)
}

func (a *App) AttachmentDataURLForComposerTarget(target ComposerTarget, rel string) (string, error) {
	root, _, err := a.composerTargetWorkspace(target)
	if err != nil {
		return "", err
	}
	return control.ImageDataURLInRoot(root, rel)
}

func (a *App) AttachDroppedForComposerTarget(target ComposerTarget, path string) (DroppedItem, error) {
	root, _, err := a.composerTargetWorkspace(target)
	if err != nil {
		return DroppedItem{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return DroppedItem{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return DroppedItem{}, errors.New("dropped path must not be a symlink")
	}
	if isImageExt(path) {
		rel, saveErr := control.SaveImageFileInRoot(root, path)
		if saveErr == nil {
			preview, _ := control.ImageDataURLInRoot(root, rel)
			return DroppedItem{Kind: "attachment", Path: rel, PreviewURL: preview}, nil
		}
	}
	if rel, ok := workspaceRelativeIn(path, root); ok {
		return DroppedItem{Kind: "workspace", Path: rel, IsDir: info.IsDir()}, nil
	}
	if info.IsDir() {
		// External folders stay as durable draft references. The controller
		// registers them immediately before first execution; no runtime is built
		// merely to create the reference chip.
		return DroppedItem{Kind: "workspace", Path: filepath.Clean(path), IsDir: true, DisplayPath: filepath.Base(path)}, nil
	}
	rel, err := control.SaveAttachmentFileInRoot(root, path)
	if err != nil {
		return DroppedItem{}, err
	}
	return DroppedItem{Kind: "attachment", Path: rel}, nil
}
