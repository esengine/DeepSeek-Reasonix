package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// topic_activation.go implements the two-phase topic activation used by the
// single-conversation-surface layout.
//
// Phase 1 (synchronous, inside StartTopicActivation): the tab is opened or
// reused, becomes the active tab, and tabs are persisted — the visible surface
// switches immediately, exactly as ActivateTopic behaves today. The caller
// gets a ticket with the tab meta right away.
//
// Phase 2 (background completion): the controller build started by the open
// path finishes (the completion waits on the tab's build-done channel), then
// — only if this activation is still the latest request — the other visible
// tabs are pruned (keepOnlyVisibleTab) and a terminal "ready"/"failed" event
// is emitted on the "topic:activation" channel. A superseded activation's
// completion emits nothing further: the "cancelled" event is emitted up front,
// at supersede time, by whichever activation or legacy surface switch replaced
// it.
//
// Generation protocol: activationGen bumps every time a new ticketed
// activation starts or a legacy surface switch (ActivateTopic,
// EnsureBlankSurface, SetActiveTab to another tab) supersedes the pending one.
// The completion re-checks gen+requestID under singleSurfaceMu immediately
// before pruning, so a stale completion can never prune tabs a newer
// activation just created. Lock order note: the completion waits on the
// build-done channel BEFORE taking singleSurfaceMu, and the synchronous phase
// never waits on a build-done channel, so the two cannot deadlock.

const (
	topicActivationEventChannel = "topic:activation"

	topicActivationPhaseStarting  = "starting"
	topicActivationPhaseReady     = "ready"
	topicActivationPhaseFailed    = "failed"
	topicActivationPhaseCancelled = "cancelled"
)

// TopicActivationRequest is the input of StartTopicActivation. Scope is
// "project" (WorkspaceRoot required) or "global". SessionPath, when set,
// selects a concrete saved session like OpenTopicSession; otherwise the topic
// resolves to its latest session. RequestID is optional — the backend
// generates one when empty.
type TopicActivationRequest struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	TopicID       string `json:"topicId"`
	SessionPath   string `json:"sessionPath"`
	RequestID     string `json:"requestId"`
}

// TopicActivationTicket is returned synchronously by StartTopicActivation. The
// frontend switches its visible surface from Meta immediately and tracks the
// background completion through "topic:activation" events keyed by RequestID.
type TopicActivationTicket struct {
	RequestID string  `json:"requestId"`
	TabID     string  `json:"tabId"`
	Meta      TabMeta `json:"meta"`
}

// TopicActivationEvent is emitted on the "topic:activation" channel. Per
// requestId, "starting" is always emitted (synchronously, before the ticket is
// returned) and is followed by exactly one terminal event: "ready" or "failed"
// for the activation that wins, "cancelled" for one superseded before its
// completion ran. A superseded activation never emits "ready"/"failed".
// Ordering across requestIds follows request order for "starting"/"cancelled"
// (emitted under singleSurfaceMu); terminal events may lag arbitrarily since
// they depend on the controller build.
type TopicActivationEvent struct {
	RequestID string `json:"requestId"`
	TabID     string `json:"tabId"`
	Phase     string `json:"phase"` // "starting" | "ready" | "failed" | "cancelled"
	Error     string `json:"error,omitempty"`
}

func newTopicActivationRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "act_" + hex.EncodeToString(b[:])
	}
	now := time.Now().UTC()
	return fmt.Sprintf("act_%s_%09d", now.Format("20060102150405"), now.Nanosecond())
}

// emitTopicActivation delivers an activation lifecycle event. The test hook
// (when installed) replaces emission so tests observe events synchronously;
// production goes through the async runtime emitter and never blocks a build.
func (a *App) emitTopicActivation(ev TopicActivationEvent) {
	a.mu.RLock()
	hook := a.activationEventHook
	a.mu.RUnlock()
	if hook != nil {
		hook(ev)
		return
	}
	a.emitRuntimeEvent(topicActivationEventChannel, ev)
}

