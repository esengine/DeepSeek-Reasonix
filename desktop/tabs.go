package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/fileutil"
	"reasonix/internal/notify"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// --- WorkspaceTab -----------------------------------------------------------

// WorkspaceTab is one open conversation tab in the desktop. Each tab owns an
// independent controller (its own agent, session, tool registry, plugin host,
// memory, permissions) scoped to a workspace root, so multiple projects and
// topics can be active concurrently without interfering.
type WorkspaceTab struct {
	ID              string             // stable random id
	Scope           string             // "project" | "global"
	WorkspaceRoot   string             // project root dir (empty for global)
	SharedHostKey   string             // opaque key for the shared plugin host (set by buildTabController)
	TopicID         string             // topic within the project
	TopicTitle      string             // display title
	SessionPath     string             // exact .jsonl file this tab continues
	ReadOnly        bool               // true for external channel transcripts opened for browsing
	Ctrl            control.SessionAPI // nil while booting / on error
	Label           string             // model label (for the tab badge)
	Ready           bool               // true once boot.Build completes
	StartupErr      string             // build error, surfaced to the frontend
	sessionLease    *agent.SessionLease
	sessionLeaseMu  sync.Mutex
	sink            *tabEventSink      // routes events with this tab's ID
	buildCancel     context.CancelFunc // cancels in-flight boot for tabs removed before Ready
	buildGeneration uint64             // identifies the current in-flight build
	removed         bool               // set when the visible tab is pruned/closed before build completes
	reconcileMu     sync.Mutex         // serializes stale controller workspace repair for this tab

	ActivityStatus string // transient project-tree status for the in-flight turn

	// Per-turn autosave per tab.
	saveMu       sync.Mutex
	saving       bool
	saveAgain    bool
	saveFailures int
	// lastAutosaveWarnAt debounces the user-facing autosave-failure notice:
	// a persistently failing disk (AV hold, full volume) otherwise emits a
	// chat warning for every completed turn. Logs are never debounced.
	lastAutosaveWarnAt time.Time

	// closing is set under saveMu when the tab is being torn down. Once set,
	// tabSnapshotLoop stops taking new snapshot work and CloseTab waits on
	// saveCond until any in-flight snapshot finishes - so no background
	// snapshot can write a session file back to disk after CloseTab returns.
	// Without this, deleting a just-closed session races that write and the
	// session "resurrects" (#4384).
	closing  bool
	saveCond *sync.Cond

	// readTelemetry tracks files read during this tab's session.
	readTelemetry  []readFileRecord
	usageTelemetry sessionUsageStats
	// telemetrySessionKey is the sessionRuntimeKey the telemetry above belongs
	// to. Controller-side session rotations (typed /new, bot /reset) bypass the
	// App bindings, so telemetry writers and readers re-key through
	// syncTelemetryToSession before trusting the in-memory totals — otherwise a
	// previous session's cost keeps accumulating under the new session and gets
	// persisted into its sidecar (#5850).
	telemetrySessionKey string
	telemMu             sync.Mutex

	// plannerDisplay keeps display-only planner output for the in-flight turn.
	// The executor session remains the model-facing transcript; this sidecar
	// lets frontend history restore the planner cards after a rebuild/reload.
	plannerDisplay      []HistoryMessage
	plannerDisplayTools map[string]string
	plannerDisplayMu    sync.Mutex

	model            string // active model ref (for meta)
	effort           *string
	tokenMode        string
	mode             string // "normal" | "plan" | "yolo" | "plan-yolo"; yolo/full access is runtime-only
	goal             string
	toolApprovalMode string
	disabledMCP      map[string]ServerView
	mcpOrder         []string
}

const (
	topicStatusThinking            = "thinking"
	topicStatusStreaming           = "streaming"
	topicStatusWaitingConfirmation = "waiting_confirmation"
	topicStatusBackgroundJob       = "background_job"
	topicStatusPaused              = "paused"
	topicStatusError               = "error"
)

type readFileRecord struct {
	Path      string `json:"path"`
	Turn      int    `json:"turn"`
	Time      int64  `json:"time"`
	Offset    int    `json:"offset,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type sessionUsageStats struct {
	PromptTokens     int                         `json:"promptTokens"`
	CompletionTokens int                         `json:"completionTokens"`
	TotalTokens      int                         `json:"totalTokens"`
	ReasoningTokens  int                         `json:"reasoningTokens"`
	CacheHitTokens   int                         `json:"cacheHitTokens"`
	CacheMissTokens  int                         `json:"cacheMissTokens"`
	RequestCount     int                         `json:"requestCount"`
	ElapsedMs        int64                       `json:"elapsedMs"`
	SessionCost      float64                     `json:"sessionCost,omitempty"`
	SessionCurrency  string                      `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64                     `json:"sessionCostUsd,omitempty"`
	Sources          map[string]usageSourceStats `json:"sources,omitempty"`

	activeTurnStartedAt int64
	sourceSessionCache  map[string]sourceSessionCacheCounters
}

