package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
)

const (
	remoteWorkbenchStateEvent      = "remote:workbench-state"
	remoteWorkbenchRollbackTimeout = 5 * time.Second
)

type remoteWorkbenchSubscriptionRollback func(context.Context) error

// remoteWorkbenchTrackedAttacher is a Desktop-private extension implemented by
// the production Remote adapter. Its rollback is bound to the exact transport
// subscription created by this attach, so it cannot unsubscribe a newer
// reconnect/reattach for the same opaque SessionRef.
type remoteWorkbenchTrackedAttacher interface {
	attachRemoteWorkbenchSession(
		context.Context,
		runtimeapi.AttachAndSubscribeInput,
	) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error)
}

type remoteWorkbenchPreviewAttacher interface {
	previewRemoteWorkbenchSession(
		context.Context,
		runtimeapi.AttachAndSubscribeInput,
	) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error)
}

type remoteWorkbenchFreshCandidateAttacher interface {
	attachRemoteWorkbenchFreshCandidate(
		context.Context,
		runtimeapi.AttachAndSubscribeInput,
	) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error)
}

type remoteWorkbenchSessionUnsubscriber interface {
	UnsubscribeSession(context.Context, runtimeapi.UnsubscribeSessionInput) error
}

func attachRemoteWorkbenchAtomic(
	ctx context.Context,
	api runtimeapi.SessionAPI,
	input runtimeapi.AttachAndSubscribeInput,
) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error) {
	if tracked, ok := api.(remoteWorkbenchTrackedAttacher); ok {
		return tracked.attachRemoteWorkbenchSession(ctx, input)
	}
	snapshot, err := api.AttachAndSubscribe(ctx, input)
	if err != nil {
		return runtimeapi.SessionSnapshot{}, nil, err
	}
	rollback := func(rollbackCtx context.Context) error {
		unsubscriber, ok := api.(remoteWorkbenchSessionUnsubscriber)
		if !ok {
			return errors.New("Remote RuntimeAPI cannot roll back a Session subscription")
		}
		return unsubscriber.UnsubscribeSession(rollbackCtx, runtimeapi.UnsubscribeSessionInput{Session: input.Session})
	}
	return snapshot, rollback, nil
}

func previewRemoteWorkbenchAtomic(
	ctx context.Context,
	api runtimeapi.SessionAPI,
	input runtimeapi.AttachAndSubscribeInput,
) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error) {
	if previewer, ok := api.(remoteWorkbenchPreviewAttacher); ok {
		return previewer.previewRemoteWorkbenchSession(ctx, input)
	}
	return attachRemoteWorkbenchAtomic(ctx, api, input)
}

func attachRemoteWorkbenchFreshCandidateAtomic(
	ctx context.Context,
	api runtimeapi.SessionAPI,
	input runtimeapi.AttachAndSubscribeInput,
) (runtimeapi.SessionSnapshot, remoteWorkbenchSubscriptionRollback, error) {
	if attacher, ok := api.(remoteWorkbenchFreshCandidateAttacher); ok {
		return attacher.attachRemoteWorkbenchFreshCandidate(ctx, input)
	}
	return attachRemoteWorkbenchAtomic(ctx, api, input)
}

func runRemoteWorkbenchSubscriptionRollback(rollback remoteWorkbenchSubscriptionRollback) error {
	if rollback == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteWorkbenchRollbackTimeout)
	defer cancel()
	return rollback(ctx)
}

func discardRemoteWorkbenchSubscription(rollback remoteWorkbenchSubscriptionRollback, ref runtimeapi.SessionRef) {
	if err := runRemoteWorkbenchSubscriptionRollback(rollback); err != nil {
		slog.Warn("desktop: roll back superseded Remote Session subscription", "session", ref, "err", err)
	}
}

// RemoteDirectoryView is a Host-owned directory selection. Ref is opaque and
// DisplayPath is presentation-only; neither may be interpreted as a Desktop
// filesystem path.
type RemoteDirectoryView struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	DisplayPath string `json:"displayPath"`
	ParentRef   string `json:"parentRef,omitempty"`
}