// supersedePendingTopicActivationLocked invalidates the pending ticketed
// activation, if any, and bumps the activation generation so its background
// completion becomes a no-op. Callers must hold a.mu. Returns the superseded
// requestID/tabID so the caller can emit "cancelled" after unlocking (""
// when nothing was pending or the pending activation already completed).
//
// When cancelBuild is true the pending activation's in-flight tab build is
// cancelled through the standard superseded-build mechanics — unless the new
// activation targets the same tab (exceptTabID), in which case the build is
// still needed. SetActiveTab passes false: a direct tab click does not prune
// the pending tab, so its build may legitimately finish.
func (a *App) supersedePendingTopicActivationLocked(exceptTabID string, cancelBuild bool) (string, string) {
	reqID := a.latestActivationRequestID
	tabID := a.pendingActivationTabID
	a.activationGen++
	a.latestActivationRequestID = ""
	a.pendingActivationTabID = ""
	if cancelBuild && tabID != "" && tabID != exceptTabID {
		if prev := a.tabs[tabID]; prev != nil {
			a.supersedeTabBuildLocked(prev)
		}
	}
	return reqID, tabID
}

func (a *App) supersedePendingTopicActivation(exceptTabID string) (string, string) {
	a.mu.Lock()
	reqID, tabID := a.supersedePendingTopicActivationLocked(exceptTabID, true)
	a.mu.Unlock()
	return reqID, tabID
}

// finishTopicActivation clears the pending marker after a completion ran, but
// only when this activation is still the latest — a newer request's marker
// must survive an older completion's cleanup.
func (a *App) finishTopicActivation(gen uint64, requestID string) {
	a.mu.Lock()
	if a.activationGen == gen && a.latestActivationRequestID == requestID {
		a.latestActivationRequestID = ""
		a.pendingActivationTabID = ""
	}
	a.mu.Unlock()
}

// StartTopicActivation activates a topic on the single visible conversation
// surface and returns a ticket immediately after the surface switch; the
// controller build, old-session snapshot/lease handling, and visible-tab
// pruning complete in the background and are reported through
// "topic:activation" events.
//
// Starting a new activation cancels the previous pending one (generation bump
// + build cancel through the existing superseded-build path). Legacy surface
// switches (ActivateTopic, EnsureBlankSurface, SetActiveTab) participate in
// the same generation, so interleaved legacy and ticketed calls resolve
// deterministically to the last call.
func (a *App) StartTopicActivation(req TopicActivationRequest) (TopicActivationTicket, error) {
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()

	var meta TabMeta
	var err error
	if strings.TrimSpace(req.SessionPath) != "" {
		meta, err = a.openTopicSession(req.Scope, req.WorkspaceRoot, req.TopicID, req.SessionPath)
	} else if strings.TrimSpace(req.Scope) == "project" {
		meta, err = a.openProjectTab(req.WorkspaceRoot, req.TopicID)
	} else {
		meta, err = a.openGlobalTab(req.TopicID)
	}
	if err != nil {
		// The open failed before anything changed hands: leave a previously
		// pending activation untouched, same as ActivateTopic leaves state
		// untouched on error.
		return TopicActivationTicket{}, err
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = newTopicActivationRequestID()
	}

	// The open succeeded and may already have started this tab's build (new
	// tab) inside the call above. Register this activation as the latest and
	// cancel the previous pending activation's build when it targets a
	// different tab — its completion is guarded off by the generation bump
	// either way.
	a.mu.Lock()
	prevReqID, prevTabID := a.supersedePendingTopicActivationLocked(meta.ID, true)
	gen := a.activationGen
	a.latestActivationRequestID = requestID
	a.pendingActivationTabID = meta.ID
	a.mu.Unlock()

	if prevReqID != "" {
		a.emitTopicActivation(TopicActivationEvent{RequestID: prevReqID, TabID: prevTabID, Phase: topicActivationPhaseCancelled})
	}
	a.emitTopicActivation(TopicActivationEvent{RequestID: requestID, TabID: meta.ID, Phase: topicActivationPhaseStarting})

	a.goSafe("topic-activation-completion", func() {
		a.runTopicActivationCompletion(gen, requestID, meta.ID)
	})

	return TopicActivationTicket{RequestID: requestID, TabID: meta.ID, Meta: meta}, nil
}