type usageSourceStats struct {
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	CacheHitTokens   int     `json:"cacheHitTokens"`
	CacheMissTokens  int     `json:"cacheMissTokens"`
	RequestCount     int     `json:"requestCount"`
	SessionCost      float64 `json:"sessionCost,omitempty"`
	SessionCurrency  string  `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64 `json:"sessionCostUsd,omitempty"`
}

type sourceSessionCacheCounters struct {
	Hit  int
	Miss int
}

func cloneSessionUsageStats(in sessionUsageStats) sessionUsageStats {
	out := in
	if len(in.Sources) > 0 {
		out.Sources = make(map[string]usageSourceStats, len(in.Sources))
		for source, stats := range in.Sources {
			out.Sources[source] = stats
		}
	}
	if len(in.sourceSessionCache) > 0 {
		out.sourceSessionCache = make(map[string]sourceSessionCacheCounters, len(in.sourceSessionCache))
		for source, counters := range in.sourceSessionCache {
			out.sourceSessionCache[source] = counters
		}
	}
	return out
}

func (s *sessionUsageStats) cacheTokenDelta(source string, u *provider.Usage, sessionHit, sessionMiss int) (hit, miss int) {
	if u != nil {
		hit = u.CacheHitTokens
		miss = u.CacheMissTokens
	}
	if source != event.UsageSourceExecutor && source != event.UsageSourcePlanner {
		return hit, miss
	}
	if sessionHit+sessionMiss <= 0 {
		return hit, miss
	}
	if s.sourceSessionCache == nil {
		s.sourceSessionCache = map[string]sourceSessionCacheCounters{}
	}
	prev, ok := s.sourceSessionCache[source]
	s.sourceSessionCache[source] = sourceSessionCacheCounters{Hit: sessionHit, Miss: sessionMiss}
	if !ok {
		return sessionHit, sessionMiss
	}
	if sessionHit < prev.Hit || sessionMiss < prev.Miss {
		if hit+miss > 0 {
			return hit, miss
		}
		return sessionHit, sessionMiss
	}
	return sessionHit - prev.Hit, sessionMiss - prev.Miss
}

type tabTelemetrySnapshot struct {
	Version   int               `json:"version"`
	ReadFiles []readFileRecord  `json:"readFiles"`
	Usage     sessionUsageStats `json:"usage"`
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func cloneServerViewMap(in map[string]ServerView) map[string]ServerView {
	out := make(map[string]ServerView, len(in))
	for name, view := range in {
		view.EnvKeys = append([]string(nil), view.EnvKeys...)
		view.HeaderKeys = append([]string(nil), view.HeaderKeys...)
		out[name] = view
	}
	return out
}

func (t *WorkspaceTab) currentSessionPath() string {
	if t == nil {
		return ""
	}
	if t.Ctrl != nil {
		if path := strings.TrimSpace(t.Ctrl.SessionPath()); path != "" {
			return path
		}
	}
	return strings.TrimSpace(t.SessionPath)
}

func (t *WorkspaceTab) hasActiveRuntimeWork() bool {
	if t == nil || t.Ctrl == nil {
		return false
	}
	status := t.Ctrl.RuntimeStatus()
	return status.Running || status.PendingPrompt || status.BackgroundJobs > 0
}

// sessionRuntimeKey is the comparison/map key for "same session" checks. It
// layers agent.CanonicalSessionPath on top of the desktop path normalization
// so the key matches the form held by session leases (lowercased on Windows).
// Comparing a lease's Path() against a raw tab path without this fold made
// every rebuild on Windows look like a foreign holder (self-lock, #5999).
// Keys are identities only — never use them as display or file paths.
func sessionRuntimeKey(path string) string {
	return agent.CanonicalSessionPath(canonicalTabSessionPath(path))
}

var sessionLeaseAcquireHookForTest func()

func (t *WorkspaceTab) ensureSessionLease(path string) error {
	if t == nil || t.ReadOnly {
		return nil
	}
	key := sessionRuntimeKey(path)
	if key == "" {
		return nil
	}
	t.sessionLeaseMu.Lock()
	if t.sessionLease != nil && sessionRuntimeKey(t.sessionLease.Path()) == key {
		t.sessionLeaseMu.Unlock()
		return nil
	}
	lease, err := agent.TryAcquireSessionLease(key)
	if err != nil {
		t.sessionLeaseMu.Unlock()
		return err
	}
	if hook := sessionLeaseAcquireHookForTest; hook != nil {
		hook()
	}
	old := t.sessionLease
	t.sessionLease = lease
	t.sessionLeaseMu.Unlock()
	if old != nil {
		old.Release()
	}
	return nil
}

func (t *WorkspaceTab) releaseSessionLease() {
	if t == nil {
		return
	}
	t.sessionLeaseMu.Lock()
	lease := t.sessionLease
	t.sessionLease = nil
	t.sessionLeaseMu.Unlock()
	if lease != nil {
		lease.Release()
	}
}

// takeSessionLease removes and returns the tab's current lease WITHOUT
// releasing it, so ownership can transfer to another holder. All access to
// t.sessionLease must go through sessionLeaseMu; never read or assign the
// field directly outside these helpers.
func (t *WorkspaceTab) takeSessionLease() *agent.SessionLease {
	if t == nil {
		return nil
	}
	t.sessionLeaseMu.Lock()
	lease := t.sessionLease
	t.sessionLease = nil
	t.sessionLeaseMu.Unlock()
	return lease
}

// adoptSessionLease installs lease as the tab's session lease, releasing any
// previously held lease unless it is the very same lease. A nil tab releases
// the lease immediately so ownership is never dropped on the floor.
func (t *WorkspaceTab) adoptSessionLease(lease *agent.SessionLease) {
	if t == nil {
		if lease != nil {
			lease.Release()
		}
		return
	}
	t.sessionLeaseMu.Lock()
	old := t.sessionLease
	t.sessionLease = lease
	t.sessionLeaseMu.Unlock()
	if old != nil && old != lease {
		old.Release()
	}
}

// sessionLeaseRuntimeKey reports the runtime key of the currently held lease,
// or "" when no lease is held. Safe to call while holding App.mu: the lock
// order App.mu → sessionLeaseMu has no reverse path (the lease helpers never
// touch App state while holding sessionLeaseMu).
func (t *WorkspaceTab) sessionLeaseRuntimeKey() string {
	if t == nil {
		return ""
	}
	t.sessionLeaseMu.Lock()
	defer t.sessionLeaseMu.Unlock()
	if t.sessionLease == nil {
		return ""
	}
	return sessionRuntimeKey(t.sessionLease.Path())
}

// releaseSessionLeaseForKey releases the tab's lease only when it is bound to
// key. Superseded builds clean up with this instead of releaseSessionLease:
// on a removed tab the keys match and the lease is released as before, but
// when a session rebind superseded the build, the rebind's replacement build
// holds a lease for a *different* session key (rebind early-returns on equal
// keys), and releasing that here would strip the live session's protection.
func (t *WorkspaceTab) releaseSessionLeaseForKey(key string) {
	if t == nil || key == "" {
		return
	}
	t.sessionLeaseMu.Lock()
	lease := t.sessionLease
	if lease == nil || sessionRuntimeKey(lease.Path()) != key {
		t.sessionLeaseMu.Unlock()
		return
	}
	t.sessionLease = nil
	t.sessionLeaseMu.Unlock()
	lease.Release()
}

func detachedRuntimeTabID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "detached_" + hex.EncodeToString(sum[:8])
}

func (a *App) ensureDetachedSessionsLocked() {
	if a.detachedSessions == nil {
		a.detachedSessions = map[string]*WorkspaceTab{}
	}
}

func (a *App) runtimeTabsLocked() []*WorkspaceTab {
	seen := map[*WorkspaceTab]bool{}
	out := make([]*WorkspaceTab, 0, len(a.tabs)+len(a.detachedSessions))
	for _, tab := range a.tabs {
		if tab != nil && !seen[tab] {
			seen[tab] = true
			out = append(out, tab)
		}
	}
	for _, tab := range a.detachedSessions {
		if tab != nil && !seen[tab] {
			seen[tab] = true
			out = append(out, tab)
		}
	}
	return out
}

func (a *App) tabByEventSinkIDLocked(tabID string) *WorkspaceTab {
	if tab := a.tabs[tabID]; tab != nil {
		return tab
	}
	for _, tab := range a.detachedSessions {
		if tab != nil && tab.ID == tabID {
			return tab
		}
	}
	return nil
}

func (a *App) detachSessionRuntime(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	a.mu.RLock()
	ctrl := tab.Ctrl
	fallbackPath := strings.TrimSpace(tab.SessionPath)
	sink := tab.sink
	a.mu.RUnlock()
	path := fallbackPath
	if ctrl != nil {
		if p := strings.TrimSpace(ctrl.SessionPath()); p != "" {
			path = p
		}
	}
	key := sessionRuntimeKey(path)
	if key == "" {
		return false
	}
	if sink != nil {
		sink.clearContext()
	}
	a.mu.Lock()
	a.ensureDetachedSessionsLocked()
	tab.SessionPath = canonicalTabSessionPath(path)
	a.detachedSessions[key] = tab
	a.mu.Unlock()
	return true
}

// cloneDetachedRuntimeTab copies a running tab's runtime state into a fresh
// detached tab. Callers must hold a.mu: the copied fields (Ctrl, Ready,
// ActivityStatus, disabledMCP, ...) are written under a.mu by bound methods
// and the event sink, and the disabledMCP map read would otherwise race those
// writers. The session lease is transferred separately by the caller through
// the sessionLeaseMu helpers. key is the runtime identity (map key / tab id
// hash); path is the real session path — keys are case-folded on Windows and
// must not leak into SessionPath, which is displayed and persisted.
func cloneDetachedRuntimeTab(tab *WorkspaceTab, key, path string) *WorkspaceTab {
	if tab == nil {
		return nil
	}
	tab.telemMu.Lock()
	readTelemetry := append([]readFileRecord(nil), tab.readTelemetry...)
	usageTelemetry := cloneSessionUsageStats(tab.usageTelemetry)
	telemetrySessionKey := tab.telemetrySessionKey
	tab.telemMu.Unlock()

	return &WorkspaceTab{
		ID:                  detachedRuntimeTabID(key),
		Scope:               tab.Scope,
		WorkspaceRoot:       tab.WorkspaceRoot,
		SharedHostKey:       tab.SharedHostKey,
		TopicID:             tab.TopicID,
		TopicTitle:          tab.TopicTitle,
		SessionPath:         canonicalTabSessionPath(path),
		Ctrl:                tab.Ctrl,
		Label:               tab.Label,
		Ready:               tab.Ready,
		StartupErr:          tab.StartupErr,
		sink:                tab.sink,
		ActivityStatus:      tab.ActivityStatus,
		readTelemetry:       readTelemetry,
		usageTelemetry:      usageTelemetry,
		telemetrySessionKey: telemetrySessionKey,
		model:               tab.model,
		effort:              cloneStringPtr(tab.effort),
		tokenMode:           tab.tokenMode,
		mode:                tab.mode,
		goal:                tab.goal,
		toolApprovalMode:    tab.toolApprovalMode,
		disabledMCP:         cloneServerViewMap(tab.disabledMCP),
		mcpOrder:            append([]string(nil), tab.mcpOrder...),
	}
}

func (a *App) detachRuntimeForReplacement(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	// One a.mu critical section covers the membership check, the field
	// snapshot, the lease/sink handover, and the re-publication:
	//   - the clone reads fields that bound methods and the event sink write
	//     under a.mu (ActivityStatus every event, disabledMCP is a map);
	//   - inserting the clone without re-checking a.tabs would resurrect a
	//     runtime that DeleteSession/TrashTopic/RemoveWorkspace already
	//     unlinked and closed (the "session resurrects" class, #4384);
	//   - publishing before the lease/sink handover would let a concurrent
	//     attachExistingSessionRuntime claim a half-initialized clone.
	// The lease transfer stays deadlock-safe here: neither side holds a lease
	// to release, so no lease I/O runs under a.mu.
	a.mu.Lock()
	if tab.removed || a.tabs[tab.ID] != tab {
		a.mu.Unlock()
		return false
	}
	sourcePath := tab.currentSessionPath()
	key := sessionRuntimeKey(sourcePath)
	if key == "" {
		a.mu.Unlock()
		return false
	}
	detached := cloneDetachedRuntimeTab(tab, key, sourcePath)
	if detached == nil {
		a.mu.Unlock()
		return false
	}
	// Transfer lease ownership through the locked helpers: a concurrent
	// ensureSessionLease (blank-session boot, recovery callback) must never
	// observe a torn pointer or have its freshly acquired lease clobbered.
	detached.adoptSessionLease(tab.takeSessionLease())
	if detached.sink != nil {
		detached.sink.setBinding(detached.ID, nil)
		// clearContext (locked nil + drain the queued emitter), not a bare
		// ctx=nil: the latter both data-races s.ctx and leaves already-queued
		// events to flush onto the rebound tab after this session is backgrounded
		// (#5352 — stale "AI 不断输出" on the now-visible session).
		detached.sink.clearContext()
	}
	a.ensureDetachedSessionsLocked()
	a.detachedSessions[key] = detached
	a.mu.Unlock()
	return true
}

// applyRuntimeTab moves source's runtime (controller, sink, lease, telemetry)
// onto target. path is the real session path for display/persistence; the
// case-folded runtime key must never be written into SessionPath.
func applyRuntimeTab(target, source *WorkspaceTab, path string, wailsCtx context.Context, app *App) {
	if target == nil || source == nil {
		return
	}
	source.telemMu.Lock()
	readTelemetry := append([]readFileRecord(nil), source.readTelemetry...)
	usageTelemetry := cloneSessionUsageStats(source.usageTelemetry)
	telemetrySessionKey := source.telemetrySessionKey
	source.telemMu.Unlock()

	if source.sink != nil {
		source.sink.setBinding(target.ID, app)
		source.sink.setContext(wailsCtx)
	}

	target.Ctrl = source.Ctrl
	target.sink = source.sink
	target.adoptSessionLease(source.takeSessionLease())
	target.SessionPath = canonicalTabSessionPath(path)
	target.SharedHostKey = source.SharedHostKey
	target.Label = source.Label
	target.Ready = true
	target.StartupErr = ""
	target.ActivityStatus = source.ActivityStatus
	target.model = source.model
	target.effort = cloneStringPtr(source.effort)
	target.tokenMode = source.tokenMode
	target.mode = source.mode
	target.goal = source.goal
	target.toolApprovalMode = source.toolApprovalMode
	target.disabledMCP = cloneServerViewMap(source.disabledMCP)
	target.mcpOrder = append([]string(nil), source.mcpOrder...)
	target.readTelemetry = readTelemetry
	target.usageTelemetry = usageTelemetry
	target.telemetrySessionKey = telemetrySessionKey
}

func (a *App) attachExistingSessionRuntime(tab *WorkspaceTab, path string, wailsCtx context.Context) bool {
	key := sessionRuntimeKey(path)
	if tab == nil || key == "" {
		return false
	}

	a.mu.Lock()
	if tab.removed || a.tabs[tab.ID] != tab {
		a.mu.Unlock()
		return false
	}
	detached := a.detachedSessions[key]
	if detached != nil {
		delete(a.detachedSessions, key)
		applyRuntimeTab(tab, detached, path, wailsCtx, a)
		if current := a.tabs[tab.ID]; current == tab {
			a.saveTabsLocked()
		}
		attachedCtrl := tab.Ctrl
		a.mu.Unlock()
		if attachedCtrl != nil {
			attachedCtrl.ReplayPendingPrompts()
		}
		return true
	}

	var source *WorkspaceTab
	for _, candidate := range a.tabs {
		if candidate == nil || candidate == tab {
			continue
		}
		if sessionRuntimeKey(candidate.currentSessionPath()) == key {
			source = candidate
			break
		}
	}
	if source == nil {
		a.mu.Unlock()
		return false
	}
	delete(a.tabs, source.ID)
	a.removeTabOrderLocked(source.ID)
	if a.activeTabID == source.ID {
		a.activeTabID = tab.ID
	}
	applyRuntimeTab(tab, source, path, wailsCtx, a)
	a.saveTabsLocked()
	attachedCtrl := tab.Ctrl
	a.mu.Unlock()

	if attachedCtrl != nil {
		attachedCtrl.ReplayPendingPrompts()
	}
	return true
}

func (t *WorkspaceTab) recordReadFile(rec readFileRecord) {
	t.telemMu.Lock()
	t.readTelemetry = append(t.readTelemetry, rec)
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) recordTurnStarted(now int64) {
	t.telemMu.Lock()
	if t.usageTelemetry.activeTurnStartedAt == 0 {
		t.usageTelemetry.activeTurnStartedAt = now
	}
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) recordTurnDone(now int64) {
	t.telemMu.Lock()
	if started := t.usageTelemetry.activeTurnStartedAt; started > 0 && now >= started {
		t.usageTelemetry.ElapsedMs += now - started
		t.usageTelemetry.activeTurnStartedAt = 0
	}
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) recordUsage(e event.Event) {
	if e.Usage == nil {
		return
	}
	u := e.Usage
	source := strings.TrimSpace(e.UsageSource)
	if source == "" {
		source = event.UsageSourceExecutor
	}
	t.telemMu.Lock()
	t.usageTelemetry.PromptTokens += u.PromptTokens
	t.usageTelemetry.CompletionTokens += u.CompletionTokens
	t.usageTelemetry.TotalTokens += u.TotalTokens
	t.usageTelemetry.ReasoningTokens += u.ReasoningTokens
	cacheHitTokens, cacheMissTokens := t.usageTelemetry.cacheTokenDelta(source, u, e.SessionHit, e.SessionMiss)
	t.usageTelemetry.CacheHitTokens += cacheHitTokens
	t.usageTelemetry.CacheMissTokens += cacheMissTokens
	t.usageTelemetry.RequestCount++
	if t.usageTelemetry.Sources == nil {
		t.usageTelemetry.Sources = map[string]usageSourceStats{}
	}
	src := t.usageTelemetry.Sources[source]
	src.PromptTokens += u.PromptTokens
	src.CompletionTokens += u.CompletionTokens
	src.TotalTokens += u.TotalTokens
	src.ReasoningTokens += u.ReasoningTokens
	src.CacheHitTokens += cacheHitTokens
	src.CacheMissTokens += cacheMissTokens
	src.RequestCount++
	if e.Pricing != nil {
		cost := e.Pricing.Cost(u)
		t.usageTelemetry.SessionCost += cost
		t.usageTelemetry.SessionCostUsd = t.usageTelemetry.SessionCost
		t.usageTelemetry.SessionCurrency = e.Pricing.Symbol()
		src.SessionCost += cost
		src.SessionCostUsd = src.SessionCost
		src.SessionCurrency = e.Pricing.Symbol()
	}
	t.usageTelemetry.Sources[source] = src
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) telemetrySnapshot() tabTelemetrySnapshot {
	t.telemMu.Lock()
	defer t.telemMu.Unlock()
	records := make([]readFileRecord, len(t.readTelemetry))
	copy(records, t.readTelemetry)
	usage := t.usageTelemetry
	if started := usage.activeTurnStartedAt; started > 0 {
		now := time.Now().UnixMilli()
		if now >= started {
			usage.ElapsedMs += now - started
		}
	}
	if len(t.usageTelemetry.Sources) > 0 {
		usage.Sources = make(map[string]usageSourceStats, len(t.usageTelemetry.Sources))
		for source, stats := range t.usageTelemetry.Sources {
			usage.Sources[source] = stats
		}
	}
	usage.activeTurnStartedAt = 0
	usage.sourceSessionCache = nil
	return tabTelemetrySnapshot{Version: 2, ReadFiles: records, Usage: usage}
}

func (t *WorkspaceTab) resetTelemetry(sessionPath string) {
	t.telemMu.Lock()
	t.readTelemetry = nil
	t.usageTelemetry = sessionUsageStats{}
	t.telemetrySessionKey = sessionRuntimeKey(sessionPath)
	t.telemMu.Unlock()
}

// syncTelemetryToSession keys the in-memory telemetry to the runtime's current
// session. When the runtime rotated to a different session underneath the tab
// (typed /new routes through Controller.Submit and never reaches App.NewSession),
// the previous session's totals must not bleed into the new one: swap in the
// new session's persisted sidecar, or start from zero when none exists. The
// sidecar is rewritten on every recorded event, so a reload never loses more
// than the sub-second in-memory delta of an in-flight record.
func (t *WorkspaceTab) syncTelemetryToSession(sessionPath string) {
	key := sessionRuntimeKey(sessionPath)
	if key == "" {
		return
	}
	t.telemMu.Lock()
	same := t.telemetrySessionKey == key
	t.telemMu.Unlock()
	if same {
		return
	}
	// File I/O stays outside telemMu; re-check the key after reacquiring in
	// case a concurrent sync or reset re-keyed the tab first.
	snapshot := loadTelemetry(sessionPath + ".telemetry.json")
	t.telemMu.Lock()
	if t.telemetrySessionKey != key {
		t.readTelemetry = snapshot.ReadFiles
		t.usageTelemetry = snapshot.Usage
		t.telemetrySessionKey = key
	}
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) resetPlannerDisplayTurn() {
	t.plannerDisplayMu.Lock()
	if len(t.plannerDisplay) == 0 {
		t.plannerDisplayTools = nil
	}
	t.plannerDisplayMu.Unlock()
}

func (t *WorkspaceTab) recordPlannerDisplayEvent(e event.Event) {
	if strings.TrimSpace(e.Source) != event.UsageSourcePlanner {
		return
	}
	t.plannerDisplayMu.Lock()
	defer t.plannerDisplayMu.Unlock()
	switch e.Kind {
	case event.Phase:
		if strings.TrimSpace(e.Text) != "" {
			t.plannerDisplay = append(t.plannerDisplay, HistoryMessage{Role: "phase", Content: e.Text})
		}
	case event.Reasoning:
		if e.Text != "" {
			hm := t.ensurePlannerAssistantLocked()
			hm.Reasoning += e.Text
		}
	case event.Text:
		if e.Text != "" {
			hm := t.ensurePlannerAssistantLocked()
			hm.Content += e.Text
		}
	case event.Message:
		if e.Text != "" || e.Reasoning != "" || len(e.MemoryCitations) > 0 {
			hm := t.ensurePlannerAssistantLocked()
			if e.Text != "" {
				hm.Content = e.Text
			}
			if e.Reasoning != "" {
				hm.Reasoning = e.Reasoning
			}
			if len(e.MemoryCitations) > 0 {
				hm.MemoryCitations = append([]provider.MemoryCitation(nil), e.MemoryCitations...)
			}
		}
	case event.ToolDispatch:
		if e.Tool.Partial || strings.TrimSpace(e.Tool.Name) == "" {
			return
		}
		hm := t.ensurePlannerAssistantForToolLocked()
		call := HistoryToolCall{
			ID:        e.Tool.ID,
			Name:      e.Tool.Name,
			Arguments: e.Tool.Args,
			Subject:   historyToolSubject(e.Tool.Name, e.Tool.Args),
			Summary:   historyToolSummary(e.Tool.Name, e.Tool.Args, ""),
			Diff:      e.Tool.Diff,
			Added:     e.Tool.Added,
			Removed:   e.Tool.Removed,
		}
		replaced := false
		if call.ID != "" {
			for i := range hm.ToolCalls {
				if hm.ToolCalls[i].ID == call.ID {
					hm.ToolCalls[i] = call
					replaced = true
					break
				}
			}
			if t.plannerDisplayTools == nil {
				t.plannerDisplayTools = map[string]string{}
			}
			t.plannerDisplayTools[call.ID] = call.Name
		}
		if !replaced {
			hm.ToolCalls = append(hm.ToolCalls, call)
		}
	case event.ToolResult:
		callID := strings.TrimSpace(e.Tool.ID)
		content := firstNonEmpty(e.Tool.Output, e.Tool.Err)
		display, errPreview := plannerToolResultDisplay(content, e.Tool.Err != "")
		if callID != "" {
			updateHistoryToolCallSummary(t.plannerDisplay, callID, content)
		}
		toolName := e.Tool.Name
		if toolName == "" && t.plannerDisplayTools != nil {
			toolName = t.plannerDisplayTools[callID]
		}
		t.plannerDisplay = append(t.plannerDisplay, HistoryMessage{
			Role:            "tool",
			ToolCallID:      callID,
			ToolName:        toolName,
			Content:         display,
			ToolResultError: errPreview,
		})
	case event.Notice:
		if strings.TrimSpace(e.Text) != "" {
			level := "info"
			if e.Level == event.LevelWarn {
				level = "warn"
			}
			t.plannerDisplay = append(t.plannerDisplay, HistoryMessage{Role: "notice", Level: level, Content: e.Text})
		}
	}
}

func (t *WorkspaceTab) ensurePlannerAssistantLocked() *HistoryMessage {
	if n := len(t.plannerDisplay); n > 0 && t.plannerDisplay[n-1].Role == "assistant" {
		return &t.plannerDisplay[n-1]
	}
	t.plannerDisplay = append(t.plannerDisplay, HistoryMessage{Role: "assistant"})
	return &t.plannerDisplay[len(t.plannerDisplay)-1]
}

func (t *WorkspaceTab) ensurePlannerAssistantForToolLocked() *HistoryMessage {
	if n := len(t.plannerDisplay); n > 0 && t.plannerDisplay[n-1].Role == "assistant" && strings.TrimSpace(t.plannerDisplay[n-1].Content) == "" {
		return &t.plannerDisplay[n-1]
	}
	t.plannerDisplay = append(t.plannerDisplay, HistoryMessage{Role: "assistant"})
	return &t.plannerDisplay[len(t.plannerDisplay)-1]
}

func plannerToolResultDisplay(content string, failed bool) (display, errPreview string) {
	if strings.TrimSpace(content) == "" {
		return "", ""
	}
	if failed || historyToolResultFailed(content) {
		display = clipHistoryToolPreview(strings.TrimSpace(content))
		return display, display
	}
	return "", ""
}

func (t *WorkspaceTab) takePlannerDisplayTurn() []HistoryMessage {
	t.plannerDisplayMu.Lock()
	defer t.plannerDisplayMu.Unlock()
	out := cloneHistoryMessages(t.plannerDisplay)
	t.plannerDisplay = nil
	t.plannerDisplayTools = nil
	return out
}

// tabEventSink wraps a parent event.Sink and prepends a tabId to every wire
// event so the frontend can route it to the correct tab's reducer.
//
// tabID and app are rebound while the controller keeps emitting when a running
// session is detached to the background or reattached to another tab, so they
// live under mu like ctx does (a bare field write would data-race Emit). Read
// them via binding(), write via setBinding().
type tabEventSink struct {
	tabID         string
	app           *App
	mu            sync.RWMutex
	ctx           context.Context
	runtimeEvents asyncRuntimeEmitter
	botSink       event.Sink // optional: when set, events are also forwarded here
}

type closeableEventSink interface {
	Close()
}

// binding snapshots the sink's current tab routing under the sink lock.
func (s *tabEventSink) binding() (string, *App) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tabID, s.app
}

// setBinding reroutes the sink to another tab. A nil app keeps the current
// App pointer (detach/close paths only change the tab id).
func (s *tabEventSink) setBinding(tabID string, app *App) {
	s.mu.Lock()
	s.tabID = tabID
	if app != nil {
		s.app = app
	}
	s.mu.Unlock()
}

func (s *tabEventSink) Emit(e event.Event) {
	tabID, app := s.binding()
	if app != nil {
		switch e.Kind {
		case event.TurnStarted:
			s.resetPlannerDisplayTurn()
			s.recordTurnStarted()
		case event.Usage:
			s.recordUsageTelemetry(e)
		case event.TurnDone:
			s.recordTurnDone()
		}
		if m := app.metrics.Load(); m != nil {
			m.observe(e)
			if e.Kind == event.TurnDone {
				m.persist()
			}
		}
		if e.Kind == event.TurnDone {
			s.flushPlannerDisplay()
		}
	}
	s.emitRuntimeEvent(eventChannel, toWireTab(e, tabID))
	if app != nil {
		if status, update := topicActivityStatusFromEvent(e); update {
			changed := app.setTabActivityStatus(tabID, status)
			if changed || isBackgroundJobLifecycleNotice(e) {
				app.emitProjectTreeChanged()
			}
		}
	}
	// Record read_file successes in the tab's telemetry.
	if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
		s.recordReadTelemetry(e)
	}
	if app != nil {
		s.recordPlannerDisplay(e)
	}
	// Persist after each turn so a force-kill loses at most the in-flight prompt.
	if e.Kind == event.TurnDone && app != nil {
		app.scheduleTabSnapshot(tabID)
	}
	// Forward event to bot channels when a bot forwarder is attached.
	// Read the sink under the read lock so SetBotSink can safely swap it
	// from another goroutine.
	s.mu.RLock()
	bs := s.botSink
	s.mu.RUnlock()
	if bs != nil {
		bs.Emit(e)
		// Detach the forwarder after TurnDone so subsequent turns on the
		// same tab do not keep pushing to bot channels.
		if e.Kind == event.TurnDone {
			s.SetBotSink(nil)
		}
	}
}

// SetBotSink atomically sets or clears the bot event forwarder on this sink.
// It is safe to call concurrently with Emit.
func (s *tabEventSink) SetBotSink(sink event.Sink) {
	s.mu.Lock()
	old := s.botSink
	s.botSink = sink
	s.mu.Unlock()
	if old != nil && old != sink {
		if closer, ok := old.(closeableEventSink); ok {
			closer.Close()
		}
	}
}

func (s *tabEventSink) setContext(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
}

func (s *tabEventSink) clearContext() {
	s.mu.Lock()
	s.ctx = nil
	s.mu.Unlock()
	s.runtimeEvents.Clear()
}

func (s *tabEventSink) context() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctx
}

func (s *tabEventSink) emitRuntimeEvent(name string, payload ...interface{}) {
	if s == nil {
		return
	}
	ctx := s.context()
	if ctx == nil {
		return
	}
	s.runtimeEvents.Emit(ctx, name, payload...)
}

type runtimeEventEmitFunc func(context.Context, string, ...interface{})

type runtimeEventEnvelope struct {
	ctx     context.Context
	name    string
	payload []interface{}
}

// asyncRuntimeEmitter decouples Wails' runtime event bridge from agent
// emission. runtime.EventsEmit can block when the single webview event channel
// backs up; callers enqueue in-order work and return without holding the
// agent's event.Sync lock.
type asyncRuntimeEmitter struct {
	mu      sync.Mutex
	emit    runtimeEventEmitFunc
	queue   []runtimeEventEnvelope
	head    int
	running bool
}

func (e *asyncRuntimeEmitter) Emit(ctx context.Context, name string, payload ...interface{}) {
	if ctx == nil {
		return
	}
	item := runtimeEventEnvelope{
		ctx:     ctx,
		name:    name,
		payload: append([]interface{}(nil), payload...),
	}
	e.mu.Lock()
	e.queue = append(e.queue, item)
	if !e.running {
		e.running = true
		go e.run()
	}
	e.mu.Unlock()
}

func (e *asyncRuntimeEmitter) Clear() {
	e.mu.Lock()
	clear(e.queue)
	e.queue = nil
	e.head = 0
	e.mu.Unlock()
}

func (e *asyncRuntimeEmitter) run() {
	for {
		e.mu.Lock()
		if e.head >= len(e.queue) {
			clear(e.queue)
			e.queue = nil
			e.head = 0
			e.running = false
			e.mu.Unlock()
			return
		}
		item := e.queue[e.head]
		var zero runtimeEventEnvelope
		e.queue[e.head] = zero
		e.head++
		if e.head > 64 && e.head*2 >= len(e.queue) {
			e.queue = append([]runtimeEventEnvelope(nil), e.queue[e.head:]...)
			e.head = 0
		}
		emit := e.emit
		if emit == nil {
			emit = runtime.EventsEmit
		}
		e.mu.Unlock()

		emit(item.ctx, item.name, item.payload...)
	}
}

func topicActivityStatusFromEvent(e event.Event) (string, bool) {
	switch e.Kind {
	case event.TurnStarted, event.Reasoning, event.ToolDispatch, event.ToolProgress, event.ToolResult, event.CompactionStarted, event.CompactionDone, event.Retrying:
		return topicStatusThinking, true
	case event.Text, event.Message:
		return topicStatusStreaming, true
	case event.ApprovalRequest, event.AskRequest:
		return topicStatusWaitingConfirmation, true
	case event.TurnDone:
		if e.Err != nil {
			return topicStatusError, true
		}
		return "", true
	case event.Notice:
		if isBackgroundJobLifecycleNotice(e) {
			return "", true
		}
		return "", false
	default:
		return "", false
	}
}

func isBackgroundJobLifecycleNotice(e event.Event) bool {
	if e.Kind != event.Notice {
		return false
	}
	text := strings.TrimSpace(e.Text)
	return strings.HasPrefix(text, "background ") &&
		(strings.Contains(text, " started: ") ||
			strings.Contains(text, " finished: ") ||
			strings.Contains(text, " failed: ") ||
			strings.Contains(text, " killed: "))
}

func (a *App) emitReady(ctx context.Context, tabID ...string) {
	a.mu.RLock()
	hook := a.readyHook
	a.mu.RUnlock()
	if hook != nil {
		hook()
		return
	}
	if ctx != nil {
		if len(tabID) > 0 && strings.TrimSpace(tabID[0]) != "" {
			runtime.EventsEmit(ctx, "agent:ready", strings.TrimSpace(tabID[0]))
			return
		}
		runtime.EventsEmit(ctx, "agent:ready")
	}
}

func (s *tabEventSink) recordReadTelemetry(e event.Event) {
	tabID, app := s.binding()
	if app == nil {
		return
	}
	app.mu.RLock()
	tab := app.tabByEventSinkIDLocked(tabID)
	var ctrl control.SessionAPI
	if tab != nil {
		ctrl = tab.Ctrl
	}
	app.mu.RUnlock()
	if tab == nil {
		return
	}
	turn := 0
	if ctrl != nil {
		turn = ctrl.Turn()
	}

	// Parse read_file args: {"path": "...", "offset": N, "limit": N}
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	path := e.Tool.Args
	offset := 0
	limit := 0
	if err := json.Unmarshal([]byte(e.Tool.Args), &args); err == nil && args.Path != "" {
		path = args.Path
		offset = args.Offset
		limit = args.Limit
	}

	truncated := e.Tool.Truncated || strings.Contains(e.Tool.Output, "truncated") ||
		strings.Contains(e.Tool.Output, "File truncated")

	sp := ""
	if ctrl != nil {
		sp = ctrl.SessionPath()
	}
	if sp != "" {
		tab.syncTelemetryToSession(sp)
	}
	tab.recordReadFile(readFileRecord{
		Path:      path,
		Turn:      turn,
		Time:      time.Now().UnixMilli(),
		Offset:    offset,
		Limit:     limit,
		Truncated: truncated,
	})
	if sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) recordTurnStarted() {
	tab, sp := s.telemetryTab()
	if tab == nil {
		return
	}
	if sp != "" {
		tab.syncTelemetryToSession(sp)
	}
	tab.recordTurnStarted(time.Now().UnixMilli())
	if sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) recordTurnDone() {
	tab, sp := s.telemetryTab()
	if tab == nil {
		return
	}
	if sp != "" {
		tab.syncTelemetryToSession(sp)
	}
	tab.recordTurnDone(time.Now().UnixMilli())
	if sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) recordUsageTelemetry(e event.Event) {
	tab, sp := s.telemetryTab()
	if tab == nil {
		return
	}
	if sp != "" {
		tab.syncTelemetryToSession(sp)
	}
	tab.recordUsage(e)
	if sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) resetPlannerDisplayTurn() {
	tab, _ := s.eventTabAndController()
	if tab != nil {
		tab.resetPlannerDisplayTurn()
	}
}

func (s *tabEventSink) recordPlannerDisplay(e event.Event) {
	tab, _ := s.eventTabAndController()
	if tab != nil {
		tab.recordPlannerDisplayEvent(e)
	}
}

func (s *tabEventSink) flushPlannerDisplay() {
	tab, ctrl := s.eventTabAndController()
	if tab == nil || ctrl == nil {
		return
	}
	messages := tab.takePlannerDisplayTurn()
	if len(messages) == 0 {
		return
	}
	sessionPath := ctrl.SessionPath()
	if sessionPath == "" {
		return
	}
	userContent := lastUserMessageContent(ctrl.History())
	if strings.TrimSpace(userContent) == "" {
		return
	}
	_ = recordSessionPlannerDisplay(controllerSessionDir(ctrl), sessionPath, userContent, messages)
}

func (s *tabEventSink) eventTabAndController() (*WorkspaceTab, control.SessionAPI) {
	tabID, app := s.binding()
	if app == nil {
		return nil, nil
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	tab := app.tabByEventSinkIDLocked(tabID)
	if tab == nil {
		return nil, nil
	}
	return tab, tab.Ctrl
}

func lastUserMessageContent(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func (s *tabEventSink) telemetryTab() (*WorkspaceTab, string) {
	tabID, app := s.binding()
	if app == nil {
		return nil, ""
	}
	app.mu.RLock()
	tab := app.tabByEventSinkIDLocked(tabID)
	var ctrl control.SessionAPI
	if tab != nil {
		ctrl = tab.Ctrl
	}
	app.mu.RUnlock()
	if tab == nil {
		return nil, ""
	}
	if ctrl == nil {
		return tab, ""
	}
	sp := ctrl.SessionPath()
	if sp == "" {
		return tab, ""
	}
	return tab, sp
}

// --- wire event with tab ----------------------------------------------------

func toWireTab(e event.Event, tabID string) wireEventTab {
	w := eventwire.ToWire(e)
	return wireEventTab{
		Event:             w,
		TabID:             tabID,
		SessionHitTokens:  e.SessionHit,
		SessionMissTokens: e.SessionMiss,
		SessionCost:       0, // filled by frontend accumulator per tab
		SessionCurrency:   "",
		SessionCostUsd:    0, // deprecated compatibility alias
	}
}

// wireEventTab extends the shared event wire with tab routing info. The frontend reducer
// uses tabId to dispatch to the correct per-tab state.
type wireEventTab struct {
	eventwire.Event
	TabID string `json:"tabId"`
	// Session-cumulative tokens per tab.
	SessionHitTokens  int `json:"sessionHitTokens,omitempty"`
	SessionMissTokens int `json:"sessionMissTokens,omitempty"`
	// SessionCost is filled by the frontend's per-tab accumulator.
	SessionCost     float64 `json:"sessionCost,omitempty"`
	SessionCurrency string  `json:"sessionCurrency,omitempty"`
	// SessionCostUsd is a deprecated compatibility alias. It mirrors
	// SessionCost and does not imply USD.
	SessionCostUsd float64 `json:"sessionCostUsd,omitempty"`
}

// --- Tab management on App --------------------------------------------------

// TabMeta is the frontend-facing shape of one tab.
type TabMeta struct {
	ID                string                   `json:"id"`
	Scope             string                   `json:"scope"`
	WorkspaceRoot     string                   `json:"workspaceRoot"`
	WorkspaceName     string                   `json:"workspaceName"`
	WorkspacePath     string                   `json:"workspacePath,omitempty"`
	GitBranch         string                   `json:"gitBranch,omitempty"`
	TopicID           string                   `json:"topicId"`
	TopicTitle        string                   `json:"topicTitle"`
	SessionPath       string                   `json:"sessionPath,omitempty"`
	ReadOnly          bool                     `json:"readOnly,omitempty"`
	ProjectColor      string                   `json:"projectColor,omitempty"`
	Label             string                   `json:"label"`
	Ready             bool                     `json:"ready"`
	Running           bool                     `json:"running"`
	PendingPrompt     bool                     `json:"pendingPrompt,omitempty"`
	BackgroundJobs    int                      `json:"backgroundJobs,omitempty"`
	CancelRequested   bool                     `json:"cancelRequested,omitempty"`
	Cancellable       bool                     `json:"cancellable"`
	Mode              string                   `json:"mode"`
	CollaborationMode string                   `json:"collaborationMode"`
	ToolApprovalMode  string                   `json:"toolApprovalMode"`
	TokenMode         string                   `json:"tokenMode"`
	Goal              string                   `json:"goal,omitempty"`
	GoalStatus        string                   `json:"goalStatus,omitempty"`
	AutoResearch      *AutoResearchCompactView `json:"autoResearch,omitempty"`
	Recovered         bool                     `json:"recovered,omitempty"`
	RecoveryReason    string                   `json:"recoveryReason,omitempty"`
	RecoveryDigest    string                   `json:"recoveryDigest,omitempty"`
	RecoveryParentID  string                   `json:"recoveryParentId,omitempty"`
	StartupErr        string                   `json:"startupErr,omitempty"`
	Active            bool                     `json:"active"`
	Cwd               string                   `json:"cwd"`
}

func enrichTabMeta(meta TabMeta) TabMeta {
	if meta.Active {
		meta.GitBranch = workspaceGitBranchForMeta(meta.WorkspaceRoot)
	}
	return meta
}

func enrichTabMetas(metas []TabMeta) []TabMeta {
	for i := range metas {
		if metas[i].Active {
			metas[i].GitBranch = workspaceGitBranchForMeta(metas[i].WorkspaceRoot)
		}
	}
	return metas
}

func (a *App) tabMeta(tab *WorkspaceTab, active bool) TabMeta {
	m := TabMeta{
		ID:                tab.ID,
		Scope:             tab.Scope,
		WorkspaceRoot:     tab.WorkspaceRoot,
		WorkspaceName:     workspaceName(tab.WorkspaceRoot),
		WorkspacePath:     tab.WorkspaceRoot,
		TopicID:           tab.TopicID,
		TopicTitle:        tab.TopicTitle,
		SessionPath:       tab.currentSessionPath(),
		ReadOnly:          tab.ReadOnly,
		Label:             tab.Label,
		Ready:             tab.Ready,
		Mode:              currentTabMode(tab),
		CollaborationMode: currentTabCollaborationMode(tab),
		ToolApprovalMode:  currentTabToolApprovalMode(tab),
		TokenMode:         currentTabTokenMode(tab),
		Goal:              currentTabGoal(tab),
		GoalStatus:        currentTabGoalStatus(tab),
		AutoResearch:      compactAutoResearch(tab),
		StartupErr:        tab.StartupErr,
		Active:            active,
		Cwd:               tab.WorkspaceRoot,
	}
	switch tab.Scope {
	case "global":
		m.ProjectColor = globalProjectColor()
		m.WorkspaceName = globalProjectTitle()
	case "project":
		m.ProjectColor = projectColor(tab.WorkspaceRoot)
	}
	if tab.Ctrl != nil {
		status := tab.Ctrl.RuntimeStatus()
		m.Running = status.Running || status.PendingPrompt || status.BackgroundJobs > 0
		m.PendingPrompt = status.PendingPrompt
		m.BackgroundJobs = status.BackgroundJobs
		m.CancelRequested = status.CancelRequested
		m.Cancellable = status.Cancellable
	}
	if meta, ok, err := agent.LoadBranchMeta(tab.currentSessionPath()); err == nil && ok && meta.Recovered {
		m.Recovered = true
		m.RecoveryReason = meta.RecoveryReason
		m.RecoveryDigest = meta.RecoveryDigest
		m.RecoveryParentID = string(meta.ParentID)
	}
	return m
}

// ListTabs returns every open view container's metadata for the frontend chrome and sidebar.
func (a *App) ListTabs() []TabMeta {
	a.mu.RLock()
	out := make([]TabMeta, 0, len(a.tabs))
	ordered, needsRepair := a.orderedTabIDsSnapshotLocked()
	for _, id := range ordered {
		if tab := a.tabs[id]; tab != nil {
			out = append(out, a.tabMeta(tab, tab.ID == a.activeTabID))
		}
	}
	a.mu.RUnlock()
	if !needsRepair {
		return enrichTabMetas(out)
	}

	a.mu.Lock()
	out = make([]TabMeta, 0, len(a.tabs))
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			out = append(out, a.tabMeta(tab, tab.ID == a.activeTabID))
		}
	}
	a.mu.Unlock()
	return enrichTabMetas(out)
}

// OpenProjectTab builds a controller scoped to workspaceRoot and opens the
// session selected by the given topic. Topic selection resolves to a concrete
// session path first; the visible tab is then attached to that session runtime.
func (a *App) OpenProjectTab(workspaceRoot, topicID string) (TabMeta, error) {
	if workspaceRoot == "" {
		return TabMeta{}, fmt.Errorf("workspaceRoot is required")
	}
	if abs, err := filepath.Abs(workspaceRoot); err == nil {
		workspaceRoot = abs
	}
	saveWorkspace(workspaceRoot)
	_ = addProject(workspaceRoot, "")

	sessionPath, _ := a.findTopicSessionForTarget("project", workspaceRoot, topicID)
	return a.openTopicTab("project", workspaceRoot, topicID, sessionPath)
}

func (a *App) openTopicTab(scope, workspaceRoot, topicID, sessionPath string) (TabMeta, error) {
	return a.openTopicTabWithActivation(scope, workspaceRoot, topicID, sessionPath, true)
}

func (a *App) openProjectTabInactive(workspaceRoot, topicID string) (TabMeta, error) {
	if workspaceRoot == "" {
		return TabMeta{}, fmt.Errorf("workspaceRoot is required")
	}
	if abs, err := filepath.Abs(workspaceRoot); err == nil {
		workspaceRoot = abs
	}
	_ = addProject(workspaceRoot, "")

	sessionPath, _ := a.findTopicSessionForTarget("project", workspaceRoot, topicID)
	return a.openTopicTabWithActivation("project", workspaceRoot, topicID, sessionPath, false)
}

func (a *App) openGlobalTabInactive(topicID string) (TabMeta, error) {
	globalRoot := globalWorkspaceRoot()
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		return TabMeta{}, fmt.Errorf("create global workspace: %w", err)
	}

	sessionPath, _ := a.findTopicSessionForTarget("global", "", topicID)
	return a.openTopicTabWithActivation("global", "", topicID, sessionPath, false)
}

func (a *App) openTopicTabWithActivation(scope, workspaceRoot, topicID, sessionPath string, activate bool) (TabMeta, error) {
	actualRoot := workspaceRoot
	if scope == "global" {
		actualRoot = globalWorkspaceRoot()
	}
	targetKey := sessionRuntimeKey(sessionPath)

	a.mu.Lock()
	if targetKey != "" {
		for _, tab := range a.tabs {
			if tab == nil {
				continue
			}
			if sessionRuntimeKey(tab.currentSessionPath()) == targetKey {
				if activate {
					a.activeTabID = tab.ID
				}
				meta := a.tabMeta(tab, tab.ID == a.activeTabID)
				a.saveTabsLocked()
				a.mu.Unlock()
				return enrichTabMeta(meta), nil
			}
		}
	}

	for _, tab := range a.tabs {
		if tabMatchesTopicTarget(tab, scope, workspaceRoot, topicID) {
			if activate {
				a.activeTabID = tab.ID
			}
			sameSession := targetKey == "" || sessionRuntimeKey(tab.currentSessionPath()) == targetKey
			meta := a.tabMeta(tab, tab.ID == a.activeTabID)
			a.saveTabsLocked()
			a.mu.Unlock()
			if sameSession {
				return enrichTabMeta(meta), nil
			}
			if err := a.rebindTabToSessionPath(tab, sessionPath); err != nil {
				return TabMeta{}, err
			}
			a.mu.RLock()
			meta = a.tabMeta(tab, tab.ID == a.activeTabID)
			a.mu.RUnlock()
			return enrichTabMeta(meta), nil
		}
	}

	tabID := a.newUniqueTabIDLocked()
	topicTitle := topicTitleForTab(scope, workspaceRoot, topicID)
	if t, source, ok := topicTitleFallbackForOpen(workspaceRoot, topicID, sessionPath); ok {
		topicTitle = t
		_ = setTopicTitleWithSource(workspaceRoot, topicID, t, source)
	}

	if sessionPath == "" {
		var err error
		sessionPath, err = createEmptySessionFile(desktopSessionDir(actualRoot), "")
		if err != nil {
			a.mu.Unlock()
			return TabMeta{}, err
		}
	}
	profile := loadTabSessionProfile(sessionPath)
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         scope,
		WorkspaceRoot: actualRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   sessionPath,
		disabledMCP:   map[string]ServerView{},
	}
	applyTabSessionProfile(tab, profile)
	tab.sink = &tabEventSink{tabID: tabID, app: a}

	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	if activate {
		a.activeTabID = tabID
	}
	a.saveTabsLocked()
	meta := a.tabMeta(tab, tab.ID == a.activeTabID)
	a.mu.Unlock()

	a.startTabControllerBuild(tab)
	if scope == "project" {
		a.emitProjectTreeChanged()
	}
	return enrichTabMeta(meta), nil
}

// OpenGlobalTab opens a new global-scope tab (no project root). The global
// workspace root is the reasonix user config directory.
func (a *App) OpenGlobalTab(topicID string) (TabMeta, error) {
	globalRoot := globalWorkspaceRoot()
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		return TabMeta{}, fmt.Errorf("create global workspace: %w", err)
	}

	sessionPath, _ := a.findTopicSessionForTarget("global", "", topicID)
	return a.openTopicTab("global", "", topicID, sessionPath)
}

// OpenTopicSession opens a concrete saved session from the sidebar. Unlike
// OpenProjectTab/OpenGlobalTab, it does not resolve the topic to the latest
// session first; sessionPath is the runtime identity being selected.
func (a *App) OpenTopicSession(scope, workspaceRoot, topicID, sessionPath string) (TabMeta, error) {
	scope = strings.TrimSpace(scope)
	if scope != "project" {
		scope = "global"
		workspaceRoot = ""
	}
	if scope == "project" {
		workspaceRoot = normalizeProjectRoot(workspaceRoot)
		if workspaceRoot == "" {
			return TabMeta{}, fmt.Errorf("workspaceRoot is required")
		}
		saveWorkspace(workspaceRoot)
		_ = addProject(workspaceRoot, "")
	}
	_, validPath, err := a.sessionDirForPath(sessionPath)
	if err != nil {
		return TabMeta{}, err
	}
	return a.openTopicTab(scope, workspaceRoot, topicID, validPath)
}

// ActivateTopic opens a topic into the single visible conversation surface used
// by layouts without a tab strip. It delegates the actual open/reuse behavior to
// the classic tab path, then prunes every non-active visible tab so historical
// clicks do not accumulate hidden startup work.
func (a *App) ActivateTopic(scope, workspaceRoot, topicID, sessionPath string) (TabMeta, error) {
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()

	var meta TabMeta
	var err error
	if strings.TrimSpace(sessionPath) != "" {
		meta, err = a.OpenTopicSession(scope, workspaceRoot, topicID, sessionPath)
	} else if strings.TrimSpace(scope) == "project" {
		meta, err = a.OpenProjectTab(workspaceRoot, topicID)
	} else {
		meta, err = a.OpenGlobalTab(topicID)
	}
	if err != nil {
		return TabMeta{}, err
	}
	return a.keepOnlyVisibleTab(meta.ID)
}

// EnsureBlankSurface mirrors EnsureBlankTab for no-tab-strip layouts: after
// creating or reusing a blank session, it removes other visible tabs while
// preserving running runtimes as detached background sessions.
func (a *App) EnsureBlankSurface(scope, workspaceRoot string) (TabMeta, error) {
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()

	meta, err := a.EnsureBlankTab(scope, workspaceRoot)
	if err != nil {
		return TabMeta{}, err
	}
	return a.keepOnlyVisibleTab(meta.ID)
}

func tabMatchesTopicTarget(tab *WorkspaceTab, scope, workspaceRoot, topicID string) bool {
	if tab == nil || tab.Scope != scope || tab.TopicID != topicID {
		return false
	}
	if scope == "global" {
		return true
	}
	return normalizeProjectRoot(tab.WorkspaceRoot) == normalizeProjectRoot(workspaceRoot)
}

func tabInWorkspace(tab *WorkspaceTab, workspaceRoot string) bool {
	return tab != nil &&
		tab.Scope == "project" &&
		normalizeProjectRoot(tab.WorkspaceRoot) == normalizeProjectRoot(workspaceRoot)
}

// EnsureBlankTab activates the existing blank tab for the target scope, or
// creates one if none exists. Reusing a blank tab keeps repeated "new session"
// clicks from piling up empty conversations.
func (a *App) EnsureBlankTab(scope, workspaceRoot string) (TabMeta, error) {
	scope = strings.TrimSpace(scope)
	if scope != "project" {
		scope = "global"
	}

	globalRoot := ""
	if scope == "project" {
		workspaceRoot = strings.TrimSpace(workspaceRoot)
		if workspaceRoot == "" {
			return TabMeta{}, fmt.Errorf("workspaceRoot is required")
		}
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
		saveWorkspace(workspaceRoot)
		_ = addProject(workspaceRoot, "")
	} else {
		workspaceRoot = ""
		globalRoot = globalWorkspaceRoot()
		if err := os.MkdirAll(globalRoot, 0o755); err != nil {
			return TabMeta{}, fmt.Errorf("create global workspace: %w", err)
		}
	}

	var created *WorkspaceTab
	// Compute actual root early — both the indexed-topic fallback and the
	// new-topic path need it when constructing the tab below.
	actualRoot := workspaceRoot
	if scope == "global" {
		actualRoot = globalRoot
	}
	defaultModel, defaultToolApprovalMode := desktopNewSessionDefaults()

	a.mu.Lock()
	for _, id := range a.orderedTabIDsLocked() {
		tab := a.tabs[id]
		if a.blankTabMatchesTargetLocked(tab, scope, workspaceRoot) {
			if err := resetReusableBlankTabTitle(tab, scope, workspaceRoot); err != nil {
				a.mu.Unlock()
				return TabMeta{}, err
			}
			a.activeTabID = tab.ID
			meta := a.tabMeta(tab, true)
			a.saveTabsLocked()
			a.mu.Unlock()
			return enrichTabMeta(meta), nil
		}
	}

	// New blank sessions start from global session defaults for model and
	// Ask/Auto/YOLO approval posture. Keep the remaining execution-local settings
	// from the active tab so a new blank session preserves effort/token/MCP
	// continuity without letting the active tab override global defaults (#4019).
	inheritedModel := defaultModel
	var inheritedEffort *string
	inheritedTokenMode := boot.TokenModeFull
	inheritedMode := tabModeFromAxes(false, defaultToolApprovalMode == control.ToolApprovalYolo)
	inheritedToolApprovalMode := defaultToolApprovalMode
	inheritedDisabledMCP := map[string]ServerView{}
	var inheritedMCPOrder []string
	if active := a.activeTabLocked(); active != nil {
		inheritedEffort = cloneStringPtr(active.effort)
		inheritedTokenMode = currentTabTokenMode(active)
		inheritedDisabledMCP = cloneServerViewMap(active.disabledMCP)
		inheritedMCPOrder = append([]string(nil), active.mcpOrder...)
	}

	if topicID := a.indexedBlankTopicIDLocked(scope, workspaceRoot); topicID != "" {
		// Reuse a previously-indexed but unused blank topic instead of
		// creating a new one.  Build it inline (not via OpenProjectTab /
		// OpenGlobalTab) so it inherits settings from the active tab.
		if loadTopicCreatedAt(topicTitleRoot(scope, workspaceRoot), topicID) <= 0 {
			createdAt := topicIDCreatedAt(topicID)
			if createdAt <= 0 {
				createdAt = time.Now().UnixMilli()
			}
			_ = setTopicCreatedAt(topicTitleRoot(scope, workspaceRoot), topicID, createdAt)
		}
		tabID := a.newUniqueTabIDLocked()
		topicTitle := topicTitleForTab(scope, workspaceRoot, topicID)
		created = &WorkspaceTab{
			ID:               tabID,
			Scope:            scope,
			WorkspaceRoot:    actualRoot,
			TopicID:          topicID,
			TopicTitle:       topicTitle,
			model:            inheritedModel,
			effort:           inheritedEffort,
			tokenMode:        inheritedTokenMode,
			mode:             inheritedMode,
			toolApprovalMode: inheritedToolApprovalMode,
			disabledMCP:      inheritedDisabledMCP,
			mcpOrder:         inheritedMCPOrder,
		}
		created.sink = &tabEventSink{tabID: tabID, app: a}
		a.tabs[tabID] = created
		a.tabOrder = append(a.tabOrder, tabID)
		a.activeTabID = tabID
		prePath, err := createEmptySessionFile(desktopSessionDir(actualRoot), inheritedModel)
		if err != nil {
			delete(a.tabs, tabID)
			a.removeTabOrderLocked(tabID)
			a.mu.Unlock()
			return TabMeta{}, err
		}
		created.SessionPath = prePath
		a.saveTabsLocked()
		meta := a.tabMeta(created, true)
		a.mu.Unlock()

		a.startTabControllerBuild(created)
		a.emitProjectTreeChanged()
		return enrichTabMeta(meta), nil
	}

	topicID := newTopicID()
	topicTitle := defaultTopicTitle
	createdAt := time.Now().UnixMilli()
	if err := setTopicTitleWithSource(workspaceRoot, topicID, topicTitle, topicTitleSourceAuto); err != nil {
		a.mu.Unlock()
		return TabMeta{}, err
	}
	if err := setTopicCreatedAt(workspaceRoot, topicID, createdAt); err != nil {
		a.mu.Unlock()
		return TabMeta{}, err
	}
	_ = prependTopicInProjectsFile(workspaceRoot, topicID, false)

	tabID := a.newUniqueTabIDLocked()
	created = &WorkspaceTab{
		ID:               tabID,
		Scope:            scope,
		WorkspaceRoot:    actualRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitleForTab(scope, workspaceRoot, topicID),
		model:            inheritedModel,
		effort:           inheritedEffort,
		tokenMode:        inheritedTokenMode,
		mode:             inheritedMode,
		toolApprovalMode: inheritedToolApprovalMode,
		disabledMCP:      inheritedDisabledMCP,
		mcpOrder:         inheritedMCPOrder,
	}
	created.sink = &tabEventSink{tabID: tabID, app: a}
	a.tabs[tabID] = created
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	prePath, err := createEmptySessionFile(desktopSessionDir(actualRoot), inheritedModel)
	if err != nil {
		delete(a.tabs, tabID)
		a.removeTabOrderLocked(tabID)
		a.mu.Unlock()
		return TabMeta{}, err
	}
	created.SessionPath = prePath
	a.saveTabsLocked()
	meta := a.tabMeta(created, true)
	a.mu.Unlock()

	a.startTabControllerBuild(created)
	a.emitProjectTreeChanged()
	return enrichTabMeta(meta), nil
}

// blankTabMatchesTargetLocked returns true if tab is a reusable blank tab
// matching the given scope/project root — no running controller, no real history.
func (a *App) blankTabMatchesTargetLocked(tab *WorkspaceTab, scope, workspaceRoot string) bool {
	if tab == nil || tab.Scope != scope {
		return false
	}
	if scope == "project" && tab.WorkspaceRoot != workspaceRoot {
		return false
	}
	if tab.Ctrl == nil {
		return blankTabSessionPathHasNoContent(tab)
	}
	if tab.hasActiveRuntimeWork() {
		return false
	}
	return !messagesHaveConversationContent(tab.Ctrl.History())
}

func createEmptySessionFile(dir, model string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("session dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for i := 0; i < 3; i++ {
		path := agent.NewSessionPath(dir, model)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			if closeErr := f.Close(); closeErr != nil {
				return "", closeErr
			}
			return path, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("create empty session file: exhausted filename retries")
}

func blankTabSessionPathHasNoContent(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	if strings.TrimSpace(tab.SessionPath) == "" {
		return true
	}
	return sessionPathHasNoContent(tabSessionDir(tab), tab.SessionPath)
}

func sessionPathHasNoContent(sessionDir, sessionPath string) bool {
	if strings.TrimSpace(sessionPath) == "" {
		return true
	}
	path, ok := pinnedTabSessionPath(sessionDir, sessionPath)
	if !ok {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if info.Size() == 0 {
		return true
	}
	session, err := agent.LoadSession(path)
	if err != nil {
		return false
	}
	return !session.HasContent()
}

func resetReusableBlankTabTitle(tab *WorkspaceTab, scope, workspaceRoot string) error {
	if tab == nil {
		return nil
	}
	topicID := strings.TrimSpace(tab.TopicID)
	if topicID == "" {
		return nil
	}
	titleRoot := topicTitleRoot(scope, workspaceRoot)
	if source := loadTopicTitleSource(titleRoot, topicID); source != topicTitleSourceAuto {
		return nil
	}
	if err := setTopicTitleWithSource(titleRoot, topicID, defaultTopicTitle, topicTitleSourceAuto); err != nil {
		return err
	}
	_ = deleteTopicAutoTitleMeta(titleRoot, topicID)
	tab.TopicTitle = defaultTopicTitle
	return nil
}

// indexedBlankTopicIDLocked finds a blank topic ID that is indexed on disk
// but not open in any tab — for reusing without creating a new topic.
func (a *App) indexedBlankTopicIDLocked(scope, workspaceRoot string) string {
	titleRoot := topicTitleRoot(scope, workspaceRoot)
	titles := loadTopicTitles(titleRoot)
	f := loadProjectsFile()

	var topicIDs []string
	if scope == "global" {
		topicIDs = orderedTopicIDs(f.GlobalTopics, titles)
	} else {
		for _, project := range f.Projects {
			if project.Root == workspaceRoot {
				topicIDs = orderedTopicIDs(project.Topics, titles)
				break
			}
		}
	}
	if len(topicIDs) == 0 {
		return ""
	}
	// Blank-tab reuse is an automatic write path: the reused ID flows into
	// ensureTopicIndexed, whose intentional single-topic prepend clears delete
	// tombstones. Picking a tombstoned topic here (its default title can
	// linger title-only after a delete raced a scan save) would therefore
	// fully resurrect a topic the user removed — skip them.
	deletedTopics := make(map[string]bool, len(f.DeletedTopics))
	for _, id := range f.DeletedTopics {
		deletedTopics[id] = true
	}

	openTopics := map[string]bool{}
	for _, tab := range a.tabs {
		if tab == nil || tab.Scope != scope || strings.TrimSpace(tab.TopicID) == "" {
			continue
		}
		if scope == "project" && tab.WorkspaceRoot != workspaceRoot {
			continue
		}
		openTopics[tab.TopicID] = true
	}
	seenSessionDirs := map[string]bool{}
	sessionIndexes := []topicSessionDirIndex{}
	addSessionIndex := func(dir string) {
		dir = cleanDesktopPath(dir)
		if dir == "" {
			return
		}
		if seenSessionDirs[dir] {
			return
		}
		seenSessionDirs[dir] = true
		if index, err := topicSessionIndexForDir(dir); err == nil {
			sessionIndexes = append(sessionIndexes, index)
		}
	}
	if scope == "project" {
		addSessionIndex(desktopSessionDir(workspaceRoot))
	} else {
		addSessionIndex(config.SessionDir())
		addSessionIndex(desktopSessionDir(globalWorkspaceRoot()))
	}
	for _, topicID := range topicIDs {
		if deletedTopics[topicID] || openTopics[topicID] {
			continue
		}
		if topicTitleForTab(scope, workspaceRoot, topicID) != defaultTopicTitle {
			continue
		}
		hasSession := false
		for _, index := range sessionIndexes {
			if topicSessionIndexHasContentTopic(index, topicID) {
				hasSession = true
				break
			}
		}
		if hasSession {
			continue
		}
		return topicID
	}
	return ""
}

// SetActiveTab switches the frontend's active tab. A no-op when tabID is
// already active or unknown.
func (a *App) SetActiveTab(tabID string) error {
	a.mu.RLock()
	_, ok := a.tabs[tabID]
	active := a.tabs[a.activeTabID]
	alreadyActive := a.activeTabID == tabID
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tab %q not found", tabID)
	}
	if alreadyActive {
		return nil
	}
	if err := a.snapshotTabForAction(active, "switching tabs"); err != nil {
		return err
	}

	a.mu.Lock()
	if _, ok := a.tabs[tabID]; !ok {
		a.mu.Unlock()
		return fmt.Errorf("tab %q not found", tabID)
	}
	if a.activeTabID == tabID {
		a.mu.Unlock()
		return nil
	}
	a.activeTabID = tabID
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.mu.Unlock()

	// I/O outside the lock — disk writes can block for hundreds of ms on
	// Windows when antivirus or the search indexer briefly locks the file.
	a.saveTabsWrite(dir, entries, activeID, version)
	return nil
}

// ReorderTabs persists the frontend's manual tab order. The submitted order must
// contain every currently open tab exactly once.
func (a *App) ReorderTabs(tabIDs []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(tabIDs) != len(a.tabs) {
		return fmt.Errorf("tab order length mismatch")
	}
	seen := make(map[string]bool, len(tabIDs))
	next := make([]string, 0, len(tabIDs))
	for _, id := range tabIDs {
		if _, ok := a.tabs[id]; !ok {
			return fmt.Errorf("tab %q not found", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate tab %q", id)
		}
		seen[id] = true
		next = append(next, id)
	}
	a.tabOrder = next
	a.saveTabsLocked()
	return nil
}

// CloseTab removes a visible tab. If the tab's session still has foreground or
// background work, the controller is detached so closing a view does not destroy
// the session runtime.
func (a *App) CloseTab(tabID string) error {
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()

	a.mu.Lock()
	tab, ok := a.tabs[tabID]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("tab %q not found", tabID)
	}
	if len(a.tabs) <= 1 {
		a.mu.Unlock()
		return fmt.Errorf("cannot close the last tab")
	}
	a.mu.Unlock()

	// Snapshot while the tab binding is still present, but outside a.mu because
	// snapshot recovery can re-enter App and acquire a.mu. sessionRemovalMu keeps
	// DeleteSession/topic/workspace removal from trashing the same files while
	// this save is in flight.
	if err := a.snapshotTab(tab); err != nil {
		slog.Warn("desktop: snapshot before closing tab failed", "tab", tabID, "err", err)
		return fmt.Errorf("save current session before closing tab: %w", err)
	}
	if err := a.saveTabSessionMetaForCurrentSession(tab); err != nil {
		slog.Warn("desktop: session metadata before closing tab failed", "tab", tabID, "err", err)
		return fmt.Errorf("save current session metadata before closing tab: %w", err)
	}

	a.mu.Lock()
	if current := a.tabs[tabID]; current != tab {
		a.mu.Unlock()
		if current == nil {
			return fmt.Errorf("tab %q not found", tabID)
		}
		return fmt.Errorf("tab %q changed while closing", tabID)
	}
	if len(a.tabs) <= 1 {
		a.mu.Unlock()
		return fmt.Errorf("cannot close the last tab")
	}
	if tab.Ctrl == nil || !tab.hasActiveRuntimeWork() {
		a.markTabRemovedLocked(tab)
	}

	ordered := a.orderedTabIDsLocked()
	closedIndex := -1
	for i, id := range ordered {
		if id == tabID {
			closedIndex = i
			break
		}
	}
	delete(a.tabs, tabID)
	a.removeTabOrderLocked(tabID)
	wasActive := a.activeTabID == tabID
	if wasActive {
		a.activeTabID = ""
		if len(a.tabOrder) > 0 {
			nextIndex := closedIndex
			if nextIndex < 0 {
				nextIndex = 0
			}
			if nextIndex >= len(a.tabOrder) {
				nextIndex = len(a.tabOrder) - 1
			}
			a.activeTabID = a.tabOrder[nextIndex]
		}
	}
	a.saveTabsLocked()
	// Snapshot the teardown targets while still holding the lock: the tab is
	// no longer reachable from a.tabs after this section, but locked writers
	// holding stale pointers (rememberTabSessionPath, applySessionBindingToTab)
	// can still write its fields under a.mu.
	closeCtrl := tab.Ctrl
	closeSink := tab.sink
	a.mu.Unlock()

	// Tear down outside the lock.
	discardPath, discardTransientBlank := a.transientBlankSessionArtifactPath(tab)
	if closeCtrl != nil {
		if controllerHasActiveRuntimeWork(closeCtrl) && a.detachSessionRuntime(tab) {
			// Detached runtimes keep running and must keep saving: do not
			// clear the path or drain for them.
			return nil
		}
		closeCtrl.SetSessionPath("") // future snapshots become no-ops
		a.quiesceTabAutosave(tab)    // wait for any in-flight snapshot to finish
		closeCtrl.Cancel()
		closeCtrl.Close()
		// Release the shared plugin host reference. The host stays alive as
		// long as any other tab for the same workspace root holds a reference;
		// on the last release the host is closed and its subprocesses exit.
		a.releaseTabSharedHost(tab)
		tab.releaseSessionLease()
	}
	if closeSink != nil {
		closeSink.clearContext() // stop further emissions (nil ctx -> Emit becomes no-op)
	}
	if discardTransientBlank {
		discardTransientBlankSessionArtifacts(discardPath)
	}
	return nil
}

func (a *App) keepOnlyVisibleTab(tabID string) (TabMeta, error) {
	type pruneCandidate struct {
		id  string
		tab *WorkspaceTab
	}

	// sessionRemovalMu covers snapshotting, pruning the hidden bindings, and
	// closing the removed runtimes (a detached runtime must finish its
	// in-flight autosave before DeleteSession can see the files). The
	// project-tree event stays outside so a listener can never re-enter a
	// removal path while the lock is held.
	meta, err := func() (TabMeta, error) {
		a.sessionRemovalMu.Lock()
		defer a.sessionRemovalMu.Unlock()

		a.mu.Lock()
		active := a.tabs[tabID]
		if active == nil {
			a.mu.Unlock()
			return TabMeta{}, fmt.Errorf("tab %q not found", tabID)
		}
		candidates := make([]pruneCandidate, 0, len(a.tabs)-1)
		for id, tab := range a.tabs {
			if id == tabID {
				continue
			}
			candidates = append(candidates, pruneCandidate{id: id, tab: tab})
		}
		a.mu.Unlock()

		// Keep tab bindings in a.tabs while saving so DeleteSession still sees
		// them, but do not hold a.mu: Snapshot can run recovery callbacks that
		// re-enter App and need the same lock.
		snapshotted := make(map[string]*WorkspaceTab, len(candidates))
		for _, candidate := range candidates {
			id, tab := candidate.id, candidate.tab
			snapshotted[id] = tab
			if err := a.snapshotTab(tab); err != nil {
				slog.Warn("desktop: snapshot before pruning hidden tab failed", "tab", id, "err", err)
				return TabMeta{}, fmt.Errorf("save current session before switching tabs: %w", err)
			}
			if err := a.saveTabSessionMetaForCurrentSession(tab); err != nil {
				slog.Warn("desktop: session metadata before pruning hidden tab failed", "tab", id, "err", err)
				return TabMeta{}, fmt.Errorf("save current session metadata before switching tabs: %w", err)
			}
		}

		a.mu.Lock()
		active = a.tabs[tabID]
		if active == nil {
			a.mu.Unlock()
			return TabMeta{}, fmt.Errorf("tab %q not found", tabID)
		}
		for id, tab := range a.tabs {
			if id != tabID && snapshotted[id] != tab {
				a.mu.Unlock()
				return TabMeta{}, fmt.Errorf("visible tabs changed while switching; retry")
			}
		}
		a.activeTabID = tabID
		removed := make([]*WorkspaceTab, 0, len(candidates))
		for _, candidate := range candidates {
			id, tab := candidate.id, candidate.tab
			if tab == nil || a.tabs[id] != tab {
				continue
			}
			if tab.Ctrl == nil || !tab.hasActiveRuntimeWork() {
				a.markTabRemovedLocked(tab)
			}
			removed = append(removed, tab)
			delete(a.tabs, id)
			a.removeTabOrderLocked(id)
		}
		a.tabOrder = []string{tabID}
		a.saveTabsLocked()
		meta := a.tabMeta(active, true)
		a.mu.Unlock()

		for _, tab := range removed {
			a.removeVisibleTabRuntime(tab)
		}
		return meta, nil
	}()
	if err != nil {
		return TabMeta{}, err
	}
	a.emitProjectTreeChanged()
	return enrichTabMeta(meta), nil
}

func (a *App) applySingleSurfaceTabPolicy() error {
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()

	a.mu.RLock()
	tabID := a.activeTabID
	if tabID == "" || a.tabs[tabID] == nil {
		for _, id := range a.tabOrder {
			if a.tabs[id] != nil {
				tabID = id
				break
			}
		}
		if tabID == "" {
			for id := range a.tabs {
				tabID = id
				break
			}
		}
	}
	a.mu.RUnlock()
	if tabID == "" {
		return nil
	}
	_, err := a.keepOnlyVisibleTab(tabID)
	return err
}

func (a *App) removeVisibleTabRuntime(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	if err := a.snapshotTab(tab); err != nil {
		slog.Warn("desktop: snapshot before removing visible tab runtime failed", "tab", tab.ID, "err", err)
	}
	discardPath, discardTransientBlank := a.transientBlankSessionArtifactPath(tab)
	a.mu.RLock()
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if ctrl != nil && controllerHasActiveRuntimeWork(ctrl) && a.detachSessionRuntime(tab) {
		return
	}
	a.markTabRemoved(tab)
	a.closeTabRuntime(tab)
	if discardTransientBlank {
		discardTransientBlankSessionArtifacts(discardPath)
	}
}

// transientBlankSessionArtifactPath reports the artifact path to discard when
// closing a still-blank tab. It snapshots the racy tab fields under a.mu and
// keeps the file probe (sessionPathHasNoContent) outside the lock. Callers
// must not hold a.mu.
func (a *App) transientBlankSessionArtifactPath(tab *WorkspaceTab) (string, bool) {
	if tab == nil {
		return "", false
	}
	snap := a.tabRuntimeSnapshot(tab)
	if snap.readOnly || strings.TrimSpace(snap.topicID) != "" || controllerHasActiveRuntimeWork(snap.ctrl) {
		return "", false
	}
	if strings.TrimSpace(snap.sessionPath) == "" {
		return "", false
	}
	dir := sessionDirForSnapshot(snap)
	if !sessionPathHasNoContent(dir, snap.sessionPath) {
		return "", false
	}
	path, ok := pinnedTabSessionPath(dir, snap.sessionPath)
	if !ok {
		return "", false
	}
	return path, true
}

func discardTransientBlankSessionArtifacts(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := removeDesktopSessionArtifacts(path); err != nil {
		slog.Warn("desktop: discard transient blank session artifacts failed", "path", path, "err", err)
	}
}

func (a *App) markTabRemoved(tab *WorkspaceTab) {
	a.mu.Lock()
	a.markTabRemovedLocked(tab)
	a.mu.Unlock()
}

func (a *App) markTabRemovedLocked(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	tab.removed = true
	if tab.buildCancel != nil {
		tab.buildCancel()
		tab.buildCancel = nil
	}
}

// tabBuildSupersededLocked reports whether an in-flight build lost ownership
// of its tab: the tab was removed/replaced, or a session rebind bumped
// buildGeneration to invalidate it. Generation 0 marks the synchronous
// rebuild paths, which serialize through runtimeRebuildMu instead and are
// never superseded by generation bumps. Callers must hold a.mu.
func (a *App) tabBuildSupersededLocked(tab *WorkspaceTab, generation uint64) bool {
	if tab == nil || tab.removed || a.tabs[tab.ID] != tab {
		return true
	}
	return generation != 0 && tab.buildGeneration != generation
}

func (a *App) tabBuildSuperseded(tab *WorkspaceTab, generation uint64) bool {
	if tab == nil {
		return true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tabBuildSupersededLocked(tab, generation)
}

// supersedeTabBuildLocked invalidates any in-flight startup build and cancels
// its context. A synchronous rebuild (model/effort/token switch) that has
// already installed its controller calls this so a slower blank-session build
// cannot finish afterward, overwrite tab.Ctrl, and release or steal the
// session lease the switch just bound. Callers must hold a.mu.
func (a *App) supersedeTabBuildLocked(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	tab.buildGeneration++
	if tab.buildCancel != nil {
		tab.buildCancel()
		tab.buildCancel = nil
	}
}

// abandonSupersededBuild cleans up after a build that lost tab ownership
// mid-flight (removed tab, or a session rebind bumped the generation). It
// releases only what THIS build acquired — its controller, its own
// shared-host reference (rootKey), and the session lease bound to its own
// path (leaseKey) — and never reads or clears the tab's SharedHostKey or
// lease outright: on a live rebound tab the replacement build may already
// have published its own key and lease there, and taking those would leak
// the new runtime's host reference (or close a host still in use) and strip
// the new session's lease. Callers must not hold a.mu.
func (a *App) abandonSupersededBuild(tab *WorkspaceTab, ctrl control.SessionAPI, rootKey, leaseKey string) {
	if ctrl != nil {
		ctrl.Close()
	}
	if rootKey != "" {
		a.releaseSharedHost(rootKey)
	}
	tab.releaseSessionLeaseForKey(leaseKey)
}

func (a *App) clearTabBuildCancel(tab *WorkspaceTab, generation uint64, cancel context.CancelFunc, keepContext bool) {
	if cancel == nil {
		return
	}
	if !keepContext {
		defer cancel()
	}
	if tab == nil {
		return
	}
	a.mu.Lock()
	if tab.buildGeneration == generation {
		tab.buildCancel = nil
	}
	a.mu.Unlock()
}

func (a *App) closeTabRuntime(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	a.mu.RLock()
	ctrl := tab.Ctrl
	sink := tab.sink
	a.mu.RUnlock()
	if ctrl != nil {
		ctrl.SetSessionPath("") // future snapshots become no-ops
		a.quiesceTabAutosave(tab)
		ctrl.Cancel()
		ctrl.Close()
		a.releaseTabSharedHost(tab)
	}
	if sink != nil {
		sink.clearContext()
	}
	tab.releaseSessionLease()
}

// buildTabController assembles a controller for a tab in the background, the
// same way buildController works for the single-controller App. On success it
// wires the controller and flips Ready; on failure it stores StartupErr.
func (a *App) startTabControllerBuild(tab *WorkspaceTab) {
	buildCtx, cancel := context.WithCancel(a.bootContext())
	a.mu.Lock()
	if tab == nil || tab.removed {
		a.mu.Unlock()
		cancel()
		return
	}
	tab.buildGeneration++
	generation := tab.buildGeneration
	tab.buildCancel = cancel
	a.mu.Unlock()
	if a.ctx == nil {
		a.buildTabControllerWithContext(tab, loadedTabSession{}, buildCtx, generation, cancel)
		return
	}
	go a.buildTabControllerWithContext(tab, loadedTabSession{}, buildCtx, generation, cancel)
}

func (a *App) buildTabController(tab *WorkspaceTab) {
	a.buildTabControllerWithLoadedSession(tab, loadedTabSession{})
}

type loadedTabSession struct {
	Path    string
	Session *agent.Session
}

func (s loadedTabSession) matches(path string) bool {
	return s.Session != nil && sessionRuntimeKey(s.Path) != "" && sessionRuntimeKey(s.Path) == sessionRuntimeKey(path)
}

func (a *App) buildTabControllerWithLoadedSession(tab *WorkspaceTab, loadedSession loadedTabSession) {
	a.buildTabControllerWithContext(tab, loadedSession, a.bootContext(), 0, nil)
}

func (a *App) desktopNotificationSender() notify.Sender {
	if a == nil {
		return notify.NewPlatformSender()
	}
	a.notificationSenderOnce.Do(func() {
		if a.notificationSender == nil {
			a.notificationSender = notify.NewPlatformSender()
		}
	})
	return a.notificationSender
}

func (a *App) desktopControllerSink(inner event.Sink, cfg config.NotificationsConfig) event.Sink {
	if !cfg.Enabled {
		return inner
	}
	sender := a.desktopNotificationSender()
	if sender == nil {
		return inner
	}
	return notify.NewSink(inner, sender, cfg)
}

func (a *App) buildTabControllerWithContext(tab *WorkspaceTab, loadedSession loadedTabSession, buildCtx context.Context, buildGeneration uint64, buildCancel context.CancelFunc) {
	defer a.recoverToPending("buildTabController")
	keepBuildContext := false
	defer func() {
		a.clearTabBuildCancel(tab, buildGeneration, buildCancel, keepBuildContext)
	}()
	wailsCtx := a.ctx
	if a.tabBuildSuperseded(tab, buildGeneration) {
		return
	}

	a.reconcileTabWithPinnedSessionMeta(tab)

	// Snapshot the identity/profile fields under a.mu before the off-lock
	// stretch: session rebinding, recovery, and topic assignment write them
	// under the lock while this goroutine builds.
	a.mu.RLock()
	tabWorkspaceRoot := tab.WorkspaceRoot
	tabScope := tab.Scope
	tabTopicID := tab.TopicID
	tabSessionPath := tab.SessionPath
	tabModel := tab.model
	tabSink := tab.sink
	a.mu.RUnlock()

	root := tabWorkspaceRoot
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}

	// Load config for this tab's workspace root.
	_ = config.MigrateLegacyCredentialsForRoot(root)
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		a.mu.Lock()
		if a.tabBuildSupersededLocked(tab, buildGeneration) {
			a.mu.Unlock()
			return
		}
		tab.StartupErr = userFacingSessionLeaseError("", err).Error()
		tab.Ready = true
		tab.releaseSessionLease()
		a.mu.Unlock()
		a.emitReady(wailsCtx, tab.ID)
		return
	}

	if a.tabBuildSuperseded(tab, buildGeneration) {
		return
	}
	if tabSink != nil {
		tabSink.setContext(wailsCtx)
	}

	sessionDir := desktopSessionDir(root)
	topicID := strings.TrimSpace(tabTopicID)

	// Assign Global topics to legacy sessions in the global session dir so
	// imported history appears in the project tree regardless of which tab
	// triggered the build (the migration now sends everything to global).
	migratedGlobalTopics := migrateLegacySessionsIntoGlobalTopics(config.SessionDir())
	if len(migratedGlobalTopics) > 0 {
		a.emitProjectTreeChanged()
	}
	if tabScope == "global" && topicID == "" && len(migratedGlobalTopics) > 0 {
		topicID = migratedGlobalTopics[0]
		topicTitle := topicTitleForTab("global", "", topicID)
		a.mu.Lock()
		if a.tabBuildSupersededLocked(tab, buildGeneration) {
			a.mu.Unlock()
			return
		}
		if strings.TrimSpace(tab.TopicID) == "" {
			tab.TopicID = topicID
			tab.TopicTitle = topicTitle
			a.saveTabsLocked()
		} else {
			topicID = strings.TrimSpace(tab.TopicID)
		}
		a.mu.Unlock()
	}
	if topicID != "" {
		if _, dir := a.findTopicSessionForTarget(tabScope, tabWorkspaceRoot, topicID); dir != "" {
			sessionDir = dir
		}
	}
	startupSessionPath := ""
	if pinnedPath, ok := pinnedTabSessionPath(sessionDir, tabSessionPath); ok {
		if !agent.IsCleanupPending(pinnedPath) {
			startupSessionPath = pinnedPath
		}
	} else if topicID != "" {
		startupSessionPath = findTopicSession(sessionDir, topicID)
	}

	model := strings.TrimSpace(tabModel)
	if sessionModel, ok := agent.LoadSessionModel(startupSessionPath); ok {
		config.NormalizeLegacyMimoCustomProvidersForRefs(cfg, sessionModel)
		if _, ok := cfg.ResolveModel(sessionModel); ok {
			model = sessionModel
		}
	}
	if model == "" {
		model = cfg.DefaultModel
	}
	config.NormalizeLegacyMimoCustomProvidersForRefs(cfg, model)
	requestedModel := model
	if resolved, fallback, ok := cfg.ResolveModelWithFallback(model); ok {
		if fallback && strings.TrimSpace(tabModel) != "" {
			a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", requestedModel, resolved))
		}
		model = resolved
	}

	// Acquire a shared plugin host for this workspace root so MCP processes
	// are launched once per root, not once per tab. SharedHostKey is an a.mu-
	// guarded field (takeTabSharedHostKey reads it under the lock during
	// teardown), so publish it under the lock alongside the model. Capture the
	// tab-local runtime profile here too: bound methods (SetModeForTab,
	// SetGoalForTab, SetEffortForTab, ...) write these under a.mu, so the
	// off-lock boot.Build below must read a locked snapshot, not the live tab.
	rootKey := tabWorkspaceRoot
	if rootKey == "" {
		rootKey = "__global__" // stable key for global workspace tabs
	}
	a.mu.Lock()
	if a.tabBuildSupersededLocked(tab, buildGeneration) {
		a.mu.Unlock()
		return
	}
	tab.model = model
	tab.Label = model
	tab.SharedHostKey = rootKey
	buildEffort := cloneStringPtr(tab.effort)
	buildTokenMode := boot.NormalizeTokenMode(tab.tokenMode)
	buildMode := tab.mode
	buildToolApprovalMode := tab.toolApprovalMode
	buildGoal := tab.goal
	buildSink := tab.sink
	a.saveTabsLocked()
	a.mu.Unlock()

	sharedHost := a.acquireSharedHost(rootKey)
	sink := a.desktopControllerSink(buildSink, cfg.Notifications)

	ctrl, err := boot.Build(buildCtx, boot.Options{
		Model:                    model,
		RequireKey:               false,
		Sink:                     sink,
		WorkspaceRoot:            root,
		SessionDir:               sessionDir,
		EffortOverride:           cloneStringPtr(buildEffort),
		TokenMode:                buildTokenMode,
		SharedHost:               sharedHost,
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
		SessionRecoveryMeta:      a.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:       a.handleTabSessionRecovered(tab),
	})
	if err != nil {
		a.mu.Lock()
		if a.tabBuildSupersededLocked(tab, buildGeneration) {
			a.mu.Unlock()
			a.abandonSupersededBuild(tab, nil, rootKey, "")
			return
		}
		tab.StartupErr = userFacingSessionLeaseError("", err).Error()
		tab.Ready = true
		hostKey := takeTabSharedHostKey(tab)
		tab.releaseSessionLease()
		a.mu.Unlock()
		if hostKey != "" {
			a.releaseSharedHost(hostKey)
		}
		a.emitReady(wailsCtx, tab.ID)
		return
	}
	if a.tabBuildSuperseded(tab, buildGeneration) {
		a.abandonSupersededBuild(tab, ctrl, rootKey, "")
		return
	}

	a.bindControllerDisplayRecorder(ctrl)
	ctrl.EnableInteractiveApproval()
	applyTabModeToController(ctrl, buildMode)
	applyTabToolApprovalModeToController(ctrl, buildToolApprovalMode)
	ctrl.SetGoal(buildGoal)

	acquiredLeaseKey := ""
	if dir := ctrl.SessionDir(); dir != "" {
		migratedTopics := migrateLegacySessionsIntoGlobalTopics(dir)
		if len(migratedTopics) > 0 {
			a.emitProjectTreeChanged()
		}
		// Refresh the topic/session locals under the lock: a rebind or the
		// recovery callback may have rewritten them since the early snapshot.
		a.mu.RLock()
		tabTopicID = strings.TrimSpace(tab.TopicID)
		tabSessionPath = tab.SessionPath
		a.mu.RUnlock()
		if tabScope == "global" && tabTopicID == "" && len(migratedTopics) > 0 {
			topicID := migratedTopics[0]
			topicTitle := topicTitleForTab("global", "", topicID)
			a.mu.Lock()
			if !a.tabBuildSupersededLocked(tab, buildGeneration) && strings.TrimSpace(tab.TopicID) == "" {
				tab.TopicID = topicID
				tab.TopicTitle = topicTitle
				tabTopicID = topicID
				a.saveTabsLocked()
			} else {
				tabTopicID = strings.TrimSpace(tab.TopicID)
			}
			a.mu.Unlock()
		}
		var path string
		var resumeSession *agent.Session
		// Prefer the exact session file persisted for this tab. Topic lookup is a
		// compatibility fallback for older desktop-tabs.json files that only stored
		// topicId and could pick the wrong session when one topic had multiple files.
		if loaded, pinnedPath, ok := loadPinnedTabSessionWithPreload(dir, tabSessionPath, loadedSession); ok {
			path = pinnedPath
			resumeSession = loaded
		}
		if path == "" && tabTopicID != "" {
			existingPath := findTopicSession(dir, tabTopicID)
			if existingPath != "" {
				if loaded, err := loadResumableSession(existingPath); err == nil {
					path = existingPath
					resumeSession = loaded
				}
			}
		}
		if path == "" {
			path = agent.NewSessionPath(dir, ctrl.Label())
		}
		// Write/update scope/session meta.
		if path != "" {
			if a.attachExistingSessionRuntime(tab, path, wailsCtx) {
				ctrl.Close()
				a.releaseSharedHost(rootKey)
				a.emitReady(wailsCtx, tab.ID)
				return
			}
			preLeaseKey := tab.sessionLeaseRuntimeKey()
			if err := tab.ensureSessionLease(path); err != nil {
				a.mu.Lock()
				if a.tabBuildSupersededLocked(tab, buildGeneration) {
					a.mu.Unlock()
					a.abandonSupersededBuild(tab, ctrl, rootKey, "")
					return
				}
				tab.StartupErr = userFacingSessionLeaseError("", err).Error()
				tab.Ready = true
				hostKey := takeTabSharedHostKey(tab)
				// Release only a lease bound to THIS build's session: a failed
				// ensure leaves any prior lease untouched, and that lease may
				// belong to a runtime a concurrent switch just installed.
				tab.releaseSessionLeaseForKey(sessionRuntimeKey(path))
				a.mu.Unlock()
				ctrl.Close()
				if hostKey != "" {
					a.releaseSharedHost(hostKey)
				}
				a.emitReady(wailsCtx, tab.ID)
				return
			}
			// Remember which lease THIS build bound: if the build is later
			// superseded, only a lease still carrying this key may be
			// released (see abandonSupersededBuild). A fast-path reuse means
			// the lease existed before this build (bound by a concurrent
			// switch or recovery) — it is not ours to release.
			if key := sessionRuntimeKey(path); key != preLeaseKey {
				acquiredLeaseKey = key
			}
			// Re-check ownership right after the (potentially slow) lease
			// bind: a rebind that superseded this build while ensure was in
			// flight has already retargeted the tab, and continuing into
			// Resume/persistTabSessionPath would write the stale session
			// path back onto the rebound tab.
			if a.tabBuildSuperseded(tab, buildGeneration) {
				a.abandonSupersededBuild(tab, ctrl, rootKey, acquiredLeaseKey)
				return
			}
			if resumeSession != nil {
				ctrl.Resume(sessionWithFreshSystemPrompt(resumeSession, systemPromptFrom(ctrl.History())), path)
			} else {
				ctrl.SetSessionPath(path)
			}
			a.persistTabSessionPath(tab, path)
			a.mu.RLock()
			indexScope := tab.Scope
			indexRoot := tab.WorkspaceRoot
			indexTopicID := strings.TrimSpace(tab.TopicID)
			indexTopicTitle := tab.TopicTitle
			a.mu.RUnlock()
			if indexTopicID != "" {
				if err := ensureTopicIndexed(indexScope, indexRoot, indexTopicID, indexTopicTitle, loadTopicTitleSource(topicTitleRoot(indexScope, indexRoot), indexTopicID)); err == nil {
					a.emitProjectTreeChanged()
				}
			}
			// Key telemetry to the session this build binds: restore its
			// persisted sidecar, or start from zero when none exists (fresh
			// session, CLI-created session, pre-telemetry session). Keeping
			// the previous session's totals here made 会话费用 accumulate
			// across sessions and persisted the stale totals into the new
			// session's sidecar on the next event (#5850).
			snapshot := loadTelemetry(path + ".telemetry.json")
			tab.telemMu.Lock()
			tab.readTelemetry = snapshot.ReadFiles
			tab.usageTelemetry = snapshot.Usage
			tab.telemetrySessionKey = sessionRuntimeKey(path)
			tab.telemMu.Unlock()
		}
	}

	a.mu.Lock()
	if a.tabBuildSupersededLocked(tab, buildGeneration) {
		a.mu.Unlock()
		a.abandonSupersededBuild(tab, ctrl, rootKey, acquiredLeaseKey)
		return
	}
	tab.Ctrl = ctrl
	tab.Label = ctrl.Label()
	tab.Ready = true
	tab.StartupErr = ""
	keepBuildContext = true
	a.mu.Unlock()
	a.emitReady(wailsCtx, tab.ID)
}

type sessionBinding struct {
	path          string
	scope         string
	workspaceRoot string
	topicID       string
	topicTitle    string
	hasMeta       bool
	meta          agent.BranchMeta
}

func (a *App) reconcileTabWithPinnedSessionMeta(tab *WorkspaceTab) (string, bool) {
	if tab == nil {
		return "", false
	}
	path := strings.TrimSpace(tab.SessionPath)
	if path != "" {
		if resolved, ok := a.reconcileTabWithSessionPath(tab, path); ok {
			return resolved, true
		}
	}
	if tab.Ctrl == nil {
		return "", false
	}
	path = strings.TrimSpace(tab.Ctrl.SessionPath())
	if path == "" {
		return "", false
	}
	binding, ok := a.resolveSessionBinding(path)
	if !ok {
		return "", false
	}
	if tab.Scope == "project" && binding.scope != "project" && normalizeProjectRoot(tab.WorkspaceRoot) != "" {
		if root, ok := safeControllerWorkspaceRoot(tab.Ctrl); ok && normalizeProjectRoot(root) == normalizeProjectRoot(tab.WorkspaceRoot) {
			return "", false
		}
	}
	a.applySessionBindingToTab(tab, binding)
	return binding.path, true
}

func (a *App) reconcileTabWithSessionPath(tab *WorkspaceTab, sessionPath string) (string, bool) {
	if tab == nil || strings.TrimSpace(sessionPath) == "" {
		return "", false
	}
	binding, ok := a.resolveSessionBinding(sessionPath)
	if !ok {
		return "", false
	}
	a.applySessionBindingToTab(tab, binding)
	return binding.path, true
}

func (a *App) applySessionBindingToTab(tab *WorkspaceTab, binding sessionBinding) {
	if tab == nil || binding.path == "" {
		return
	}
	scope := binding.scope
	workspaceRoot := binding.workspaceRoot
	if scope == "" {
		scope = "global"
	}
	if scope == "project" {
		workspaceRoot = normalizeProjectRoot(workspaceRoot)
		if workspaceRoot == "" {
			return
		}
		_ = addProject(workspaceRoot, "")
	} else {
		scope = "global"
		workspaceRoot = globalTabWorkspaceRoot()
	}
	topicID := strings.TrimSpace(binding.topicID)
	topicTitle := strings.TrimSpace(binding.topicTitle)
	if topicTitle == "" && topicID != "" {
		topicTitle = topicTitleForTab(scope, workspaceRoot, topicID)
	}

	a.mu.Lock()
	current := a.tabs[tab.ID]
	if current != nil && current != tab {
		a.mu.Unlock()
		return
	}
	oldScope := tab.Scope
	oldWorkspaceRoot := tab.WorkspaceRoot
	changed := tab.Scope != scope ||
		tab.WorkspaceRoot != workspaceRoot ||
		canonicalTabSessionPath(tab.SessionPath) != canonicalTabSessionPath(binding.path)
	workspaceChanged := tab.Scope != scope || tab.WorkspaceRoot != workspaceRoot
	tab.Scope = scope
	tab.WorkspaceRoot = workspaceRoot
	tab.SessionPath = canonicalTabSessionPath(binding.path)
	if topicID != "" {
		changed = changed || tab.TopicID != topicID
		tab.TopicID = topicID
	}
	if topicTitle != "" {
		changed = changed || tab.TopicTitle != topicTitle
		tab.TopicTitle = topicTitle
	}
	if changed && current == tab {
		a.saveTabsLocked()
	}
	sink := tab.sink
	a.mu.Unlock()
	if workspaceChanged && sink != nil {
		sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  sessionBindingWorkspaceNotice(oldScope, oldWorkspaceRoot, scope, workspaceRoot),
		})
	}
}

func sessionBindingWorkspaceNotice(oldScope, oldWorkspaceRoot, scope, workspaceRoot string) string {
	return "Session belongs to " + describeSessionBindingWorkspace(scope, workspaceRoot) +
		"; switched tab from " + describeSessionBindingWorkspace(oldScope, oldWorkspaceRoot) +
		" to match the saved session."
}

func describeSessionBindingWorkspace(scope, workspaceRoot string) string {
	if strings.TrimSpace(scope) == "project" && strings.TrimSpace(workspaceRoot) != "" {
		return fmt.Sprintf("project workspace %q", strings.TrimSpace(workspaceRoot))
	}
	return "global workspace"
}

func (a *App) resolveSessionBinding(sessionPath string) (sessionBinding, bool) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return sessionBinding{}, false
	}
	for _, dir := range a.knownSessionDirs() {
		if binding, ok := sessionBindingInDir(dir, sessionPath); ok {
			return binding, true
		}
	}
	if !filepath.IsAbs(sessionPath) {
		return sessionBinding{}, false
	}
	path, err := filepath.Abs(sessionPath)
	if err != nil {
		return sessionBinding{}, false
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		return sessionBinding{}, false
	}
	for _, dir := range sessionBindingCandidateDirs(meta) {
		if binding, ok := sessionBindingInDir(dir, path); ok {
			return binding, true
		}
	}
	return sessionBindingFromMeta(path, meta)
}

func sessionBindingCandidateDirs(meta agent.BranchMeta) []string {
	if meta.DefaultScope() == "project" {
		if root := normalizeProjectRoot(meta.WorkspaceRoot); root != "" {
			return []string{desktopSessionDir(root)}
		}
		return nil
	}
	return []string{desktopSessionDir(globalWorkspaceRoot()), config.SessionDir()}
}

func sessionBindingInDir(dir, sessionPath string) (sessionBinding, bool) {
	path, ok := pinnedTabSessionPath(dir, sessionPath)
	if !ok {
		return sessionBinding{}, false
	}
	meta, hasMeta, err := agent.LoadBranchMeta(path)
	if err != nil {
		return sessionBinding{}, false
	}
	scope, workspaceRoot, _, ownerOK := legacyMigrationTargetForDir(dir)
	if !ownerOK {
		if !hasMeta {
			return sessionBinding{}, false
		}
		return sessionBindingFromMeta(path, meta)
	}
	if scope == "global" {
		if !hasMeta {
			return sessionBinding{}, false
		}
		return sessionBindingFromMeta(path, meta)
	}
	binding := sessionBinding{
		path:          path,
		scope:         scope,
		workspaceRoot: workspaceRoot,
		hasMeta:       hasMeta,
		meta:          meta,
	}
	if hasMeta {
		binding.topicID = strings.TrimSpace(meta.TopicID)
		binding.topicTitle = strings.TrimSpace(meta.TopicTitle)
	}
	if binding.scope == "project" {
		binding.workspaceRoot = normalizeProjectRoot(binding.workspaceRoot)
	}
	return binding, true
}

func sessionBindingFromMeta(path string, meta agent.BranchMeta) (sessionBinding, bool) {
	scope := meta.DefaultScope()
	workspaceRoot := ""
	if scope == "project" {
		workspaceRoot = normalizeProjectRoot(meta.WorkspaceRoot)
		if workspaceRoot == "" {
			return sessionBinding{}, false
		}
	} else {
		scope = "global"
		workspaceRoot = globalTabWorkspaceRoot()
	}
	return sessionBinding{
		path:          path,
		scope:         scope,
		workspaceRoot: workspaceRoot,
		topicID:       strings.TrimSpace(meta.TopicID),
		topicTitle:    strings.TrimSpace(meta.TopicTitle),
		hasMeta:       true,
		meta:          meta,
	}, true
}

// --- active tab helpers -----------------------------------------------------

// activeTab returns the currently active tab (nil when there are no tabs).
// Self-locking; safe to call from any goroutine without external lock.
func (a *App) activeTab() *WorkspaceTab {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.activeTabID == "" {
		return nil
	}
	return a.tabs[a.activeTabID]
}

// activeTabLocked is like activeTab but assumes the caller already holds a.mu
// (either RLock or Lock). Use this inside critical sections that already own
// the lock to avoid double-locking a write-lock holder.
func (a *App) activeTabLocked() *WorkspaceTab {
	if a.activeTabID == "" {
		return nil
	}
	return a.tabs[a.activeTabID]
}

// activeCtrl returns the controller of the active tab, or nil.
// Self-locking; safe to call from any goroutine without external lock.
func (a *App) activeCtrl() control.SessionAPI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeCtrlLocked()
}

// activeCtrlLocked is like activeCtrl but assumes the caller already holds a.mu.
func (a *App) activeCtrlLocked() control.SessionAPI {
	t := a.activeTabLocked()
	if t == nil {
		return nil
	}
	return t.Ctrl
}

func (a *App) tabByID(tabID string) *WorkspaceTab {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tabByIDLocked(tabID)
}

func (a *App) tabByIDLocked(tabID string) *WorkspaceTab {
	if strings.TrimSpace(tabID) == "" {
		return a.activeTabLocked()
	}
	return a.tabs[tabID]
}

func (a *App) ctrlByTabID(tabID string) control.SessionAPI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		return nil
	}
	return tab.Ctrl
}

// --- autosave per tab -------------------------------------------------------

const maxTabSnapshotFailureRetries = 2

// autosaveWarnInterval rate-limits the user-facing autosave-failure notice
// per tab; slog keeps recording every failure regardless.
const autosaveWarnInterval = 5 * time.Minute

func tabSnapshotRetryDelay(failures int) time.Duration {
	switch {
	case failures <= 1:
		return 100 * time.Millisecond
	case failures == 2:
		return 250 * time.Millisecond
	default:
		return 500 * time.Millisecond
	}
}

func (a *App) scheduleTabSnapshot(tabID string) {
	a.mu.RLock()
	tab := a.tabByEventSinkIDLocked(tabID)
	a.mu.RUnlock()
	if tab == nil {
		return
	}
	tab.saveMu.Lock()
	defer tab.saveMu.Unlock()
	if tab.closing {
		// Tab is being torn down: don't start new snapshot work that could
		// race DeleteSession and resurrect a trashed session file (#4384).
		return
	}
	if tab.saving {
		tab.saveAgain = true
		return
	}
	tab.saving = true
	tab.saveFailures = 0
	go a.tabSnapshotLoop(tab)
}

// quiesceTabAutosave marks the tab as closing and blocks until any in-flight
// tabSnapshotLoop has finished its current (and final) write. After it returns,
// no background goroutine can call Snapshot on this tab's controller again, so
// a subsequent DeleteSession cannot race a late write. Safe to call after the
// controller's session path has been cleared: the loop's Snapshot becomes a
// no-op and it exits on its next iteration.
func (a *App) quiesceTabAutosave(tab *WorkspaceTab) {
	if tab == nil {
		return
	}
	tab.saveMu.Lock()
	if tab.saveCond == nil {
		// saveCond is lazily initialized on first snapshot; if it was never
		// set there is no loop to wait for.
		tab.closing = true
		tab.saveMu.Unlock()
		return
	}
	tab.closing = true
	for tab.saving {
		tab.saveCond.Wait()
	}
	tab.saveMu.Unlock()
}

func (a *App) tabSnapshotLoop(tab *WorkspaceTab) {
	defer a.recoverToPending("tabSnapshotLoop")
	for {
		var snapshotErr error
		a.mu.RLock()
		ctrl := tab.Ctrl
		a.mu.RUnlock()
		if ctrl != nil {
			if err := a.snapshotTab(tab); err == nil {
				if !a.maybeAutoTitleTopic(tab) {
					a.emitProjectTreeChanged()
				}
			} else {
				snapshotErr = err
			}
		}
		tab.saveMu.Lock()
		if tab.saveCond == nil {
			tab.saveCond = sync.NewCond(&tab.saveMu)
		}
		if snapshotErr == nil {
			tab.saveFailures = 0
		} else {
			tab.saveFailures++
		}
		if tab.closing {
			// Tab is being torn down: stop without picking up saveAgain work.
			tab.saving = false
			tab.saveCond.Broadcast()
			tab.saveMu.Unlock()
			if snapshotErr != nil {
				slog.Warn("desktop: session autosave failed during teardown", "tab", tab.ID, "err", snapshotErr)
			}
			return
		}
		if tab.saveAgain {
			tab.saveAgain = false
			tab.saveMu.Unlock()
			if snapshotErr != nil {
				slog.Warn("desktop: session autosave failed; newer snapshot queued", "tab", tab.ID, "err", snapshotErr)
			}
			continue
		}
		if snapshotErr != nil && tab.saveFailures <= maxTabSnapshotFailureRetries {
			delay := tabSnapshotRetryDelay(tab.saveFailures)
			attempt := tab.saveFailures
			tab.saveMu.Unlock()
			// Retries are routine (transient AV/indexer holds); tell the user
			// only when the whole burst gives up, not once per attempt.
			slog.Warn("desktop: session autosave failed; retrying", "tab", tab.ID, "attempt", attempt, "err", snapshotErr)
			time.Sleep(delay)
			continue
		}
		exhausted := snapshotErr
		tab.saving = false
		tab.saveCond.Broadcast()
		tab.saveMu.Unlock()
		if exhausted != nil {
			a.reportTabSnapshotError(tab, "autosave", exhausted)
		}
		return
	}
}

func (a *App) maybeAutoTitleTopic(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	// Runs on the autosave goroutine; TopicID/Scope/WorkspaceRoot/Ctrl are
	// written under a.mu by session switches and recovery.
	a.mu.RLock()
	topicID := strings.TrimSpace(tab.TopicID)
	titleRoot := tab.WorkspaceRoot
	if tab.Scope == "global" {
		titleRoot = ""
	}
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if topicID == "" || ctrl == nil {
		return false
	}
	if source := loadTopicTitleSource(titleRoot, topicID); source != topicTitleSourceAuto {
		return false
	}
	sessionPath := ctrl.SessionPath()
	if sessionPath == "" {
		return false
	}
	if sessionHasManualDisplayTitle(sessionPath) {
		return false
	}
	nextTitle, updated := autoTitleTopicFromSession(titleRoot, topicID, sessionPath)
	if !updated {
		return false
	}
	a.updateOpenTopicTitle(topicID, nextTitle)
	a.updateTopicSessionTitles(topicID, nextTitle)
	a.emitProjectTreeChanged()
	return true
}

func autoTitleTopicFromSession(workspaceRoot, topicID, sessionPath string) (string, bool) {
	if source := loadTopicTitleSource(workspaceRoot, topicID); source != topicTitleSourceAuto {
		return "", false
	}
	if sessionHasManualDisplayTitle(sessionPath) {
		return "", false
	}
	proposal := autoTopicTitleProposalFromSession(sessionPath)
	if proposal.Title == "" {
		return "", false
	}
	if !shouldApplyAutoTopicTitle(workspaceRoot, topicID, proposal) {
		return "", false
	}
	nextTitle := proposal.Title
	if nextTitle == strings.TrimSpace(loadTopicTitle(workspaceRoot, topicID)) {
		_ = recordTopicAutoTitleMeta(workspaceRoot, topicID, proposal)
		return "", false
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, nextTitle, topicTitleSourceAuto); err != nil {
		return "", false
	}
	_ = recordTopicAutoTitleMeta(workspaceRoot, topicID, proposal)
	return nextTitle, true
}

type autoTopicTitleProposal struct {
	Title     string
	Stage     int
	UserTurns int
	BasisHash string
}

func autoTopicTitleProposalFromSession(path string) autoTopicTitleProposal {
	users := topicTitleUserTurnsFromSession(path)
	if len(users) == 0 {
		return autoTopicTitleProposal{}
	}
	stage := 1
	if len(users) >= 3 {
		stage = 3
	}
	basis := users
	if len(basis) > stage {
		basis = basis[:stage]
	}
	title := topicTitleFromUserTurns(basis)
	if title == "" {
		return autoTopicTitleProposal{}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", stage, strings.Join(basis, "\x00"))))
	return autoTopicTitleProposal{
		Title:     title,
		Stage:     stage,
		UserTurns: len(users),
		BasisHash: hex.EncodeToString(sum[:8]),
	}
}

func shouldApplyAutoTopicTitle(workspaceRoot, topicID string, proposal autoTopicTitleProposal) bool {
	if proposal.Stage <= 0 || proposal.BasisHash == "" {
		return false
	}
	meta := loadTopicAutoTitleMeta(workspaceRoot)[topicID]
	if meta.Stage > proposal.Stage {
		return false
	}
	if meta.Stage == proposal.Stage && meta.BasisHash == proposal.BasisHash {
		return false
	}
	return true
}

func sessionHasManualDisplayTitle(sessionPath string) bool {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return false
	}
	if meta, ok, err := agent.LoadBranchMeta(sessionPath); err == nil && ok {
		if strings.TrimSpace(meta.CustomTitle) != "" {
			return true
		}
	}
	dir := filepath.Dir(sessionPath)
	if dir == "." || dir == string(filepath.Separator) {
		return false
	}
	return strings.TrimSpace(loadSessionTitles(dir)[filepath.Base(sessionPath)]) != ""
}

func topicTitleFallbackForOpen(workspaceRoot, topicID, sessionPath string) (string, string, bool) {
	topicID = strings.TrimSpace(topicID)
	sessionPath = strings.TrimSpace(sessionPath)
	if topicID == "" || sessionPath == "" {
		return "", "", false
	}
	storedTitle := strings.TrimSpace(loadTopicTitle(workspaceRoot, topicID))
	storedSource := strings.TrimSpace(loadTopicTitleSource(workspaceRoot, topicID))
	if storedTitle != "" {
		if storedSource == topicTitleSourceManual || storedTitle != defaultTopicTitle {
			return "", "", false
		}
	}

	if storedTitle == "" {
		dir := filepath.Dir(sessionPath)
		if meta, ok, err := agent.LoadBranchMeta(sessionPath); err == nil && ok {
			if title := storedSessionTopicTitle(dir, sessionPath, meta); title != "" {
				return title, topicTitleSourceManual, true
			}
		} else if title := topicTitleFromText(loadSessionTitles(dir)[filepath.Base(sessionPath)]); title != "" {
			return title, topicTitleSourceManual, true
		}
	}

	if storedSource == topicTitleSourceManual {
		return "", "", false
	}
	if storedSource == "" || storedSource == topicTitleSourceAuto {
		if title := topicTitleFromSession(sessionPath); title != "" {
			return title, topicTitleSourceAuto, true
		}
	}
	return "", "", false
}

func topicTitleFromSession(path string) string {
	users := topicTitleUserTurnsFromSession(path)
	if len(users) == 0 {
		return ""
	}
	return topicTitleFromText(users[0])
}

func topicTitleUserTurnsFromSession(path string) []string {
	// Event-log aware: decoding the .jsonl checkpoint directly would stop
	// seeing user turns after the first save, silently disabling the ≥3-turn
	// title upgrade.
	msgs, err := agent.LoadSessionUserMessages(path)
	if err != nil {
		return nil
	}
	var users []string
	for _, msg := range msgs {
		content := control.StripComposePrefixes(agent.HandoffTask(msg.Text))
		content = control.StripReferencedContextPrefix(content)
		if strings.TrimSpace(content) != "" {
			users = append(users, content)
		}
	}
	return users
}

func topicTitleFromUserTurns(users []string) string {
	type candidate struct {
		title string
		score int
	}
	best := candidate{score: -1}
	for i, text := range users {
		title := topicTitleFromText(text)
		if title == "" || lowSignalTopicTitle(title) {
			continue
		}
		runes := len([]rune(title))
		score := runes
		if score > 24 {
			score = 24
		}
		if i == 0 {
			score += 3
		}
		if runes < 5 {
			score -= 6
		}
		if score > best.score {
			best = candidate{title: title, score: score}
		}
	}
	if best.title != "" {
		return best.title
	}
	if len(users) > 0 {
		return topicTitleFromText(users[0])
	}
	return ""
}

func lowSignalTopicTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	normalized = strings.Trim(normalized, " \t\r\n，。！？；：、,.!?;:\"'`“”‘’()（）[]【】")
	switch normalized {
	case "", "好", "好的", "好啊", "可以", "嗯", "对", "是的", "继续", "继续吧", "采纳建议", "采用建议", "收到", "明白", "ok", "okay", "yes", "yep", "go on", "continue", "thanks", "thank you":
		return true
	default:
		return false
	}
}

func topicTitleFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, " \t\r\n，。！？；：、,.!?;:\"'`“”‘’()（）[]【】")
	if text == "" {
		return ""
	}
	const maxRunes = 18
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = strings.TrimRightFunc(string(runes[:maxRunes]), unicode.IsPunct) + "…"
	}
	if text == defaultTopicTitle {
		return ""
	}
	return text
}

// --- persistence: desktop-projects.json -------------------------------------

const desktopProjectsFile = "desktop-projects.json"
const tabsFileName = "desktop-tabs.json"
const desktopGlobalOrderToken = "__global__"
const legacyProjectSidebarRecoveryMarker = "desktop-projects-legacy-recovered"

var desktopProjectsFileMu sync.Mutex

type desktopProject struct {
	Root         string   `json:"root"`
	Title        string   `json:"title,omitempty"`
	Color        string   `json:"color,omitempty"`
	Topics       []string `json:"topics"` // ordered topic IDs
	PinnedTopics []string `json:"pinnedTopics,omitempty"`
}

type desktopProjectFile struct {
	GlobalTitle        string           `json:"globalTitle,omitempty"`
	GlobalColor        string           `json:"globalColor,omitempty"`
	GlobalTopics       []string         `json:"globalTopics,omitempty"`
	GlobalPinnedTopics []string         `json:"globalPinnedTopics,omitempty"`
	DeletedTopics      []string         `json:"deletedTopics,omitempty"`
	PinnedProjects     []string         `json:"pinnedProjects,omitempty"`
	SidebarOrder       []string         `json:"sidebarOrder,omitempty"`
	Projects           []desktopProject `json:"projects"`
}