type RemoteWorkspaceBrowseInput struct {
	DirectoryRef string `json:"directoryRef,omitempty"`
	TypedPath    string `json:"typedPath,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type RemoteWorkspacePageView struct {
	Directory RemoteDirectoryView   `json:"directory"`
	Entries   []RemoteDirectoryView `json:"entries"`
	HasMore   bool                  `json:"hasMore"`
	Next      string                `json:"next,omitempty"`
}

type RemoteCreateWorkspaceSessionInput struct {
	PrimaryDirectoryRef     string   `json:"primaryDirectoryRef"`
	AdditionalDirectoryRefs []string `json:"additionalDirectoryRefs"`
	TopicTitle              string   `json:"topicTitle"`
}

type RemoteWorkbenchStatusView struct {
	HostID               string `json:"hostId,omitempty"`
	WorkspaceName        string `json:"workspaceName,omitempty"`
	WorkspaceDisplayPath string `json:"workspaceDisplayPath,omitempty"`
	SessionAttached      bool   `json:"sessionAttached"`
	TabID                string `json:"tabId,omitempty"`
	TopicTitle           string `json:"topicTitle,omitempty"`
}

type remoteWorkbenchSession struct {
	Created            runtimeapi.CreatedSession
	Snapshot           runtimeapi.SessionSnapshot
	HistoryCursors     map[int]runtimeapi.Cursor
	AttachedGeneration uint64
	ReattachGeneration uint64
	LastAttachError    string
}

type pendingRemoteSessionCreate struct {
	HostID      string
	Fingerprint string
	Workspace   runtimeapi.Workspace
	Created     runtimeapi.CreatedSession
}

// remoteWorkbenchState binds the set of Host-owned workspaces and Sessions that
// are open in the current Desktop workbench. Every key is an opaque RuntimeAPI
// identity. In particular, Workspace.DisplayPath is never used as a key or fed
// to a Desktop filesystem API.
type remoteWorkbenchState struct {
	HostID      string
	Workspaces  map[runtimeapi.WorkspaceID]runtimeapi.Workspace
	Sessions    map[runtimeapi.SessionRef]*remoteWorkbenchSession
	SessionTabs map[string]runtimeapi.SessionRef
	TabOrder    []string
	ActiveTabID string
	Pending     map[string]*pendingRemoteSessionCreate
}

type remoteSessionUnsubscriber interface {
	UnsubscribeSession(context.Context, runtimeapi.UnsubscribeSessionInput) error
}

type remoteOperationCanceller interface {
	CancelOperation(context.Context, runtimeapi.CancelOperationInput) (runtimeapi.CancelOperationResult, error)
}

func (s *remoteWorkbenchState) ensureMaps() {
	if s.Workspaces == nil {
		s.Workspaces = make(map[runtimeapi.WorkspaceID]runtimeapi.Workspace)
	}
	if s.Sessions == nil {
		s.Sessions = make(map[runtimeapi.SessionRef]*remoteWorkbenchSession)
	}
	if s.SessionTabs == nil {
		s.SessionTabs = make(map[string]runtimeapi.SessionRef)
	}
	if s.Pending == nil {
		s.Pending = make(map[string]*pendingRemoteSessionCreate)
	}
}

func (s *remoteWorkbenchState) activeSession() (*remoteWorkbenchSession, runtimeapi.Workspace, bool) {
	if s == nil || s.ActiveTabID == "" {
		return nil, runtimeapi.Workspace{}, false
	}
	ref, ok := s.SessionTabs[s.ActiveTabID]
	if !ok {
		return nil, runtimeapi.Workspace{}, false
	}
	session := s.Sessions[ref]
	workspace, workspaceOK := s.Workspaces[ref.WorkspaceID]
	return session, workspace, session != nil && workspaceOK
}

func validateRemoteWorkbenchHistoryTurns(turns int) int {
	if runtimeapi.ValidateHistoryTurns(turns) != nil {
		return runtimeapi.DefaultAttachHistoryTurns
	}
	return turns
}

func remoteHistoryCursorsFromSnapshot(snapshot runtimeapi.SessionSnapshot) map[int]runtimeapi.Cursor {
	cursors := make(map[int]runtimeapi.Cursor)
	if snapshot.History.HasOlder && snapshot.History.Next != "" {
		cursors[snapshot.History.StartTurn] = snapshot.History.Next
	}
	return cursors
}

func (a *App) BrowseRemoteWorkspace(input RemoteWorkspaceBrowseInput) (RemoteWorkspacePageView, error) {
	api, _, err := a.remoteConnectedRuntime()
	if err != nil {
		return RemoteWorkspacePageView{}, err
	}
	if input.Limit < 0 || input.Limit > 1000 {
		return RemoteWorkspacePageView{}, errors.New("Remote directory page limit must be between 1 and 1000")
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	page, err := api.BrowseWorkspace(ctx, runtimeapi.BrowseWorkspaceInput{
		DirectoryRef: runtimeapi.DirectoryRef(strings.TrimSpace(input.DirectoryRef)),
		TypedPath:    strings.TrimSpace(input.TypedPath), Cursor: runtimeapi.Cursor(input.Cursor), Limit: input.Limit,
	})
	if err != nil {
		return RemoteWorkspacePageView{}, err
	}
	entries := make([]RemoteDirectoryView, len(page.Entries))
	for index, entry := range page.Entries {
		entries[index] = remoteDirectoryView(entry)
	}
	return RemoteWorkspacePageView{
		Directory: remoteDirectoryView(page.Directory), Entries: entries,
		HasMore: page.HasMore, Next: string(page.Next),
	}, nil
}

func remoteDirectoryView(value runtimeapi.Directory) RemoteDirectoryView {
	return RemoteDirectoryView{
		Ref: string(value.Ref), Name: value.Name, DisplayPath: value.DisplayPath,
		ParentRef: string(value.ParentRef),
	}
}

// CreateRemoteWorkspaceSession opens the primary Host directory, creates a
// Host-owned Session, and publishes it only after an atomic snapshot and event
// subscription have succeeded. A create that succeeded before attach failed is
// retained so the same UI retry cannot create a duplicate Session.
func (a *App) CreateRemoteWorkspaceSession(input RemoteCreateWorkspaceSessionInput) (RemoteWorkbenchStatusView, error) {
	finishCreate, err := a.beginRemoteWorkbenchSessionCreate()
	if err != nil {
		return RemoteWorkbenchStatusView{}, err
	}
	defer finishCreate()

	primary := strings.TrimSpace(input.PrimaryDirectoryRef)
	if primary == "" {
		return RemoteWorkbenchStatusView{}, errors.New("a primary Remote directory is required")
	}
	additional, err := normalizeRemoteAdditionalDirectories(primary, input.AdditionalDirectoryRefs)
	if err != nil {
		return RemoteWorkbenchStatusView{}, err
	}
	title := strings.TrimSpace(input.TopicTitle)
	fingerprint := remoteSessionCreateFingerprint(primary, additional, title)

	api, expected, err := a.remoteConnectedRuntimeSnapshot()
	if err != nil {
		return RemoteWorkbenchStatusView{}, err
	}
	manager := a.remote.manager
	if manager == nil {
		return RemoteWorkbenchStatusView{}, ErrRuntimeTargetUnavailable
	}
	target := expected.Target

	pending := a.remoteWorkspacePending(target.ID, fingerprint)

	var workspace runtimeapi.Workspace
	var created runtimeapi.CreatedSession
	if pending != nil {
		workspace = pending.Workspace
		created = pending.Created
	} else {
		ctx, cancel := a.remoteActionContext()
		opened, openErr := api.OpenWorkspace(ctx, runtimeapi.OpenWorkspaceInput{PrimaryDirectory: runtimeapi.DirectoryRef(primary)})
		cancel()
		if openErr != nil {
			return RemoteWorkbenchStatusView{}, openErr
		}
		workspace = opened.Workspace
		if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
			return RemoteWorkbenchStatusView{}, ErrTargetTransitionSuperseded
		}
		ctx, cancel = a.remoteActionContext()
		created, err = api.CreateSession(ctx, runtimeapi.CreateSessionInput{
			WorkspaceID: workspace.ID, AdditionalDirectories: additional,
			Topic:   runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: title},
			Profile: runtimeapi.ProfileSelection{},
		})
		cancel()
		if err != nil {
			return RemoteWorkbenchStatusView{}, err
		}
		if !created.Session.Valid() || created.Session.WorkspaceID != workspace.ID {
			return RemoteWorkbenchStatusView{}, errors.New("Remote create returned a Session in a different workspace")
		}
		pending = &pendingRemoteSessionCreate{
			HostID: target.ID, Fingerprint: fingerprint, Workspace: workspace, Created: created,
		}
		// Persist the successful Host mutation before consulting current target
		// state. A newer StateSink may already have installed Host B, but this
		// Host-scoped entry remains hidden from B and is reusable after A returns.
		a.storeRemoteWorkspacePending(pending)
		if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
			return RemoteWorkbenchStatusView{}, ErrTargetTransitionSuperseded
		}
	}
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return RemoteWorkbenchStatusView{}, ErrTargetTransitionSuperseded
	}

	ctx, cancel := a.remoteActionContext()
	snapshot, rollback, err := attachRemoteWorkbenchAtomic(ctx, api, runtimeapi.AttachAndSubscribeInput{
		Session: created.Session, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	})
	cancel()
	if err != nil {
		if !a.withRemoteWorkbenchTarget(expected, func(state *remoteWorkbenchState) {
			state.ensureMaps()
			if current := state.Sessions[created.Session]; current != nil {
				current.LastAttachError = err.Error()
			}
		}) {
			return RemoteWorkbenchStatusView{}, ErrTargetTransitionSuperseded
		}
		return RemoteWorkbenchStatusView{}, fmt.Errorf("attach Remote Session: %w", err)
	}
	retainSubscription := false
	defer func() {
		if !retainSubscription {
			discardRemoteWorkbenchSubscription(rollback, created.Session)
		}
	}()
	if snapshot.Session != created.Session {
		return RemoteWorkbenchStatusView{}, errors.New("Remote attach returned a different Session identity")
	}
	_, status, committed := a.commitRemoteWorkbenchSession(expected, workspace, created, snapshot, true)
	if !committed {
		return RemoteWorkbenchStatusView{}, ErrTargetTransitionSuperseded
	}
	retainSubscription = true
	return status, nil
}

func (a *App) remoteWorkspacePending(hostID, fingerprint string) *pendingRemoteSessionCreate {
	if a == nil || hostID == "" || fingerprint == "" {
		return nil
	}
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	byFingerprint := a.remote.workspacePending[hostID]
	pending := clonePendingRemoteCreate(byFingerprint[fingerprint])
	if pending == nil {
		return nil
	}
	if pending.HostID != hostID || pending.Fingerprint != fingerprint || pending.Workspace.ID == "" ||
		!pending.Created.Session.Valid() || pending.Created.Session.WorkspaceID != pending.Workspace.ID {
		delete(byFingerprint, fingerprint)
		if len(byFingerprint) == 0 {
			delete(a.remote.workspacePending, hostID)
		}
		return nil
	}
	return pending
}

func (a *App) storeRemoteWorkspacePending(pending *pendingRemoteSessionCreate) {
	if a == nil || pending == nil || pending.HostID == "" || pending.Fingerprint == "" {
		return
	}
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	if a.remote.workspacePending == nil {
		a.remote.workspacePending = make(map[string]map[string]*pendingRemoteSessionCreate)
	}
	byFingerprint := a.remote.workspacePending[pending.HostID]
	if byFingerprint == nil {
		byFingerprint = make(map[string]*pendingRemoteSessionCreate)
		a.remote.workspacePending[pending.HostID] = byFingerprint
	}
	byFingerprint[pending.Fingerprint] = clonePendingRemoteCreate(pending)
}

// clearRemoteWorkspacePending removes only the exact Host mutation observed by
// the caller. A later retry may already have installed a different opaque
// SessionRef under the same fingerprint, so clearing by fingerprint alone
// would make that newer successful create duplicable.
func (a *App) clearRemoteWorkspacePending(hostID, fingerprint string, ref runtimeapi.SessionRef) bool {
	if a == nil || hostID == "" || fingerprint == "" || !ref.Valid() {
		return false
	}
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	byFingerprint := a.remote.workspacePending[hostID]
	current := byFingerprint[fingerprint]
	if current == nil || current.HostID != hostID || current.Fingerprint != fingerprint || current.Created.Session != ref {
		return false
	}
	delete(byFingerprint, fingerprint)
	if len(byFingerprint) == 0 {
		delete(a.remote.workspacePending, hostID)
	}
	return true
}

// withRemoteWorkbenchTarget linearizes a workbench mutation with TargetManager's
// ordered StateSink. State transitions hold stateDispatchMu until their sink has
// applied, so an older Host result can neither race after nor overwrite the
// state of a newer target generation.
func (a *App) withRemoteWorkbenchTarget(expected TargetManagerSnapshot, update func(*remoteWorkbenchState)) bool {
	if a == nil || update == nil {
		return false
	}
	manager := a.remote.manager
	if manager == nil {
		return false
	}
	manager.stateDispatchMu.Lock()
	defer manager.stateDispatchMu.Unlock()
	if !remoteWorkbenchTargetMatches(manager.Snapshot(), expected) {
		return false
	}
	a.remote.workbenchMu.Lock()
	update(&a.remote.workbench)
	a.remote.workbenchMu.Unlock()
	return true
}

func (a *App) remoteWorkspaceSessionCreateInFlight() bool {
	if a == nil {
		return false
	}
	a.remote.workspaceSessionCreateMu.Lock()
	defer a.remote.workspaceSessionCreateMu.Unlock()
	return a.remote.workspaceSessionCreates > 0
}

var errRemoteWorkspaceSessionCreateWhileClosing = errors.New("Reasonix is closing; a Remote Session cannot be created")

// beginRemoteWorkbenchSessionCreate admits a complete Host-side Session-create
// decision before queueing behind workbenchOp. The fixed lock order keeps every
// create path serialized without allowing final shutdown to pass a queued
// caller. The returned function must be called exactly once by the admitting
// caller; keeping this helper at the outer entry point avoids nested admission.
func (a *App) beginRemoteWorkbenchSessionCreate() (func(), error) {
	if err := a.beginRemoteWorkspaceSessionCreate(); err != nil {
		return nil, err
	}
	a.remote.workbenchOp.Lock()
	return func() {
		a.remote.workbenchOp.Unlock()
		a.endRemoteWorkspaceSessionCreate()
	}, nil
}

func (a *App) beginRemoteWorkspaceSessionCreate() error {
	if a == nil {
		return errRemoteWorkspaceSessionCreateWhileClosing
	}
	a.remote.workspaceSessionCreateMu.Lock()
	defer a.remote.workspaceSessionCreateMu.Unlock()
	if a.remote.workspaceSessionClosing {
		return errRemoteWorkspaceSessionCreateWhileClosing
	}
	a.remote.workspaceSessionCreates++
	return nil
}

func (a *App) endRemoteWorkspaceSessionCreate() {
	if a == nil {
		return
	}
	a.remote.workspaceSessionCreateMu.Lock()
	defer a.remote.workspaceSessionCreateMu.Unlock()
	if a.remote.workspaceSessionCreates > 0 {
		a.remote.workspaceSessionCreates--
	}
}

// beginRemoteDesktopClose atomically excludes every later Host-side Session
// create. Repeated calls are allowed because runtime.Quit triggers Wails'
// OnBeforeClose after a bound close action has already admitted shutdown.
func (a *App) beginRemoteDesktopClose() bool {
	if a == nil {
		return true
	}
	a.remote.workspaceSessionCreateMu.Lock()
	defer a.remote.workspaceSessionCreateMu.Unlock()
	if a.remote.workspaceSessionCreates > 0 {
		return false
	}
	a.remote.workspaceSessionClosing = true
	return true
}

type remoteDesktopBackgroundCloseDecision uint8

const (
	remoteDesktopBackgroundCloseBlocked remoteDesktopBackgroundCloseDecision = iota
	remoteDesktopBackgroundCloseHide
	remoteDesktopBackgroundCloseAlreadyClosing
)

// decideRemoteDesktopBackgroundClose observes the create/close gate without
// taking ownership of final shutdown. A background close must never clear an
// already-admitted final close from another native or tray quit callback.
func (a *App) decideRemoteDesktopBackgroundClose() remoteDesktopBackgroundCloseDecision {
	if a == nil {
		return remoteDesktopBackgroundCloseAlreadyClosing
	}
	a.remote.workspaceSessionCreateMu.Lock()
	defer a.remote.workspaceSessionCreateMu.Unlock()
	if a.remote.workspaceSessionClosing {
		return remoteDesktopBackgroundCloseAlreadyClosing
	}
	if a.remote.workspaceSessionCreates > 0 {
		return remoteDesktopBackgroundCloseBlocked
	}
	return remoteDesktopBackgroundCloseHide
}

func (a *App) markRemoteDesktopClosing() {
	if a == nil {
		return
	}
	a.remote.workspaceSessionCreateMu.Lock()
	a.remote.workspaceSessionClosing = true
	a.remote.workspaceSessionCreateMu.Unlock()
}

func normalizeRemoteAdditionalDirectories(primary string, values []string) ([]runtimeapi.DirectoryRef, error) {
	seen := map[string]bool{primary: true}
	out := make([]runtimeapi.DirectoryRef, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, errors.New("additional Remote directory references cannot be empty")
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, runtimeapi.DirectoryRef(value))
	}
	return out, nil
}

func remoteSessionCreateFingerprint(primary string, additional []runtimeapi.DirectoryRef, title string) string {
	var value strings.Builder
	value.WriteString("workspace-create")
	value.WriteByte(0)
	value.WriteString(primary)
	value.WriteByte(0)
	for _, directory := range additional {
		value.WriteString(string(directory))
		value.WriteByte(0)
	}
	value.WriteString(title)
	return value.String()
}

func remoteBlankSessionCreateFingerprint(workspaceID runtimeapi.WorkspaceID) string {
	return "blank-create\x00" + string(workspaceID)
}

func remoteForkSessionCreateFingerprint(source runtimeapi.SessionRef, checkpoint runtimeapi.CheckpointID) string {
	return "fork-create\x00" + string(source.WorkspaceID) + "\x00" + string(source.SessionID) + "\x00" + string(checkpoint)
}

func remoteProjectSessionCreateFingerprint(workspaceID runtimeapi.WorkspaceID, topicID runtimeapi.TopicID) string {
	return "project-create\x00" + string(workspaceID) + "\x00" + string(topicID)
}

func remoteWorkspaceSwitchSessionCreateFingerprint(workspaceID runtimeapi.WorkspaceID) string {
	return "workspace-switch-create\x00" + string(workspaceID)
}

func clonePendingRemoteCreate(value *pendingRemoteSessionCreate) *pendingRemoteSessionCreate {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (a *App) RemoteWorkbenchStatus() RemoteWorkbenchStatusView {
	manager := a.remote.manager
	if manager == nil {
		return RemoteWorkbenchStatusView{}
	}
	target := manager.Snapshot()
	a.remote.workbenchMu.RLock()
	status := remoteWorkbenchStatusLocked(a.remote.workbench, target)
	a.remote.workbenchMu.RUnlock()
	return status
}

func remoteWorkbenchStatusLocked(state remoteWorkbenchState, target TargetManagerSnapshot) RemoteWorkbenchStatusView {
	status := RemoteWorkbenchStatusView{}
	if target.Target.Kind == TargetRemote {
		status.HostID = target.Target.ID
	}
	if state.HostID == "" || state.HostID != status.HostID {
		return status
	}
	if session, workspace, ok := state.activeSession(); ok {
		status.WorkspaceName = workspace.Name
		status.WorkspaceDisplayPath = workspace.DisplayPath
		status.SessionAttached = target.State == TargetRemoteReconnecting ||
			(target.State == TargetRemoteConnected && session.AttachedGeneration == target.Generation)
		status.TabID = state.ActiveTabID
		status.TopicTitle = session.Created.TopicTitle
	}
	return status
}

func (a *App) publishRemoteWorkbenchReady(status RemoteWorkbenchStatusView) {
	a.enqueueRemoteWorkbenchReady(status)
	a.emitProjectTreeChanged()
}

// enqueueRemoteWorkbenchReady only appends to the ordered runtime-event queue.
// commitRemoteWorkbenchSession calls it while holding TargetManager's
// stateDispatchMu, so a newer StateSink cannot enqueue its replacement state in
// the middle of this workbench/rebuilt/ready sequence.
func (a *App) enqueueRemoteWorkbenchReady(status RemoteWorkbenchStatusView) {
	a.emitRuntimeEvent(remoteWorkbenchStateEvent, status)
	if status.SessionAttached && status.TabID != "" {
		a.emitRuntimeEvent("runtime:rebuilt", status.TabID)
		a.emitRuntimeEvent("agent:ready", status.TabID)
	}
}

func (a *App) publishRemoteWorkbenchChanged() {
	a.emitRuntimeEvent(remoteWorkbenchStateEvent, a.RemoteWorkbenchStatus())
	a.emitProjectTreeChanged()
}

// applyRemoteTargetState is called from TargetManager's ordered state sink. It
// only updates memory and schedules I/O; it never calls back into the manager on
// the sink stack.
func (a *App) applyRemoteTargetState(target TargetManagerSnapshot) {
	var reattach bool
	var changed bool
	a.remote.workbenchMu.Lock()
	state := &a.remote.workbench
	if target.Target.Kind == TargetRemote && state.HostID != "" && state.HostID != target.Target.ID {
		*state = remoteWorkbenchState{HostID: target.Target.ID}
		changed = true
	}
	if target.State == TargetRemoteConnected && state.HostID == target.Target.ID {
		for _, session := range state.Sessions {
			if session != nil && session.AttachedGeneration != target.Generation && session.ReattachGeneration != target.Generation {
				session.ReattachGeneration = target.Generation
				session.LastAttachError = ""
				reattach = true
			}
		}
	}
	a.remote.workbenchMu.Unlock()
	if changed {
		a.emitRuntimeEvent(remoteWorkbenchStateEvent, remoteWorkbenchStatusLocked(remoteWorkbenchState{}, target))
		a.emitProjectTreeChanged()
	}
	if reattach {
		a.goSafe("reattachRemoteWorkbench", func() { a.reattachRemoteWorkbench(target) })
	}
}

func (a *App) reattachRemoteWorkbench(expected TargetManagerSnapshot) {
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	a.remote.workbenchMu.RLock()
	if a.remote.workbench.HostID != expected.Target.ID {
		a.remote.workbenchMu.RUnlock()
		return
	}
	refs := make([]runtimeapi.SessionRef, 0, len(a.remote.workbench.TabOrder))
	for _, tabID := range a.remote.workbench.TabOrder {
		ref, ok := a.remote.workbench.SessionTabs[tabID]
		if !ok {
			continue
		}
		if session := a.remote.workbench.Sessions[ref]; session != nil && session.ReattachGeneration == expected.Generation {
			refs = append(refs, ref)
		}
	}
	a.remote.workbenchMu.RUnlock()

	api, connected, connectionErr := a.remoteConnectedRuntimeSnapshot()
	if connectionErr == nil && !remoteWorkbenchTargetMatches(connected, expected) {
		connectionErr = ErrTargetTransitionSuperseded
	}
	for _, ref := range refs {
		err := connectionErr
		var snapshot runtimeapi.SessionSnapshot
		var rollback remoteWorkbenchSubscriptionRollback
		if err == nil {
			ctx, cancel := context.WithTimeout(a.bootContext(), remoteAppActionTimeout)
			snapshot, rollback, err = attachRemoteWorkbenchAtomic(ctx, api, runtimeapi.AttachAndSubscribeInput{
				Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
			})
			cancel()
			if err == nil && snapshot.Session != ref {
				err = errors.New("Remote reattach returned a different Session identity")
			}
		}
		current := a.remote.manager.Snapshot()
		if err == nil && (current.State != TargetRemoteConnected || current.Generation != expected.Generation || !sameTarget(current.Target, expected.Target)) {
			err = ErrTargetTransitionSuperseded
		}
		committed := false
		a.remote.workbenchMu.Lock()
		state := &a.remote.workbench
		if state.HostID == expected.Target.ID {
			if session := state.Sessions[ref]; session != nil && session.ReattachGeneration == expected.Generation {
				session.ReattachGeneration = 0
				if err == nil {
					session.Snapshot = cloneRemoteSessionSnapshot(snapshot)
					session.HistoryCursors = remoteHistoryCursorsFromSnapshot(snapshot)
					session.Created.TopicID = snapshot.TopicID
					session.Created.TopicTitle = snapshot.Title
					session.Created.ResolvedProfile = snapshot.Profile
					session.AttachedGeneration = expected.Generation
					session.LastAttachError = ""
					committed = true
				} else {
					session.LastAttachError = err.Error()
				}
			}
		}
		a.remote.workbenchMu.Unlock()
		if err == nil && !committed {
			err = ErrTargetTransitionSuperseded
		}
		if err != nil && rollback != nil {
			discardRemoteWorkbenchSubscription(rollback, ref)
		}
		if errors.Is(err, ErrTargetTransitionSuperseded) {
			break
		}
	}
	a.publishRemoteWorkbenchChanged()
}

func (a *App) remoteConnectedRuntime() (runtimeapi.RuntimeAPI, TargetDescriptor, error) {
	api, snapshot, err := a.remoteConnectedRuntimeSnapshot()
	return api, snapshot.Target, err
}

func (a *App) remoteConnectedRuntimeSnapshot() (runtimeapi.RuntimeAPI, TargetManagerSnapshot, error) {
	manager := a.remote.manager
	if manager == nil {
		return nil, TargetManagerSnapshot{}, ErrRuntimeTargetUnavailable
	}
	api, snapshot, err := manager.RuntimeAPISnapshot()
	if err != nil {
		return nil, snapshot, err
	}
	if snapshot.State != TargetRemoteConnected || snapshot.Target.Kind != TargetRemote {
		return nil, snapshot, ErrRuntimeTargetUnavailable
	}
	return api, snapshot, nil
}

// remoteConnectedRuntimeFor pairs a previously projected workbench Session
// with the exact RuntimeAPI generation that projection came from. Without the
// second snapshot comparison an A -> B transition between remoteSessionView
// and RuntimeAPI lookup could send A's opaque SessionRef to B's adapter.
func (a *App) remoteConnectedRuntimeFor(expected TargetManagerSnapshot) (runtimeapi.RuntimeAPI, error) {
	api, current, err := a.remoteConnectedRuntimeSnapshot()
	if err != nil {
		return nil, err
	}
	if !remoteWorkbenchTargetMatches(current, expected) {
		return nil, ErrTargetTransitionSuperseded
	}
	return api, nil
}

// remoteConnectedV1Runtime is the workbench's frozen V1 admission boundary.
// TargetManager intentionally retains its Phase-5-compatible RuntimeAPI
// surface, so consumers that need catalogs/lifecycle/file domains must prove
// that the connected adapter implements the complete V1 contract here.
func (a *App) remoteConnectedV1Runtime() (runtimeapi.V1RuntimeAPI, TargetDescriptor, error) {
	api, snapshot, err := a.remoteConnectedV1RuntimeSnapshot()
	return api, snapshot.Target, err
}

func (a *App) remoteConnectedV1RuntimeSnapshot() (runtimeapi.V1RuntimeAPI, TargetManagerSnapshot, error) {
	api, snapshot, err := a.remoteConnectedRuntimeSnapshot()
	if err != nil {
		return nil, snapshot, err
	}
	v1, ok := api.(runtimeapi.V1RuntimeAPI)
	if !ok {
		return nil, snapshot, runtimeapi.Unavailable(runtimeapi.CapabilitySessionAttach, "the connected target does not implement RuntimeAPI V1")
	}
	return v1, snapshot, nil
}

func (a *App) remoteTargetSelected() bool {
	manager := a.remote.manager
	if manager == nil {
		return false
	}
	snapshot := manager.Snapshot()
	return snapshot.Target.Kind == TargetRemote && snapshot.State != TargetLocalConnected
}

func (a *App) remoteSessionView(tabID string) (remoteWorkbenchSession, runtimeapi.Workspace, TargetManagerSnapshot, bool) {
	manager := a.remote.manager
	if manager == nil {
		return remoteWorkbenchSession{}, runtimeapi.Workspace{}, TargetManagerSnapshot{}, false
	}
	target := manager.Snapshot()
	if target.Target.Kind != TargetRemote || (target.State != TargetRemoteConnected && target.State != TargetRemoteReconnecting) {
		return remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, false
	}
	a.remote.workbenchMu.RLock()
	defer a.remote.workbenchMu.RUnlock()
	state := a.remote.workbench
	if state.HostID != target.Target.ID {
		return remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, false
	}
	if tabID == "" {
		tabID = state.ActiveTabID
	}
	ref, exists := state.SessionTabs[tabID]
	if !exists {
		return remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, false
	}
	session := state.Sessions[ref]
	workspace, workspaceOK := state.Workspaces[ref.WorkspaceID]
	if session == nil || !workspaceOK {
		return remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, false
	}
	copy := *session
	copy.Snapshot = cloneRemoteSessionSnapshot(copy.Snapshot)
	copy.HistoryCursors = make(map[int]runtimeapi.Cursor, len(session.HistoryCursors))
	for turn, cursor := range session.HistoryCursors {
		copy.HistoryCursors[turn] = cursor
	}
	return copy, workspace, target, true
}

func (a *App) remoteV1ForTab(tabID string) (runtimeapi.V1RuntimeAPI, remoteWorkbenchSession, runtimeapi.Workspace, TargetManagerSnapshot, error) {
	session, workspace, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected || session.AttachedGeneration != target.Generation {
		return nil, remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, ErrRuntimeTargetUnavailable
	}
	api, connected, err := a.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		return nil, remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, err
	}
	if !remoteWorkbenchTargetMatches(connected, target) {
		return nil, remoteWorkbenchSession{}, runtimeapi.Workspace{}, target, ErrTargetTransitionSuperseded
	}
	return api, session, workspace, target, nil
}

func cloneRemoteSessionSnapshot(value runtimeapi.SessionSnapshot) runtimeapi.SessionSnapshot {
	copy := value
	copy.History.Messages = append([]runtimeapi.HistoryMessage(nil), value.History.Messages...)
	copy.Runtime.LiveEvents = append([]eventwire.Event(nil), value.Runtime.LiveEvents...)
	copy.Todos = append([]runtimeapi.TodoItem(nil), value.Todos...)
	copy.Jobs = append([]runtimeapi.Job(nil), value.Jobs...)
	copy.Checkpoints = append([]runtimeapi.Checkpoint(nil), value.Checkpoints...)
	if value.Runtime.CurrentTurn != nil {
		v := *value.Runtime.CurrentTurn
		copy.Runtime.CurrentTurn = &v
	}
	if value.Runtime.CurrentOperation != nil {
		v := *value.Runtime.CurrentOperation
		copy.Runtime.CurrentOperation = &v
	}
	if value.PendingPrompt != nil {
		v := *value.PendingPrompt
		copy.PendingPrompt = &v
	}
	return copy
}

func (a *App) updateRemoteWorkbenchEvent(value TargetRuntimeEvent) bool {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	state := &a.remote.workbench
	session := state.Sessions[value.Event.Session]
	if state.HostID != value.Target.ID || session == nil || session.AttachedGeneration != value.Generation {
		return false
	}
	runtimeState := &session.Snapshot.Runtime
	runtimeState.LiveEvents = append(runtimeState.LiveEvents, value.Event.Value)
	if len(runtimeState.LiveEvents) > 512 {
		runtimeState.LiveEvents = append([]eventwire.Event(nil), runtimeState.LiveEvents[len(runtimeState.LiveEvents)-512:]...)
	}
	switch value.Event.Value.Kind {
	case "turn_started":
		runtimeState.Running = true
		runtimeState.CancelRequested = false
		if value.Event.TurnID != "" {
			runtimeState.CurrentTurn = &runtimeapi.TurnState{ID: value.Event.TurnID}
		}
	case "approval_request":
		session.Snapshot.PendingPrompt = pendingApprovalFromWire(value.Event.Value)
	case "ask_request":
		session.Snapshot.PendingPrompt = pendingAskFromWire(value.Event.Value)
	case "turn_done", "operation_done":
		runtimeState.Running = false
		runtimeState.CurrentTurn = nil
		runtimeState.CurrentOperation = nil
		runtimeState.CancelRequested = false
		runtimeState.LiveEvents = nil
		session.Snapshot.PendingPrompt = nil
	}
	return true
}

// updateRemoteWorkbenchSnapshot commits an adapter-validated atomic
// subscription migration. The previous opaque Session identifies the Desktop
// binding being replaced; neither a Host path nor transport identity is used.
func (a *App) updateRemoteWorkbenchSnapshot(value TargetRuntimeEvent) bool {
	update := value.Event.Snapshot
	if update == nil || !update.Previous.Valid() || !update.Snapshot.Session.Valid() ||
		update.Snapshot.Session != value.Event.Session {
		return false
	}
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	state := &a.remote.workbench
	session := state.Sessions[update.Previous]
	if state.HostID != value.Target.ID || session == nil || session.AttachedGeneration != value.Generation {
		return false
	}
	if existing := state.Sessions[update.Snapshot.Session]; existing != nil && existing != session {
		return false
	}
	previousTabID := remoteSessionTabID(update.Previous)
	replacementTabID := remoteSessionTabID(update.Snapshot.Session)
	delete(state.Sessions, update.Previous)
	delete(state.SessionTabs, previousTabID)
	session.Created.Session = update.Snapshot.Session
	session.Created.TopicID = update.Snapshot.TopicID
	session.Created.TopicTitle = update.Snapshot.Title
	session.Created.ResolvedProfile = update.Snapshot.Profile
	session.Snapshot = cloneRemoteSessionSnapshot(update.Snapshot)
	session.HistoryCursors = remoteHistoryCursorsFromSnapshot(update.Snapshot)
	session.AttachedGeneration = value.Generation
	session.ReattachGeneration = 0
	session.LastAttachError = ""
	state.Sessions[update.Snapshot.Session] = session
	state.SessionTabs[replacementTabID] = update.Snapshot.Session
	for index, tabID := range state.TabOrder {
		if tabID == previousTabID {
			state.TabOrder[index] = replacementTabID
			break
		}
	}
	if state.ActiveTabID == previousTabID {
		state.ActiveTabID = replacementTabID
	}
	return true
}

func pendingApprovalFromWire(value eventwire.Event) *runtimeapi.PendingPrompt {
	if value.Approval == nil {
		return nil
	}
	reason := stringPointerOrNil(value.Approval.Reason)
	return &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptApproval, Approval: &runtimeapi.ApprovalPrompt{
		ID: runtimeapi.PromptID(value.Approval.ID), Tool: value.Approval.Tool, Subject: value.Approval.Subject,
		Reason: reason, Fresh: value.Approval.Fresh,
		AllowedDecisions: []runtimeapi.PromptDecision{
			runtimeapi.DecisionAllowOnce, runtimeapi.DecisionAllowSession,
			runtimeapi.DecisionAllowPersistent, runtimeapi.DecisionDeny,
		},
	}}
}

func pendingAskFromWire(value eventwire.Event) *runtimeapi.PendingPrompt {
	if value.Ask == nil {
		return nil
	}
	questions := make([]runtimeapi.AskQuestion, len(value.Ask.Questions))
	for index, question := range value.Ask.Questions {
		options := make([]runtimeapi.AskOption, len(question.Options))
		for optionIndex, option := range question.Options {
			options[optionIndex] = runtimeapi.AskOption{Label: option.Label, Description: stringPointerOrNil(option.Description)}
		}
		prompt := question.Prompt
		questions[index] = runtimeapi.AskQuestion{
			ID: runtimeapi.QuestionID(question.ID), Header: question.Header, Prompt: &prompt,
			Options: options, Multi: question.Multi,
		}
	}
	return &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptAsk, Ask: &runtimeapi.AskPrompt{
		ID: runtimeapi.PromptID(value.Ask.ID), Questions: questions,
	}}
}

func stringPointerOrNil(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func (a *App) remoteTabMeta(tabID string) (TabMeta, bool) {
	session, workspace, target, ok := a.remoteSessionView(tabID)
	if !ok {
		return TabMeta{}, false
	}
	snapshot := session.Snapshot
	running := snapshot.Runtime.Running || len(snapshot.Jobs) > 0 || snapshot.PendingPrompt != nil
	ready := target.State == TargetRemoteConnected && session.AttachedGeneration == target.Generation
	return TabMeta{
		ID: remoteSessionTabID(session.Created.Session), Scope: "project",
		WorkspaceRoot: string(session.Created.Session.WorkspaceID),
		WorkspaceName: workspace.Name, WorkspacePath: workspace.DisplayPath,
		TopicID: string(session.Created.TopicID), TopicTitle: session.Created.TopicTitle,
		SessionPath: remoteSessionToken(session.Created.Session),
		Label:       remoteProfileModelLabel(snapshot.Profile.Model), Ready: ready, Running: running,
		PendingPrompt: snapshot.PendingPrompt != nil, BackgroundJobs: len(snapshot.Jobs),
		CancelRequested: snapshot.Runtime.CancelRequested,
		Cancellable:     snapshot.Runtime.CurrentTurn != nil || snapshot.Runtime.CurrentOperation != nil,
		Mode:            remoteProfileTabMode(snapshot.Profile), CollaborationMode: snapshot.Profile.CollaborationMode,
		ToolApprovalMode: snapshot.Profile.ToolApprovalMode, TokenMode: snapshot.Profile.TokenMode,
		Goal: dereferenceString(snapshot.Goal), GoalStatus: string(snapshot.GoalStatus),
		StartupErr: session.LastAttachError,
		Active:     a.remoteTabIsActive(remoteSessionTabID(session.Created.Session)), Cwd: workspace.DisplayPath,
		TargetKind: string(TargetRemote), WorkspaceID: string(session.Created.Session.WorkspaceID),
		SessionID: string(session.Created.Session.SessionID),
	}, true
}

func remoteProfileTabMode(profile runtimeapi.ResolvedProfile) string {
	plan := strings.EqualFold(strings.TrimSpace(profile.CollaborationMode), "plan")
	yolo := normalizeToolApprovalMode(profile.ToolApprovalMode) == "yolo"
	return tabModeFromAxes(plan, yolo)
}

func (a *App) remoteTabIsActive(tabID string) bool {
	a.remote.workbenchMu.RLock()
	defer a.remote.workbenchMu.RUnlock()
	return a.remote.workbench.ActiveTabID == tabID
}

func (a *App) remoteTabMetas() []TabMeta {
	a.remote.workbenchMu.RLock()
	order := append([]string(nil), a.remote.workbench.TabOrder...)
	a.remote.workbenchMu.RUnlock()
	out := make([]TabMeta, 0, len(order))
	for _, tabID := range order {
		if meta, ok := a.remoteTabMeta(tabID); ok {
			out = append(out, meta)
		}
	}
	return out
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *App) remoteSubmit(tabID, input string) error {
	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected || session.AttachedGeneration != target.Generation {
		return ErrRuntimeTargetUnavailable
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	result, err := api.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{Session: session.Created.Session, Input: input})
	cancel()
	if err != nil {
		return err
	}
	// An in-place state rewrite (notably raw /rewind) intentionally preserves
	// the runtime epoch and therefore has no replacement-resync notification.
	// Honor its explicit snapshotRequired result with an atomic same-target
	// resubscribe before publishing any further workbench state.
	if result.SnapshotRequired && result.Effect == string(runtimeapi.EffectStateChanged) && result.Session == session.Created.Session {
		if _, attachErr := a.refreshRemoteWorkbenchSession(api, result.Session, target.Generation); attachErr != nil {
			return fmt.Errorf("refresh Remote Session after composer state rewrite: %w", attachErr)
		}
		a.publishRemoteWorkbenchReady(a.RemoteWorkbenchStatus())
		return nil
	}
	a.recordRemoteSubmitResult(session.Created.Session, target.Generation, result)
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) recordRemoteSubmitResult(ref runtimeapi.SessionRef, generation uint64, result runtimeapi.ComposerSubmitResult) bool {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	current := a.remote.workbench.Sessions[ref]
	if current == nil || current.AttachedGeneration != generation {
		return false
	}
	current.Snapshot.Runtime.Running = result.Kind == runtimeapi.SubmitTurn || result.Kind == runtimeapi.SubmitOperation
	current.Snapshot.Runtime.CancelRequested = false
	current.Snapshot.Runtime.CurrentTurn = nil
	current.Snapshot.Runtime.CurrentOperation = nil
	if result.TurnID != "" {
		current.Snapshot.Runtime.CurrentTurn = &runtimeapi.TurnState{ID: result.TurnID}
	}
	if result.OperationID != "" {
		current.Snapshot.Runtime.CurrentOperation = &runtimeapi.OperationState{ID: result.OperationID, Kind: result.Operation}
	}
	return true
}

func (a *App) recordRemoteOperationStarted(
	ref runtimeapi.SessionRef,
	generation uint64,
	kind runtimeapi.OperationKind,
	result runtimeapi.OperationStartedResult,
) bool {
	if result.OperationID == "" {
		return false
	}
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	current := a.remote.workbench.Sessions[ref]
	if current == nil || current.AttachedGeneration != generation {
		return false
	}
	current.Snapshot.Runtime.Running = true
	current.Snapshot.Runtime.CancelRequested = false
	current.Snapshot.Runtime.CurrentTurn = nil
	current.Snapshot.Runtime.CurrentOperation = &runtimeapi.OperationState{ID: result.OperationID, Kind: string(kind)}
	return true
}

func (a *App) recordRemoteProfileResult(
	ref runtimeapi.SessionRef,
	generation uint64,
	result runtimeapi.SetProfileResult,
) bool {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	current := a.remote.workbench.Sessions[ref]
	if current == nil || current.AttachedGeneration != generation || result.SnapshotRequired {
		return false
	}
	current.Created.ResolvedProfile = result.ResolvedProfile
	current.Snapshot.Profile = result.ResolvedProfile
	if current.Snapshot.PendingPrompt == nil || len(result.AutoResolvedPromptIDs) == 0 {
		return true
	}
	var pendingID runtimeapi.PromptID
	switch current.Snapshot.PendingPrompt.Kind {
	case runtimeapi.PromptApproval:
		if current.Snapshot.PendingPrompt.Approval != nil {
			pendingID = current.Snapshot.PendingPrompt.Approval.ID
		}
	case runtimeapi.PromptAsk:
		if current.Snapshot.PendingPrompt.Ask != nil {
			pendingID = current.Snapshot.PendingPrompt.Ask.ID
		}
	}
	for _, resolvedID := range result.AutoResolvedPromptIDs {
		if resolvedID == pendingID {
			current.Snapshot.PendingPrompt = nil
			break
		}
	}
	return true
}

// refreshRemoteWorkbenchSession performs the same-target atomic snapshot
// refresh required by in-place state rewrites. Callers supply the already
// selected RuntimeAPI so the target generation checked here is the same one
// used for the mutation.
func (a *App) refreshRemoteWorkbenchSession(api runtimeapi.RuntimeAPI, ref runtimeapi.SessionRef, generation uint64) (runtimeapi.SessionSnapshot, error) {
	if api == nil || !ref.Valid() {
		return runtimeapi.SessionSnapshot{}, ErrRuntimeTargetUnavailable
	}
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	ctx, cancel := a.remoteActionContext()
	fresh, rollback, err := attachRemoteWorkbenchAtomic(ctx, api, runtimeapi.AttachAndSubscribeInput{
		Session: ref, HistoryTurns: validateRemoteWorkbenchHistoryTurns(a.desktopHistoryPageTurns()),
	})
	cancel()
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	retainSubscription := false
	defer func() {
		if !retainSubscription {
			discardRemoteWorkbenchSubscription(rollback, ref)
		}
	}()
	if fresh.Session != ref {
		return runtimeapi.SessionSnapshot{}, errors.New("Remote snapshot refresh returned a different Session identity")
	}
	manager := a.remote.manager
	if manager == nil {
		return runtimeapi.SessionSnapshot{}, ErrRuntimeTargetUnavailable
	}
	target := manager.Snapshot()
	if target.State != TargetRemoteConnected || target.Generation != generation {
		return runtimeapi.SessionSnapshot{}, ErrTargetTransitionSuperseded
	}
	a.remote.workbenchMu.Lock()
	current := a.remote.workbench.Sessions[ref]
	if a.remote.workbench.HostID != target.Target.ID || current == nil || current.AttachedGeneration != generation {
		a.remote.workbenchMu.Unlock()
		return runtimeapi.SessionSnapshot{}, ErrTargetTransitionSuperseded
	}
	current.Snapshot = cloneRemoteSessionSnapshot(fresh)
	current.HistoryCursors = remoteHistoryCursorsFromSnapshot(fresh)
	current.Created.TopicID = fresh.TopicID
	current.Created.TopicTitle = fresh.Title
	current.Created.ResolvedProfile = fresh.Profile
	current.LastAttachError = ""
	a.remote.workbenchMu.Unlock()
	retainSubscription = true
	return cloneRemoteSessionSnapshot(fresh), nil
}

func (a *App) remoteSteer(tabID, text string) error {
	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected || session.Snapshot.Runtime.CurrentTurn == nil {
		return errors.New("the Remote Session has no active Turn to steer")
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	return api.SteerTurn(ctx, runtimeapi.SteerInput{
		Session: session.Created.Session, TurnID: session.Snapshot.Runtime.CurrentTurn.ID, Text: text,
	})
}

func (a *App) remoteCancel(tabID string) error {
	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected {
		return ErrRuntimeTargetUnavailable
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	switch {
	case session.Snapshot.Runtime.CurrentTurn != nil:
		err = api.CancelTurn(ctx, runtimeapi.CancelTurnInput{
			Session: session.Created.Session, TurnID: session.Snapshot.Runtime.CurrentTurn.ID,
		})
	case session.Snapshot.Runtime.CurrentOperation != nil:
		canceller, supported := api.(remoteOperationCanceller)
		if !supported {
			err = runtimeapi.Unavailable(runtimeapi.CapabilityTurnCancel, "Operation cancellation is unavailable")
			break
		}
		_, err = canceller.CancelOperation(ctx, runtimeapi.CancelOperationInput{
			Session: session.Created.Session, OperationID: session.Snapshot.Runtime.CurrentOperation.ID,
		})
	default:
		err = errors.New("the Remote Session has no active Turn or Operation to cancel")
	}
	cancel()
	if err == nil {
		a.remote.workbenchMu.Lock()
		if current := a.remote.workbench.Sessions[session.Created.Session]; current != nil {
			current.Snapshot.Runtime.CancelRequested = true
			if current.Snapshot.Runtime.CurrentTurn != nil {
				current.Snapshot.Runtime.CurrentTurn.CancelRequested = true
			}
			if current.Snapshot.Runtime.CurrentOperation != nil {
				current.Snapshot.Runtime.CurrentOperation.CancelRequested = true
			}
		}
		a.remote.workbenchMu.Unlock()
	}
	return err
}

func (a *App) remoteApprove(tabID, promptID string, allow, sessionGrant, persist bool) error {
	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected {
		return ErrRuntimeTargetUnavailable
	}
	decision := runtimeapi.DecisionDeny
	if allow {
		switch {
		case persist:
			decision = runtimeapi.DecisionAllowPersistent
		case sessionGrant:
			decision = runtimeapi.DecisionAllowSession
		default:
			decision = runtimeapi.DecisionAllowOnce
		}
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	err = api.ApprovePrompt(ctx, runtimeapi.ApproveInput{
		Session: session.Created.Session, PromptID: runtimeapi.PromptID(promptID), Decision: decision,
	})
	cancel()
	if err != nil {
		return err
	}
	if !a.clearRemotePendingPrompt(target, session.Created.Session, runtimeapi.PromptID(promptID)) {
		return ErrTargetTransitionSuperseded
	}
	return nil
}

func (a *App) remoteAnswer(tabID, promptID string, answers []QuestionAnswer) error {
	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected {
		return ErrRuntimeTargetUnavailable
	}
	projected := make([]runtimeapi.QuestionAnswer, len(answers))
	for index, answer := range answers {
		projected[index] = runtimeapi.QuestionAnswer{
			QuestionID: runtimeapi.QuestionID(answer.QuestionID), Selected: append([]string(nil), answer.Selected...),
		}
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	err = api.AnswerPrompt(ctx, runtimeapi.AnswerInput{
		Session: session.Created.Session, PromptID: runtimeapi.PromptID(promptID), Answers: projected,
	})
	cancel()
	if err != nil {
		return err
	}
	if !a.clearRemotePendingPrompt(target, session.Created.Session, runtimeapi.PromptID(promptID)) {
		return ErrTargetTransitionSuperseded
	}
	return nil
}

func (a *App) clearRemotePendingPrompt(
	expected TargetManagerSnapshot,
	ref runtimeapi.SessionRef,
	promptID runtimeapi.PromptID,
) bool {
	changed := false
	committed := false
	if !a.withRemoteWorkbenchTarget(expected, func(state *remoteWorkbenchState) {
		current := state.Sessions[ref]
		if current == nil || current.AttachedGeneration != expected.Generation {
			return
		}
		committed = true
		pending := current.Snapshot.PendingPrompt
		if pending == nil {
			return
		}
		var currentID runtimeapi.PromptID
		switch pending.Kind {
		case runtimeapi.PromptApproval:
			if pending.Approval != nil {
				currentID = pending.Approval.ID
			}
		case runtimeapi.PromptAsk:
			if pending.Ask != nil {
				currentID = pending.Ask.ID
			}
		}
		if currentID == promptID {
			current.Snapshot.PendingPrompt = nil
			changed = true
		}
	}) || !committed {
		return false
	}
	if changed {
		a.emitProjectTreeChanged()
	}
	return true
}

func (a *App) closeRemoteWorkbenchSession(tabID string) error {
	a.remote.workbenchOp.Lock()
	defer a.remote.workbenchOp.Unlock()

	session, _, target, ok := a.remoteSessionView(tabID)
	if !ok || target.State != TargetRemoteConnected {
		return ErrRuntimeTargetUnavailable
	}
	a.remote.workbenchMu.RLock()
	tabCount := len(a.remote.workbench.TabOrder)
	a.remote.workbenchMu.RUnlock()
	if tabCount <= 1 {
		return errors.New("cannot close the last tab")
	}
	api, err := a.remoteConnectedRuntimeFor(target)
	if err != nil {
		return err
	}
	unsubscriber, ok := api.(remoteSessionUnsubscriber)
	if !ok {
		return runtimeapi.Unavailable(runtimeapi.CapabilitySessionAttach, "Session unsubscribe is unavailable")
	}
	ctx, cancel := a.remoteActionContext()
	err = unsubscriber.UnsubscribeSession(ctx, runtimeapi.UnsubscribeSessionInput{Session: session.Created.Session})
	cancel()
	if err != nil {
		return err
	}
	if v1, ok := api.(runtimeapi.V1RuntimeAPI); ok {
		ctx, cancel = a.remoteActionContext()
		_, closeErr := v1.CloseSession(ctx, runtimeapi.CloseSessionInput{Session: session.Created.Session})
		cancel()
		if closeErr != nil {
			// session/close is only a runtime release hint. The view subscription
			// is already gone and the Host-owned task/session must remain alive.
			slog.Warn("desktop: Remote Session close hint failed after tab unsubscribe", "session", session.Created.Session.SessionID, "err", closeErr)
		}
	}
	if _, committed := a.removeRemoteWorkbenchSession(target, session.Created.Session); !committed {
		// The exact old subscription is gone, but a newer target generation now
		// owns the visible workbench. Preserve that state; its queued reattach is
		// serialized behind workbenchOp and will establish a fresh binding.
		return ErrTargetTransitionSuperseded
	}
	return nil
}

func (a *App) remoteHistoryPage(tabID string) (HistoryPage, bool) {
	session, _, _, ok := a.remoteSessionView(tabID)
	if !ok {
		return HistoryPage{}, false
	}
	page := session.Snapshot.History
	return HistoryPage{
		Messages:  mapRemoteHistoryMessages(page.Messages, session.Snapshot.Checkpoints),
		StartTurn: page.StartTurn, EndTurn: page.EndTurn, TotalTurns: page.TotalTurns, HasOlder: page.HasOlder,
	}, true
}

func (a *App) remoteHistoryCursor(tabID string, beforeTurn int) (runtimeapi.Cursor, runtimeapi.SessionRef, bool) {
	a.remote.workbenchMu.RLock()
	defer a.remote.workbenchMu.RUnlock()
	state := &a.remote.workbench
	if tabID == "" {
		tabID = state.ActiveTabID
	}
	ref, ok := state.SessionTabs[tabID]
	if !ok {
		return "", runtimeapi.SessionRef{}, false
	}
	session := state.Sessions[ref]
	if session == nil {
		return "", runtimeapi.SessionRef{}, false
	}
	cursor, ok := session.HistoryCursors[beforeTurn]
	return cursor, ref, ok && cursor != ""
}

func (a *App) recordRemoteHistoryPage(ref runtimeapi.SessionRef, page runtimeapi.HistoryPage) bool {
	a.remote.workbenchMu.Lock()
	defer a.remote.workbenchMu.Unlock()
	session := a.remote.workbench.Sessions[ref]
	if session == nil {
		return false
	}
	if session.HistoryCursors == nil {
		session.HistoryCursors = make(map[int]runtimeapi.Cursor)
	}
	if page.HasOlder && page.Next != "" {
		session.HistoryCursors[page.StartTurn] = page.Next
	} else {
		delete(session.HistoryCursors, page.StartTurn)
	}
	return true
}

func mapRemoteHistoryMessages(messages []runtimeapi.HistoryMessage, checkpoints []runtimeapi.Checkpoint) []HistoryMessage {
	turns := make(map[runtimeapi.CheckpointID]int, len(checkpoints))
	for _, checkpoint := range checkpoints {
		turns[checkpoint.ID] = checkpoint.DisplayTurn
	}
	out := make([]HistoryMessage, len(messages))
	for index, message := range messages {
		toolCalls := make([]HistoryToolCall, len(message.ToolCalls))
		for toolIndex, tool := range message.ToolCalls {
			toolCalls[toolIndex] = HistoryToolCall{
				ID: tool.ID, Name: tool.Name, Arguments: dereferenceString(tool.Arguments), Subject: tool.Subject,
				Summary: dereferenceString(tool.Summary), Diff: dereferenceString(tool.Diff), Added: tool.Added,
				Removed: tool.Removed, ArgumentsArchived: tool.ArgumentsArchived,
			}
		}
		citations := make([]provider.MemoryCitation, len(message.MemoryCitations))
		for citationIndex, citation := range message.MemoryCitations {
			citations[citationIndex] = provider.MemoryCitation{
				ID: citation.ID, Source: citation.Source, LineStart: citation.LineStart, LineEnd: citation.LineEnd,
				Note: citation.Note, Kind: citation.Kind,
			}
		}
		var checkpointTurn *int
		if turn, found := turns[message.CheckpointID]; found {
			value := turn
			checkpointTurn = &value
		}
		out[index] = HistoryMessage{
			Role: message.Role, Content: dereferenceString(message.Content), Detail: dereferenceString(message.Detail),
			Code: message.Code, SubmitText: dereferenceString(message.SubmitText), CheckpointTurn: checkpointTurn,
			Reasoning: dereferenceString(message.Reasoning), MemoryCitations: citations,
			WorkDurationMs: message.WorkDurationMillis, Level: message.Level, ToolCalls: toolCalls,
			ToolCallID: message.ToolCallID, ToolName: message.ToolName, ToolResultArchived: message.ToolResultArchived,
			ToolResultError: dereferenceString(message.ToolResultError), Pending: message.Pending, Trigger: message.Trigger,
			Messages: message.Messages, Summary: dereferenceString(message.Summary), Archive: dereferenceString(message.Archive),
		}
	}
	return out
}

func (a *App) remoteCheckpointViews(tabID string) ([]CheckpointMeta, bool) {
	session, _, _, ok := a.remoteSessionView(tabID)
	if !ok {
		return nil, false
	}
	out := make([]CheckpointMeta, len(session.Snapshot.Checkpoints))
	for index, checkpoint := range session.Snapshot.Checkpoints {
		out[index] = CheckpointMeta{
			Turn: checkpoint.DisplayTurn, Prompt: dereferenceString(checkpoint.Prompt),
			Files: append([]string(nil), checkpoint.Files...), FileCount: checkpoint.FileCount,
			FilesTruncated: checkpoint.FilesTruncated, TurnFileCount: len(checkpoint.Files),
			Time: checkpoint.CreatedAtMillis, CanCode: checkpoint.CanCode, CanConversation: checkpoint.CanConversation,
		}
	}
	return out, true
}

func (a *App) replayRemotePendingPrompt() bool {
	a.remote.workbenchMu.RLock()
	type pendingReplay struct {
		tabID  string
		prompt *runtimeapi.PendingPrompt
	}
	replays := make([]pendingReplay, 0, len(a.remote.workbench.TabOrder))
	for _, tabID := range a.remote.workbench.TabOrder {
		ref, ok := a.remote.workbench.SessionTabs[tabID]
		if !ok {
			continue
		}
		session := a.remote.workbench.Sessions[ref]
		if session == nil || session.Snapshot.PendingPrompt == nil {
			continue
		}
		// Clone the snapshot-owned prompt while holding the workbench lock so
		// reconnect/resync cannot replace it while the events are projected.
		prompt := cloneRemoteSessionSnapshot(session.Snapshot).PendingPrompt
		replays = append(replays, pendingReplay{tabID: tabID, prompt: prompt})
	}
	hasWorkbench := len(a.remote.workbench.Sessions) != 0
	a.remote.workbenchMu.RUnlock()

	for _, replay := range replays {
		if value, ok := remotePendingPromptEvent(replay.prompt); ok {
			a.emitRuntimeEvent(eventChannel, wireEventTab{Event: value, TabID: replay.tabID})
		}
	}
	return hasWorkbench
}

func remotePendingPromptEvent(prompt *runtimeapi.PendingPrompt) (eventwire.Event, bool) {
	if prompt == nil {
		return eventwire.Event{}, false
	}
	var value eventwire.Event
	switch prompt.Kind {
	case runtimeapi.PromptApproval:
		if prompt.Approval == nil {
			return eventwire.Event{}, false
		}
		value = eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{
			ID: string(prompt.Approval.ID), Tool: prompt.Approval.Tool, Subject: prompt.Approval.Subject,
			Reason: dereferenceString(prompt.Approval.Reason), Fresh: prompt.Approval.Fresh,
		}}
	case runtimeapi.PromptAsk:
		if prompt.Ask == nil {
			return eventwire.Event{}, false
		}
		questions := make([]eventwire.AskQuestion, len(prompt.Ask.Questions))
		for index, question := range prompt.Ask.Questions {
			options := make([]eventwire.AskOption, len(question.Options))
			for optionIndex, option := range question.Options {
				options[optionIndex] = eventwire.AskOption{Label: option.Label, Description: dereferenceString(option.Description)}
			}
			questions[index] = eventwire.AskQuestion{
				ID: string(question.ID), Header: question.Header, Prompt: dereferenceString(question.Prompt),
				Options: options, Multi: question.Multi,
			}
		}
		value = eventwire.Event{Kind: "ask_request", Ask: &eventwire.Ask{ID: string(prompt.Ask.ID), Questions: questions}}
	default:
		return eventwire.Event{}, false
	}
	return value, true
}

func (a *App) remoteMeta(tabID string) (Meta, bool) {
	session, workspace, target, ok := a.remoteSessionView(tabID)
	if !ok {
		return Meta{}, false
	}
	autoApproveTools := normalizeToolApprovalMode(session.Snapshot.Profile.ToolApprovalMode) == "yolo"
	return Meta{
		// Meta.Label is the model label consumed by the Composer model switcher
		// and status bar. TopicTitle belongs to the tab/session navigation model;
		// projecting it here made Remote sessions show their topic as the selected
		// model even though the authoritative profile and catalog were correct.
		Label:      remoteProfileModelLabel(session.Snapshot.Profile.Model),
		Ready:      target.State == TargetRemoteConnected && session.AttachedGeneration == target.Generation,
		StartupErr: session.LastAttachError, EventChannel: eventChannel,
		Cwd: workspace.DisplayPath, WorkspaceRoot: string(session.Created.Session.WorkspaceID),
		WorkspaceName: workspace.Name, WorkspacePath: workspace.DisplayPath,
		ImageInputEnabled: false, AutoApproveTools: autoApproveTools, Bypass: autoApproveTools,
		CollaborationMode: session.Snapshot.Profile.CollaborationMode,
		ToolApprovalMode:  session.Snapshot.Profile.ToolApprovalMode, TokenMode: session.Snapshot.Profile.TokenMode,
		Goal: dereferenceString(session.Snapshot.Goal), GoalStatus: string(session.Snapshot.GoalStatus),
	}, true
}

func remoteProfileModelLabel(ref string) string {
	ref = strings.TrimSpace(ref)
	if _, model, ok := strings.Cut(ref, "/"); ok && strings.TrimSpace(model) != "" {
		return model
	}
	return ref
}

func removeRemoteTabID(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

type remoteWorkbenchSessionRemovalMatch func(runtimeapi.SessionRef, *remoteWorkbenchSession) bool
type remoteWorkbenchPendingRemovalMatch func(*pendingRemoteSessionCreate) bool

// removeRemoteWorkbenchRecords commits a Host-confirmed record removal only
// against the target generation that issued it. Host-scoped pending creates are
// safe to clear even after the UI has moved away, but current workbench state
// must never be mutated after A -> B or A -> B -> A supersedes expected.
func (a *App) removeRemoteWorkbenchRecords(
	expected TargetManagerSnapshot,
	matchSession remoteWorkbenchSessionRemovalMatch,
	matchPending remoteWorkbenchPendingRemovalMatch,
	removeWorkspace runtimeapi.WorkspaceID,
) (int, bool) {
	if a == nil || a.remote.manager == nil || matchSession == nil || matchPending == nil {
		return 0, false
	}
	manager := a.remote.manager
	manager.stateDispatchMu.Lock()
	currentMatches := remoteWorkbenchTargetMatches(manager.Snapshot(), expected)

	a.remote.workbenchMu.Lock()
	if byFingerprint := a.remote.workspacePending[expected.Target.ID]; byFingerprint != nil {
		for fingerprint, pending := range byFingerprint {
			if pending != nil && matchPending(pending) {
				delete(byFingerprint, fingerprint)
			}
		}
		if len(byFingerprint) == 0 {
			delete(a.remote.workspacePending, expected.Target.ID)
		}
	}

	state := &a.remote.workbench
	removed := 0
	stateChanged := false
	var status RemoteWorkbenchStatusView
	if currentMatches && state.HostID == expected.Target.ID {
		for ref, session := range state.Sessions {
			if !matchSession(ref, session) {
				continue
			}
			tabID := remoteSessionTabID(ref)
			delete(state.Sessions, ref)
			delete(state.SessionTabs, tabID)
			state.TabOrder = removeRemoteTabID(state.TabOrder, tabID)
			removed++
			stateChanged = true
		}
		if removeWorkspace != "" {
			if _, exists := state.Workspaces[removeWorkspace]; exists {
				delete(state.Workspaces, removeWorkspace)
				stateChanged = true
			}
		}
		for fingerprint, pending := range state.Pending {
			if pending != nil && matchPending(pending) {
				delete(state.Pending, fingerprint)
				stateChanged = true
			}
		}
		if _, ok := state.SessionTabs[state.ActiveTabID]; !ok {
			state.ActiveTabID = ""
			if len(state.TabOrder) != 0 {
				state.ActiveTabID = state.TabOrder[0]
			}
		}
		if stateChanged {
			status = remoteWorkbenchStatusLocked(*state, expected)
		}
	}
	a.remote.workbenchMu.Unlock()
	if stateChanged {
		a.emitRuntimeEvent(remoteWorkbenchStateEvent, status)
	}
	manager.stateDispatchMu.Unlock()
	if currentMatches {
		a.emitProjectTreeChanged()
	}
	return removed, currentMatches
}

// removeRemoteWorkbenchSession does not unsubscribe: the caller has already
// completed the Host lifecycle operation (or an exact view unsubscribe).
func (a *App) removeRemoteWorkbenchSession(expected TargetManagerSnapshot, ref runtimeapi.SessionRef) (bool, bool) {
	removed, committed := a.removeRemoteWorkbenchRecords(
		expected,
		func(candidate runtimeapi.SessionRef, _ *remoteWorkbenchSession) bool { return candidate == ref },
		func(pending *pendingRemoteSessionCreate) bool { return pending.Created.Session == ref },
		"",
	)
	return removed != 0, committed
}

func (a *App) removeRemoteWorkbenchWorkspace(expected TargetManagerSnapshot, workspaceID runtimeapi.WorkspaceID) (int, bool) {
	return a.removeRemoteWorkbenchRecords(
		expected,
		func(ref runtimeapi.SessionRef, _ *remoteWorkbenchSession) bool { return ref.WorkspaceID == workspaceID },
		func(pending *pendingRemoteSessionCreate) bool {
			return pending.Workspace.ID == workspaceID || pending.Created.Session.WorkspaceID == workspaceID
		},
		workspaceID,
	)
}

func (a *App) removeRemoteWorkbenchTopic(
	expected TargetManagerSnapshot,
	workspaceID runtimeapi.WorkspaceID,
	topicID runtimeapi.TopicID,
) (int, bool) {
	return a.removeRemoteWorkbenchRecords(
		expected,
		func(ref runtimeapi.SessionRef, session *remoteWorkbenchSession) bool {
			return ref.WorkspaceID == workspaceID && session != nil && session.Created.TopicID == topicID
		},
		func(pending *pendingRemoteSessionCreate) bool {
			return pending.Created.Session.WorkspaceID == workspaceID && pending.Created.TopicID == topicID
		},
		"",
	)
}