// runTopicActivationCompletion is phase 2 of a ticketed activation. It waits
// for the tab's in-flight controller build (if any), then — only when the
// activation is still the latest — prunes the other visible tabs exactly once
// and emits the terminal event. Superseded completions return silently; their
// "cancelled" event was already emitted at supersede time.
func (a *App) runTopicActivationCompletion(gen uint64, requestID, tabID string) {
	// Wait for the in-flight build first, WITHOUT holding singleSurfaceMu:
	// the synchronous phase of a newer activation needs that mutex and never
	// waits on a build, so this ordering cannot deadlock. A nil channel means
	// no build is in flight (reuse/fast path, or the synchronous test build
	// already finished) and the completion proceeds immediately.
	a.mu.RLock()
	tab := a.tabs[tabID]
	var buildDone chan struct{}
	if tab != nil {
		buildDone = tab.buildDone
	}
	a.mu.RUnlock()
	if buildDone != nil {
		<-buildDone
	}

	// The generation check and the prune serialize against new activations
	// through singleSurfaceMu: either this completion runs entirely before the
	// next activation's synchronous phase (its tabs are not there to prune),
	// or after it (the generation no longer matches and nothing is pruned).
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()

	a.mu.RLock()
	latest := a.activationGen == gen &&
		a.latestActivationRequestID == requestID &&
		a.pendingActivationTabID == tabID &&
		a.tabs[tabID] != nil
	a.mu.RUnlock()
	if !latest {
		return
	}

	if _, err := a.keepOnlyVisibleTab(tabID); err != nil {
		a.finishTopicActivation(gen, requestID)
		a.emitTopicActivation(TopicActivationEvent{
			RequestID: requestID,
			TabID:     tabID,
			Phase:     topicActivationPhaseFailed,
			// keepOnlyVisibleTab errors can wrap snapshot/path details; the
			// event stays generic, the slog entry keeps the specifics.
			Error: "failed to switch the visible session",
		})
		return
	}

	// Preserve today's failure semantics: a failed build leaves the tab
	// visible with StartupErr and a failed/lease_blocked runtime phase; the
	// prune still happened (the surface switched), only the terminal event
	// differs.
	a.mu.RLock()
	tab = a.tabs[tabID]
	ready := tab != nil && tab.Ready && tab.Ctrl != nil
	startupErr, leaseHeld := "", false
	if tab != nil {
		startupErr = tab.StartupErr
		leaseHeld = tab.StartupErrLeaseHeld
	}
	a.mu.RUnlock()

	a.finishTopicActivation(gen, requestID)
	switch {
	case ready:
		a.emitTopicActivation(TopicActivationEvent{RequestID: requestID, TabID: tabID, Phase: topicActivationPhaseReady})
		// The activation just made this tab visible: refresh the expensive
		// meta fields off-lock and push them to the frontend.
		a.scheduleTabMetaExtrasRefresh(tabID)
	default:
		a.emitTopicActivation(TopicActivationEvent{
			RequestID: requestID,
			TabID:     tabID,
			Phase:     topicActivationPhaseFailed,
			Error:     sanitizedTopicActivationError(startupErr, leaseHeld),
		})
	}
}

// sanitizedTopicActivationError keeps local paths and lease-holder writer IDs
// out of the activation event. The lease-busy message is already sanitized;
// everything else degrades to a generic summary — the full detail remains
// available to the frontend through Meta.StartupErr, same as today.
func sanitizedTopicActivationError(startupErr string, leaseHeld bool) string {
	if leaseHeld && strings.TrimSpace(startupErr) != "" {
		return startupErr
	}
	if strings.TrimSpace(startupErr) != "" {
		return "session failed to start"
	}
	return "session is not ready"
}

// --- MetaForTab fast-path cache --------------------------------------------