type desktopTabEntry struct {
	ID               string  `json:"id"`
	Scope            string  `json:"scope"`
	WorkspaceRoot    string  `json:"workspaceRoot"`
	TopicID          string  `json:"topicId"`
	SessionPath      string  `json:"sessionPath,omitempty"`
	ReadOnly         bool    `json:"readOnly,omitempty"`
	Model            string  `json:"model,omitempty"`
	Effort           *string `json:"effort,omitempty"`
	TokenMode        string  `json:"tokenMode,omitempty"`
	Mode             string  `json:"mode,omitempty"`
	Goal             string  `json:"goal,omitempty"`
	ToolApprovalMode string  `json:"toolApprovalMode,omitempty"`
}

type desktopTabsFile struct {
	Tabs      []desktopTabEntry `json:"tabs"`
	ActiveTab string            `json:"activeTab"`
}

func singleSurfaceLayoutStyle(style string) bool {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "workbench", "creation":
		return true
	default:
		return false
	}
}

func singleSurfaceTabsFile(f desktopTabsFile) desktopTabsFile {
	if len(f.Tabs) <= 1 {
		return f
	}
	chosen := f.Tabs[0]
	if active := strings.TrimSpace(f.ActiveTab); active != "" {
		for _, entry := range f.Tabs {
			if entry.ID == active {
				chosen = entry
				break
			}
		}
	}
	return desktopTabsFile{Tabs: []desktopTabEntry{chosen}, ActiveTab: chosen.ID}
}

