package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

// --- WorkspaceTab -----------------------------------------------------------

// WorkspaceTab is one open conversation tab in the desktop. Each tab owns an
// independent controller (its own agent, session, tool registry, plugin host,
// memory, permissions) scoped to a workspace root, so multiple projects and
// topics can be active concurrently without interfering.
type WorkspaceTab struct {
	ID            string             // stable random id
	Scope         string             // "project" | "global"
	WorkspaceRoot string             // project root dir (empty for global)
	SharedHostKey string             // opaque key for the shared plugin host (set by buildTabController)
	TopicID       string             // topic within the project
	TopicTitle    string             // display title
	SessionPath   string             // exact .jsonl file this tab continues
	ReadOnly      bool               // true for external channel transcripts opened for browsing
	Ctrl          control.SessionAPI // nil while booting / on error
	Label         string             // model label (for the tab badge)
	Ready         bool               // true once boot.Build completes
	StartupErr    string             // build error, surfaced to the frontend
	sink          *tabEventSink      // routes events with this tab's ID

	ActivityStatus string // transient project-tree status for the in-flight turn

	// Per-turn autosave per tab.
	saveMu    sync.Mutex
	saving    bool
	saveAgain bool

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
	telemMu        sync.Mutex

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

	activeTurnStartedAt            int64
	executorSessionCacheHitTokens  int
	executorSessionCacheMissTokens int
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

func sessionRuntimeKey(path string) string {
	return canonicalTabSessionPath(path)
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
	key := sessionRuntimeKey(tab.currentSessionPath())
	if key == "" {
		return false
	}
	if tab.sink != nil {
		tab.sink.ctx = nil
	}
	a.mu.Lock()
	a.ensureDetachedSessionsLocked()
	tab.SessionPath = key
	a.detachedSessions[key] = tab
	a.mu.Unlock()
	return true
}

func cloneDetachedRuntimeTab(tab *WorkspaceTab, key string) *WorkspaceTab {
	if tab == nil {
		return nil
	}
	tab.telemMu.Lock()
	readTelemetry := append([]readFileRecord(nil), tab.readTelemetry...)
	usageTelemetry := tab.usageTelemetry
	tab.telemMu.Unlock()

	return &WorkspaceTab{
		ID:               detachedRuntimeTabID(key),
		Scope:            tab.Scope,
		WorkspaceRoot:    tab.WorkspaceRoot,
		SharedHostKey:    tab.SharedHostKey,
		TopicID:          tab.TopicID,
		TopicTitle:       tab.TopicTitle,
		SessionPath:      key,
		Ctrl:             tab.Ctrl,
		Label:            tab.Label,
		Ready:            tab.Ready,
		StartupErr:       tab.StartupErr,
		sink:             tab.sink,
		ActivityStatus:   tab.ActivityStatus,
		readTelemetry:    readTelemetry,
		usageTelemetry:   usageTelemetry,
		model:            tab.model,
		effort:           cloneStringPtr(tab.effort),
		tokenMode:        tab.tokenMode,
		mode:             tab.mode,
		goal:             tab.goal,
		toolApprovalMode: tab.toolApprovalMode,
		disabledMCP:      cloneServerViewMap(tab.disabledMCP),
		mcpOrder:         append([]string(nil), tab.mcpOrder...),
	}
}

func (a *App) detachRuntimeForReplacement(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	key := sessionRuntimeKey(tab.currentSessionPath())
	if key == "" {
		return false
	}
	detached := cloneDetachedRuntimeTab(tab, key)
	if detached == nil {
		return false
	}
	if detached.sink != nil {
		detached.sink.tabID = detached.ID
		detached.sink.ctx = nil
	}

	a.mu.Lock()
	a.ensureDetachedSessionsLocked()
	a.detachedSessions[key] = detached
	a.mu.Unlock()
	return true
}

func applyRuntimeTab(target, source *WorkspaceTab, key string, wailsCtx context.Context, app *App) {
	if target == nil || source == nil {
		return
	}
	source.telemMu.Lock()
	readTelemetry := append([]readFileRecord(nil), source.readTelemetry...)
	usageTelemetry := source.usageTelemetry
	source.telemMu.Unlock()

	if source.sink != nil {
		source.sink.tabID = target.ID
		source.sink.app = app
		source.sink.ctx = wailsCtx
	}

	target.Ctrl = source.Ctrl
	target.sink = source.sink
	target.SessionPath = key
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
}

func (a *App) attachExistingSessionRuntime(tab *WorkspaceTab, path string, wailsCtx context.Context) bool {
	key := sessionRuntimeKey(path)
	if tab == nil || key == "" {
		return false
	}

	a.mu.Lock()
	detached := a.detachedSessions[key]
	if detached != nil {
		delete(a.detachedSessions, key)
		applyRuntimeTab(tab, detached, key, wailsCtx, a)
		if current := a.tabs[tab.ID]; current == tab {
			a.saveTabsLocked()
		}
		a.mu.Unlock()
		if tab.Ctrl != nil {
			tab.Ctrl.ReplayPendingPrompts()
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
	applyRuntimeTab(tab, source, key, wailsCtx, a)
	a.saveTabsLocked()
	a.mu.Unlock()

	if tab.Ctrl != nil {
		tab.Ctrl.ReplayPendingPrompts()
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
	if source == event.UsageSourceExecutor && e.SessionHit+e.SessionMiss > 0 {
		if e.SessionHit < t.usageTelemetry.executorSessionCacheHitTokens || e.SessionMiss < t.usageTelemetry.executorSessionCacheMissTokens {
			t.usageTelemetry.CacheHitTokens += u.CacheHitTokens
			t.usageTelemetry.CacheMissTokens += u.CacheMissTokens
		} else {
			t.usageTelemetry.CacheHitTokens += e.SessionHit - t.usageTelemetry.executorSessionCacheHitTokens
			t.usageTelemetry.CacheMissTokens += e.SessionMiss - t.usageTelemetry.executorSessionCacheMissTokens
		}
		t.usageTelemetry.executorSessionCacheHitTokens = e.SessionHit
		t.usageTelemetry.executorSessionCacheMissTokens = e.SessionMiss
	} else {
		t.usageTelemetry.CacheHitTokens += u.CacheHitTokens
		t.usageTelemetry.CacheMissTokens += u.CacheMissTokens
	}
	t.usageTelemetry.RequestCount++
	if t.usageTelemetry.Sources == nil {
		t.usageTelemetry.Sources = map[string]usageSourceStats{}
	}
	src := t.usageTelemetry.Sources[source]
	src.PromptTokens += u.PromptTokens
	src.CompletionTokens += u.CompletionTokens
	src.TotalTokens += u.TotalTokens
	src.ReasoningTokens += u.ReasoningTokens
	src.CacheHitTokens += u.CacheHitTokens
	src.CacheMissTokens += u.CacheMissTokens
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
	usage.executorSessionCacheHitTokens = 0
	usage.executorSessionCacheMissTokens = 0
	return tabTelemetrySnapshot{Version: 2, ReadFiles: records, Usage: usage}
}

func (t *WorkspaceTab) resetTelemetry() {
	t.telemMu.Lock()
	t.readTelemetry = nil
	t.usageTelemetry = sessionUsageStats{}
	t.telemMu.Unlock()
}

// tabEventSink wraps a parent event.Sink and prepends a tabId to every wire
// event so the frontend can route it to the correct tab's reducer.
type tabEventSink struct {
	tabID         string
	app           *App
	mu            sync.RWMutex
	ctx           context.Context
	runtimeEvents asyncRuntimeEmitter
}

func (s *tabEventSink) Emit(e event.Event) {
	if s.app != nil {
		switch e.Kind {
		case event.TurnStarted:
			s.recordTurnStarted()
		case event.Usage:
			s.recordUsageTelemetry(e)
		case event.TurnDone:
			s.recordTurnDone()
		}
		if m := s.app.metrics.Load(); m != nil {
			m.observe(e)
			if e.Kind == event.TurnDone {
				m.persist()
			}
		}
	}
	s.emitRuntimeEvent(eventChannel, toWireTab(e, s.tabID))
	if s.app != nil {
		if status, update := topicActivityStatusFromEvent(e); update {
			changed := s.app.setTabActivityStatus(s.tabID, status)
			if changed || isBackgroundJobLifecycleNotice(e) {
				s.app.emitProjectTreeChanged()
			}
		}
	}
	// Record read_file successes in the tab's telemetry.
	if e.Kind == event.ToolResult && e.Tool.Name == "read_file" && e.Tool.Err == "" {
		s.recordReadTelemetry(e)
	}
	// Persist after each turn so a force-kill loses at most the in-flight prompt.
	if e.Kind == event.TurnDone && s.app != nil {
		s.app.scheduleTabSnapshot(s.tabID)
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

func (a *App) emitReady(ctx context.Context) {
	a.mu.RLock()
	hook := a.readyHook
	a.mu.RUnlock()
	if hook != nil {
		hook()
		return
	}
	if ctx != nil {
		runtime.EventsEmit(ctx, "agent:ready")
	}
}

func (s *tabEventSink) recordReadTelemetry(e event.Event) {
	if s.app == nil {
		return
	}
	s.app.mu.RLock()
	tab := s.app.tabByEventSinkIDLocked(s.tabID)
	var ctrl control.SessionAPI
	if tab != nil {
		ctrl = tab.Ctrl
	}
	s.app.mu.RUnlock()
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

	tab.recordReadFile(readFileRecord{
		Path:      path,
		Turn:      turn,
		Time:      time.Now().UnixMilli(),
		Offset:    offset,
		Limit:     limit,
		Truncated: truncated,
	})
	if ctrl == nil {
		return
	}
	if sp := ctrl.SessionPath(); sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) recordTurnStarted() {
	tab, sp := s.telemetryTab()
	if tab == nil {
		return
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
	tab.recordUsage(e)
	if sp != "" {
		_ = saveTelemetry(sp+".telemetry.json", tab.telemetrySnapshot())
	}
}

func (s *tabEventSink) telemetryTab() (*WorkspaceTab, string) {
	if s.app == nil {
		return nil, ""
	}
	s.app.mu.RLock()
	tab := s.app.tabByEventSinkIDLocked(s.tabID)
	var ctrl control.SessionAPI
	if tab != nil {
		ctrl = tab.Ctrl
	}
	s.app.mu.RUnlock()
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
	w := toWire(e)
	return wireEventTab{
		wireEvent:         w,
		TabID:             tabID,
		SessionHitTokens:  e.SessionHit,
		SessionMissTokens: e.SessionMiss,
		SessionCost:       0, // filled by frontend accumulator per tab
		SessionCurrency:   "",
		SessionCostUsd:    0, // deprecated compatibility alias
	}
}

// wireEventTab extends wireEvent with tab routing info. The frontend reducer
// uses tabId to dispatch to the correct per-tab state.
type wireEventTab struct {
	wireEvent
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
	ID                string `json:"id"`
	Scope             string `json:"scope"`
	WorkspaceRoot     string `json:"workspaceRoot"`
	WorkspaceName     string `json:"workspaceName"`
	WorkspacePath     string `json:"workspacePath,omitempty"`
	GitBranch         string `json:"gitBranch,omitempty"`
	TopicID           string `json:"topicId"`
	TopicTitle        string `json:"topicTitle"`
	SessionPath       string `json:"sessionPath,omitempty"`
	ReadOnly          bool   `json:"readOnly,omitempty"`
	ProjectColor      string `json:"projectColor,omitempty"`
	Label             string `json:"label"`
	Ready             bool   `json:"ready"`
	Running           bool   `json:"running"`
	PendingPrompt     bool   `json:"pendingPrompt,omitempty"`
	BackgroundJobs    int    `json:"backgroundJobs,omitempty"`
	CancelRequested   bool   `json:"cancelRequested,omitempty"`
	Cancellable       bool   `json:"cancellable"`
	Mode              string `json:"mode"`
	CollaborationMode string `json:"collaborationMode"`
	ToolApprovalMode  string `json:"toolApprovalMode"`
	TokenMode         string `json:"tokenMode"`
	Goal              string `json:"goal,omitempty"`
	GoalStatus        string `json:"goalStatus,omitempty"`
	StartupErr        string `json:"startupErr,omitempty"`
	Active            bool   `json:"active"`
	Cwd               string `json:"cwd"`
}

func enrichTabMeta(meta TabMeta) TabMeta {
	if meta.Active {
		meta.GitBranch = workspaceGitBranch(meta.WorkspaceRoot)
	}
	return meta
}

func enrichTabMetas(metas []TabMeta) []TabMeta {
	for i := range metas {
		if metas[i].Active {
			metas[i].GitBranch = workspaceGitBranch(metas[i].WorkspaceRoot)
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
	tab := &WorkspaceTab{
		ID:               tabID,
		Scope:            scope,
		WorkspaceRoot:    actualRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitle,
		SessionPath:      sessionPath,
		tokenMode:        boot.TokenModeFull,
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
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

func tabMatchesTopicTarget(tab *WorkspaceTab, scope, workspaceRoot, topicID string) bool {
	if tab == nil || tab.Scope != scope || tab.TopicID != topicID {
		return false
	}
	if scope == "global" {
		return true
	}
	return normalizeProjectRoot(tab.WorkspaceRoot) == normalizeProjectRoot(workspaceRoot)
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

	// Inherit model, effort, token mode, mode, tool-approval, and MCP state from the
	// active tab so a new blank session keeps the same settings (#4019).
	var inheritedModel string
	var inheritedEffort *string
	inheritedTokenMode := boot.TokenModeFull
	inheritedMode := "normal"
	inheritedToolApprovalMode := control.ToolApprovalAsk
	inheritedDisabledMCP := map[string]ServerView{}
	var inheritedMCPOrder []string
	if active := a.activeTabLocked(); active != nil {
		inheritedModel = active.model
		inheritedEffort = cloneStringPtr(active.effort)
		inheritedTokenMode = currentTabTokenMode(active)
		inheritedMode = currentTabMode(active)
		inheritedToolApprovalMode = currentTabToolApprovalMode(active)
		inheritedDisabledMCP = cloneServerViewMap(active.disabledMCP)
		inheritedMCPOrder = append([]string(nil), active.mcpOrder...)
	}

	if topicID := a.indexedBlankTopicIDLocked(scope, workspaceRoot); topicID != "" {
		// Reuse a previously-indexed but unused blank topic instead of
		// creating a new one.  Build it inline (not via OpenProjectTab /
		// OpenGlobalTab) so it inherits settings from the active tab.
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
		a.saveTabsLocked()
		meta := a.tabMeta(created, true)
		a.mu.Unlock()

		a.startTabControllerBuild(created)
		a.emitProjectTreeChanged()
		return enrichTabMeta(meta), nil
	}

	topicID := newTopicID()
	topicTitle := defaultTopicTitle
	if err := setTopicTitleWithSource(workspaceRoot, topicID, topicTitle, topicTitleSourceAuto); err != nil {
		a.mu.Unlock()
		return TabMeta{}, err
	}
	f := loadProjectsFile()
	if workspaceRoot == "" {
		f.GlobalTopics = prependUniqueString(f.GlobalTopics, topicID)
		_ = saveProjectsFile(f)
	} else {
		for i, p := range f.Projects {
			if p.Root == workspaceRoot {
				f.Projects[i].Topics = prependUniqueString(p.Topics, topicID)
				_ = saveProjectsFile(f)
				break
			}
		}
	}

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
		return strings.TrimSpace(tab.SessionPath) == ""
	}
	if tab.hasActiveRuntimeWork() {
		return false
	}
	return !messagesHaveConversationContent(tab.Ctrl.History())
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
	sessionIndex, _ := topicSessionIndexForDir(config.SessionDir())
	for _, topicID := range topicIDs {
		if openTopics[topicID] {
			continue
		}
		if topicTitleForTab(scope, workspaceRoot, topicID) != defaultTopicTitle {
			continue
		}
		if topicSessionIndexHasTopic(sessionIndex, topicID) {
			continue
		}
		return topicID
	}
	return ""
}

// SetActiveTab switches the frontend's active tab. A no-op when tabID is
// already active or unknown.
func (a *App) SetActiveTab(tabID string) error {
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
	// Snapshot the session state before removing the tab from a.tabs.
	// This closes a race window with DeleteSession: if Snapshot runs
	// after delete(a.tabs, tabID), a concurrent DeleteSession can delete
	// the session files, and the deferred Snapshot recreates them.
	if tab.Ctrl != nil && !tab.ReadOnly {
		_ = tab.Ctrl.Snapshot()
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
	a.mu.Unlock()

	// Tear down outside the lock.
	if tab.Ctrl != nil {
		if tab.hasActiveRuntimeWork() && a.detachSessionRuntime(tab) {
			// Detached runtimes keep running and must keep saving: do not
			// clear the path or drain for them.
			return nil
		}
		tab.Ctrl.SetSessionPath("") // future snapshots become no-ops
		a.quiesceTabAutosave(tab)   // wait for any in-flight snapshot to finish
		tab.Ctrl.Cancel()
		tab.Ctrl.Close()
		// Release the shared plugin host reference. The host stays alive as
		// long as any other tab for the same workspace root holds a reference;
		// on the last release the host is closed and its subprocesses exit.
		a.releaseTabSharedHost(tab)
	}
	if tab.sink != nil {
		tab.sink.clearContext() // stop further emissions (nil ctx -> Emit becomes no-op)
	}
	return nil
}

// buildTabController assembles a controller for a tab in the background, the
// same way buildController works for the single-controller App. On success it
// wires the controller and flips Ready; on failure it stores StartupErr.
func (a *App) startTabControllerBuild(tab *WorkspaceTab) {
	if a.ctx == nil {
		a.buildTabController(tab)
		return
	}
	go a.buildTabController(tab)
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
	defer a.recoverToPending("buildTabController")
	wailsCtx := a.ctx
	buildCtx := a.bootContext()

	a.reconcileTabWithPinnedSessionMeta(tab)

	root := tab.WorkspaceRoot
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}

	// Load config for this tab's workspace root.
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
		a.mu.Unlock()
		a.emitReady(wailsCtx)
		return
	}

	// One-time legacy bridge: lift old ~/.env provider keys into Reasonix's
	// global .env so every workspace observes the same saved credentials.
	promoteProviderKeysToCredentials(cfg)

	if tab.sink != nil {
		tab.sink.setContext(wailsCtx)
	}

	sessionDir := desktopSessionDir(root)
	topicID := strings.TrimSpace(tab.TopicID)

	// Assign Global topics to legacy sessions in the global session dir so
	// imported history appears in the project tree regardless of which tab
	// triggered the build (the migration now sends everything to global).
	migratedGlobalTopics := migrateLegacySessionsIntoGlobalTopics(config.SessionDir())
	if len(migratedGlobalTopics) > 0 {
		a.emitProjectTreeChanged()
	}
	if tab.Scope == "global" && topicID == "" && len(migratedGlobalTopics) > 0 {
		topicID = migratedGlobalTopics[0]
		topicTitle := topicTitleForTab("global", "", topicID)
		a.mu.Lock()
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
		if _, dir := a.findTopicSessionForTarget(tab.Scope, tab.WorkspaceRoot, topicID); dir != "" {
			sessionDir = dir
		}
	}
	startupSessionPath := ""
	if pinnedPath, ok := pinnedTabSessionPath(sessionDir, tab.SessionPath); ok {
		if !agent.IsCleanupPending(pinnedPath) {
			startupSessionPath = pinnedPath
		}
	} else if topicID != "" {
		startupSessionPath = findTopicSession(sessionDir, topicID)
	}

	model := strings.TrimSpace(tab.model)
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
		if fallback && strings.TrimSpace(tab.model) != "" {
			a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", requestedModel, resolved))
		}
		model = resolved
	}

	a.mu.Lock()
	tab.model = model
	tab.Label = model
	a.saveTabsLocked()
	a.mu.Unlock()

	// Acquire a shared plugin host for this workspace root so MCP processes
	// are launched once per root, not once per tab.
	rootKey := tab.WorkspaceRoot
	if rootKey == "" {
		rootKey = "__global__" // stable key for global workspace tabs
	}
	tab.SharedHostKey = rootKey
	sharedHost := a.acquireSharedHost(rootKey)

	ctrl, err := boot.Build(buildCtx, boot.Options{
		Model:                    model,
		RequireKey:               false,
		Sink:                     tab.sink,
		WorkspaceRoot:            root,
		SessionDir:               sessionDir,
		EffortOverride:           cloneStringPtr(tab.effort),
		TokenMode:                currentTabTokenMode(tab),
		SharedHost:               sharedHost,
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
	})
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
		a.releaseTabSharedHost(tab)
		a.mu.Unlock()
		a.emitReady(wailsCtx)
		return
	}

	a.bindControllerDisplayRecorder(ctrl)
	ctrl.EnableInteractiveApproval()
	applyTabModeToController(ctrl, tab.mode)
	applyTabToolApprovalModeToController(ctrl, tab.toolApprovalMode)
	ctrl.SetGoal(tab.goal)

	if dir := ctrl.SessionDir(); dir != "" {
		migratedTopics := migrateLegacySessionsIntoGlobalTopics(dir)
		if len(migratedTopics) > 0 {
			a.emitProjectTreeChanged()
		}
		if tab.Scope == "global" && strings.TrimSpace(tab.TopicID) == "" && len(migratedTopics) > 0 {
			topicID := migratedTopics[0]
			topicTitle := topicTitleForTab("global", "", topicID)
			a.mu.Lock()
			tab.TopicID = topicID
			tab.TopicTitle = topicTitle
			a.saveTabsLocked()
			a.mu.Unlock()
		}
		var path string
		// Prefer the exact session file persisted for this tab. Topic lookup is a
		// compatibility fallback for older desktop-tabs.json files that only stored
		// topicId and could pick the wrong session when one topic had multiple files.
		if loaded, pinnedPath, ok := loadPinnedTabSessionWithPreload(dir, tab.SessionPath, loadedSession); ok {
			if loaded != nil {
				ctrl.Resume(loaded, pinnedPath)
			} else {
				ctrl.SetSessionPath(pinnedPath)
			}
			path = pinnedPath
		}
		if path == "" && tab.TopicID != "" {
			existingPath := findTopicSession(dir, tab.TopicID)
			if existingPath != "" {
				if loaded, err := loadResumableSession(existingPath); err == nil {
					ctrl.Resume(loaded, existingPath)
					path = existingPath
				}
			}
		}
		if path == "" {
			path = agent.NewSessionPath(dir, ctrl.Label())
			ctrl.SetSessionPath(path)
		}
		// Write/update scope/session meta.
		if path != "" {
			a.persistTabSessionPath(tab, path)
			if strings.TrimSpace(tab.TopicID) != "" {
				if err := ensureTopicIndexed(tab.Scope, tab.WorkspaceRoot, tab.TopicID, tab.TopicTitle, loadTopicTitleSource(topicTitleRoot(tab.Scope, tab.WorkspaceRoot), tab.TopicID)); err == nil {
					a.emitProjectTreeChanged()
				}
			}
			// Restore existing telemetry if resuming a session.
			telemetryPath := path + ".telemetry.json"
			if snapshot := loadTelemetry(telemetryPath); len(snapshot.ReadFiles) > 0 || snapshot.Usage.RequestCount > 0 {
				tab.telemMu.Lock()
				tab.readTelemetry = snapshot.ReadFiles
				tab.usageTelemetry = snapshot.Usage
				tab.telemMu.Unlock()
			}
			if a.attachExistingSessionRuntime(tab, path, wailsCtx) {
				ctrl.Close()
				a.releaseSharedHost(rootKey)
				a.emitReady(wailsCtx)
				return
			}
		}
	}

	a.mu.Lock()
	tab.Ctrl = ctrl
	tab.Label = ctrl.Label()
	tab.Ready = true
	tab.StartupErr = ""
	a.mu.Unlock()
	a.emitReady(wailsCtx)
}

func (a *App) reconcileTabWithPinnedSessionMeta(tab *WorkspaceTab) {
	if tab == nil || strings.TrimSpace(tab.SessionPath) == "" {
		return
	}
	path, meta, ok := a.pinnedSessionMeta(tab.SessionPath)
	if !ok {
		return
	}

	scope := meta.DefaultScope()
	workspaceRoot := ""
	if scope == "project" {
		workspaceRoot = normalizeProjectRoot(meta.WorkspaceRoot)
		if workspaceRoot == "" {
			return
		}
		_ = addProject(workspaceRoot, "")
	} else {
		workspaceRoot = globalTabWorkspaceRoot()
	}

	topicID := strings.TrimSpace(meta.TopicID)
	topicTitle := strings.TrimSpace(meta.TopicTitle)
	if topicTitle == "" && topicID != "" {
		topicTitle = topicTitleForTab(scope, workspaceRoot, topicID)
	}

	a.mu.Lock()
	current := a.tabs[tab.ID]
	if current != nil && current != tab {
		a.mu.Unlock()
		return
	}
	changed := tab.Scope != scope ||
		tab.WorkspaceRoot != workspaceRoot ||
		canonicalTabSessionPath(tab.SessionPath) != canonicalTabSessionPath(path)
	tab.Scope = scope
	tab.WorkspaceRoot = workspaceRoot
	tab.SessionPath = canonicalTabSessionPath(path)
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
	a.mu.Unlock()
}

func (a *App) pinnedSessionMeta(sessionPath string) (string, agent.BranchMeta, bool) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return "", agent.BranchMeta{}, false
	}
	for _, dir := range a.knownSessionDirs() {
		path, _, err := validateSessionPath(dir, sessionPath)
		if err != nil {
			continue
		}
		if meta, ok, err := agent.LoadBranchMeta(path); err == nil && ok {
			return path, meta, true
		}
	}

	if !filepath.IsAbs(sessionPath) {
		return "", agent.BranchMeta{}, false
	}
	path, err := filepath.Abs(sessionPath)
	if err != nil {
		return "", agent.BranchMeta{}, false
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		return "", agent.BranchMeta{}, false
	}

	var candidateDirs []string
	if meta.DefaultScope() == "project" {
		if root := normalizeProjectRoot(meta.WorkspaceRoot); root != "" {
			candidateDirs = append(candidateDirs, desktopSessionDir(root))
		}
	} else {
		candidateDirs = append(candidateDirs, desktopSessionDir(globalWorkspaceRoot()), config.SessionDir())
	}
	for _, dir := range candidateDirs {
		validPath, _, err := validateSessionPath(dir, path)
		if err == nil {
			return validPath, meta, true
		}
	}
	return "", agent.BranchMeta{}, false
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
		a.mu.RLock()
		ctrl := tab.Ctrl
		a.mu.RUnlock()
		if ctrl != nil {
			if err := a.snapshotTab(tab); err == nil {
				if !a.maybeAutoTitleTopic(tab) {
					a.emitProjectTreeChanged()
				}
			}
		}
		tab.saveMu.Lock()
		if tab.saveCond == nil {
			tab.saveCond = sync.NewCond(&tab.saveMu)
		}
		if tab.closing {
			// Tab is being torn down: stop without picking up saveAgain work.
			tab.saving = false
			tab.saveCond.Broadcast()
			tab.saveMu.Unlock()
			return
		}
		if tab.saveAgain {
			tab.saveAgain = false
			tab.saveMu.Unlock()
			continue
		}
		tab.saving = false
		tab.saveCond.Broadcast()
		tab.saveMu.Unlock()
		return
	}
}

func (a *App) maybeAutoTitleTopic(tab *WorkspaceTab) bool {
	if tab == nil || strings.TrimSpace(tab.TopicID) == "" || tab.Ctrl == nil {
		return false
	}
	titleRoot := tab.WorkspaceRoot
	if tab.Scope == "global" {
		titleRoot = ""
	}
	if source := loadTopicTitleSource(titleRoot, tab.TopicID); source != topicTitleSourceAuto {
		return false
	}
	sessionPath := tab.Ctrl.SessionPath()
	if sessionPath == "" {
		return false
	}
	nextTitle, updated := autoTitleTopicFromSession(titleRoot, tab.TopicID, sessionPath)
	if !updated {
		return false
	}
	a.updateOpenTopicTitle(tab.TopicID, nextTitle)
	a.updateTopicSessionTitles(tab.TopicID, nextTitle)
	a.emitProjectTreeChanged()
	return true
}

func autoTitleTopicFromSession(workspaceRoot, topicID, sessionPath string) (string, bool) {
	if source := loadTopicTitleSource(workspaceRoot, topicID); source != topicTitleSourceAuto {
		return "", false
	}
	nextTitle := topicTitleFromSession(sessionPath)
	if nextTitle == "" || nextTitle == loadTopicTitle(workspaceRoot, topicID) {
		return "", false
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, nextTitle, topicTitleSourceAuto); err != nil {
		return "", false
	}
	return nextTitle, true
}

func topicTitleFromSession(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := dec.Decode(&msg); err != nil {
			return ""
		}
		if msg.Role == "user" {
			content := control.StripComposePrefixes(agent.HandoffTask(msg.Content))
			content = control.StripReferencedContextPrefix(content)
			return topicTitleFromText(content)
		}
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

func desktopConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".reasonix")
	}
	return filepath.Join(dir, "reasonix")
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
				Goal:             strings.TrimSpace(currentTabGoal(tab)),
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
	f := loadProjectsFile()
	for i, p := range f.Projects {
		if p.Root == root {
			if title != "" {
				f.Projects[i].Title = title
			}
			return saveProjectsFile(f)
		}
	}
	f.Projects = append(f.Projects, desktopProject{Root: root, Title: title})
	return saveProjectsFile(f)
}

func renameProject(root, title string) error {
	title = strings.TrimSpace(title)
	f := loadProjectsFile()
	if strings.TrimSpace(root) == "" {
		f.GlobalTitle = title
		return saveProjectsFile(f)
	}
	root = normalizeProjectRoot(root)
	for i, p := range f.Projects {
		if p.Root == root {
			f.Projects[i].Title = title
			return saveProjectsFile(f)
		}
	}
	f.Projects = append(f.Projects, desktopProject{Root: root, Title: title})
	return saveProjectsFile(f)
}

func setProjectColor(root, color string) error {
	root = normalizeProjectRoot(root)
	color = normalizeProjectColor(color)
	if root == "" {
		f := loadProjectsFile()
		f.GlobalColor = color
		return saveProjectsFile(f)
	}
	f := loadProjectsFile()
	for i, p := range f.Projects {
		if p.Root == root {
			f.Projects[i].Color = color
			return saveProjectsFile(f)
		}
	}
	f.Projects = append(f.Projects, desktopProject{Root: root, Color: color})
	return saveProjectsFile(f)
}

func removeProject(root string) error {
	root = normalizeProjectRoot(root)
	f := loadProjectsFile()
	projects := make([]desktopProject, 0, len(f.Projects))
	for _, p := range f.Projects {
		if p.Root != root {
			projects = append(projects, p)
		}
	}
	f.Projects = projects
	return saveProjectsFile(f)
}

// --- topic helpers ----------------------------------------------------------

const (
	topicTitlesFile        = "desktop-topic-titles.json"
	topicTitleSourcesFile  = "desktop-topic-title-sources.json"
	topicCreatedAtsFile    = "desktop-topic-created-at.json"
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

func loadTopicTitle(workspaceRoot, topicID string) string {
	return loadTopicTitles(workspaceRoot)[topicID]
}

func loadTopicTitleSource(workspaceRoot, topicID string) string {
	return loadTopicTitleSources(workspaceRoot)[topicID]
}

func loadTopicCreatedAt(workspaceRoot, topicID string) int64 {
	return loadTopicCreatedAts(workspaceRoot)[topicID]
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
	return saveTopicTitleSources(workspaceRoot, sources)
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
	f := loadProjectsFile()
	if workspaceRoot == "" {
		f.GlobalTopics = prependUniqueString(f.GlobalTopics, topicID)
		return saveProjectsFile(f)
	}
	for i, p := range f.Projects {
		if p.Root == workspaceRoot {
			f.Projects[i].Topics = prependUniqueString(p.Topics, topicID)
			return saveProjectsFile(f)
		}
	}
	f.Projects = append(f.Projects, desktopProject{Root: workspaceRoot, Topics: []string{topicID}})
	return saveProjectsFile(f)
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
	Key            string        `json:"key"`  // stable key for React
	Kind           string        `json:"kind"` // "project" | "topic" | "session" | "global_folder" | "global_topic" | "global_session"
	Label          string        `json:"label"`
	Root           string        `json:"root,omitempty"` // project workspace root
	TopicID        string        `json:"topicId,omitempty"`
	SessionPath    string        `json:"sessionPath,omitempty"`
	ProjectColor   string        `json:"projectColor,omitempty"`
	Turns          int           `json:"turns,omitempty"`
	CreatedAt      int64         `json:"createdAt,omitempty"`
	LastActivityAt int64         `json:"lastActivityAt,omitempty"`
	Open           bool          `json:"open,omitempty"`
	Running        bool          `json:"running,omitempty"`
	Status         string        `json:"status,omitempty"`
	Pinned         bool          `json:"pinned,omitempty"`
	Children       []ProjectNode `json:"children,omitempty"`
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

func topicMigrationDone(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	markerInfo, err := os.Stat(filepath.Join(dir, topicMigrationMarker))
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
		if filepath.Ext(name) != ".jsonl" && !strings.HasSuffix(name, ".jsonl.meta") {
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

func markTopicMigrationDone(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, topicMigrationMarker), nil, 0o644)
}

func migrateLegacySessionsIntoGlobalTopics(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	// One-shot per dir: once the migration pass has completed, skip the full
	// per-render session scan entirely.
	if topicMigrationDone(dir) {
		return nil
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

		meta, err := agent.EnsureBranchMeta(info.Path)
		if err != nil {
			deferred = true
			continue
		}
		// Preserve scoped sessions only when their existing ownership matches
		// the directory being migrated.
		if !legacySessionMetaMatchesMigrationTarget(meta, scope, workspaceRoot) {
			continue
		}
		meta.Scope = scope
		meta.WorkspaceRoot = workspaceRoot
		meta.TopicID = topicID
		meta.TopicTitle = title
		if err := agent.SaveBranchMetaPreserveUpdated(info.Path, meta); err != nil {
			deferred = true
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
		return nil
	}
	f := loadProjectsFile()
	if scope == "global" {
		f.GlobalTopics = uniqueStrings(append(migratedTopicIDs, f.GlobalTopics...))
	} else {
		// Find the project entry and add topics.
		for i, p := range f.Projects {
			if p.Root == workspaceRoot {
				f.Projects[i].Topics = uniqueStrings(append(migratedTopicIDs, f.Projects[i].Topics...))
				break
			}
		}
	}
	_ = saveProjectsFile(f)
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
	return migratedTopicIDs
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
		migrateLegacySessionsIntoGlobalTopics(dir)
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

	f := loadProjectsFile()
	if scope == "global" {
		f.GlobalTopics = prependUniqueString(f.GlobalTopics, topicID)
		meta.Scope = "global"
		meta.WorkspaceRoot = ""
	} else {
		found := false
		for i, p := range f.Projects {
			if p.Root != workspaceRoot {
				continue
			}
			f.Projects[i].Topics = prependUniqueString(p.Topics, topicID)
			found = true
			break
		}
		if !found {
			f.Projects = append(f.Projects, desktopProject{
				Root:   workspaceRoot,
				Topics: []string{topicID},
			})
		}
		meta.Scope = "project"
		meta.WorkspaceRoot = workspaceRoot
	}
	meta.TopicID = topicID
	meta.TopicTitle = title
	if err := saveProjectsFile(f); err != nil {
		return err
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, meta); err != nil {
		return err
	}
	invalidateTopicSessionIndexForPath(sessionPath)
	return nil
}

func restoredSessionTopicTitle(dir, sessionPath string, meta agent.BranchMeta) string {
	if title := topicTitleFromText(meta.TopicTitle); title != "" {
		return title
	}
	if title := topicTitleFromText(loadSessionTitles(dir)[filepath.Base(sessionPath)]); title != "" {
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
		_ = addProject(workspaceRoot, "")
	}
	if err := setTopicTitleWithSource(workspaceRoot, topicID, trimmedTitle, titleSource); err != nil {
		return TopicMeta{}, err
	}
	if err := setTopicCreatedAt(workspaceRoot, topicID, createdAt); err != nil {
		return TopicMeta{}, err
	}
	// New topics should appear first in their project/global group so the item
	// just created is immediately visible and selected in the sidebar.
	f := loadProjectsFile()
	if workspaceRoot == "" {
		f.GlobalTopics = prependUniqueString(f.GlobalTopics, topicID)
		_ = saveProjectsFile(f)
	} else {
		for i, p := range f.Projects {
			if p.Root == workspaceRoot {
				f.Projects[i].Topics = prependUniqueString(p.Topics, topicID)
				_ = saveProjectsFile(f)
				break
			}
		}
	}
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
	f := loadProjectsFile()
	found := false
	for _, project := range f.Projects {
		if project.Root == root {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("project %q not found", root)
	}
	if pinned {
		f.PinnedProjects = prependUniqueString(f.PinnedProjects, root)
	} else {
		f.PinnedProjects = removeString(f.PinnedProjects, root)
	}
	if err := saveProjectsFile(f); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// ReorderProjects persists the user-defined order of project folders and,
// when present, the virtual Global sidebar section.
func (a *App) ReorderProjects(workspaceRoots []string) error {
	f := loadProjectsFile()
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
				return fmt.Errorf("duplicate global section")
			}
			seen[root] = true
			hasGlobalOrder = true
			sidebarOrder = append(sidebarOrder, root)
			continue
		}
		root = normalizeProjectRoot(root)
		project, ok := byRoot[root]
		if !ok {
			return fmt.Errorf("project %q not found", root)
		}
		if seen[root] {
			return fmt.Errorf("duplicate project %q", root)
		}
		seen[root] = true
		next = append(next, project)
		sidebarOrder = append(sidebarOrder, root)
	}
	if len(next) != len(f.Projects) {
		return fmt.Errorf("project order length mismatch")
	}
	f.Projects = next
	if hasGlobalOrder {
		f.SidebarOrder = sidebarOrder
	} else {
		f.SidebarOrder = nil
	}
	if err := saveProjectsFile(f); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// RenameTopic updates a topic's display title.
func (a *App) RenameTopic(topicID, title string) error {
	trimmed := strings.TrimSpace(title)
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
			meta, ok, err := agent.LoadBranchMeta(match.path)
			if err != nil || !ok {
				continue
			}
			meta.TopicTitle = title
			if err := agent.SaveBranchMetaPreserveUpdated(match.path, meta); err == nil {
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
			f.GlobalTopics = removeString(f.GlobalTopics, topicID)
			f.GlobalPinnedTopics = removeString(f.GlobalPinnedTopics, topicID)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("topic %q not found", topicID)
	}
	// Remove from project topic list.
	f.GlobalPinnedTopics = removeString(f.GlobalPinnedTopics, topicID)
	for i, p := range f.Projects {
		for j, tid := range p.Topics {
			if tid == topicID {
				f.Projects[i].Topics = append(f.Projects[i].Topics[:j], f.Projects[i].Topics[j+1:]...)
				break
			}
		}
		f.Projects[i].PinnedTopics = removeString(f.Projects[i].PinnedTopics, topicID)
	}
	_ = saveProjectsFile(f)
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
	f := loadProjectsFile()
	for i, p := range f.Projects {
		m := loadTopicTitles(p.Root)
		if _, ok := m[topicID]; !ok && !containsDesktopString(p.Topics, topicID) {
			continue
		}
		if pinned {
			f.Projects[i].PinnedTopics = prependUniqueString(f.Projects[i].PinnedTopics, topicID)
		} else {
			f.Projects[i].PinnedTopics = removeString(f.Projects[i].PinnedTopics, topicID)
		}
		if err := saveProjectsFile(f); err != nil {
			return err
		}
		a.emitProjectTreeChanged()
		return nil
	}
	globalTitles := loadTopicTitles("")
	if _, ok := globalTitles[topicID]; !ok && !containsDesktopString(f.GlobalTopics, topicID) {
		return fmt.Errorf("topic %q not found", topicID)
	}
	if pinned {
		f.GlobalPinnedTopics = prependUniqueString(f.GlobalPinnedTopics, topicID)
	} else {
		f.GlobalPinnedTopics = removeString(f.GlobalPinnedTopics, topicID)
	}
	if err := saveProjectsFile(f); err != nil {
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
	if strings.TrimSpace(topicID) == "" {
		return fmt.Errorf("topicID is required")
	}

	targets, err := a.topicTrashTargets(topicID)
	if err != nil {
		return err
	}
	removed, fallback := a.removeTopicRuntimeBindings(topicID)
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
	if err := a.DeleteTopic(topicID); err != nil {
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
		infos, err := agent.ListSessions(dir)
		if err != nil {
			return nil, err
		}
		for _, info := range infos {
			if info.TopicID != topicID {
				continue
			}
			if err := addTarget(dir, info.Path); err != nil {
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
	turns          int
	lastActivityAt int64
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
	out := []ProjectNode{}
	type runtimeSessionStatus struct {
		sessionPath    string
		label          string
		turns          int
		createdAt      int64
		lastActivityAt int64
		open           bool
		running        bool
		status         string
	}
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
			sessionPath:    sessionPath,
			label:          label,
			turns:          info.Turns,
			createdAt:      unixMilliOrZero(info.CreatedAt),
			lastActivityAt: unixMilliOrZero(info.LastActivityAt),
			open:           open,
			running:        running,
			status:         status,
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
				Key:            projectSessionNodeKey(scope, session.sessionPath),
				Kind:           kind,
				Label:          session.label,
				Root:           workspaceRoot,
				TopicID:        topicID,
				SessionPath:    session.sessionPath,
				ProjectColor:   projectColor,
				Turns:          session.turns,
				CreatedAt:      session.createdAt,
				LastActivityAt: session.lastActivityAt,
				Open:           session.open,
				Running:        session.running,
				Status:         session.status,
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
			title := globalTitleMap[id]
			summary := topicSummaries[topicSummaryKey("global", "", id)]
			open, running, status := topicRuntimeStatus(topicSummaryKey("global", "", id))
			pinned := containsDesktopString(f.GlobalPinnedTopics, id)
			children = append(children, ProjectNode{
				Key:            "global_topic_" + id,
				Kind:           "global_topic",
				Label:          title,
				TopicID:        id,
				ProjectColor:   globalColor,
				Turns:          summary.turns,
				CreatedAt:      globalCreatedMap[id],
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
			topicTitle := strings.TrimSpace(titleMap[tid])
			if topicTitle == "" {
				topicTitle = defaultTopicTitle
			}
			summary := topicSummaries[topicSummaryKey("project", p.Root, tid)]
			open, running, status := topicRuntimeStatus(topicSummaryKey("project", p.Root, tid))
			pinned := containsDesktopString(p.PinnedTopics, tid)
			children = append(children, ProjectNode{
				Key:            "topic_" + tid,
				Kind:           "topic",
				Label:          topicTitle,
				Root:           p.Root,
				TopicID:        tid,
				ProjectColor:   p.Color,
				Turns:          summary.turns,
				CreatedAt:      createdMap[tid],
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
	UsedTokens       int                         `json:"usedTokens"`
	WindowTokens     int                         `json:"windowTokens"`
	PromptTokens     int                         `json:"promptTokens"`
	CompletionTokens int                         `json:"completionTokens"`
	TotalTokens      int                         `json:"totalTokens"`
	ReasoningTokens  int                         `json:"reasoningTokens"`
	CacheHitTokens   int                         `json:"cacheHitTokens"`
	CacheMissTokens  int                         `json:"cacheMissTokens"`
	RequestCount     int                         `json:"requestCount"`
	ElapsedMs        int64                       `json:"elapsedMs"`
	SessionCost      float64                     `json:"sessionCost"`
	SessionCurrency  string                      `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64                     `json:"sessionCostUsd,omitempty"`
	Sources          map[string]usageSourceStats `json:"sources,omitempty"`
	Mock             bool                        `json:"mock,omitempty"`
	ReadFiles        []readFileRecord            `json:"readFiles"`
	ChangedFiles     []ChangedFileInfo           `json:"changedFiles"`
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
	if tab.Ctrl != nil && tab.Ctrl.PlanMode() {
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
	rand.Read(b[:])
	return "topic_" + time.Now().UTC().Format("20060102-150405") + "_" + hex.EncodeToString(b[:])
}

func globalWorkspaceRoot() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".reasonix", "global-workspace")
	}
	return filepath.Join(dir, "reasonix", "global-workspace")
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
	return loadPinnedTabSessionWithPreload(dir, sessionPath, loadedTabSession{})
}

func loadPinnedTabSessionWithPreload(dir, sessionPath string, preloaded loadedTabSession) (*agent.Session, string, bool) {
	path, ok := pinnedTabSessionPath(dir, sessionPath)
	if !ok {
		return nil, "", false
	}
	if agent.IsCleanupPending(path) {
		return nil, "", false
	}
	if preloaded.matches(path) {
		return preloaded.Session, path, true
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, true
		}
		return nil, "", false
	}
	return loaded, path, true
}

func pinnedTabSessionPath(dir, sessionPath string) (string, bool) {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" || dir == "" {
		return "", false
	}
	path, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
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

func saveTabSessionMeta(tab *WorkspaceTab, path string) error {
	if tab == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	m, err := agent.EnsureBranchMeta(path)
	if err != nil {
		return err
	}
	m.Scope = tab.Scope
	m.WorkspaceRoot = tab.WorkspaceRoot
	m.TopicID = tab.TopicID
	m.TopicTitle = tab.TopicTitle
	if err := agent.SaveBranchMetaPreserveUpdated(path, m); err != nil {
		return err
	}
	invalidateTopicSessionIndexForPath(path)
	return nil
}

func canonicalTabSessionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if validPath, _, err := validateSessionPath(config.SessionDir(), path); err == nil {
		return validPath
	}
	return path
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
	_ = saveTabSessionMeta(tab, path)
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
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", ""
	}
	var bestPath string
	var bestDir string
	var bestTime time.Time
	for _, dir := range a.knownSessionDirs() {
		for _, match := range topicSessionMatches(dir, topicID) {
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
			sessionTitles[sessionKey] = titles[filepath.Base(info.Path)]
		}
		if strings.TrimSpace(info.TopicID) == "" {
			continue
		}
		key := topicSummaryKey(info.Scope, info.WorkspaceRoot, info.TopicID)
		summary := topicSummaries[key]
		summary.turns += info.Turns
		lastActivityAt := info.LastActivityAt.UnixMilli()
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
		isSession := filepath.Ext(name) == ".jsonl"
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

func topicSessionIndexHasTopic(index topicSessionDirIndex, topicID string) bool {
	matches := index.byTopic[strings.TrimSpace(topicID)]
	for _, match := range matches {
		if !agent.IsCleanupPending(match.path) {
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