// tabMetaRefreshEventChannel carries TabMetaRefreshEvent after a background
// refresh of the expensive Meta fields (git branch, image input capability).
const tabMetaRefreshEventChannel = "tab:meta"

// TabMetaRefreshEvent pushes a freshly recomputed Meta to the frontend after
// the cached expensive fields changed. The frontend should treat it like a
// MetaForTab response for TabID.
type TabMetaRefreshEvent struct {
	TabID string `json:"tabId"`
	Meta  Meta   `json:"meta"`
}

// tabMetaExtras is the per-tab cached snapshot of the MetaForTab fields that
// are too expensive to compute on the request path. It is keyed conservatively
// by the workspace root the values were computed for: a root mismatch serves
// empty values rather than another root's branch/capability. model keys the
// image-input computation so a model switch invalidates it without
// invalidating the (root-scoped) git branch.
type tabMetaExtras struct {
	workspaceRoot     string
	model             string
	gitBranch         string
	imageInputEnabled bool
	fetchedAt         time.Time
}

// tabMetaExtrasFor returns the cached extras valid for (root, model) and
// whether a background refresh should be scheduled. A stale-but-root-matching
// git branch is served while refreshing (same policy the old
// workspaceGitBranchForMeta cache used); a model mismatch hides only the
// image-input flag, and a root mismatch hides both.
func tabMetaExtrasFor(tab *WorkspaceTab, root, model string) (tabMetaExtras, bool) {
	var zero tabMetaExtras
	if tab == nil || root == "" {
		return zero, false
	}
	extras := tab.metaExtras.Load()
	if extras == nil {
		return zero, true
	}
	if extras.workspaceRoot != root {
		return zero, true
	}
	out := *extras
	refresh := time.Since(extras.fetchedAt) > workspaceGitBranchCacheTTL
	if extras.model != model {
		out.imageInputEnabled = false
		refresh = true
	}
	return out, refresh
}

// scheduleTabMetaExtrasRefresh starts a background refresh unless one is
// already in flight for this tab. Safe to call from request paths.
func (a *App) scheduleTabMetaExtrasRefresh(tabID string) {
	a.mu.RLock()
	tab := a.tabs[tabID]
	a.mu.RUnlock()
	if tab == nil || !tab.metaExtrasRefreshing.CompareAndSwap(false, true) {
		return
	}
	a.goSafe("tab-meta-extras-refresh", func() {
		a.refreshTabMetaExtras(tab)
	})
}

// refreshTabMetaExtras recomputes the expensive Meta fields off-lock (a cheap
// `git rev-parse` plus one config load per refresh is fine in the background),
// publishes them into the tab's cache, and pushes the refreshed Meta to the
// frontend on "tab:meta". The tab-identity re-check after the off-lock stretch
// keeps a pruned/replaced tab from receiving another tab's values.
func (a *App) refreshTabMetaExtras(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	defer tab.metaExtrasRefreshing.Store(false)
	a.mu.RLock()
	if a.tabs[tab.ID] != tab {
		a.mu.RUnlock()
		return
	}
	root := tab.WorkspaceRoot
	model := tab.model
	a.mu.RUnlock()
	if root == "" {
		root, _ = os.Getwd()
	}

	gitBranch := ""
	if root != "" {
		gitBranch = workspaceGitBranch(root)
	}
	imageInputEnabled := a.imageInputEnabledForRootModel(root, model)

	a.mu.Lock()
	if a.tabs[tab.ID] != tab {
		a.mu.Unlock()
		return
	}
	tab.metaExtras.Store(&tabMetaExtras{
		workspaceRoot:     root,
		model:             model,
		gitBranch:         gitBranch,
		imageInputEnabled: imageInputEnabled,
		fetchedAt:         time.Now(),
	})
	a.mu.Unlock()

	meta := a.MetaForTab(tab.ID)
	a.emitRuntimeEvent(tabMetaRefreshEventChannel, TabMetaRefreshEvent{TabID: tab.ID, Meta: meta})
}