func desktopConfigDir() string {
	return config.ReasonixHomeDir()
}

func (a *App) saveTabsLocked() {
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.saveTabsWrite(dir, entries, activeID, version)
}

// saveTabsCollectLocked gathers the tab-snapshot data under the caller's lock
// (it calls orderedTabIDsLocked which requires a.mu). Returns the config dir,
// the serializable entries, the active tab ID, and a monotonic snapshot version.
// The write can happen outside the lock to avoid blocking the UI with disk I/O.
func (a *App) saveTabsCollectLocked() (string, []desktopTabEntry, string, uint64) {
	dir := desktopConfigDir()
	var entries []desktopTabEntry
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			entries = append(entries, desktopTabEntry{
				ID:               tab.ID,
				Scope:            tab.Scope,
				WorkspaceRoot:    tab.WorkspaceRoot,
				TopicID:          tab.TopicID,
				SessionPath:      tab.currentSessionPath(),
				ReadOnly:         tab.ReadOnly,
				Model:            tab.model,
				Effort:           cloneStringPtr(tab.effort),
				TokenMode:        persistedTabTokenMode(currentTabTokenMode(tab)),
				Mode:             persistedTabMode(currentTabMode(tab)),
				Goal:             persistedTabGoal(tab),
				ToolApprovalMode: persistedToolApprovalMode(currentTabToolApprovalMode(tab)),
			})
		}
	}
	a.tabsSaveVersion++
	return dir, entries, a.activeTabID, a.tabsSaveVersion
}

// saveTabsWrite writes the tab-snapshot to disk. It does not require a.mu, but
// writes must be serialized because every save uses the same destination and
// fixed .tmp path.
func (a *App) saveTabsWrite(dir string, entries []desktopTabEntry, activeID string, version uint64) {
	a.tabsSaveMu.Lock()
	defer a.tabsSaveMu.Unlock()
	if version < a.tabsLastWrittenVersion {
		return
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f := desktopTabsFile{Tabs: entries, ActiveTab: activeID}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(dir, tabsFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	if err := fileutil.ReplaceFile(tmp, path); err != nil {
		return
	}
	a.tabsLastWrittenVersion = version
}

func (a *App) orderedTabIDsLocked() []string {
	ordered, needsRepair := a.orderedTabIDsSnapshotLocked()
	if needsRepair {
		a.tabOrder = append([]string(nil), ordered...)
	}
	return ordered
}

func (a *App) orderedTabIDsSnapshotLocked() ([]string, bool) {
	seen := make(map[string]bool, len(a.tabs))
	ordered := make([]string, 0, len(a.tabs))
	for _, id := range a.tabOrder {
		if _, ok := a.tabs[id]; ok && !seen[id] {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	var missing []string
	for id := range a.tabs {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	ordered = append(ordered, missing...)
	return ordered, len(ordered) != len(a.tabOrder) || len(missing) > 0
}

func (a *App) removeTabOrderLocked(tabID string) {
	next := a.tabOrder[:0]
	for _, id := range a.tabOrder {
		if id != tabID {
			next = append(next, id)
		}
	}
	a.tabOrder = next
}

func loadTabsFile() desktopTabsFile {
	path := filepath.Join(desktopConfigDir(), tabsFileName)
	b, err := os.ReadFile(path)
	if err != nil {
		return desktopTabsFile{}
	}
	var f desktopTabsFile
	_ = json.Unmarshal(b, &f)
	return f
}

func desktopMCPMigrationRoots(tabs desktopTabsFile) []string {
	seen := map[string]bool{}
	var roots []string
	add := func(root string) {
		root = normalizeProjectRoot(root)
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		roots = append(roots, root)
	}
	if cur := loadWorkspace(); cur != "" {
		add(cur)
	}
	for _, root := range loadWorkspaces() {
		add(root)
	}
	for _, entry := range tabs.Tabs {
		if entry.Scope == "project" {
			add(entry.WorkspaceRoot)
		}
	}
	for _, project := range loadProjectsFile().Projects {
		add(project.Root)
	}
	return roots
}

func recoverLegacyProjectSidebarRoots(tabs desktopTabsFile) (bool, error) {
	markerPath := filepath.Join(desktopConfigDir(), legacyProjectSidebarRecoveryMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return false, nil
	}

	changed := false
	err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		seen := map[string]bool{}
		for _, project := range f.Projects {
			root := normalizeProjectRoot(project.Root)
			if root != "" {
				seen[root] = true
			}
		}

		add := func(root string) {
			root = normalizeProjectRoot(root)
			if root == "" || seen[root] || !existingDirectory(root) {
				return
			}
			seen[root] = true
			f.Projects = append(f.Projects, desktopProject{Root: root})
			changed = true
		}
		if cur := loadWorkspace(); cur != "" {
			add(cur)
		}
		for _, root := range loadWorkspaces() {
			add(root)
		}
		for _, entry := range tabs.Tabs {
			if entry.Scope == "project" {
				add(entry.WorkspaceRoot)
			}
		}
		return changed, nil
	})
	if err != nil {
		return false, err
	}
	return changed, writeLegacyProjectSidebarRecoveryMarker(markerPath)
}

func existingDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func writeLegacyProjectSidebarRecoveryMarker(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("ok\n"), 0o644)
}

func loadProjectsFile() desktopProjectFile {
	path := filepath.Join(desktopConfigDir(), desktopProjectsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return desktopProjectFile{}
	}
	var f desktopProjectFile
	_ = json.Unmarshal(b, &f)
	return normalizeProjectsFile(f)
}

func saveProjectsFile(f desktopProjectFile) error {
	dir := desktopConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f = normalizeProjectsFile(f)
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, desktopProjectsFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func updateProjectsFile(mutator func(*desktopProjectFile) (bool, error)) error {
	desktopProjectsFileMu.Lock()
	defer desktopProjectsFileMu.Unlock()

	f := loadProjectsFile()
	changed, err := mutator(&f)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return saveProjectsFile(f)
}

func prependTopicInProjectsFile(workspaceRoot, topicID string, ensureProject bool) error {
	// Single-topic prepends are intentional writes (topic creation, a live tab
	// indexing its session, restore from trash): they clear any delete
	// tombstone so the topic fully returns instead of landing in a half-state
	// where only its title resurfaces.
	return prependTopicsInProjectsFileOpts(workspaceRoot, []string{topicID}, ensureProject, false)
}

func prependTopicsInProjectsFile(workspaceRoot string, topicIDs []string, ensureProject bool) error {
	// Batch prepends come from the legacy migration and index-repair scans:
	// they must respect delete tombstones so a scan never resurrects a topic
	// the user removed.
	return prependTopicsInProjectsFileOpts(workspaceRoot, topicIDs, ensureProject, true)
}

func prependTopicsInProjectsFileOpts(workspaceRoot string, topicIDs []string, ensureProject, respectTombstones bool) error {
	workspaceRoot = normalizeProjectRoot(workspaceRoot)
	topicIDs = uniqueStrings(topicIDs)
	if len(topicIDs) == 0 {
		return nil
	}
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		// Tombstones are checked under the projects-file lock: a DeleteTopic
		// that lands between a scan reading DeletedTopics and this write must
		// not be resurrected by the stale batch.
		live := topicIDs
		changed := false
		if respectTombstones {
			live = make([]string, 0, len(topicIDs))
			for _, id := range topicIDs {
				if !containsDesktopString(f.DeletedTopics, id) {
					live = append(live, id)
				}
			}
			if len(live) == 0 {
				return false, nil
			}
		} else {
			for _, id := range topicIDs {
				if next := removeString(f.DeletedTopics, id); !sameStringList(next, f.DeletedTopics) {
					f.DeletedTopics = next
					changed = true
				}
			}
		}
		if workspaceRoot == "" {
			next := uniqueStrings(append(append([]string(nil), live...), f.GlobalTopics...))
			if sameStringList(next, f.GlobalTopics) {
				return changed, nil
			}
			f.GlobalTopics = next
			return true, nil
		}
		for i, p := range f.Projects {
			if p.Root != workspaceRoot {
				continue
			}
			next := uniqueStrings(append(append([]string(nil), live...), p.Topics...))
			if sameStringList(next, p.Topics) {
				return changed, nil
			}
			f.Projects[i].Topics = next
			return true, nil
		}
		if !ensureProject {
			return changed, nil
		}
		f.Projects = append(f.Projects, desktopProject{Root: workspaceRoot, Topics: live})
		return true, nil
	})
}

func removeTopicFromProjectsFile(topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		changed := false
		if next := removeString(f.GlobalTopics, topicID); !sameStringList(next, f.GlobalTopics) {
			f.GlobalTopics = next
			changed = true
		}
		if next := removeString(f.GlobalPinnedTopics, topicID); !sameStringList(next, f.GlobalPinnedTopics) {
			f.GlobalPinnedTopics = next
			changed = true
		}
		if next := prependUniqueString(f.DeletedTopics, topicID); !sameStringList(next, f.DeletedTopics) {
			f.DeletedTopics = next
			changed = true
		}
		for i, p := range f.Projects {
			if next := removeString(p.Topics, topicID); !sameStringList(next, p.Topics) {
				f.Projects[i].Topics = next
				changed = true
			}
			if next := removeString(p.PinnedTopics, topicID); !sameStringList(next, p.PinnedTopics) {
				f.Projects[i].PinnedTopics = next
				changed = true
			}
		}
		return changed, nil
	})
}

func normalizeProjectRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

func normalizeProjectsFile(f desktopProjectFile) desktopProjectFile {
	out := desktopProjectFile{
		GlobalTitle:        strings.TrimSpace(f.GlobalTitle),
		GlobalColor:        normalizeProjectColor(f.GlobalColor),
		GlobalTopics:       uniqueStrings(f.GlobalTopics),
		GlobalPinnedTopics: uniqueStrings(f.GlobalPinnedTopics),
		DeletedTopics:      uniqueStrings(f.DeletedTopics),
	}
	index := map[string]int{}
	for _, p := range f.Projects {
		root := normalizeProjectRoot(p.Root)
		if root == "" {
			continue
		}
		p.Root = root
		p.Title = strings.TrimSpace(p.Title)
		p.Color = normalizeProjectColor(p.Color)
		p.Topics = uniqueStrings(p.Topics)
		p.PinnedTopics = uniqueStrings(p.PinnedTopics)
		if i, ok := index[root]; ok {
			if out.Projects[i].Title == "" && p.Title != "" {
				out.Projects[i].Title = p.Title
			}
			if out.Projects[i].Color == "" && p.Color != "" {
				out.Projects[i].Color = p.Color
			}
			out.Projects[i].Topics = uniqueStrings(append(out.Projects[i].Topics, p.Topics...))
			out.Projects[i].PinnedTopics = uniqueStrings(append(out.Projects[i].PinnedTopics, p.PinnedTopics...))
			continue
		}
		index[root] = len(out.Projects)
		out.Projects = append(out.Projects, p)
	}
	projectRoots := make(map[string]bool, len(out.Projects))
	for _, project := range out.Projects {
		projectRoots[project.Root] = true
	}
	for _, root := range uniqueStrings(f.PinnedProjects) {
		root = normalizeProjectRoot(root)
		if root != "" && projectRoots[root] && !containsDesktopString(out.PinnedProjects, root) {
			out.PinnedProjects = append(out.PinnedProjects, root)
		}
	}
	out.SidebarOrder = normalizeSidebarOrder(f.SidebarOrder, out.Projects)
	return out
}

func normalizeSidebarOrder(order []string, projects []desktopProject) []string {
	projectRoots := make(map[string]bool, len(projects))
	for _, project := range projects {
		if project.Root != "" {
			projectRoots[project.Root] = true
		}
	}
	seen := make(map[string]bool, len(order))
	out := make([]string, 0, len(order))
	for _, value := range order {
		value = strings.TrimSpace(value)
		if value == desktopGlobalOrderToken {
			if !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
			continue
		}
		root := normalizeProjectRoot(value)
		if root == "" || !projectRoots[root] || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	return out
}

func sameProjectOrder(a, b []desktopProject) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Root != b[i].Root {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func prependUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return uniqueStrings(values)
	}
	return uniqueStrings(append([]string{value}, values...))
}

func removeString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return uniqueStrings(values)
	}
	out := make([]string, 0, len(values))
	for _, item := range uniqueStrings(values) {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

func containsDesktopString(values []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range uniqueStrings(values) {
		if item == value {
			return true
		}
	}
	return false
}

func pinnedTopicIDs(topicIDs []string, pinned []string) []string {
	if len(topicIDs) == 0 || len(pinned) == 0 {
		return topicIDs
	}
	available := make(map[string]bool, len(topicIDs))
	for _, tid := range topicIDs {
		available[tid] = true
	}
	out := make([]string, 0, len(topicIDs))
	seen := make(map[string]bool, len(topicIDs))
	for _, tid := range uniqueStrings(pinned) {
		if available[tid] && !seen[tid] {
			out = append(out, tid)
			seen[tid] = true
		}
	}
	for _, tid := range topicIDs {
		if !seen[tid] {
			out = append(out, tid)
		}
	}
	return out
}

func orderedTopicIDs(explicit []string, titleMap map[string]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(explicit)+len(titleMap))
	for _, tid := range explicit {
		tid = strings.TrimSpace(tid)
		if tid == "" || seen[tid] {
			continue
		}
		seen[tid] = true
		out = append(out, tid)
	}
	var remaining []string
	for tid := range titleMap {
		if !seen[tid] {
			remaining = append(remaining, tid)
		}
	}
	sort.Strings(remaining)
	return append(out, remaining...)
}

func projectTreeOrderKey(node ProjectNode) string {
	switch node.Kind {
	case "global_folder":
		return desktopGlobalOrderToken
	case "project":
		return normalizeProjectRoot(node.Root)
	default:
		return ""
	}
}

func applyProjectTreeOrder(nodes []ProjectNode, order []string) []ProjectNode {
	if len(order) == 0 {
		return nodes
	}
	byKey := make(map[string]ProjectNode, len(nodes))
	for _, node := range nodes {
		key := projectTreeOrderKey(node)
		if key != "" {
			byKey[key] = node
		}
	}
	seen := make(map[string]bool, len(nodes))
	out := make([]ProjectNode, 0, len(nodes))
	for _, value := range order {
		key := strings.TrimSpace(value)
		if key != desktopGlobalOrderToken {
			key = normalizeProjectRoot(key)
		}
		if key == "" || seen[key] {
			continue
		}
		node, ok := byKey[key]
		if !ok {
			continue
		}
		seen[key] = true
		out = append(out, node)
	}
	for _, node := range nodes {
		key := projectTreeOrderKey(node)
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		out = append(out, node)
	}
	return out
}

func applyPinnedProjectOrder(nodes []ProjectNode, pinnedRoots []string) []ProjectNode {
	pinnedRoots = uniqueStrings(pinnedRoots)
	if len(pinnedRoots) == 0 {
		return nodes
	}
	byRoot := make(map[string]ProjectNode, len(nodes))
	for _, node := range nodes {
		if node.Kind == "project" && node.Root != "" {
			byRoot[normalizeProjectRoot(node.Root)] = node
		}
	}
	seen := make(map[string]bool, len(pinnedRoots))
	out := make([]ProjectNode, 0, len(nodes))
	for _, root := range pinnedRoots {
		root = normalizeProjectRoot(root)
		node, ok := byRoot[root]
		if !ok || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, node)
	}
	for _, node := range nodes {
		if node.Kind == "project" && node.Root != "" && seen[normalizeProjectRoot(node.Root)] {
			continue
		}
		out = append(out, node)
	}
	return out
}

func projectDisplayName(p desktopProject) string {
	if title := strings.TrimSpace(p.Title); title != "" {
		return title
	}
	return workspaceName(p.Root)
}

func normalizeProjectColor(color string) string {
	switch strings.TrimSpace(strings.ToLower(color)) {
	case "red", "orange", "amber", "green", "teal", "blue", "purple", "pink":
		return strings.TrimSpace(strings.ToLower(color))
	default:
		return ""
	}
}

func projectColor(root string) string {
	root = normalizeProjectRoot(root)
	if root == "" {
		return globalProjectColor()
	}
	for _, p := range loadProjectsFile().Projects {
		if p.Root == root {
			return normalizeProjectColor(p.Color)
		}
	}
	return ""
}

func globalProjectColor() string {
	return normalizeProjectColor(loadProjectsFile().GlobalColor)
}

func globalProjectTitle() string {
	if title := strings.TrimSpace(loadProjectsFile().GlobalTitle); title != "" {
		return title
	}
	return "Global"
}

func addProject(root, title string) error {
	root = normalizeProjectRoot(root)
	if root == "" {
		return fmt.Errorf("project root is required")
	}
	title = strings.TrimSpace(title)
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		for i, p := range f.Projects {
			if p.Root == root {
				if title == "" || f.Projects[i].Title == title {
					return false, nil
				}
				f.Projects[i].Title = title
				return true, nil
			}
		}
		f.Projects = append(f.Projects, desktopProject{Root: root, Title: title})
		return true, nil
	})
}

func renameProject(root, title string) error {
	title = strings.TrimSpace(title)
	root = normalizeProjectRoot(root)
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		if root == "" {
			if f.GlobalTitle == title {
				return false, nil
			}
			f.GlobalTitle = title
			return true, nil
		}
		for i, p := range f.Projects {
			if p.Root == root {
				if f.Projects[i].Title == title {
					return false, nil
				}
				f.Projects[i].Title = title
				return true, nil
			}
		}
		f.Projects = append(f.Projects, desktopProject{Root: root, Title: title})
		return true, nil
	})
}

func setProjectColor(root, color string) error {
	root = normalizeProjectRoot(root)
	color = normalizeProjectColor(color)
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		if root == "" {
			if f.GlobalColor == color {
				return false, nil
			}
			f.GlobalColor = color
			return true, nil
		}
		for i, p := range f.Projects {
			if p.Root == root {
				if f.Projects[i].Color == color {
					return false, nil
				}
				f.Projects[i].Color = color
				return true, nil
			}
		}
		f.Projects = append(f.Projects, desktopProject{Root: root, Color: color})
		return true, nil
	})
}

func removeProject(root string) error {
	root = normalizeProjectRoot(root)
	return updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		projects := make([]desktopProject, 0, len(f.Projects))
		for _, p := range f.Projects {
			if p.Root != root {
				projects = append(projects, p)
			}
		}
		if len(projects) == len(f.Projects) {
			return false, nil
		}
		f.Projects = projects
		return true, nil
	})
}

// --- topic helpers ----------------------------------------------------------

const (
	topicTitlesFile        = "desktop-topic-titles.json"
	topicTitleSourcesFile  = "desktop-topic-title-sources.json"
	topicCreatedAtsFile    = "desktop-topic-created-at.json"
	topicAutoTitlesFile    = "desktop-topic-auto-title-meta.json"
	defaultTopicTitle      = "新的会话"
	topicTitleSourceAuto   = "auto"
	topicTitleSourceManual = "manual"
)

func topicTitlesPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitlesFile)
}

func topicTitleSourcesPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicTitleSourcesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitleSourcesFile)
}

func topicCreatedAtsPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicCreatedAtsFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicCreatedAtsFile)
}

func topicAutoTitleMetaPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicAutoTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicAutoTitlesFile)
}

const topicFileReadTimeout = 200 * time.Millisecond

var readFileWithTimeoutSlots = make(chan struct{}, 16)

func readFileWithTimeout(path string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		return os.ReadFile(path)
	}
	select {
	case readFileWithTimeoutSlots <- struct{}{}:
	default:
		return nil, fmt.Errorf("too many pending file reads")
	}
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := os.ReadFile(path)
		<-readFileWithTimeoutSlots
		ch <- result{data: data, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-timer.C:
		return nil, fmt.Errorf("timed out after %v reading %s", timeout, filepath.Base(path))
	}
}

func loadTopicTitles(workspaceRoot string) map[string]string {
	m := map[string]string{}
	b, err := readFileWithTimeout(topicTitlesPath(workspaceRoot), topicFileReadTimeout)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func loadTopicTitleSources(workspaceRoot string) map[string]string {
	m := map[string]string{}
	b, err := readFileWithTimeout(topicTitleSourcesPath(workspaceRoot), topicFileReadTimeout)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func loadTopicCreatedAts(workspaceRoot string) map[string]int64 {
	m := map[string]int64{}
	b, err := readFileWithTimeout(topicCreatedAtsPath(workspaceRoot), topicFileReadTimeout)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

type topicAutoTitleMeta struct {
	Stage     int    `json:"stage,omitempty"`
	UserTurns int    `json:"userTurns,omitempty"`
	BasisHash string `json:"basisHash,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

func loadTopicAutoTitleMeta(workspaceRoot string) map[string]topicAutoTitleMeta {
	m := map[string]topicAutoTitleMeta{}
	b, err := readFileWithTimeout(topicAutoTitleMetaPath(workspaceRoot), topicFileReadTimeout)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func loadStringMapForUpdate(path string) (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return map[string]string{}, nil
	}
	return m, nil
}

func loadTopicAutoTitleMetaForUpdate(workspaceRoot string) (map[string]topicAutoTitleMeta, error) {
	m := map[string]topicAutoTitleMeta{}
	path := topicAutoTitleMetaPath(workspaceRoot)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return map[string]topicAutoTitleMeta{}, nil
	}
	return m, nil
}

func loadInt64MapForUpdate(path string) (map[string]int64, error) {
	m := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return map[string]int64{}, nil
	}
	return m, nil
}

func loadTopicTitlesForUpdate(workspaceRoot string) (map[string]string, error) {
	return loadStringMapForUpdate(topicTitlesPath(workspaceRoot))
}

func loadTopicTitleSourcesForUpdate(workspaceRoot string) (map[string]string, error) {
	return loadStringMapForUpdate(topicTitleSourcesPath(workspaceRoot))
}

func loadTopicCreatedAtsForUpdate(workspaceRoot string) (map[string]int64, error) {
	return loadInt64MapForUpdate(topicCreatedAtsPath(workspaceRoot))
}

func saveTopicTitles(workspaceRoot string, m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := topicTitlesPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func saveTopicTitleSources(workspaceRoot string, m map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := topicTitleSourcesPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func saveTopicCreatedAts(workspaceRoot string, m map[string]int64) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := topicCreatedAtsPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func saveTopicAutoTitleMeta(workspaceRoot string, m map[string]topicAutoTitleMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := topicAutoTitleMetaPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func loadTopicTitle(workspaceRoot, topicID string) string {
	return loadTopicTitles(workspaceRoot)[topicID]
}

func loadTopicTitleSource(workspaceRoot, topicID string) string {
	return loadTopicTitleSources(workspaceRoot)[topicID]
}

func loadTopicCreatedAt(workspaceRoot, topicID string) int64 {
	return loadTopicCreatedAts(workspaceRoot)[topicID]
}

func topicIDCreatedAt(topicID string) int64 {
	topicID = strings.TrimSpace(topicID)
	for _, prefix := range []string{"topic_", "legacy_"} {
		if !strings.HasPrefix(topicID, prefix) {
			continue
		}
		stamp := strings.TrimPrefix(topicID, prefix)
		if len(stamp) < len("20060102-150405") {
			continue
		}
		stamp = stamp[:len("20060102-150405")]
		t, err := time.ParseInLocation("20060102-150405", stamp, time.UTC)
		if err != nil {
			continue
		}
		return t.UnixMilli()
	}
	return 0
}

func topicCreatedAtForTree(createdAts map[string]int64, topicID string) int64 {
	if createdAt := createdAts[topicID]; createdAt > 0 {
		return createdAt
	}
	return topicIDCreatedAt(topicID)
}

func topicTitleForTab(scope, workspaceRoot, topicID string) string {
	titleRoot := topicTitleRoot(scope, workspaceRoot)
	if title := strings.TrimSpace(loadTopicTitle(titleRoot, topicID)); title != "" {
		return title
	}
	if scope == "global" {
		return "Global"
	}
	return defaultTopicTitle
}

func topicTitleRoot(scope, workspaceRoot string) string {
	if scope == "global" {
		return ""
	}
	return workspaceRoot
}

func forkTopicTitle(title string) string {
	base := strings.TrimSpace(title)
	if base == "" || base == defaultTopicTitle || base == "Global" {
		return "分叉会话"
	}
	if strings.HasSuffix(base, " · 分叉") {
		return base
	}
	return base + " · 分叉"
}

type sessionRecoveryEvent struct {
	OriginalPath     string `json:"originalPath,omitempty"`
	RecoveryPath     string `json:"recoveryPath"`
	Scope            string `json:"scope,omitempty"`
	WorkspaceRoot    string `json:"workspaceRoot,omitempty"`
	TopicID          string `json:"topicId,omitempty"`
	TopicTitle       string `json:"topicTitle,omitempty"`
	RecoveryReason   string `json:"recoveryReason,omitempty"`
	RecoveryDigest   string `json:"recoveryDigest,omitempty"`
	RecoveryParentID string `json:"recoveryParentId,omitempty"`
	Existing         bool   `json:"existing,omitempty"`
}

type sessionRecoveryFailedEvent struct {
	Reason string `json:"reason,omitempty"`
}

func (a *App) tabSessionRecoveryMeta(tab *WorkspaceTab) func(control.SessionRecoveryRequest) agent.BranchMeta {
	return func(req control.SessionRecoveryRequest) agent.BranchMeta {
		if tab == nil {
			return agent.BranchMeta{Name: agent.RecoveryBranchDefaultName}
		}
		// This runs on the snapshot-recovery path, which can fire from the
		// controller's autosave goroutine; snapshot the tab fields under a.mu so
		// we don't read them mid-mutation. Recovery callbacks never hold a.mu, so
		// taking it here can't deadlock. Controller reads happen off-lock.
		a.mu.RLock()
		ctrl := tab.Ctrl
		scope := strings.TrimSpace(tab.Scope)
		workspaceRoot := strings.TrimSpace(tab.WorkspaceRoot)
		topicID := tab.TopicID
		topicTitle := tab.TopicTitle
		model := strings.TrimSpace(tab.model)
		tokenMode := persistedTabTokenMode(boot.NormalizeTokenMode(tab.tokenMode))
		mode := normalizeTabMode(tab.mode)
		toolApprovalMode := normalizeToolApprovalMode(tab.toolApprovalMode)
		goal := strings.TrimSpace(tab.goal)
		a.mu.RUnlock()
		if ctrl != nil {
			mode = tabModeFromAxes(ctrl.PlanMode(), ctrl.AutoApproveTools())
			toolApprovalMode = normalizeToolApprovalMode(ctrl.ToolApprovalMode())
			if g := strings.TrimSpace(ctrl.Goal()); g != "" && ctrl.GoalStatus() == control.GoalStatusRunning {
				goal = g
			} else {
				goal = ""
			}
		}
		if scope != "project" {
			scope = "global"
		}
		if scope == "global" {
			workspaceRoot = ""
		}
		return agent.BranchMeta{
			Name:             agent.RecoveryBranchDefaultName,
			Scope:            scope,
			WorkspaceRoot:    workspaceRoot,
			TopicID:          topicID,
			TopicTitle:       topicTitle,
			Model:            model,
			TokenMode:        tokenMode,
			Mode:             persistedTabMode(mode),
			ToolApprovalMode: persistedToolApprovalMode(toolApprovalMode),
			Goal:             goal,
		}
	}
}

func (a *App) handleTabSessionRecovered(tab *WorkspaceTab) func(control.SessionRecoveryInfo) error {
	return func(info control.SessionRecoveryInfo) error {
		if strings.TrimSpace(info.RecoveryPath) == "" {
			return nil
		}
		if tab != nil && !tab.ReadOnly {
			if err := tab.ensureSessionLease(info.RecoveryPath); err != nil {
				slog.Warn("desktop: acquire recovery session lease", "path", info.RecoveryPath, "err", err)
				reason := "lease_unavailable"
				if errors.Is(err, agent.ErrSessionLeaseHeld) {
					reason = "lease_held"
				}
				a.emitRuntimeEvent("session:recovery-failed", sessionRecoveryFailedEvent{Reason: reason})
				// This error propagates out through ctrl.Snapshot() into chat
				// notices and bridge returns (reportTabSnapshotError). The raw
				// path/holder id stayed in the slog line above; keep it out of
				// the surfaced message.
				return fmt.Errorf("acquire recovery session lease: %w",
					userFacingSessionLeaseError("", err))
			}
		}
		meta := info.Meta
		scope := strings.TrimSpace(meta.Scope)
		if scope != "project" {
			scope = "global"
		}
		workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
		if scope == "global" {
			workspaceRoot = ""
		}
		invalidateTopicSessionIndexForPath(info.RecoveryPath)
		a.mu.Lock()
		if tab != nil && !tab.removed {
			oldKey := sessionRuntimeKey(info.OriginalPath)
			newKey := sessionRuntimeKey(info.RecoveryPath)
			if oldKey != "" && newKey != "" && a.detachedSessions[oldKey] == tab {
				delete(a.detachedSessions, oldKey)
				a.ensureDetachedSessionsLocked()
				a.detachedSessions[newKey] = tab
			}
			tab.SessionPath = canonicalTabSessionPath(info.RecoveryPath)
			if a.tabs[tab.ID] == tab {
				a.saveTabsLocked()
			}
		}
		a.mu.Unlock()
		// The fork continues the same conversation, so its cost history moves
		// with it: re-key the in-memory telemetry from the original session to
		// the recovery path and persist the sidecar right away. Without this
		// the fork's sidecar stays empty until the next usage event (cost
		// shows 0 after a restart), and the sync-by-key reload would wipe the
		// carried totals as "another session's" numbers.
		if tab != nil && !tab.removed {
			origKey := sessionRuntimeKey(info.OriginalPath)
			newKey := sessionRuntimeKey(info.RecoveryPath)
			carried := false
			if newKey != "" {
				tab.telemMu.Lock()
				if tab.telemetrySessionKey == origKey || tab.telemetrySessionKey == "" {
					tab.telemetrySessionKey = newKey
					carried = true
				}
				tab.telemMu.Unlock()
			}
			if carried {
				_ = saveTelemetry(info.RecoveryPath+".telemetry.json", tab.telemetrySnapshot())
			}
		}
		a.emitProjectTreeChanged()
		a.emitRuntimeEvent("session:recovered", sessionRecoveryEvent{
			OriginalPath:     info.OriginalPath,
			RecoveryPath:     info.RecoveryPath,
			Scope:            scope,
			WorkspaceRoot:    workspaceRoot,
			TopicID:          meta.TopicID,
			TopicTitle:       meta.TopicTitle,
			RecoveryReason:   meta.RecoveryReason,
			RecoveryDigest:   meta.RecoveryDigest,
			RecoveryParentID: string(meta.ParentID),
			Existing:         info.Existing,
		})
		a.invalidatePromptHistoryCache()
		return nil
	}
}

func setTopicTitle(workspaceRoot, topicID, title string) error {
	return setTopicTitleWithSource(workspaceRoot, topicID, title, topicTitleSourceManual)
}

func setTopicTitleWithSource(workspaceRoot, topicID, title, source string) error {
	m, err := loadTopicTitlesForUpdate(workspaceRoot)
	if err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		delete(m, topicID)
	} else {
		m[topicID] = strings.TrimSpace(title)
	}
	if err := saveTopicTitles(workspaceRoot, m); err != nil {
		return err
	}

	sources, err := loadTopicTitleSourcesForUpdate(workspaceRoot)
	if err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" || strings.TrimSpace(source) == "" {
		delete(sources, topicID)
	} else {
		sources[topicID] = strings.TrimSpace(source)
	}
	if err := saveTopicTitleSources(workspaceRoot, sources); err != nil {
		return err
	}
	if strings.TrimSpace(source) == topicTitleSourceManual ||
		(strings.TrimSpace(source) == topicTitleSourceAuto && strings.TrimSpace(title) == defaultTopicTitle) {
		_ = deleteTopicAutoTitleMeta(workspaceRoot, topicID)
	}
	return nil
}

func recordTopicAutoTitleMeta(workspaceRoot, topicID string, proposal autoTopicTitleProposal) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || proposal.Stage <= 0 || proposal.BasisHash == "" {
		return nil
	}
	m, err := loadTopicAutoTitleMetaForUpdate(workspaceRoot)
	if err != nil {
		return err
	}
	m[topicID] = topicAutoTitleMeta{
		Stage:     proposal.Stage,
		UserTurns: proposal.UserTurns,
		BasisHash: proposal.BasisHash,
		UpdatedAt: time.Now().UnixMilli(),
	}
	return saveTopicAutoTitleMeta(workspaceRoot, m)
}

func deleteTopicAutoTitleMeta(workspaceRoot, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	m, err := loadTopicAutoTitleMetaForUpdate(workspaceRoot)
	if err != nil {
		return err
	}
	if _, ok := m[topicID]; !ok {
		return nil
	}
	delete(m, topicID)
	return saveTopicAutoTitleMeta(workspaceRoot, m)
}

func setTopicCreatedAt(workspaceRoot, topicID string, createdAt int64) error {
	created, err := loadTopicCreatedAtsForUpdate(workspaceRoot)
	if err != nil {
		return err
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || createdAt <= 0 {
		delete(created, topicID)
	} else {
		created[topicID] = createdAt
	}
	return saveTopicCreatedAts(workspaceRoot, created)
}

func deleteTopicCreatedAt(workspaceRoot, topicID string) {
	created, err := loadTopicCreatedAtsForUpdate(workspaceRoot)
	if err != nil {
		return
	}
	delete(created, topicID)
	_ = saveTopicCreatedAts(workspaceRoot, created)
}

// topicIndexMu serializes recovery writes to desktop-projects.json and topic
// title indexes. Startup builds restored tabs concurrently, and each tab may
// repair its missing index.
var topicIndexMu sync.Mutex

func ensureTopicIndexed(scope, workspaceRoot, topicID, title, source string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("topicID is required")
	}
	topicIndexMu.Lock()
	defer topicIndexMu.Unlock()
	if strings.TrimSpace(scope) == "global" {
		workspaceRoot = ""
	} else {
		workspaceRoot = normalizeProjectRoot(workspaceRoot)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultTopicTitle
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = topicTitleSourceManual
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, title, source); err != nil {
		return err
	}
	return prependTopicInProjectsFile(workspaceRoot, topicID, true)
}

// --- telemetry --------------------------------------------------------------

func saveTelemetry(path string, snapshot tabTelemetrySnapshot) error {
	if snapshot.Version == 0 {
		snapshot.Version = 2
	}
	if snapshot.ReadFiles == nil {
		snapshot.ReadFiles = []readFileRecord{}
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func loadTelemetry(path string) tabTelemetrySnapshot {
	b, err := os.ReadFile(path)
	if err != nil {
		return tabTelemetrySnapshot{Version: 2, ReadFiles: []readFileRecord{}}
	}
	var snapshot tabTelemetrySnapshot
	if err := json.Unmarshal(b, &snapshot); err == nil && (snapshot.Version > 0 || snapshot.ReadFiles != nil) {
		if snapshot.ReadFiles == nil {
			snapshot.ReadFiles = []readFileRecord{}
		}
		if snapshot.Usage.SessionCost == 0 && snapshot.Usage.SessionCostUsd > 0 {
			snapshot.Usage.SessionCost = snapshot.Usage.SessionCostUsd
		}
		return snapshot
	}
	var records []readFileRecord
	if err := json.Unmarshal(b, &records); err != nil || records == nil {
		records = []readFileRecord{}
	}
	return tabTelemetrySnapshot{Version: 1, ReadFiles: records}
}

// --- project tree -----------------------------------------------------------

// ProjectNode is one node in the sidebar project tree (a project folder or a
// topic leaf).
type ProjectNode struct {
	Key              string        `json:"key"`  // stable key for React
	Kind             string        `json:"kind"` // "project" | "topic" | "session" | "global_folder" | "global_topic" | "global_session"
	Label            string        `json:"label"`
	Root             string        `json:"root,omitempty"` // project workspace root
	TopicID          string        `json:"topicId,omitempty"`
	SessionPath      string        `json:"sessionPath,omitempty"`
	ProjectColor     string        `json:"projectColor,omitempty"`
	Turns            int           `json:"turns,omitempty"`
	CreatedAt        int64         `json:"createdAt,omitempty"`
	LastActivityAt   int64         `json:"lastActivityAt,omitempty"`
	Open             bool          `json:"open,omitempty"`
	Running          bool          `json:"running,omitempty"`
	Status           string        `json:"status,omitempty"`
	Pinned           bool          `json:"pinned,omitempty"`
	Recovered        bool          `json:"recovered,omitempty"`
	RecoveryReason   string        `json:"recoveryReason,omitempty"`
	RecoveryDigest   string        `json:"recoveryDigest,omitempty"`
	RecoveryParentID string        `json:"recoveryParentId,omitempty"`
	Children         []ProjectNode `json:"children,omitempty"`
}

func normalizeTopicStatus(status string) string {
	switch status {
	case topicStatusThinking, topicStatusStreaming, topicStatusWaitingConfirmation, topicStatusBackgroundJob, topicStatusPaused, topicStatusError:
		return status
	default:
		return ""
	}
}

func activityStatusForTab(tab *WorkspaceTab) string {
	if tab == nil {
		return ""
	}
	status := normalizeTopicStatus(tab.ActivityStatus)
	if tab.Ctrl == nil {
		return status
	}
	runtimeStatus := tab.Ctrl.RuntimeStatus()
	if runtimeStatus.PendingPrompt {
		return topicStatusWaitingConfirmation
	}
	if runtimeStatus.Running {
		if status == "" || status == topicStatusError {
			return topicStatusThinking
		}
		return status
	}
	if runtimeStatus.BackgroundJobs > 0 {
		return topicStatusBackgroundJob
	}
	if status == topicStatusError || status == topicStatusPaused {
		return status
	}
	return ""
}

// migrateLegacySessionsIntoGlobalTopics makes pre-topic desktop history visible
// in the v2 sidebar. Imported v0.x sessions and older desktop sessions are plain
// .jsonl files, sometimes with branch metadata but no topic metadata; the
// history panel can list them, but the project tree cannot. Give each such
// session a deterministic Global topic so every old conversation has a direct
// sidebar entry without guessing a project workspace.
// legacyMigrationMu serializes the lockless load-modify-save of the projects /
// topic-title files: this migration runs from every concurrent buildTabController
// and from ListProjectTree, so without it parallel runs lose each other's appends.
var legacyMigrationMu sync.Mutex

// topicMigrationMarker, once written into a session dir, records that the
// pre-topic → Global-topic migration pass completed for that dir. Later
// ListProjectTree calls can skip the full session decode while the marker is
// newer than the directory's session files, but a newly-created CLI session
// invalidates the marker and gets a bounded re-scan.
// It is stamped only when the pass left nothing deferred (an empty legacy
// session that could gain content later keeps the dir unmarked), so the gate
// never hides a session that should still be migrated.
const topicMigrationMarker = ".topics-migrated"
const topicIndexRepairMarker = ".topic-indexes-repaired"

func topicDirMarkerDone(dir, marker string) bool {
	dir = strings.TrimSpace(dir)
	marker = strings.TrimSpace(marker)
	if dir == "" || marker == "" {
		return false
	}
	markerInfo, err := os.Stat(filepath.Join(dir, marker))
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	markerTime := markerInfo.ModTime()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !store.IsSessionTranscriptName(name) && !strings.HasSuffix(name, ".jsonl.meta") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return false
		}
		if info.ModTime().After(markerTime) {
			return false
		}
	}
	return true
}

func topicMigrationDone(dir string) bool {
	return topicDirMarkerDone(dir, topicMigrationMarker)
}

func topicIndexRepairDone(dir string) bool {
	return topicDirMarkerDone(dir, topicIndexRepairMarker)
}

func markTopicDirMarkerDone(dir, marker string) {
	dir = strings.TrimSpace(dir)
	marker = strings.TrimSpace(marker)
	if dir == "" || marker == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, marker), nil, 0o644)
}

func markTopicMigrationDone(dir string) {
	markTopicDirMarkerDone(dir, topicMigrationMarker)
}

func markTopicIndexRepairDone(dir string) {
	markTopicDirMarkerDone(dir, topicIndexRepairMarker)
}

func migrateLegacySessionsIntoGlobalTopics(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	repairedTopicIDs := repairIndexedSessionTopics(dir)
	// One-shot per dir: once the migration pass has completed, skip the full
	// per-render session scan entirely.
	if topicMigrationDone(dir) {
		return repairedTopicIDs
	}
	scope, workspaceRoot, topicTitleRoot, ok := legacyMigrationTargetForDir(dir)
	if !ok {
		return nil
	}
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	// Re-check under the lock: another render may have completed the pass while
	// this one waited.
	if topicMigrationDone(dir) {
		return nil
	}
	infos, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil // transient read error — retry on the next render, leave unmarked
	}

	var migratedTopicIDs []string
	var titles map[string]string
	var topicTitles map[string]string
	var topicSources map[string]string
	// deferred stays false only when every session was either migrated or is
	// permanently non-migratable. A transient skip (unreadable meta, empty
	// session that may gain content, failed write) sets it, keeping the dir
	// unmarked so the next render retries instead of the gate hiding it forever.
	deferred := false
	for _, info := range infos {
		if strings.TrimSpace(info.TopicID) != "" {
			continue
		}
		if meta, ok, err := agent.LoadBranchMeta(info.Path); err != nil {
			deferred = true
			continue
		} else if ok && !legacySessionMetaMatchesMigrationTarget(meta, scope, workspaceRoot) {
			continue
		}
		topicID := legacySessionTopicID(info.Path)
		if topicID == "" {
			continue
		}
		preview, turns := agent.SessionPreview(info.Path)
		if turns == 0 {
			deferred = true // empty now, but a later turn could make it migratable
			continue
		}
		if titles == nil {
			titles = loadSessionTitles(dir)
		}
		title := strings.TrimSpace(titles[filepath.Base(info.Path)])
		if title == "" {
			title = topicTitleFromText(preview)
		} else if normalized := topicTitleFromText(title); normalized != "" {
			title = normalized
		}
		if title == "" {
			when := info.LastActivityAt
			if when.IsZero() {
				when = info.ModTime
			}
			if when.IsZero() {
				title = "历史会话"
			} else {
				title = "历史会话 " + when.Local().Format("2006-01-02")
			}
		}

		migrated, err := func() (bool, error) {
			// Read-modify-write on the branch-meta sidecar: hold the per-path
			// meta lock so agent-side writers (autosave revision bumps,
			// in-flight markers) can't interleave between the load and save
			// below and lose their fields.
			unlock := agent.LockSessionMetaPath(info.Path)
			defer unlock()
			meta, err := agent.EnsureBranchMeta(info.Path)
			if err != nil {
				return false, err
			}
			// Preserve scoped sessions only when their existing ownership matches
			// the directory being migrated.
			if !legacySessionMetaMatchesMigrationTarget(meta, scope, workspaceRoot) {
				return false, nil
			}
			meta.Scope = scope
			meta.WorkspaceRoot = workspaceRoot
			meta.TopicID = topicID
			meta.TopicTitle = title
			return true, agent.SaveBranchMetaPreserveUpdated(info.Path, meta)
		}()
		if err != nil {
			deferred = true
			continue
		}
		if !migrated {
			continue
		}
		if topicTitles == nil {
			topicTitles = loadTopicTitles(topicTitleRoot)
		}
		if topicSources == nil {
			topicSources = loadTopicTitleSources(topicTitleRoot)
		}
		if strings.TrimSpace(topicTitles[topicID]) == "" {
			topicTitles[topicID] = title
			topicSources[topicID] = topicTitleSourceManual
		}
		migratedTopicIDs = append(migratedTopicIDs, topicID)
	}
	if len(migratedTopicIDs) == 0 {
		if !deferred {
			markTopicMigrationDone(dir) // nothing left to migrate — gate future scans
		}
		return repairedTopicIDs
	}
	_ = prependTopicsInProjectsFile(workspaceRoot, migratedTopicIDs, false)
	// Same fresh tombstone re-check as the repair pass: these are whole-map
	// saves, so a concurrent DeleteTopic of an unrelated topic must not have
	// its title written back by this migration batch.
	pruneDeletedTopicEntries(topicTitles, topicSources)
	if topicTitles != nil {
		_ = saveTopicTitles(topicTitleRoot, topicTitles)
	}
	if topicSources != nil {
		_ = saveTopicTitleSources(topicTitleRoot, topicSources)
	}
	invalidateTopicSessionIndex(dir)
	projectSessionCache.invalidate()
	if !deferred {
		markTopicMigrationDone(dir) // pass complete with nothing deferred
	}
	return uniqueStrings(append(repairedTopicIDs, migratedTopicIDs...))
}

// pruneDeletedTopicEntries drops tombstoned topics from scan-built title and
// source maps just before they are persisted, re-reading DeletedTopics so a
// DeleteTopic that landed after the scan snapshot wins. The maps are loaded
// whole at scan start and saved whole at the end; without this re-check the
// save would write the deleted topic's stale entries back, and a title-map
// entry alone resurrects a topic in the sidebar (orderedTopicIDs lists topics
// that exist only in the title map). Returns the tombstone list so callers can
// filter their returned topic-ID slices against the same snapshot.
func pruneDeletedTopicEntries(maps ...map[string]string) []string {
	deleted := loadProjectsFile().DeletedTopics
	if len(deleted) == 0 {
		return nil
	}
	for _, m := range maps {
		for _, id := range deleted {
			delete(m, id)
		}
	}
	return deleted
}

func repairIndexedSessionTopics(dir string) []string {
	if strings.TrimSpace(dir) == "" || topicIndexRepairDone(dir) {
		return nil
	}
	scope, workspaceRoot, topicTitleRoot, ok := legacyMigrationTargetForDir(dir)
	if !ok {
		return nil
	}
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	if topicIndexRepairDone(dir) {
		return nil
	}
	infos, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil
	}

	topicTitles, err := loadTopicTitlesForUpdate(topicTitleRoot)
	if err != nil {
		return nil
	}
	topicSources, err := loadTopicTitleSourcesForUpdate(topicTitleRoot)
	if err != nil {
		return nil
	}
	projects := loadProjectsFile()
	deletedTopics := projects.DeletedTopics
	// Repair only topics missing from the sidebar index. Skipping topics that
	// are already listed and titled keeps steady-state rescans write-free:
	// otherwise every rescan (any session activity invalidates the marker)
	// would prepend the full topic list back in most-recently-active order,
	// reordering the user's sidebar and handing already-visible topics to the
	// blank-tab binding in the migration callers.
	indexedTopics := projects.GlobalTopics
	if scope == "project" {
		indexedTopics = nil
		for _, p := range projects.Projects {
			if p.Root == workspaceRoot {
				indexedTopics = p.Topics
				break
			}
		}
	}
	indexedSet := make(map[string]bool, len(indexedTopics))
	for _, id := range indexedTopics {
		indexedSet[id] = true
	}
	var repairedTopicIDs []string
	var sessionTitles map[string]string
	titlesChanged := false
	sourcesChanged := false
	deferred := false
	for _, info := range infos {
		topicID := strings.TrimSpace(info.TopicID)
		if topicID == "" {
			continue
		}
		if indexedSet[topicID] && strings.TrimSpace(topicTitles[topicID]) != "" {
			continue // fully indexed already — nothing to repair, skip the meta read
		}
		meta, ok, err := agent.LoadBranchMeta(info.Path)
		if err != nil {
			deferred = true
			continue
		}
		if !ok || strings.TrimSpace(meta.TopicID) == "" {
			continue
		}
		if containsDesktopString(deletedTopics, topicID) {
			continue
		}
		if !legacySessionScopeMatchesMigrationTarget(meta, scope, workspaceRoot) {
			continue
		}
		repairedTopicIDs = append(repairedTopicIDs, topicID)
		if strings.TrimSpace(topicTitles[topicID]) == "" {
			if sessionTitles == nil {
				sessionTitles = loadSessionTitles(dir)
			}
			title := indexedSessionTopicTitle(sessionTitles, info, meta)
			if title == "" {
				title = defaultTopicTitle
			}
			topicTitles[topicID] = title
			titlesChanged = true
		}
		if strings.TrimSpace(topicSources[topicID]) == "" {
			topicSources[topicID] = topicTitleSourceManual
			sourcesChanged = true
		}
	}
	if len(repairedTopicIDs) > 0 {
		// Re-check tombstones right before persisting: a DeleteTopic landing
		// after the scan snapshot must win. The prepend re-filters under the
		// projects-file lock; the whole-map title/source saves and the
		// returned IDs (callers bind blank Global tabs to the first entry)
		// need the same fresh read.
		if deletedNow := pruneDeletedTopicEntries(topicTitles, topicSources); len(deletedNow) > 0 {
			deletedSet := make(map[string]bool, len(deletedNow))
			for _, id := range deletedNow {
				deletedSet[id] = true
			}
			live := repairedTopicIDs[:0]
			for _, id := range repairedTopicIDs {
				if !deletedSet[id] {
					live = append(live, id)
				}
			}
			repairedTopicIDs = live
		}
	}
	if len(repairedTopicIDs) > 0 {
		if err := prependTopicsInProjectsFile(workspaceRoot, repairedTopicIDs, false); err != nil {
			deferred = true
		}
		if titlesChanged {
			if err := saveTopicTitles(topicTitleRoot, topicTitles); err != nil {
				deferred = true
			}
		}
		if sourcesChanged {
			if err := saveTopicTitleSources(topicTitleRoot, topicSources); err != nil {
				deferred = true
			}
		}
		if !deferred {
			projectSessionCache.invalidate()
		}
	}
	if !deferred {
		markTopicIndexRepairDone(dir)
		return uniqueStrings(repairedTopicIDs)
	}
	return nil
}

func indexedSessionTopicTitle(sessionTitles map[string]string, info agent.SessionOrderInfo, meta agent.BranchMeta) string {
	if title := topicTitleFromText(meta.TopicTitle); title != "" {
		return title
	}
	if title := topicTitleFromText(info.TopicTitle); title != "" {
		return title
	}
	if title := topicTitleFromText(sessionTitles[filepath.Base(info.Path)]); title != "" {
		return title
	}
	return topicTitleFromText(info.Preview)
}

func legacyMigrationTargetForDir(dir string) (scope, workspaceRoot, topicTitleRoot string, ok bool) {
	dir = cleanDesktopPath(dir)
	if dir == "" {
		return "", "", "", false
	}
	if sameDesktopPath(dir, config.SessionDir()) || sameDesktopPath(dir, desktopSessionDir(globalWorkspaceRoot())) {
		return "global", "", "", true
	}
	for _, p := range loadProjectsFile().Projects {
		if sameDesktopPath(config.ProjectSessionDir(p.Root), dir) {
			return "project", p.Root, p.Root, true
		}
	}
	return "", "", "", false
}

func legacySessionMetaMatchesMigrationTarget(meta agent.BranchMeta, scope, workspaceRoot string) bool {
	if strings.TrimSpace(meta.TopicID) != "" {
		return false
	}
	return legacySessionScopeMatchesMigrationTarget(meta, scope, workspaceRoot)
}

func legacySessionScopeMatchesMigrationTarget(meta agent.BranchMeta, scope, workspaceRoot string) bool {
	metaScope := strings.TrimSpace(meta.Scope)
	if metaScope != "" && metaScope != scope {
		return false
	}
	metaRoot := normalizeProjectRoot(meta.WorkspaceRoot)
	if scope == "project" {
		return metaRoot == "" || normalizeProjectRoot(workspaceRoot) == metaRoot
	}
	return metaRoot == "" || normalizeProjectRoot(globalWorkspaceRoot()) == metaRoot
}

func cleanDesktopPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func sameDesktopPath(a, b string) bool {
	a = cleanDesktopPath(a)
	b = cleanDesktopPath(b)
	if a == "" || b == "" {
		return false
	}
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func restoreSessionTopicIndex(dir, sessionPath string) error {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(meta.TopicID) == "" {
		// The migration pass takes per-session meta locks itself, so it must
		// run outside the lock taken below.
		migrateLegacySessionsIntoGlobalTopics(dir)
		return nil
	}

	// Read-modify-write on the branch-meta sidecar: re-read and save under the
	// per-path meta lock so a concurrent save's revision bump can't land in
	// between and get rolled back by the write at the end.
	unlock := agent.LockSessionMetaPath(sessionPath)
	defer unlock()
	meta, ok, err = agent.LoadBranchMeta(sessionPath)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(meta.TopicID) == "" {
		return nil
	}

	topicID := strings.TrimSpace(meta.TopicID)
	scope := strings.TrimSpace(meta.Scope)
	workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
	if scope != "global" && scope != "project" {
		if workspaceRoot == "" {
			scope = "global"
		} else {
			scope = "project"
		}
	}
	if scope == "global" {
		workspaceRoot = ""
	} else {
		workspaceRoot = normalizeProjectRoot(workspaceRoot)
		if workspaceRoot == "" {
			scope = "global"
		}
	}

	title := restoredSessionTopicTitle(dir, sessionPath, meta)
	if title == "" {
		title = defaultTopicTitle
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, title, topicTitleSourceManual); err != nil {
		return err
	}

	if scope == "global" {
		meta.Scope = "global"
		meta.WorkspaceRoot = ""
	} else {
		meta.Scope = "project"
		meta.WorkspaceRoot = workspaceRoot
	}
	meta.TopicID = topicID
	meta.TopicTitle = title
	if err := prependTopicInProjectsFile(workspaceRoot, topicID, scope == "project"); err != nil {
		return err
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, meta); err != nil {
		return err
	}
	invalidateTopicSessionIndexForPath(sessionPath)
	return nil
}

func restoredSessionTopicTitle(dir, sessionPath string, meta agent.BranchMeta) string {
	if title := storedSessionTopicTitle(dir, sessionPath, meta); title != "" {
		return title
	}
	if s, err := agent.LoadSession(sessionPath); err == nil {
		for _, msg := range s.Messages {
			if msg.Role == provider.RoleUser {
				if title := topicTitleFromText(control.StripComposePrefixes(agent.HandoffTask(msg.Content))); title != "" {
					return title
				}
			}
		}
	}
	return ""
}

func storedSessionTopicTitle(dir, sessionPath string, meta agent.BranchMeta) string {
	if title := topicTitleFromText(meta.TopicTitle); title != "" {
		return title
	}
	return topicTitleFromText(loadSessionTitles(dir)[filepath.Base(sessionPath)])
}

func legacySessionTopicID(path string) string {
	id := agent.BranchID(path)
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	var b strings.Builder
	b.WriteString("legacy_")
	for _, r := range id {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	prefix := strings.TrimRight(b.String(), "_")
	if prefix == "legacy" {
		prefix = "legacy_session"
	}
	return prefix + "_" + hex.EncodeToString(sum[:])[:12]
}

// TopicMeta describes a topic for the project tree.
type TopicMeta struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
}

// CreateTopic creates a new topic under a project workspace and returns its metadata.
func (a *App) CreateTopic(scope, workspaceRoot, title string) (TopicMeta, error) {
	trimmedTitle := strings.TrimSpace(title)
	titleSource := topicTitleSourceManual
	if trimmedTitle == "" {
		trimmedTitle = defaultTopicTitle
		titleSource = topicTitleSourceAuto
	}
	topicID := newTopicID()
	createdAt := time.Now().UnixMilli()
	if scope == "global" {
		workspaceRoot = ""
	}
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, trimmedTitle, titleSource); err != nil {
		return TopicMeta{}, err
	}
	if err := setTopicCreatedAt(workspaceRoot, topicID, createdAt); err != nil {
		return TopicMeta{}, err
	}
	// New topics should appear first in their project/global group so the item
	// just created is immediately visible and selected in the sidebar.
	_ = prependTopicInProjectsFile(workspaceRoot, topicID, workspaceRoot != "")
	a.emitProjectTreeChanged()
	return TopicMeta{ID: topicID, Title: trimmedTitle, CreatedAt: createdAt}, nil
}

// RenameProject updates the sidebar-only display title for a project folder.
// Empty title clears the override and falls back to the folder name.
func (a *App) RenameProject(workspaceRoot, title string) error {
	if err := renameProject(workspaceRoot, title); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// SetProjectColor updates the project-level accent color used by project topics
// in the sidebar and tabs. Empty color restores the default accent.
func (a *App) SetProjectColor(workspaceRoot, color string) error {
	if err := setProjectColor(workspaceRoot, color); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// SetProjectPinned controls whether a project folder is pinned above the rest of
// the desktop project tree.
func (a *App) SetProjectPinned(workspaceRoot string, pinned bool) error {
	root := normalizeProjectRoot(workspaceRoot)
	if root == "" {
		return fmt.Errorf("workspaceRoot is required")
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		found := false
		for _, project := range f.Projects {
			if project.Root == root {
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("project %q not found", root)
		}
		next := removeString(f.PinnedProjects, root)
		if pinned {
			next = prependUniqueString(f.PinnedProjects, root)
		}
		if sameStringList(next, f.PinnedProjects) {
			return false, nil
		}
		f.PinnedProjects = next
		return true, nil
	}); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// ReorderProjects persists the user-defined order of project folders and,
// when present, the virtual Global sidebar section.
func (a *App) ReorderProjects(workspaceRoots []string) error {
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		byRoot := make(map[string]desktopProject, len(f.Projects))
		for _, project := range f.Projects {
			byRoot[project.Root] = project
		}
		seen := make(map[string]bool, len(workspaceRoots))
		next := make([]desktopProject, 0, len(workspaceRoots))
		sidebarOrder := make([]string, 0, len(workspaceRoots))
		hasGlobalOrder := false
		for _, root := range workspaceRoots {
			root = strings.TrimSpace(root)
			if root == desktopGlobalOrderToken {
				if seen[root] {
					return false, fmt.Errorf("duplicate global section")
				}
				seen[root] = true
				hasGlobalOrder = true
				sidebarOrder = append(sidebarOrder, root)
				continue
			}
			root = normalizeProjectRoot(root)
			project, ok := byRoot[root]
			if !ok {
				return false, fmt.Errorf("project %q not found", root)
			}
			if seen[root] {
				return false, fmt.Errorf("duplicate project %q", root)
			}
			seen[root] = true
			next = append(next, project)
			sidebarOrder = append(sidebarOrder, root)
		}
		if len(next) != len(f.Projects) {
			return false, fmt.Errorf("project order length mismatch")
		}
		changed := !sameProjectOrder(next, f.Projects)
		f.Projects = next
		if hasGlobalOrder {
			if !sameStringList(sidebarOrder, f.SidebarOrder) {
				changed = true
			}
			f.SidebarOrder = sidebarOrder
		} else {
			if len(f.SidebarOrder) > 0 {
				changed = true
			}
			f.SidebarOrder = nil
		}
		return changed, nil
	}); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// RenameTopic updates a topic's display title.
func (a *App) RenameTopic(topicID, title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		trimmed = defaultTopicTitle
	}
	// Find which workspace this topic belongs to by scanning all project topic titles.
	f := loadProjectsFile()
	for _, p := range f.Projects {
		m := loadTopicTitles(p.Root)
		if _, ok := m[topicID]; ok {
			if err := setTopicTitle(p.Root, topicID, trimmed); err != nil {
				return err
			}
			a.updateOpenTopicTitle(topicID, trimmed)
			a.updateTopicSessionTitles(topicID, trimmed)
			a.emitProjectTreeChanged()
			return nil
		}
	}
	// Check global.
	m := loadTopicTitles("")
	if _, ok := m[topicID]; ok {
		if err := setTopicTitle("", topicID, trimmed); err != nil {
			return err
		}
		a.updateOpenTopicTitle(topicID, trimmed)
		a.updateTopicSessionTitles(topicID, trimmed)
		a.emitProjectTreeChanged()
		return nil
	}
	if scope, workspaceRoot, ok := a.findTopicLocation(topicID); ok {
		if err := ensureTopicIndexed(scope, workspaceRoot, topicID, trimmed, topicTitleSourceManual); err != nil {
			return err
		}
		a.updateOpenTopicTitle(topicID, trimmed)
		a.updateTopicSessionTitles(topicID, trimmed)
		a.emitProjectTreeChanged()
		return nil
	}
	return fmt.Errorf("topic %q not found", topicID)
}

func (a *App) findTopicLocation(topicID string) (string, string, bool) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", "", false
	}
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab == nil || tab.TopicID != topicID {
			continue
		}
		scope := tab.Scope
		workspaceRoot := tab.WorkspaceRoot
		a.mu.RUnlock()
		if scope == "global" {
			return "global", "", true
		}
		return "project", normalizeProjectRoot(workspaceRoot), true
	}
	a.mu.RUnlock()

	infos, err := agent.ListSessions(config.SessionDir())
	if err != nil {
		return "", "", false
	}
	for _, info := range infos {
		if strings.TrimSpace(info.TopicID) != topicID {
			continue
		}
		scope := strings.TrimSpace(info.Scope)
		if scope == "" {
			scope = "global"
		}
		if scope == "global" {
			return "global", "", true
		}
		return "project", normalizeProjectRoot(info.WorkspaceRoot), true
	}
	return "", "", false
}

func (a *App) updateOpenTopicTitle(topicID, title string) {
	if strings.TrimSpace(topicID) == "" || strings.TrimSpace(title) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, tab := range a.tabs {
		if tab != nil && tab.TopicID == topicID {
			tab.TopicTitle = title
		}
	}
}

func (a *App) updateTopicSessionTitles(topicID, title string) {
	if strings.TrimSpace(topicID) == "" || strings.TrimSpace(title) == "" {
		return
	}
	for _, dir := range a.knownSessionDirs() {
		for _, match := range topicSessionMatches(dir, topicID) {
			// Read-modify-write on the branch-meta sidecar: hold the per-path
			// meta lock so a concurrent save's revision bump can't land between
			// the load and save below and get rolled back by this write.
			unlock := agent.LockSessionMetaPath(match.path)
			meta, ok, err := agent.LoadBranchMeta(match.path)
			if err != nil || !ok {
				unlock()
				continue
			}
			meta.TopicTitle = title
			err = agent.SaveBranchMetaPreserveUpdated(match.path, meta)
			unlock()
			if err == nil {
				invalidateTopicSessionIndex(dir)
			}
		}
	}
}

func (a *App) setTabActivityStatus(tabID, status string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	tab := a.tabByEventSinkIDLocked(tabID)
	if tab == nil {
		return false
	}
	status = normalizeTopicStatus(status)
	if tab.ActivityStatus == status {
		return false
	}
	tab.ActivityStatus = status
	return true
}

func (a *App) emitProjectTreeChanged() {
	projectSessionCache.invalidate()
	if a.projectTreeChangedHook != nil {
		a.projectTreeChangedHook()
		return
	}
	a.emitRuntimeEvent("project-tree:changed")
}

func (a *App) emitRuntimeEvent(name string, payload ...interface{}) {
	if a == nil || a.ctx == nil {
		return
	}
	a.runtimeEvents.Emit(a.ctx, name, payload...)
}

// DeleteTopic removes a topic and its title metadata.
func (a *App) DeleteTopic(topicID string) error {
	return friendlySessionFileError(a.deleteTopic(topicID))
}

func (a *App) deleteTopic(topicID string) error {
	f := loadProjectsFile()
	found := false
	for _, p := range f.Projects {
		m, err := loadTopicTitlesForUpdate(p.Root)
		if err != nil {
			return err
		}
		if _, ok := m[topicID]; ok {
			delete(m, topicID)
			if err := saveTopicTitles(p.Root, m); err != nil {
				return err
			}
			sources, err := loadTopicTitleSourcesForUpdate(p.Root)
			if err != nil {
				return err
			}
			delete(sources, topicID)
			if err := saveTopicTitleSources(p.Root, sources); err != nil {
				return err
			}
			deleteTopicCreatedAt(p.Root, topicID)
			found = true
			break
		}
	}
	if !found {
		m, err := loadTopicTitlesForUpdate("")
		if err != nil {
			return err
		}
		if _, ok := m[topicID]; ok {
			delete(m, topicID)
			if err := saveTopicTitles("", m); err != nil {
				return err
			}
			sources, err := loadTopicTitleSourcesForUpdate("")
			if err != nil {
				return err
			}
			delete(sources, topicID)
			if err := saveTopicTitleSources("", sources); err != nil {
				return err
			}
			deleteTopicCreatedAt("", topicID)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("topic %q not found", topicID)
	}
	if err := removeTopicFromProjectsFile(topicID); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// SetTopicPinned controls whether a topic is pinned to the top of its project
// or Global section in the desktop project tree.
func (a *App) SetTopicPinned(topicID string, pinned bool) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("topicID is required")
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		for i, p := range f.Projects {
			m := loadTopicTitles(p.Root)
			if _, ok := m[topicID]; !ok && !containsDesktopString(p.Topics, topicID) {
				continue
			}
			next := removeString(f.Projects[i].PinnedTopics, topicID)
			if pinned {
				next = prependUniqueString(f.Projects[i].PinnedTopics, topicID)
			}
			if sameStringList(next, f.Projects[i].PinnedTopics) {
				return false, nil
			}
			f.Projects[i].PinnedTopics = next
			return true, nil
		}
		globalTitles := loadTopicTitles("")
		if _, ok := globalTitles[topicID]; !ok && !containsDesktopString(f.GlobalTopics, topicID) {
			return false, fmt.Errorf("topic %q not found", topicID)
		}
		next := removeString(f.GlobalPinnedTopics, topicID)
		if pinned {
			next = prependUniqueString(f.GlobalPinnedTopics, topicID)
		}
		if sameStringList(next, f.GlobalPinnedTopics) {
			return false, nil
		}
		f.GlobalPinnedTopics = next
		return true, nil
	}); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// TrashTopic removes a topic from the project tree and moves its saved session
// records into the session trash. Any in-process runtimes for the topic are
// cancelled and detached from the app first, so their autosave/jobs cannot
// recreate state after the topic is gone.
func (a *App) TrashTopic(topicID string) error {
	return friendlySessionFileError(a.trashTopic(topicID))
}

func (a *App) trashTopic(topicID string) error {
	if strings.TrimSpace(topicID) == "" {
		return fmt.Errorf("topicID is required")
	}

	var fallback fallbackRuntimeTarget
	if err := func() error {
		a.sessionRemovalMu.Lock()
		defer a.sessionRemovalMu.Unlock()

		targets, err := a.topicTrashTargets(topicID)
		if err != nil {
			return err
		}
		removed, nextFallback := a.removeTopicRuntimeBindings(topicID)
		fallback = nextFallback
		if err := a.prepareRemovedSessionRuntimes(removed); err != nil {
			a.closeRemovedSessionRuntimes(removed)
			return err
		}
		destroyBegun := false
		closedRemoved := map[control.SessionAPI]bool{}
		defer func() {
			if destroyBegun {
				a.closeRemainingRemovedSessionRuntimesAfterDestroy(removed, closedRemoved)
				return
			}
			a.closeRemovedSessionRuntimes(removed)
		}()

		for _, target := range targets {
			destroys := a.destroyHandlesForSession(target.dir, target.sessionPath, removed)
			if len(destroys) > 0 {
				destroyBegun = true
			}
			teardownTimedOut := waitDestroyHandles(destroys)
			a.closeRemovedSessionRuntimesForSessionAfterDestroy(removed, target.dir, target.sessionPath, closedRemoved)
			if teardownTimedOut {
				if err := agent.MarkCleanupPending(target.sessionPath, "delete"); err != nil {
					return err
				}
				go delayedDesktopSessionTrash(target.dir, target.sessionPath, target.key, destroys)
			} else {
				err := trashSessionArtifacts(target.dir, target.sessionPath, target.key)
				finishDestroyHandles(destroys)
				if err != nil {
					return err
				}
			}
		}
		return a.deleteTopic(topicID)
	}(); err != nil {
		return err
	}
	if fallback.needs {
		fallback.topicID = ""
		if err := a.openFallbackRuntime(fallback); err != nil {
			return err
		}
	}
	a.emitProjectTreeChanged()
	return nil
}

type topicTrashTarget struct {
	dir         string
	sessionPath string
	key         string
}

func (a *App) topicTrashTargets(topicID string) ([]topicTrashTarget, error) {
	topicID = strings.TrimSpace(topicID)
	var targets []topicTrashTarget
	seen := map[string]bool{}
	addTarget := func(dir, path string) error {
		sessionPath, key, err := validateSessionPath(dir, path)
		if err != nil {
			return err
		}
		id := dir + "\x00" + sessionPath
		if seen[id] {
			return nil
		}
		seen[id] = true
		if err := validateSessionTrashTarget(dir, sessionPath, key); err != nil {
			return err
		}
		targets = append(targets, topicTrashTarget{dir: dir, sessionPath: sessionPath, key: key})
		return nil
	}
	for _, dir := range a.knownSessionDirs() {
		index, err := topicSessionIndexForDir(dir)
		if err != nil {
			return nil, err
		}
		for _, match := range index.byTopic[topicID] {
			if agent.IsCleanupPending(match.path) {
				continue
			}
			if err := addTarget(dir, match.path); err != nil {
				return nil, err
			}
		}
	}
	a.mu.RLock()
	var runtimeTargets []struct {
		dir  string
		path string
	}
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || tab.TopicID != topicID {
			continue
		}
		if path := canonicalTabSessionPath(tab.currentSessionPath()); path != "" {
			dir := tabSessionDir(tab)
			if filepath.IsAbs(path) {
				dir = filepath.Dir(path)
			}
			runtimeTargets = append(runtimeTargets, struct {
				dir  string
				path string
			}{dir: dir, path: path})
		}
	}
	a.mu.RUnlock()
	for _, target := range runtimeTargets {
		if err := addTarget(target.dir, target.path); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

// ListProjectTree builds the sidebar tree: project folders each containing
// their topics, plus a Global section.
// topicSummary is used by ListProjectTree and mergeSessionInfos to track
// per-topic turn count and last activity.
type topicSummary struct {
	turns            int
	lastActivityAt   int64
	hasNormalSession bool
	hasRecoveryOnly  bool
}

// runtimeSessionStatus is one open or detached runtime session, as shown in
// the sidebar tree.
type runtimeSessionStatus struct {
	sessionPath      string
	label            string
	turns            int
	createdAt        int64
	lastActivityAt   int64
	open             bool
	running          bool
	status           string
	recovered        bool
	recoveryReason   string
	recoveryDigest   string
	recoveryParentID string
}

// topicHiddenAsRecoveryOnly hides topics whose only on-disk sessions are
// conflict-recovery copies: they stay reachable from History, but must not sit
// in the tree as regular conversations. Pinned topics and topics with any
// open or running runtime session remain visible — note topicRuntimeStatus
// reports open/running only for single-session topics, so it must not gate
// topic existence.
func topicHiddenAsRecoveryOnly(summary topicSummary, pinned bool, runtimeSessions []runtimeSessionStatus) bool {
	if !summary.hasRecoveryOnly || summary.hasNormalSession || pinned {
		return false
	}
	for _, session := range runtimeSessions {
		if session.open || session.running {
			return false
		}
	}
	return true
}

var listProjectTreeMu sync.Mutex

func (a *App) ListProjectTree() []ProjectNode {
	listProjectTreeMu.Lock()
	defer listProjectTreeMu.Unlock()

	knownDirs := a.knownSessionDirs()
	for _, dir := range knownDirs {
		migrateLegacySessionsIntoGlobalTopics(dir)
	}
	f := loadProjectsFile()
	// Render-side tombstone guard: a deleted topic whose title survived a racy
	// whole-map save would otherwise reappear via the orderedTopicIDs title-map
	// fallback. The tombstone is authoritative until an intentional
	// single-topic write (create/restore/tab indexing) clears it.
	deletedTopicSet := make(map[string]bool, len(f.DeletedTopics))
	for _, id := range f.DeletedTopics {
		deletedTopicSet[id] = true
	}
	out := []ProjectNode{}
	topicSummaries := map[string]topicSummary{}
	sessionInfos := map[string]agent.SessionInfo{}
	sessionTitles := map[string]string{}

	// Read session listings from all known directories concurrently, since
	// each dir is independent I/O. With N workspaces × dozens of sessions,
	// sequential reads add up to seconds of wall time on cold start.
	cacheToken := projectSessionCache.versionToken()
	type sessionDirLoadResult struct {
		dir    string
		infos  []agent.SessionInfo
		titles map[string]string
		ok     bool
	}
	results := make(chan sessionDirLoadResult, len(knownDirs))
	pendingLoads := 0
	for _, dir := range knownDirs {
		infos, titles, ok := projectSessionCache.get(dir)
		if ok {
			mergeSessionInfos(dir, infos, titles, sessionInfos, sessionTitles, topicSummaries)
			continue
		}
		pendingLoads++
		dir := dir // capture
		go func() {
			result := sessionDirLoadResult{dir: dir}
			defer func() {
				if recover() != nil {
					result.ok = false
				}
				results <- result
			}()

			// Sidecar-backed listing: ListSessions reads turn count + preview from
			// each session's .meta sidecar, so even large directories list in a few
			// milliseconds without decoding any .jsonl body. The in-memory
			// projectSessionCache still elides repeat listings within a session.
			infos, err := agent.ListSessions(dir)
			if err != nil {
				return
			}
			titles := loadSessionTitles(dir)
			projectSessionCache.put(dir, infos, titles, cacheToken)
			result.infos = infos
			result.titles = titles
			result.ok = true
		}()
	}
	if pendingLoads > 0 {
		timer := time.NewTimer(5 * time.Second)
		for received := 0; received < pendingLoads; {
			select {
			case result := <-results:
				received++
				if result.ok {
					mergeSessionInfos(result.dir, result.infos, result.titles, sessionInfos, sessionTitles, topicSummaries)
				}
			case <-timer.C:
				received = pendingLoads
			}
		}
		timer.Stop()
	}

	runtimeSessionsByTopic := map[string][]runtimeSessionStatus{}
	a.mu.RLock()
	seenRuntimePaths := map[string]bool{}
	addRuntimeSession := func(tab *WorkspaceTab, open bool) {
		if tab == nil || strings.TrimSpace(tab.TopicID) == "" {
			return
		}
		sessionPath := sessionRuntimeKey(tab.currentSessionPath())
		if sessionPath == "" || seenRuntimePaths[sessionPath] {
			return
		}
		seenRuntimePaths[sessionPath] = true
		info := sessionInfos[sessionPath]
		label := runtimeSessionTreeLabel(tab, info, sessionTitles[sessionPath])
		status := activityStatusForTab(tab)
		runtimeStatus := control.RuntimeStatus{}
		if tab.Ctrl != nil {
			runtimeStatus = tab.Ctrl.RuntimeStatus()
		}
		running := status != "" || runtimeStatus.Running || runtimeStatus.PendingPrompt || runtimeStatus.BackgroundJobs > 0
		runtimeSessionsByTopic[topicSummaryKey(tab.Scope, tab.WorkspaceRoot, tab.TopicID)] = append(runtimeSessionsByTopic[topicSummaryKey(tab.Scope, tab.WorkspaceRoot, tab.TopicID)], runtimeSessionStatus{
			sessionPath:      sessionPath,
			label:            label,
			turns:            info.Turns,
			createdAt:        unixMilliOrZero(info.CreatedAt),
			lastActivityAt:   unixMilliOrZero(info.LastActivityAt),
			open:             open,
			running:          running,
			status:           status,
			recovered:        info.Recovered,
			recoveryReason:   info.RecoveryReason,
			recoveryDigest:   info.RecoveryDigest,
			recoveryParentID: string(info.ParentID),
		})
	}
	for _, tab := range a.tabs {
		addRuntimeSession(tab, true)
	}
	for _, tab := range a.detachedSessions {
		addRuntimeSession(tab, false)
	}
	a.mu.RUnlock()
	for key := range runtimeSessionsByTopic {
		sort.Slice(runtimeSessionsByTopic[key], func(i, j int) bool {
			left := runtimeSessionsByTopic[key][i]
			right := runtimeSessionsByTopic[key][j]
			if left.lastActivityAt != right.lastActivityAt {
				return left.lastActivityAt > right.lastActivityAt
			}
			return left.sessionPath < right.sessionPath
		})
	}
	topicRuntimeStatus := func(key string) (open, running bool, status string) {
		sessions := runtimeSessionsByTopic[key]
		if len(sessions) != 1 {
			return false, false, ""
		}
		session := sessions[0]
		return session.open, session.running, session.status
	}
	runtimeSessionNodes := func(scope, workspaceRoot, topicID, projectColor string) []ProjectNode {
		key := topicSummaryKey(scope, workspaceRoot, topicID)
		sessions := runtimeSessionsByTopic[key]
		if len(sessions) <= 1 {
			return nil
		}
		nodes := make([]ProjectNode, 0, len(sessions))
		for _, session := range sessions {
			kind := "session"
			if scope == "global" {
				kind = "global_session"
			}
			nodes = append(nodes, ProjectNode{
				Key:              projectSessionNodeKey(scope, session.sessionPath),
				Kind:             kind,
				Label:            session.label,
				Root:             workspaceRoot,
				TopicID:          topicID,
				SessionPath:      session.sessionPath,
				ProjectColor:     projectColor,
				Turns:            session.turns,
				CreatedAt:        session.createdAt,
				LastActivityAt:   session.lastActivityAt,
				Open:             session.open,
				Running:          session.running,
				Status:           session.status,
				Recovered:        session.recovered,
				RecoveryReason:   session.recoveryReason,
				RecoveryDigest:   session.recoveryDigest,
				RecoveryParentID: session.recoveryParentID,
			})
		}
		return nodes
	}

	// Global section.
	globalTitleMap := loadTopicTitles("")
	globalCreatedMap := loadTopicCreatedAts("")
	if len(globalTitleMap) > 0 || len(f.Projects) == 0 {
		globalTitle := strings.TrimSpace(f.GlobalTitle)
		if globalTitle == "" {
			globalTitle = "Global"
		}
		globalColor := normalizeProjectColor(f.GlobalColor)
		globalTopicIDs := pinnedTopicIDs(orderedTopicIDs(f.GlobalTopics, globalTitleMap), f.GlobalPinnedTopics)
		children := make([]ProjectNode, 0, len(globalTopicIDs))
		for _, id := range globalTopicIDs {
			if deletedTopicSet[id] {
				continue
			}
			title := globalTitleMap[id]
			summaryKey := topicSummaryKey("global", "", id)
			summary := topicSummaries[summaryKey]
			open, running, status := topicRuntimeStatus(summaryKey)
			pinned := containsDesktopString(f.GlobalPinnedTopics, id)
			if topicHiddenAsRecoveryOnly(summary, pinned, runtimeSessionsByTopic[summaryKey]) {
				continue
			}
			children = append(children, ProjectNode{
				Key:            "global_topic_" + id,
				Kind:           "global_topic",
				Label:          title,
				TopicID:        id,
				ProjectColor:   globalColor,
				Turns:          summary.turns,
				CreatedAt:      topicCreatedAtForTree(globalCreatedMap, id),
				LastActivityAt: summary.lastActivityAt,
				Open:           open,
				Running:        running,
				Status:         status,
				Pinned:         pinned,
				Children:       runtimeSessionNodes("global", "", id, globalColor),
			})
		}
		out = append(out, ProjectNode{
			Key:          "global_folder",
			Kind:         "global_folder",
			Label:        globalTitle,
			Root:         globalWorkspaceRoot(),
			ProjectColor: globalColor,
			Children:     children,
		})
	}

	// Project sections.
	type projectTopics struct {
		project    desktopProject
		titles     map[string]string
		createdAts map[string]int64
	}
	projectTopicResults := make([]projectTopics, len(f.Projects))
	var topicLoadWg sync.WaitGroup
	for i, p := range f.Projects {
		i, p := i, p
		topicLoadWg.Add(1)
		go func() {
			defer topicLoadWg.Done()
			projectTopicResults[i] = projectTopics{
				project:    p,
				titles:     loadTopicTitles(p.Root),
				createdAts: loadTopicCreatedAts(p.Root),
			}
		}()
	}
	topicLoadWg.Wait()
	for _, loaded := range projectTopicResults {
		p := loaded.project
		title := p.Title
		if title == "" {
			title = workspaceName(p.Root)
		}
		node := ProjectNode{
			Key:    "project_" + p.Root,
			Kind:   "project",
			Root:   p.Root,
			Pinned: containsDesktopString(f.PinnedProjects, p.Root),
		}

		// Gather topics: explicit topic list + all known topic titles.
		titleMap := loaded.titles
		createdMap := loaded.createdAts
		topicIDs := pinnedTopicIDs(orderedTopicIDs(p.Topics, titleMap), p.PinnedTopics)

		children := make([]ProjectNode, 0, len(topicIDs))
		for _, tid := range topicIDs {
			if deletedTopicSet[tid] {
				continue
			}
			topicTitle := strings.TrimSpace(titleMap[tid])
			if topicTitle == "" {
				topicTitle = defaultTopicTitle
			}
			summaryKey := topicSummaryKey("project", p.Root, tid)
			summary := topicSummaries[summaryKey]
			open, running, status := topicRuntimeStatus(summaryKey)
			pinned := containsDesktopString(p.PinnedTopics, tid)
			if topicHiddenAsRecoveryOnly(summary, pinned, runtimeSessionsByTopic[summaryKey]) {
				continue
			}
			children = append(children, ProjectNode{
				Key:            "topic_" + tid,
				Kind:           "topic",
				Label:          topicTitle,
				Root:           p.Root,
				TopicID:        tid,
				ProjectColor:   p.Color,
				Turns:          summary.turns,
				CreatedAt:      topicCreatedAtForTree(createdMap, tid),
				LastActivityAt: summary.lastActivityAt,
				Open:           open,
				Running:        running,
				Status:         status,
				Pinned:         pinned,
				Children:       runtimeSessionNodes("project", p.Root, tid, p.Color),
			})
		}
		node.Label = title
		node.ProjectColor = p.Color
		node.Children = children
		out = append(out, node)
	}

	return applyPinnedProjectOrder(applyProjectTreeOrder(out, f.SidebarOrder), f.PinnedProjects)
}

func topicSummaryKey(scope, workspaceRoot, topicID string) string {
	if scope == "global" {
		return "global::" + topicID
	}
	return "project:" + workspaceRoot + ":" + topicID
}

func projectSessionNodeKey(scope, sessionPath string) string {
	sum := sha256.Sum256([]byte(sessionRuntimeKey(sessionPath)))
	return scope + "_session_" + hex.EncodeToString(sum[:8])
}

func runtimeSessionTreeLabel(tab *WorkspaceTab, info agent.SessionInfo, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	if preview := topicTitleFromText(info.Preview); preview != "" {
		return preview
	}
	if tab != nil {
		if title := strings.TrimSpace(tab.TopicTitle); title != "" {
			return title
		}
	}
	if path := strings.TrimSpace(info.Path); path != "" {
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if tab != nil {
		if path := strings.TrimSpace(tab.currentSessionPath()); path != "" {
			return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}
	return defaultTopicTitle
}

func unixMilliOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// ContextPanelInfo is the right-side panel's data for one tab.
type ContextPanelInfo struct {
	UsedTokens       int `json:"usedTokens"`
	WindowTokens     int `json:"windowTokens"`
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
	ReasoningTokens  int `json:"reasoningTokens"`
	CacheHitTokens   int `json:"cacheHitTokens"`
	CacheMissTokens  int `json:"cacheMissTokens"`
	// Session-cumulative token counts (from telemetry, atomic snapshot).
	// Separate from the per-turn fields above so existing consumers (status bar
	// turn tokens, donut chart) are unaffected.
	SessionCacheHitTokens   int                         `json:"sessionCacheHitTokens"`
	SessionCacheMissTokens  int                         `json:"sessionCacheMissTokens"`
	SessionCompletionTokens int                         `json:"sessionCompletionTokens"`
	RequestCount            int                         `json:"requestCount"`
	ElapsedMs               int64                       `json:"elapsedMs"`
	SessionCost             float64                     `json:"sessionCost"`
	SessionCurrency         string                      `json:"sessionCurrency,omitempty"`
	SessionCostUsd          float64                     `json:"sessionCostUsd,omitempty"`
	Sources                 map[string]usageSourceStats `json:"sources,omitempty"`
	Mock                    bool                        `json:"mock,omitempty"`
	ReadFiles               []readFileRecord            `json:"readFiles"`
	ChangedFiles            []ChangedFileInfo           `json:"changedFiles"`
}

type ChangedFileInfo struct {
	Path         string   `json:"path"`
	OldPath      string   `json:"oldPath,omitempty"`
	Sources      []string `json:"sources"`
	GitStatus    string   `json:"gitStatus,omitempty"`
	Turns        []int    `json:"turns"`
	LatestPrompt string   `json:"latestPrompt,omitempty"`
	LatestTime   int64    `json:"latestTime,omitempty"`
}

// ContextPanel returns the context usage, read files, and changed files for a
// specific tab.
func (a *App) ContextPanel(tabID string) ContextPanelInfo {
	a.mu.RLock()
	tab, ok := a.tabs[tabID]
	var ctrl control.SessionAPI
	if ok && tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if !ok {
		return ContextPanelInfo{ReadFiles: []readFileRecord{}, ChangedFiles: []ChangedFileInfo{}}
	}

	info := ContextPanelInfo{ReadFiles: []readFileRecord{}, ChangedFiles: []ChangedFileInfo{}}
	if ctrl != nil {
		if sp := ctrl.SessionPath(); sp != "" {
			tab.syncTelemetryToSession(sp)
		}
		used, window := ctrl.ContextSnapshot()
		info.UsedTokens = used
		info.WindowTokens = window
		// Per-turn token breakdown from LastUsage (same snapshot as UsedTokens)
		// so the donut segments are proportional to the current context fill,
		// not inflated by cumulative session totals.
		if u := ctrl.LastUsage(); u != nil {
			info.PromptTokens = u.PromptTokens
			info.CompletionTokens = u.CompletionTokens
			info.ReasoningTokens = u.ReasoningTokens
			info.CacheHitTokens = u.CacheHitTokens
			info.CacheMissTokens = u.CacheMissTokens
		}
	}

	telemetry := tab.telemetrySnapshot()
	if records := telemetry.ReadFiles; records != nil {
		info.ReadFiles = records
	}
	usage := telemetry.Usage
	info.TotalTokens = usage.TotalTokens
	info.RequestCount = usage.RequestCount
	info.ElapsedMs = usage.ElapsedMs
	info.SessionCost = usage.SessionCost
	info.SessionCurrency = usage.SessionCurrency
	info.SessionCostUsd = usage.SessionCostUsd
	info.Sources = usage.Sources
	info.SessionCacheHitTokens = usage.CacheHitTokens
	info.SessionCacheMissTokens = usage.CacheMissTokens
	info.SessionCompletionTokens = usage.CompletionTokens

	// Gather workspace changes for this tab's root.
	if ctrl != nil && tab.WorkspaceRoot != "" {
		for _, meta := range ctrl.Checkpoints() {
			for _, path := range meta.Paths {
				info.ChangedFiles = append(info.ChangedFiles, ChangedFileInfo{
					Path:         path,
					Sources:      []string{"session"},
					Turns:        []int{meta.Turn},
					LatestPrompt: meta.Prompt,
					LatestTime:   meta.Time.UnixMilli(),
				})
			}
		}
	}

	return info
}

// --- utility ----------------------------------------------------------------

func (a *App) newUniqueTabIDLocked() string {
	for {
		id := newTabID()
		if _, exists := a.tabs[id]; !exists {
			return id
		}
	}
}

func (a *App) restoredTabIDLocked(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return a.newUniqueTabIDLocked()
	}
	if _, exists := a.tabs[id]; exists {
		return a.newUniqueTabIDLocked()
	}
	return id
}

func normalizeTabMode(mode string) string {
	switch mode {
	case "plan", "yolo", "plan-yolo", "yolo-plan":
		if mode == "yolo-plan" {
			return "plan-yolo"
		}
		return mode
	default:
		return "normal"
	}
}

func tabModeFromAxes(plan, autoApproveTools bool) string {
	switch {
	case plan && autoApproveTools:
		return "plan-yolo"
	case plan:
		return "plan"
	case autoApproveTools:
		return "yolo"
	default:
		return "normal"
	}
}

func tabModeHasPlan(mode string) bool {
	switch normalizeTabMode(mode) {
	case "plan", "plan-yolo":
		return true
	default:
		return false
	}
}

func tabModeHasAutoApproveTools(mode string) bool {
	switch normalizeTabMode(mode) {
	case "yolo", "plan-yolo":
		return true
	default:
		return false
	}
}

func currentTabMode(tab *WorkspaceTab) string {
	if tab == nil {
		return "normal"
	}
	if tab.Ctrl != nil {
		return tabModeFromAxes(tab.Ctrl.PlanMode(), tab.Ctrl.AutoApproveTools())
	}
	return normalizeTabMode(tab.mode)
}

func currentTabGoal(tab *WorkspaceTab) string {
	if tab == nil {
		return ""
	}
	if tab.Ctrl != nil {
		return tab.Ctrl.Goal()
	}
	return strings.TrimSpace(tab.goal)
}

func currentTabGoalStatus(tab *WorkspaceTab) string {
	if tab == nil {
		return control.GoalStatusStopped
	}
	if tab.Ctrl != nil {
		return tab.Ctrl.GoalStatus()
	}
	if strings.TrimSpace(tab.goal) != "" {
		return control.GoalStatusRunning
	}
	return control.GoalStatusStopped
}

func currentTabCollaborationMode(tab *WorkspaceTab) string {
	if tab == nil {
		return "normal"
	}
	if tabModeHasPlan(currentTabMode(tab)) {
		return "plan"
	}
	if strings.TrimSpace(currentTabGoal(tab)) != "" && currentTabGoalStatus(tab) == control.GoalStatusRunning {
		return "goal"
	}
	return "normal"
}

func currentTabToolApprovalMode(tab *WorkspaceTab) string {
	if tab == nil {
		return control.ToolApprovalAsk
	}
	if tab.Ctrl != nil {
		return tab.Ctrl.ToolApprovalMode()
	}
	return normalizeToolApprovalMode(tab.toolApprovalMode)
}

func currentTabTokenMode(tab *WorkspaceTab) string {
	if tab == nil {
		return boot.TokenModeFull
	}
	return boot.NormalizeTokenMode(tab.tokenMode)
}

// tabRuntimeSnapshot is a consistent under-a.mu copy of the per-tab fields
// that bound methods and rebuild paths need after releasing the lock. The
// build/rebuild goroutines write these fields under a.mu, so lock-free reads
// from other goroutines are data races (same class as the sessionLease race
// fixed for #5955). Controller methods are invoked on the snapshot's ctrl
// AFTER unlocking, never while holding a.mu.
type tabRuntimeSnapshot struct {
	ctrl             control.SessionAPI
	sink             *tabEventSink
	label            string
	ready            bool
	readOnly         bool
	startupErr       string
	scope            string
	workspaceRoot    string
	sessionPath      string
	topicID          string
	topicTitle       string
	sharedHostKey    string
	model            string
	effort           *string
	tokenMode        string
	mode             string
	goal             string
	toolApprovalMode string
}

// snapshotTabRuntimeLocked copies the racy per-tab fields. Callers must hold
// a.mu (read or write side).
func snapshotTabRuntimeLocked(tab *WorkspaceTab) tabRuntimeSnapshot {
	if tab == nil {
		return tabRuntimeSnapshot{}
	}
	return tabRuntimeSnapshot{
		ctrl:             tab.Ctrl,
		sink:             tab.sink,
		label:            tab.Label,
		ready:            tab.Ready,
		readOnly:         tab.ReadOnly,
		startupErr:       tab.StartupErr,
		scope:            tab.Scope,
		workspaceRoot:    tab.WorkspaceRoot,
		sessionPath:      tab.SessionPath,
		topicID:          tab.TopicID,
		topicTitle:       tab.TopicTitle,
		sharedHostKey:    tab.SharedHostKey,
		model:            tab.model,
		effort:           cloneStringPtr(tab.effort),
		tokenMode:        tab.tokenMode,
		mode:             tab.mode,
		goal:             tab.goal,
		toolApprovalMode: tab.toolApprovalMode,
	}
}

func (a *App) tabRuntimeSnapshot(tab *WorkspaceTab) tabRuntimeSnapshot {
	if tab == nil {
		return tabRuntimeSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return snapshotTabRuntimeLocked(tab)
}

// Snapshot-based forms of the currentTabX helpers, for callers that already
// hold a consistent tabRuntimeSnapshot.

func (s tabRuntimeSnapshot) currentMode() string {
	if s.ctrl != nil {
		return tabModeFromAxes(s.ctrl.PlanMode(), s.ctrl.AutoApproveTools())
	}
	return normalizeTabMode(s.mode)
}

func (s tabRuntimeSnapshot) currentGoal() string {
	if s.ctrl != nil {
		return s.ctrl.Goal()
	}
	return strings.TrimSpace(s.goal)
}

func (s tabRuntimeSnapshot) currentGoalStatus() string {
	if s.ctrl != nil {
		return s.ctrl.GoalStatus()
	}
	if strings.TrimSpace(s.goal) != "" {
		return control.GoalStatusRunning
	}
	return control.GoalStatusStopped
}

func (s tabRuntimeSnapshot) collaborationMode() string {
	if tabModeHasPlan(s.currentMode()) {
		return "plan"
	}
	if strings.TrimSpace(s.currentGoal()) != "" && s.currentGoalStatus() == control.GoalStatusRunning {
		return "goal"
	}
	return "normal"
}

func (s tabRuntimeSnapshot) currentToolApprovalMode() string {
	if s.ctrl != nil {
		return s.ctrl.ToolApprovalMode()
	}
	return normalizeToolApprovalMode(s.toolApprovalMode)
}

func (s tabRuntimeSnapshot) currentTokenMode() string {
	return boot.NormalizeTokenMode(s.tokenMode)
}

func persistedTabTokenMode(mode string) string {
	mode = boot.NormalizeTokenMode(mode)
	if mode == boot.TokenModeEconomy {
		return mode
	}
	return ""
}

func normalizeToolApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case control.ToolApprovalAuto:
		return control.ToolApprovalAuto
	case control.ToolApprovalYolo, "full", "full-access", "bypass":
		return control.ToolApprovalYolo
	default:
		return control.ToolApprovalAsk
	}
}

func persistedToolApprovalMode(mode string) string {
	switch normalizeToolApprovalMode(mode) {
	case control.ToolApprovalAuto, control.ToolApprovalYolo:
		return normalizeToolApprovalMode(mode)
	default:
		return ""
	}
}

// persistedTabMode is the composer mode saved with a tab so it survives reload
// and app relaunch. plan, yolo, and plan-yolo are remembered (a restored yolo
// tab keeps its status-bar indicator); "normal" is the default and isn't
// persisted. (#3517)
func persistedTabMode(mode string) string {
	switch normalizeTabMode(mode) {
	case "plan", "yolo", "plan-yolo":
		return normalizeTabMode(mode)
	}
	return ""
}

func newTabID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		now := time.Now().UTC()
		return "tab_" + now.Format("20060102150405") + "_" + fmt.Sprintf("%09d", now.Nanosecond())
	}
	return "tab_" + hex.EncodeToString(b[:])
}

func newTopicID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		now := time.Now().UTC()
		return "topic_" + now.Format("20060102-150405") + "_" + fmt.Sprintf("%09d", now.Nanosecond())
	}
	return "topic_" + time.Now().UTC().Format("20060102-150405") + "_" + hex.EncodeToString(b[:])
}

func globalWorkspaceRoot() string {
	return filepath.Join(desktopConfigDir(), "global-workspace")
}

func ensureGlobalWorkspaceRoot() (string, error) {
	root := globalWorkspaceRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func globalTabWorkspaceRoot() string {
	root, err := ensureGlobalWorkspaceRoot()
	if err != nil {
		return globalWorkspaceRoot()
	}
	return root
}

func loadPinnedTabSession(dir, sessionPath string) (*agent.Session, string, bool) {
	return loadPinnedTabSessionWithPreloadAndMigrationFallback(dir, sessionPath, loadedTabSession{}, true)
}

func loadPinnedTabSessionWithPreload(dir, sessionPath string, preloaded loadedTabSession) (*agent.Session, string, bool) {
	return loadPinnedTabSessionWithPreloadAndMigrationFallback(dir, sessionPath, preloaded, false)
}

func loadPinnedTabSessionWithPreloadAndMigrationFallback(dir, sessionPath string, preloaded loadedTabSession, allowMigrationFallback bool) (*agent.Session, string, bool) {
	path, ok := pinnedTabSessionPath(dir, sessionPath)
	if !ok && allowMigrationFallback {
		path, ok = migratedPinnedTabSessionPath(dir, sessionPath)
	}
	if !ok {
		return nil, "", false
	}
	if agent.IsCleanupPending(path) {
		return nil, "", false
	}
	if preloaded.matches(path) {
		if preloaded.Session != nil && len(preloaded.Session.Snapshot()) == 0 {
			return nil, path, true
		}
		return preloaded.Session, path, true
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, true
		}
		return nil, "", false
	}
	// An empty file (0 messages) is a pre-created placeholder, not a real
	// session to resume.  Treating it as valid would make ctrl.Resume replace
	// the executor's live session (with system prompt) with the empty one,
	// causing the saved transcript to lack the agent identity contract.
	if len(loaded.Snapshot()) == 0 {
		return nil, path, true
	}
	return loaded, path, true
}

func migratedPinnedTabSessionPath(dir, sessionPath string) (string, bool) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" || dir == "" || !filepath.IsAbs(sessionPath) {
		return "", false
	}
	if _, err := os.Stat(sessionPath); err == nil || !os.IsNotExist(err) {
		return "", false
	}
	base := filepath.Base(sessionPath)
	if base == "." || base == string(filepath.Separator) || !strings.HasSuffix(base, ".jsonl") {
		return "", false
	}
	path, _, err := validateSessionPath(dir, filepath.Join(dir, base))
	if err != nil {
		return "", false
	}
	return path, true
}

func pinnedTabSessionPath(dir, sessionPath string) (string, bool) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" || dir == "" {
		return "", false
	}
	path, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		if filepath.IsAbs(sessionPath) {
			return "", false
		}
		base := filepath.Base(sessionPath)
		if base == "." || base == string(filepath.Separator) || !strings.HasSuffix(base, ".jsonl") {
			return "", false
		}
		path, _, err = validateSessionPath(dir, filepath.Join(dir, base))
		if err != nil {
			return "", false
		}
	}
	return path, true
}

// saveTabSessionMeta persists the tab's scope/topic/mode fields into the
// session's branch-meta sidecar at path. Tab fields are snapshotted under a.mu
// (controller reads happen off-lock) so a concurrent tab mutation can't tear
// the persisted record.
func (a *App) saveTabSessionMeta(tab *WorkspaceTab, path string) error {
	if tab == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	a.mu.RLock()
	ctrl := tab.Ctrl
	snap := tabSessionMetaSnapshot{
		path:             path,
		scope:            tab.Scope,
		workspaceRoot:    tab.WorkspaceRoot,
		topicID:          tab.TopicID,
		topicTitle:       tab.TopicTitle,
		tokenMode:        boot.NormalizeTokenMode(tab.tokenMode),
		mode:             normalizeTabMode(tab.mode),
		toolApprovalMode: normalizeToolApprovalMode(tab.toolApprovalMode),
		goal:             strings.TrimSpace(tab.goal),
	}
	a.mu.RUnlock()
	if ctrl != nil {
		snap.mode = tabModeFromAxes(ctrl.PlanMode(), ctrl.AutoApproveTools())
		snap.toolApprovalMode = normalizeToolApprovalMode(ctrl.ToolApprovalMode())
		if goal := strings.TrimSpace(ctrl.Goal()); goal != "" && ctrl.GoalStatus() == control.GoalStatusRunning {
			snap.goal = goal
		} else {
			snap.goal = ""
		}
	}
	return saveTabSessionMetaSnapshot(snap)
}

type tabSessionMetaSnapshot struct {
	path             string
	scope            string
	workspaceRoot    string
	topicID          string
	topicTitle       string
	tokenMode        string
	mode             string
	toolApprovalMode string
	goal             string
}

func (a *App) saveTabSessionMetaForCurrentSession(tab *WorkspaceTab) error {
	snap, ok := a.tabSessionMetaSnapshotForCurrentSession(tab)
	if !ok {
		return nil
	}
	return saveTabSessionMetaSnapshot(snap)
}

func (a *App) tabSessionMetaSnapshotForCurrentSession(tab *WorkspaceTab) (tabSessionMetaSnapshot, bool) {
	if tab == nil {
		return tabSessionMetaSnapshot{}, false
	}
	a.mu.RLock()
	if tab.ID != "" && a.tabs[tab.ID] != tab {
		a.mu.RUnlock()
		return tabSessionMetaSnapshot{}, false
	}
	readOnly := tab.ReadOnly
	ctrl := tab.Ctrl
	storedPath := strings.TrimSpace(tab.SessionPath)
	scope := tab.Scope
	workspaceRoot := tab.WorkspaceRoot
	topicID := tab.TopicID
	topicTitle := tab.TopicTitle
	tokenMode := boot.NormalizeTokenMode(tab.tokenMode)
	mode := normalizeTabMode(tab.mode)
	toolApprovalMode := normalizeToolApprovalMode(tab.toolApprovalMode)
	goal := strings.TrimSpace(tab.goal)
	a.mu.RUnlock()
	if readOnly {
		return tabSessionMetaSnapshot{}, false
	}

	ctrlPath := ""
	ctrlDir := ""
	activeWork := false
	if ctrl != nil {
		ctrlPath = strings.TrimSpace(ctrl.SessionPath())
		if dir, ok := safeControllerSessionDir(ctrl); ok {
			ctrlDir = strings.TrimSpace(dir)
		}
		status := ctrl.RuntimeStatus()
		activeWork = status.Running || status.PendingPrompt || status.BackgroundJobs > 0
		mode = tabModeFromAxes(ctrl.PlanMode(), ctrl.AutoApproveTools())
		toolApprovalMode = normalizeToolApprovalMode(ctrl.ToolApprovalMode())
		if ctrl.GoalStatus() == control.GoalStatusRunning {
			goal = strings.TrimSpace(ctrl.Goal())
		} else {
			goal = ""
		}
	}

	currentPath := ctrlPath
	if currentPath == "" {
		currentPath = storedPath
	}
	if currentPath == "" {
		return tabSessionMetaSnapshot{}, false
	}

	sessionDir := desktopSessionDir("")
	if workspaceRoot != "" {
		sessionDir = desktopSessionDir(workspaceRoot)
	} else if ctrlDir != "" {
		sessionDir = ctrlDir
	}
	runtimeDir := sessionDir
	if ctrlDir != "" {
		if _, _, err := validateSessionPath(ctrlDir, currentPath); err == nil {
			runtimeDir = ctrlDir
		}
	}
	if topicID == "" && !activeWork && storedPath != "" && sessionPathHasNoContent(sessionDir, storedPath) {
		return tabSessionMetaSnapshot{}, false
	}
	path := tabSessionMetaPathForSession(runtimeDir, sessionDir, currentPath)
	if path == "" {
		return tabSessionMetaSnapshot{}, false
	}
	return tabSessionMetaSnapshot{
		path:             path,
		scope:            scope,
		workspaceRoot:    workspaceRoot,
		topicID:          topicID,
		topicTitle:       topicTitle,
		tokenMode:        tokenMode,
		mode:             mode,
		toolApprovalMode: toolApprovalMode,
		goal:             goal,
	}, true
}

func saveTabSessionMetaSnapshot(snap tabSessionMetaSnapshot) error {
	if strings.TrimSpace(snap.path) == "" {
		return nil
	}
	// Read-modify-write on the branch-meta sidecar: hold the per-path meta lock
	// so agent-side writers (autosave UpdateSessionMeta, in-flight markers)
	// can't interleave and drop fields.
	unlock := agent.LockSessionMetaPath(snap.path)
	defer unlock()
	m, err := agent.EnsureBranchMeta(snap.path)
	if err != nil {
		return err
	}
	scope := snap.scope
	workspaceRoot := snap.workspaceRoot
	if ownerScope, ownerRoot, _, ok := legacyMigrationTargetForDir(filepath.Dir(snap.path)); ok {
		if ownerScope == "project" {
			scope = ownerScope
			workspaceRoot = ownerRoot
		}
	}
	if scope == "project" {
		workspaceRoot = normalizeProjectRoot(workspaceRoot)
	} else {
		scope = "global"
		workspaceRoot = ""
	}
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = snap.topicID
	m.TopicTitle = snap.topicTitle
	m.TokenMode = persistedTabTokenMode(snap.tokenMode)
	m.Mode = persistedTabMode(snap.mode)
	m.ToolApprovalMode = persistedToolApprovalMode(snap.toolApprovalMode)
	m.Goal = strings.TrimSpace(snap.goal)
	if err := agent.SaveBranchMetaPreserveUpdated(snap.path, m); err != nil {
		return err
	}
	invalidateTopicSessionIndexForPath(snap.path)
	return nil
}

func tabSessionMetaPathForSession(runtimeDir, sessionDir, sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	for _, dir := range []string{runtimeDir, sessionDir} {
		if resolved, ok := pinnedTabSessionPath(dir, sessionPath); ok {
			return resolved
		}
	}
	path := canonicalTabSessionPath(sessionPath)
	if filepath.IsAbs(path) {
		return path
	}
	return ""
}

type tabSessionProfile struct {
	tokenMode        string
	mode             string
	toolApprovalMode string
	goal             string
}

func defaultTabSessionProfile() tabSessionProfile {
	return tabSessionProfile{
		tokenMode:        boot.TokenModeFull,
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
	}
}

func tabSessionProfileFromMeta(sessionPath string, meta agent.BranchMeta) tabSessionProfile {
	profile := defaultTabSessionProfile()
	profile.tokenMode = boot.NormalizeTokenMode(meta.TokenMode)
	profile.mode = normalizeTabMode(meta.Mode)
	profile.toolApprovalMode = normalizeToolApprovalMode(meta.ToolApprovalMode)
	if profile.toolApprovalMode == control.ToolApprovalAsk && tabModeHasAutoApproveTools(meta.Mode) {
		profile.toolApprovalMode = control.ToolApprovalYolo
	}
	profile.goal = runningTabSessionGoal(sessionPath, meta.Goal)
	return profile
}

func loadTabSessionProfile(sessionPath string) tabSessionProfile {
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return defaultTabSessionProfile()
	}
	return tabSessionProfileFromMeta(sessionPath, meta)
}

func applyTabSessionProfile(tab *WorkspaceTab, profile tabSessionProfile) {
	if tab == nil {
		return
	}
	tab.tokenMode = boot.NormalizeTokenMode(profile.tokenMode)
	tab.mode = normalizeTabMode(profile.mode)
	tab.toolApprovalMode = normalizeToolApprovalMode(profile.toolApprovalMode)
	if tab.toolApprovalMode == control.ToolApprovalAsk && tabModeHasAutoApproveTools(tab.mode) {
		tab.toolApprovalMode = control.ToolApprovalYolo
	}
	tab.mode = tabModeFromAxes(tabModeHasPlan(tab.mode), tab.toolApprovalMode == control.ToolApprovalYolo)
	tab.goal = strings.TrimSpace(profile.goal)
}

func persistedTabGoal(tab *WorkspaceTab) string {
	goal := strings.TrimSpace(currentTabGoal(tab))
	if goal == "" || currentTabGoalStatus(tab) != control.GoalStatusRunning {
		return ""
	}
	return goal
}

type tabSessionGoalState struct {
	Goal   string `json:"goal,omitempty"`
	Status string `json:"status,omitempty"`
}

func runningTabSessionGoal(sessionPath, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return ""
	}
	data, err := os.ReadFile(store.SessionGoalState(sessionPath))
	if err != nil {
		return fallback
	}
	var state tabSessionGoalState
	if err := json.Unmarshal(data, &state); err != nil {
		return fallback
	}
	switch state.Status {
	case control.GoalStatusRunning:
		if goal := strings.TrimSpace(state.Goal); goal != "" {
			return goal
		}
		return fallback
	case "", control.GoalStatusStopped:
		return ""
	default:
		return ""
	}
}

func canonicalTabSessionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if validPath, _, err := validateSessionPath(config.SessionDir(), path); err == nil {
		return validPath
	}
	// Project-scope sessions live outside config.SessionDir(), so validation
	// against it always fails. Still normalize the shape: without clean+abs,
	// the same file spelled with a different separator or a relative prefix
	// splits into distinct runtime keys.
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		return abs
	}
	return cleaned
}

func (a *App) rememberTabSessionPath(tab *WorkspaceTab, path string) {
	path = canonicalTabSessionPath(path)
	if tab == nil || path == "" {
		return
	}
	a.mu.Lock()
	if current := a.tabs[tab.ID]; current == tab {
		tab.SessionPath = path
		a.saveTabsLocked()
	} else {
		tab.SessionPath = path
	}
	a.mu.Unlock()
}

func (a *App) persistTabSessionPath(tab *WorkspaceTab, path string) {
	path = canonicalTabSessionPath(path)
	if tab == nil || path == "" {
		return
	}
	if reconciled, ok := a.reconcileTabWithSessionPath(tab, path); ok {
		path = canonicalTabSessionPath(reconciled)
	}
	_ = a.saveTabSessionMeta(tab, path)
	a.rememberTabSessionPath(tab, path)
}

func (a *App) knownSessionDirs() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		if seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	add(config.SessionDir()) // legacy/global sessions from earlier desktop builds
	add(desktopSessionDir(globalWorkspaceRoot()))
	for _, project := range loadProjectsFile().Projects {
		dir := desktopSessionDir(project.Root)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue // project dir removed or external volume unmounted
		}
		add(dir)
	}
	a.mu.RLock()
	for _, tab := range a.tabs {
		add(tabSessionDir(tab))
	}
	for _, tab := range a.detachedSessions {
		add(tabSessionDir(tab))
	}
	a.mu.RUnlock()
	return out
}

func topicSessionMatchMatchesTarget(match topicSessionMatch, scope, workspaceRoot string) bool {
	if scope == "project" {
		return match.scope == "project" && normalizeProjectRoot(match.workspaceRoot) == normalizeProjectRoot(workspaceRoot)
	}
	return match.scope == "" || match.scope == "global"
}

func (a *App) findTopicSessionForTarget(scope, workspaceRoot, topicID string) (string, string) {
	return a.findTopicSessionForTargetByContent(scope, workspaceRoot, topicID, false)
}

func (a *App) findTopicContentSessionForTarget(scope, workspaceRoot, topicID string) (string, string) {
	return a.findTopicSessionForTargetByContent(scope, workspaceRoot, topicID, true)
}

func (a *App) findTopicSessionForTargetByContent(scope, workspaceRoot, topicID string, requireContent bool) (string, string) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", ""
	}
	var bestPath string
	var bestDir string
	var bestTime time.Time
	for _, dir := range a.knownSessionDirs() {
		for _, match := range topicSessionMatches(dir, topicID) {
			if requireContent && !sessionFileHasConversationContent(match.path) {
				continue
			}
			if !topicSessionMatchMatchesTarget(match, scope, workspaceRoot) {
				continue
			}
			if bestPath == "" || match.updatedAt.After(bestTime) ||
				(match.updatedAt.Equal(bestTime) && match.path < bestPath) {
				bestPath = match.path
				bestDir = dir
				bestTime = match.updatedAt
			}
		}
	}
	return bestPath, bestDir
}

type topicSessionFileSignature struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

type topicSessionMatch struct {
	path          string
	updatedAt     time.Time
	scope         string
	workspaceRoot string
}

type topicSessionDirIndex struct {
	signature []topicSessionFileSignature
	byTopic   map[string][]topicSessionMatch
}

// sessionListCache caches ListSessions results per directory so that
// ListProjectTree (called on every sidebar render) does not re-read every
// session dir from disk. Invalidated by emitProjectTreeChanged — any create/
// delete/rename session bumps the project tree version.
type sessionListCacheEntry struct {
	infos  []agent.SessionInfo
	titles map[string]string
}

type sessionListCache struct {
	mu      sync.Mutex
	byDir   map[string]sessionListCacheEntry
	version atomic.Uint64
}

func (c *sessionListCache) get(dir string) ([]agent.SessionInfo, map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byDir[dir]
	if !ok {
		return nil, nil, false
	}
	return e.infos, e.titles, true
}

func (c *sessionListCache) versionToken() uint64 {
	return c.version.Load()
}

func (c *sessionListCache) put(dir string, infos []agent.SessionInfo, titles map[string]string, token uint64) {
	if c.version.Load() != token {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version.Load() != token {
		return
	}
	c.byDir[dir] = sessionListCacheEntry{infos: infos, titles: titles}
}

func (c *sessionListCache) invalidate() {
	c.version.Add(1)
	c.mu.Lock()
	c.byDir = map[string]sessionListCacheEntry{}
	c.mu.Unlock()
}

var projectSessionCache = &sessionListCache{byDir: map[string]sessionListCacheEntry{}}

// mergeSessionInfos merges one directory's session listing into the maps used by
// ListProjectTree. The result collection loop calls it serially.
func mergeSessionInfos(dir string, infos []agent.SessionInfo, titles map[string]string, sessionInfos map[string]agent.SessionInfo, sessionTitles map[string]string, topicSummaries map[string]topicSummary) {
	for _, info := range infos {
		sessionKey := sessionRuntimeKey(info.Path)
		if sessionKey != "" {
			sessionInfos[sessionKey] = info
			title := strings.TrimSpace(info.CustomTitle)
			if title == "" {
				title = titles[filepath.Base(info.Path)]
			}
			sessionTitles[sessionKey] = title
		}
		if strings.TrimSpace(info.TopicID) == "" {
			continue
		}
		key := topicSummaryKey(info.Scope, info.WorkspaceRoot, info.TopicID)
		summary := topicSummaries[key]
		lastActivityAt := info.LastActivityAt.UnixMilli()
		if info.Recovered {
			// A conflict copy duplicates its original, so its turns would
			// double-count — but its activity is real: once a tab adopts the
			// copy as its live transcript, all new work lands here, and
			// skipping it would freeze the topic's recency, unread state, and
			// time filters at the original's last save.
			summary.hasRecoveryOnly = true
			if lastActivityAt > summary.lastActivityAt {
				summary.lastActivityAt = lastActivityAt
			}
			topicSummaries[key] = summary
			continue
		}
		summary.hasNormalSession = true
		summary.turns += info.Turns
		if lastActivityAt > summary.lastActivityAt {
			summary.lastActivityAt = lastActivityAt
		}
		topicSummaries[key] = summary
	}
}

var topicSessionIndexCache = struct {
	sync.Mutex
	byDir map[string]topicSessionDirIndex
}{byDir: map[string]topicSessionDirIndex{}}

func topicSessionDirKey(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func topicSessionDirSnapshot(dir string) ([]topicSessionFileSignature, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	signature := []topicSessionFileSignature{}
	sessionNames := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		isSession := store.IsSessionTranscriptName(name)
		isMeta := strings.HasSuffix(name, ".jsonl.meta")
		if !isSession && !isMeta {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		signature = append(signature, topicSessionFileSignature{
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
		if isSession {
			sessionNames = append(sessionNames, name)
		}
	}
	sort.Slice(signature, func(i, j int) bool {
		return signature[i].Name < signature[j].Name
	})
	sort.Strings(sessionNames)
	return signature, sessionNames, nil
}

func topicSessionSignaturesEqual(a, b []topicSessionFileSignature) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func topicSessionIndexForDir(dir string) (topicSessionDirIndex, error) {
	key := topicSessionDirKey(dir)
	if key == "" {
		return topicSessionDirIndex{}, nil
	}
	signature, sessionNames, err := topicSessionDirSnapshot(key)
	if err != nil {
		if os.IsNotExist(err) {
			return topicSessionDirIndex{}, nil
		}
		return topicSessionDirIndex{}, err
	}
	topicSessionIndexCache.Lock()
	cached, ok := topicSessionIndexCache.byDir[key]
	if ok && topicSessionSignaturesEqual(cached.signature, signature) {
		topicSessionIndexCache.Unlock()
		return cached, nil
	}
	topicSessionIndexCache.Unlock()

	index := topicSessionDirIndex{
		signature: signature,
		byTopic:   map[string][]topicSessionMatch{},
	}
	for _, name := range sessionNames {
		path := filepath.Join(key, name)
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok {
			continue
		}
		topicID := strings.TrimSpace(meta.TopicID)
		if topicID == "" {
			continue
		}
		index.byTopic[topicID] = append(index.byTopic[topicID], topicSessionMatch{
			path:          path,
			updatedAt:     meta.UpdatedAt,
			scope:         meta.DefaultScope(),
			workspaceRoot: meta.WorkspaceRoot,
		})
	}

	topicSessionIndexCache.Lock()
	topicSessionIndexCache.byDir[key] = index
	topicSessionIndexCache.Unlock()
	return index, nil
}

func topicSessionIndexHasContentTopic(index topicSessionDirIndex, topicID string) bool {
	matches := index.byTopic[strings.TrimSpace(topicID)]
	for _, match := range matches {
		if sessionFileHasConversationContent(match.path) {
			return true
		}
	}
	return false
}

func topicSessionMatches(dir, topicID string) []topicSessionMatch {
	index, err := topicSessionIndexForDir(dir)
	if err != nil {
		return nil
	}
	matches := index.byTopic[strings.TrimSpace(topicID)]
	if len(matches) == 0 {
		return nil
	}
	out := make([]topicSessionMatch, 0, len(matches))
	for _, match := range matches {
		if agent.IsCleanupPending(match.path) {
			continue
		}
		out = append(out, match)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func invalidateTopicSessionIndex(dir string) {
	key := topicSessionDirKey(dir)
	if key == "" {
		return
	}
	topicSessionIndexCache.Lock()
	delete(topicSessionIndexCache.byDir, key)
	topicSessionIndexCache.Unlock()
}

func invalidateTopicSessionIndexForPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	invalidateTopicSessionIndex(filepath.Dir(path))
}

// findTopicSession returns the most recently updated .jsonl file whose .meta
// carries the given topicID, using a directory-level sidecar index cache.
func findTopicSession(dir, topicID string) string {
	if topicID == "" || dir == "" {
		return ""
	}
	var bestPath string
	var bestTime time.Time
	for _, match := range topicSessionMatches(dir, topicID) {
		if match.updatedAt.After(bestTime) {
			bestTime = match.updatedAt
			bestPath = match.path
		}
	}
	return bestPath
}
