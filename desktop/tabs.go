package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// --- WorkspaceTab -----------------------------------------------------------

// WorkspaceTab is one open conversation tab in the desktop. Each tab owns an
// independent controller (its own agent, session, tool registry, plugin host,
// memory, permissions) scoped to a workspace root, so multiple projects and
// topics can be active concurrently without interfering.
type WorkspaceTab struct {
	ID            string              // stable random id
	Scope         string              // "project" | "global"
	WorkspaceRoot string              // project root dir (empty for global)
	TopicID       string              // topic within the project
	TopicTitle    string              // display title
	SessionPath   string              // exact .jsonl file this tab continues
	Ctrl          *control.Controller // nil while booting / on error
	Label         string              // model label (for the tab badge)
	Ready         bool                // true once boot.Build completes
	StartupErr    string              // build error, surfaced to the frontend
	sink          *tabEventSink       // routes events with this tab's ID

	ActivityStatus string // transient project-tree status for the in-flight turn

	// Per-turn autosave per tab.
	saveMu    sync.Mutex
	saving    bool
	saveAgain bool

	// readTelemetry tracks files read during this tab's session.
	readTelemetry  []readFileRecord
	usageTelemetry sessionUsageStats
	telemMu        sync.Mutex

	model            string // active model ref (for meta)
	effort           *string
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
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	CacheHitTokens   int     `json:"cacheHitTokens"`
	CacheMissTokens  int     `json:"cacheMissTokens"`
	RequestCount     int     `json:"requestCount"`
	ElapsedMs        int64   `json:"elapsedMs"`
	SessionCost      float64 `json:"sessionCost,omitempty"`
	SessionCurrency  string  `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64 `json:"sessionCostUsd,omitempty"`

	activeTurnStartedAt int64
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
	t.telemMu.Lock()
	t.usageTelemetry.PromptTokens += u.PromptTokens
	t.usageTelemetry.CompletionTokens += u.CompletionTokens
	t.usageTelemetry.TotalTokens += u.TotalTokens
	t.usageTelemetry.ReasoningTokens += u.ReasoningTokens
	if e.SessionHit+e.SessionMiss > 0 {
		t.usageTelemetry.CacheHitTokens = e.SessionHit
		t.usageTelemetry.CacheMissTokens = e.SessionMiss
	} else {
		t.usageTelemetry.CacheHitTokens += u.CacheHitTokens
		t.usageTelemetry.CacheMissTokens += u.CacheMissTokens
	}
	t.usageTelemetry.RequestCount++
	if e.Pricing != nil {
		cost := e.Pricing.Cost(u)
		t.usageTelemetry.SessionCost += cost
		t.usageTelemetry.SessionCostUsd = t.usageTelemetry.SessionCost
		t.usageTelemetry.SessionCurrency = e.Pricing.Symbol()
	}
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
	usage.activeTurnStartedAt = 0
	return tabTelemetrySnapshot{Version: 2, ReadFiles: records, Usage: usage}
}

// tabEventSink wraps a parent event.Sink and prepends a tabId to every wire
// event so the frontend can route it to the correct tab's reducer.
type tabEventSink struct {
	tabID string
	app   *App
	ctx   context.Context
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
	if s.ctx != nil {
		runtime.EventsEmit(s.ctx, eventChannel, toWireTab(e, s.tabID))
	}
	if s.app != nil {
		if status, update := topicActivityStatusFromEvent(e); update && s.app.setTabActivityStatus(s.tabID, status) {
			s.app.emitProjectTreeChanged()
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
	default:
		return "", false
	}
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
	tab, ok := s.app.tabs[s.tabID]
	var ctrl *control.Controller
	if ok && tab != nil {
		ctrl = tab.Ctrl
	}
	s.app.mu.RUnlock()
	if !ok || tab == nil {
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
	tab, ok := s.app.tabs[s.tabID]
	var ctrl *control.Controller
	if ok && tab != nil {
		ctrl = tab.Ctrl
	}
	s.app.mu.RUnlock()
	if !ok || tab == nil {
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
	TopicID           string `json:"topicId"`
	TopicTitle        string `json:"topicTitle"`
	ProjectColor      string `json:"projectColor,omitempty"`
	Label             string `json:"label"`
	Ready             bool   `json:"ready"`
	Running           bool   `json:"running"`
	Mode              string `json:"mode"`
	CollaborationMode string `json:"collaborationMode"`
	ToolApprovalMode  string `json:"toolApprovalMode"`
	Goal              string `json:"goal,omitempty"`
	GoalStatus        string `json:"goalStatus,omitempty"`
	StartupErr        string `json:"startupErr,omitempty"`
	Active            bool   `json:"active"`
	Cwd               string `json:"cwd"`
}

func (a *App) tabMeta(tab *WorkspaceTab, active bool) TabMeta {
	m := TabMeta{
		ID:                tab.ID,
		Scope:             tab.Scope,
		WorkspaceRoot:     tab.WorkspaceRoot,
		WorkspaceName:     workspaceName(tab.WorkspaceRoot),
		TopicID:           tab.TopicID,
		TopicTitle:        tab.TopicTitle,
		Label:             tab.Label,
		Ready:             tab.Ready,
		Mode:              currentTabMode(tab),
		CollaborationMode: currentTabCollaborationMode(tab),
		ToolApprovalMode:  currentTabToolApprovalMode(tab),
		Goal:              currentTabGoal(tab),
		GoalStatus:        currentTabGoalStatus(tab),
		StartupErr:        tab.StartupErr,
		Active:            active,
		Cwd:               tab.WorkspaceRoot,
	}
	if tab.Scope == "global" {
		m.ProjectColor = globalProjectColor()
		m.WorkspaceName = globalProjectTitle()
	} else if tab.Scope == "project" {
		m.ProjectColor = projectColor(tab.WorkspaceRoot)
	}
	if tab.Ctrl != nil {
		m.Running = tab.Ctrl.Running()
	}
	return m
}

// ListTabs returns every open tab's metadata for the frontend TabBar.
func (a *App) ListTabs() []TabMeta {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]TabMeta, 0, len(a.tabs))
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			out = append(out, a.tabMeta(tab, tab.ID == a.activeTabID))
		}
	}
	return out
}

// OpenProjectTab builds a controller scoped to workspaceRoot and opens a tab
// for the given topic. If a tab with the same (workspaceRoot, topicID) is
// already open, it just activates the existing tab.
func (a *App) OpenProjectTab(workspaceRoot, topicID string) (TabMeta, error) {
	if workspaceRoot == "" {
		return TabMeta{}, fmt.Errorf("workspaceRoot is required")
	}
	if abs, err := filepath.Abs(workspaceRoot); err == nil {
		workspaceRoot = abs
	}
	saveWorkspace(workspaceRoot)
	_ = addProject(workspaceRoot, "")

	a.mu.Lock()
	// If already open, just activate.
	for _, tab := range a.tabs {
		if tab.Scope == "project" && tab.WorkspaceRoot == workspaceRoot && tab.TopicID == topicID {
			a.activeTabID = tab.ID
			meta := a.tabMeta(tab, true)
			a.saveTabsLocked()
			a.mu.Unlock()
			return meta, nil
		}
	}

	tabID := a.newUniqueTabIDLocked()
	topicTitle := topicTitleForTab("project", workspaceRoot, topicID)
	tab := &WorkspaceTab{
		ID:               tabID,
		Scope:            "project",
		WorkspaceRoot:    workspaceRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitle,
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}

	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	a.mu.Unlock()

	a.startTabControllerBuild(tab)
	a.emitProjectTreeChanged()
	return a.tabMeta(tab, true), nil
}

// OpenGlobalTab opens a new global-scope tab (no project root). The global
// workspace root is the reasonix user config directory.
func (a *App) OpenGlobalTab(topicID string) (TabMeta, error) {
	globalRoot := globalWorkspaceRoot()
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		return TabMeta{}, fmt.Errorf("create global workspace: %w", err)
	}

	a.mu.Lock()
	for _, tab := range a.tabs {
		if tab.Scope == "global" && tab.TopicID == topicID {
			a.activeTabID = tab.ID
			meta := a.tabMeta(tab, true)
			a.saveTabsLocked()
			a.mu.Unlock()
			return meta, nil
		}
	}

	tabID := a.newUniqueTabIDLocked()
	topicTitle := topicTitleForTab("global", "", topicID)
	tab := &WorkspaceTab{
		ID:               tabID,
		Scope:            "global",
		WorkspaceRoot:    globalRoot,
		TopicID:          topicID,
		TopicTitle:       topicTitle,
		mode:             "normal",
		toolApprovalMode: control.ToolApprovalAsk,
		disabledMCP:      map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}

	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	a.mu.Unlock()

	a.startTabControllerBuild(tab)
	return a.tabMeta(tab, true), nil
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
			a.activeTabID = tab.ID
			meta := a.tabMeta(tab, true)
			a.saveTabsLocked()
			a.mu.Unlock()
			return meta, nil
		}
	}

	// Inherit model, effort, mode, tool-approval, and MCP state from the
	// active tab so a new blank session keeps the same settings (#4019).
	var inheritedModel string
	var inheritedEffort *string
	inheritedMode := "normal"
	inheritedToolApprovalMode := control.ToolApprovalAsk
	inheritedDisabledMCP := map[string]ServerView{}
	var inheritedMCPOrder []string
	if active := a.activeTabLocked(); active != nil {
		inheritedModel = active.model
		inheritedEffort = cloneStringPtr(active.effort)
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
		return meta, nil
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
	return meta, nil
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
	if tab.Ctrl.Running() {
		return false
	}
	return !messagesHaveConversationContent(tab.Ctrl.History())
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
	for _, topicID := range topicIDs {
		if openTopics[topicID] {
			continue
		}
		if topicTitleForTab(scope, workspaceRoot, topicID) != defaultTopicTitle {
			continue
		}
		if findTopicSession(config.SessionDir(), topicID) != "" {
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
	defer a.mu.Unlock()
	if _, ok := a.tabs[tabID]; !ok {
		return fmt.Errorf("tab %q not found", tabID)
	}
	if a.activeTabID == tabID {
		return nil
	}
	a.activeTabID = tabID
	a.saveTabsLocked()
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

// CloseTab shuts down a tab's controller (snapshot + cancel + close) and
// removes it. The active tab cannot be closed when it is the last one; the
// frontend should prompt first.
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
		tab.Ctrl.Cancel()
		_ = tab.Ctrl.Snapshot()
		tab.Ctrl.Close()
	}
	if tab.sink != nil {
		tab.sink.ctx = nil // stop further emissions (nil ctx → Emit becomes no-op)
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
	wailsCtx := a.ctx
	buildCtx := a.bootContext()

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

	// A key resolved from this project's .env is project-scoped; lift it into the
	// global credentials store so it follows the user to every other workspace.
	promoteProviderKeysToCredentials(cfg)

	model := strings.TrimSpace(tab.model)
	if model == "" {
		model = cfg.DefaultModel
	}
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

	if tab.sink != nil {
		tab.sink.ctx = wailsCtx
	}

	sessionDir := desktopSessionDir(root)
	topicID := strings.TrimSpace(tab.TopicID)
	if tab.Scope == "global" {
		migratedTopics := migrateLegacySessionsIntoGlobalTopics(config.SessionDir())
		if len(migratedTopics) > 0 {
			a.emitProjectTreeChanged()
		}
		if topicID == "" && len(migratedTopics) > 0 {
			topicID = migratedTopics[0]
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
	}
	if topicID != "" {
		if _, dir := a.findKnownTopicSession(topicID); dir != "" {
			sessionDir = dir
		}
	}

	ctrl, err := boot.Build(buildCtx, boot.Options{
		Model:          model,
		RequireKey:     false,
		Sink:           tab.sink,
		WorkspaceRoot:  root,
		SessionDir:     sessionDir,
		EffortOverride: cloneStringPtr(tab.effort),
	})
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
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
		if loaded, pinnedPath, ok := loadPinnedTabSession(dir, tab.SessionPath); ok {
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
				if loaded, err := agent.LoadSession(existingPath); err == nil {
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
func (a *App) activeCtrl() *control.Controller {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeCtrlLocked()
}

// activeCtrlLocked is like activeCtrl but assumes the caller already holds a.mu.
func (a *App) activeCtrlLocked() *control.Controller {
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

func (a *App) ctrlByTabID(tabID string) *control.Controller {
	a.mu.RLock()
	defer a.mu.RUnlock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		return nil
	}
	return tab.Ctrl
}

// activeSink returns the active tab's event sink, or nil.
func (a *App) activeSink() *tabEventSink {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t := a.activeTabLocked()
	if t == nil {
		return nil
	}
	return t.sink
}

// activeModel returns the active tab's model ref.
func (a *App) activeModel() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t := a.activeTabLocked()
	if t == nil {
		return ""
	}
	return t.model
}

// activeDisabledMCP returns the active tab's disabled MCP map.
func (a *App) activeDisabledMCP() map[string]ServerView {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t := a.activeTabLocked()
	if t == nil {
		return map[string]ServerView{}
	}
	return t.disabledMCP
}

// activeMCPOrder returns the active tab's MCP order.
func (a *App) activeMCPOrder() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t := a.activeTabLocked()
	if t == nil {
		return nil
	}
	return t.mcpOrder
}

// --- autosave per tab -------------------------------------------------------

func (a *App) scheduleTabSnapshot(tabID string) {
	a.mu.RLock()
	tab, ok := a.tabs[tabID]
	a.mu.RUnlock()
	if !ok {
		return
	}
	tab.saveMu.Lock()
	if tab.saving {
		tab.saveAgain = true
		tab.saveMu.Unlock()
		return
	}
	tab.saving = true
	tab.saveMu.Unlock()
	go a.tabSnapshotLoop(tab)
}

func (a *App) tabSnapshotLoop(tab *WorkspaceTab) {
	for {
		a.mu.RLock()
		ctrl := tab.Ctrl
		a.mu.RUnlock()
		if ctrl != nil {
			if err := ctrl.Snapshot(); err == nil {
				if !a.maybeAutoTitleTopic(tab) {
					a.emitProjectTreeChanged()
				}
			}
		}
		tab.saveMu.Lock()
		if tab.saveAgain {
			tab.saveAgain = false
			tab.saveMu.Unlock()
			continue
		}
		tab.saving = false
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
			return topicTitleFromText(agent.HandoffTask(msg.Content))
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
	Root   string   `json:"root"`
	Title  string   `json:"title,omitempty"`
	Color  string   `json:"color,omitempty"`
	Topics []string `json:"topics"` // ordered topic IDs
}

type desktopProjectFile struct {
	GlobalTitle  string           `json:"globalTitle,omitempty"`
	GlobalColor  string           `json:"globalColor,omitempty"`
	GlobalTopics []string         `json:"globalTopics,omitempty"`
	SidebarOrder []string         `json:"sidebarOrder,omitempty"`
	Projects     []desktopProject `json:"projects"`
}

type desktopTabEntry struct {
	ID               string  `json:"id"`
	Scope            string  `json:"scope"`
	WorkspaceRoot    string  `json:"workspaceRoot"`
	TopicID          string  `json:"topicId"`
	SessionPath      string  `json:"sessionPath,omitempty"`
	Model            string  `json:"model,omitempty"`
	Effort           *string `json:"effort,omitempty"`
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
	dir := desktopConfigDir()
	os.MkdirAll(dir, 0o755)
	var entries []desktopTabEntry
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			entries = append(entries, desktopTabEntry{
				ID:               tab.ID,
				Scope:            tab.Scope,
				WorkspaceRoot:    tab.WorkspaceRoot,
				TopicID:          tab.TopicID,
				SessionPath:      tab.currentSessionPath(),
				Model:            tab.model,
				Effort:           cloneStringPtr(tab.effort),
				Mode:             persistedTabMode(currentTabMode(tab)),
				Goal:             strings.TrimSpace(currentTabGoal(tab)),
				ToolApprovalMode: persistedToolApprovalMode(currentTabToolApprovalMode(tab)),
			})
		}
	}
	f := desktopTabsFile{Tabs: entries, ActiveTab: a.activeTabID}
	b, _ := json.MarshalIndent(f, "", "  ")
	path := filepath.Join(dir, tabsFileName)
	tmp := path + ".tmp"
	os.WriteFile(tmp, b, 0o644)
	os.Rename(tmp, path)
}

func (a *App) orderedTabIDsLocked() []string {
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
	if len(ordered) != len(a.tabOrder) || len(missing) > 0 {
		a.tabOrder = append([]string(nil), ordered...)
	}
	return ordered
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
	json.Unmarshal(b, &f)
	return f
}

func loadProjectsFile() desktopProjectFile {
	path := filepath.Join(desktopConfigDir(), desktopProjectsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return desktopProjectFile{}
	}
	var f desktopProjectFile
	json.Unmarshal(b, &f)
	return normalizeProjectsFile(f)
}

func saveProjectsFile(f desktopProjectFile) error {
	dir := desktopConfigDir()
	os.MkdirAll(dir, 0o755)
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
	return os.Rename(tmp, path)
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
		GlobalTitle:  strings.TrimSpace(f.GlobalTitle),
		GlobalColor:  normalizeProjectColor(f.GlobalColor),
		GlobalTopics: uniqueStrings(f.GlobalTopics),
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
		if i, ok := index[root]; ok {
			if out.Projects[i].Title == "" && p.Title != "" {
				out.Projects[i].Title = p.Title
			}
			if out.Projects[i].Color == "" && p.Color != "" {
				out.Projects[i].Color = p.Color
			}
			out.Projects[i].Topics = uniqueStrings(append(out.Projects[i].Topics, p.Topics...))
			continue
		}
		index[root] = len(out.Projects)
		out.Projects = append(out.Projects, p)
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

func projectTitle(root string) string {
	root = normalizeProjectRoot(root)
	for _, p := range loadProjectsFile().Projects {
		if p.Root == root {
			return projectDisplayName(p)
		}
	}
	return workspaceName(root)
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

func loadTopicTitles(workspaceRoot string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(topicTitlesPath(workspaceRoot))
	if err != nil {
		return m
	}
	json.Unmarshal(b, &m)
	return m
}

func loadTopicTitleSources(workspaceRoot string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(topicTitleSourcesPath(workspaceRoot))
	if err != nil {
		return m
	}
	json.Unmarshal(b, &m)
	return m
}

func loadTopicCreatedAts(workspaceRoot string) map[string]int64 {
	m := map[string]int64{}
	b, err := os.ReadFile(topicCreatedAtsPath(workspaceRoot))
	if err != nil {
		return m
	}
	json.Unmarshal(b, &m)
	return m
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
	return os.Rename(tmp, path)
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
	return os.Rename(tmp, path)
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
	return os.Rename(tmp, path)
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
	m := loadTopicTitles(workspaceRoot)
	if strings.TrimSpace(title) == "" {
		delete(m, topicID)
	} else {
		m[topicID] = strings.TrimSpace(title)
	}
	if err := saveTopicTitles(workspaceRoot, m); err != nil {
		return err
	}

	sources := loadTopicTitleSources(workspaceRoot)
	if strings.TrimSpace(title) == "" || strings.TrimSpace(source) == "" {
		delete(sources, topicID)
	} else {
		sources[topicID] = strings.TrimSpace(source)
	}
	return saveTopicTitleSources(workspaceRoot, sources)
}

func setTopicTitleSource(workspaceRoot, topicID, source string) error {
	sources := loadTopicTitleSources(workspaceRoot)
	if strings.TrimSpace(source) == "" {
		delete(sources, topicID)
	} else {
		sources[topicID] = strings.TrimSpace(source)
	}
	return saveTopicTitleSources(workspaceRoot, sources)
}

func setTopicCreatedAt(workspaceRoot, topicID string, createdAt int64) error {
	created := loadTopicCreatedAts(workspaceRoot)
	topicID = strings.TrimSpace(topicID)
	if topicID == "" || createdAt <= 0 {
		delete(created, topicID)
	} else {
		created[topicID] = createdAt
	}
	return saveTopicCreatedAts(workspaceRoot, created)
}

func deleteTopicCreatedAt(workspaceRoot, topicID string) {
	created := loadTopicCreatedAts(workspaceRoot)
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

func (a *App) tabTelemetryPath(tabID string) string {
	a.mu.RLock()
	tab, ok := a.tabs[tabID]
	var ctrl *control.Controller
	if ok && tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if !ok || ctrl == nil {
		return ""
	}
	sp := ctrl.SessionPath()
	if sp == "" {
		return ""
	}
	return sp + ".telemetry.json"
}

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
	return os.Rename(tmp, path)
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
	Kind           string        `json:"kind"` // "project" | "topic" | "global_folder" | "global_topic"
	Label          string        `json:"label"`
	Root           string        `json:"root,omitempty"` // project workspace root
	TopicID        string        `json:"topicId,omitempty"`
	ProjectColor   string        `json:"projectColor,omitempty"`
	Turns          int           `json:"turns,omitempty"`
	CreatedAt      int64         `json:"createdAt,omitempty"`
	LastActivityAt int64         `json:"lastActivityAt,omitempty"`
	Open           bool          `json:"open,omitempty"`
	Running        bool          `json:"running,omitempty"`
	Status         string        `json:"status,omitempty"`
	Children       []ProjectNode `json:"children,omitempty"`
}

func normalizeTopicStatus(status string) string {
	switch status {
	case topicStatusThinking, topicStatusStreaming, topicStatusWaitingConfirmation, topicStatusPaused, topicStatusError:
		return status
	default:
		return ""
	}
}

func topicStatusPriority(status string) int {
	switch normalizeTopicStatus(status) {
	case topicStatusWaitingConfirmation:
		return 60
	case topicStatusStreaming:
		return 40
	case topicStatusThinking:
		return 30
	case topicStatusPaused:
		return 20
	case topicStatusError:
		return 10
	default:
		return 0
	}
}

func mergeTopicStatus(current, candidate string) string {
	if topicStatusPriority(candidate) > topicStatusPriority(current) {
		return normalizeTopicStatus(candidate)
	}
	return normalizeTopicStatus(current)
}

func activityStatusForTab(tab *WorkspaceTab) string {
	if tab == nil {
		return ""
	}
	status := normalizeTopicStatus(tab.ActivityStatus)
	running := tab.Ctrl != nil && tab.Ctrl.Running()
	if running {
		if status == "" || status == topicStatusError {
			return topicStatusThinking
		}
		return status
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

func migrateLegacySessionsIntoGlobalTopics(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	infos, err := agent.ListSessions(dir)
	if err != nil || len(infos) == 0 {
		return nil
	}
	titles := loadSessionTitles(dir)
	topicTitles := loadTopicTitles("")
	topicSources := loadTopicTitleSources("")
	f := loadProjectsFile()

	var migratedTopicIDs []string
	for _, info := range infos {
		if strings.TrimSpace(info.TopicID) != "" {
			continue
		}
		topicID := legacySessionTopicID(info.Path)
		if topicID == "" {
			continue
		}
		title := strings.TrimSpace(titles[filepath.Base(info.Path)])
		if title == "" {
			title = topicTitleFromText(info.Preview)
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
			continue
		}
		// Only adopt genuinely-global, unscoped legacy sessions. A session that
		// already carries a project scope or workspace root is not legacy — never
		// strip its binding into Global.
		if meta.Scope == "project" || strings.TrimSpace(meta.WorkspaceRoot) != "" {
			continue
		}
		meta.Scope = "global"
		meta.WorkspaceRoot = ""
		meta.TopicID = topicID
		meta.TopicTitle = title
		if err := agent.SaveBranchMetaPreserveUpdated(info.Path, meta); err != nil {
			continue
		}
		if strings.TrimSpace(topicTitles[topicID]) == "" {
			topicTitles[topicID] = title
			topicSources[topicID] = topicTitleSourceManual
		}
		migratedTopicIDs = append(migratedTopicIDs, topicID)
	}
	if len(migratedTopicIDs) == 0 {
		return nil
	}
	f.GlobalTopics = uniqueStrings(append(migratedTopicIDs, f.GlobalTopics...))
	_ = saveProjectsFile(f)
	_ = saveTopicTitles("", topicTitles)
	_ = saveTopicTitleSources("", topicSources)
	return migratedTopicIDs
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
	return agent.SaveBranchMetaPreserveUpdated(sessionPath, meta)
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
				if title := topicTitleFromText(msg.Content); title != "" {
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
		infos, err := agent.ListSessions(dir)
		if err != nil {
			continue
		}
		for _, info := range infos {
			if info.TopicID != topicID {
				continue
			}
			meta, ok, err := agent.LoadBranchMeta(info.Path)
			if err != nil || !ok {
				continue
			}
			meta.TopicTitle = title
			_ = agent.SaveBranchMetaPreserveUpdated(info.Path, meta)
		}
	}
}

func (a *App) setTabActivityStatus(tabID, status string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	tab := a.tabs[tabID]
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
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "project-tree:changed")
	}
}

// DeleteTopic removes a topic and its title metadata.
func (a *App) DeleteTopic(topicID string) error {
	f := loadProjectsFile()
	found := false
	for _, p := range f.Projects {
		m := loadTopicTitles(p.Root)
		if _, ok := m[topicID]; ok {
			delete(m, topicID)
			_ = saveTopicTitles(p.Root, m)
			sources := loadTopicTitleSources(p.Root)
			delete(sources, topicID)
			_ = saveTopicTitleSources(p.Root, sources)
			deleteTopicCreatedAt(p.Root, topicID)
			found = true
			break
		}
	}
	if !found {
		m := loadTopicTitles("")
		if _, ok := m[topicID]; ok {
			delete(m, topicID)
			_ = saveTopicTitles("", m)
			sources := loadTopicTitleSources("")
			delete(sources, topicID)
			_ = saveTopicTitleSources("", sources)
			deleteTopicCreatedAt("", topicID)
			f.GlobalTopics = removeString(f.GlobalTopics, topicID)
			found = true
		}
	}
	if !found {
		return fmt.Errorf("topic %q not found", topicID)
	}
	// Remove from project topic list.
	for i, p := range f.Projects {
		for j, tid := range p.Topics {
			if tid == topicID {
				f.Projects[i].Topics = append(f.Projects[i].Topics[:j], f.Projects[i].Topics[j+1:]...)
				break
			}
		}
	}
	_ = saveProjectsFile(f)
	a.emitProjectTreeChanged()
	return nil
}

// TrashTopic removes a topic from the project tree and moves its saved session
// records into the session trash. Open non-running tabs for the topic are first
// snapshotted and closed so their autosave files can be moved instead of being
// recreated immediately after deletion.
func (a *App) TrashTopic(topicID string) error {
	if strings.TrimSpace(topicID) == "" {
		return fmt.Errorf("topicID is required")
	}

	type topicTab struct {
		id            string
		tab           *WorkspaceTab
		ctrl          *control.Controller
		sink          *tabEventSink
		scope         string
		workspaceRoot string
	}
	var openTabs []topicTab
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab == nil || tab.TopicID != topicID {
			continue
		}
		if tab.Ctrl != nil && tab.Ctrl.Running() {
			a.mu.RUnlock()
			return fmt.Errorf("can't move a running conversation to trash; stop it first")
		}
		openTabs = append(openTabs, topicTab{
			id:            tab.ID,
			tab:           tab,
			ctrl:          tab.Ctrl,
			sink:          tab.sink,
			scope:         tab.Scope,
			workspaceRoot: tab.WorkspaceRoot,
		})
	}
	a.mu.RUnlock()

	for _, item := range openTabs {
		if item.ctrl != nil {
			if err := item.ctrl.Snapshot(); err != nil {
				return err
			}
			item.ctrl.Close()
		}
		if item.sink != nil {
			item.sink.ctx = nil
		}
	}

	var fallbackScope, fallbackRoot string
	needsFallback := false
	if len(openTabs) > 0 {
		fallbackScope = openTabs[0].scope
		fallbackRoot = openTabs[0].workspaceRoot
		a.mu.Lock()
		removedActive := false
		for _, item := range openTabs {
			if a.tabs[item.id] != item.tab {
				continue
			}
			if a.activeTabID == item.id {
				removedActive = true
			}
			delete(a.tabs, item.id)
			a.removeTabOrderLocked(item.id)
		}
		if removedActive {
			a.activeTabID = ""
			if len(a.tabOrder) > 0 {
				a.activeTabID = a.tabOrder[0]
			}
		}
		needsFallback = len(a.tabs) == 0
		a.saveTabsLocked()
		a.mu.Unlock()
	}

	for _, dir := range a.knownSessionDirs() {
		infos, err := agent.ListSessions(dir)
		if err != nil {
			return err
		}
		for _, info := range infos {
			if info.TopicID != topicID {
				continue
			}
			sessionPath, _, err := validateSessionPath(dir, info.Path)
			if err != nil {
				return err
			}
			if err := deleteSessionFile(dir, sessionPath); err != nil {
				return err
			}
		}
	}
	if err := a.DeleteTopic(topicID); err != nil {
		return err
	}
	if needsFallback {
		if fallbackScope == "global" {
			fallbackRoot = ""
		}
		topic, err := a.CreateTopic(fallbackScope, fallbackRoot, "")
		if err != nil {
			return err
		}
		if fallbackScope == "global" {
			_, err = a.OpenGlobalTab(topic.ID)
		} else {
			_, err = a.OpenProjectTab(fallbackRoot, topic.ID)
		}
		if err != nil {
			return err
		}
	}
	a.emitProjectTreeChanged()
	return nil
}

// ListProjectTree builds the sidebar tree: project folders each containing
// their topics, plus a Global section.
func (a *App) ListProjectTree() []ProjectNode {
	migrateLegacySessionsIntoGlobalTopics(config.SessionDir())
	f := loadProjectsFile()
	out := []ProjectNode{}
	type topicSummary struct {
		turns          int
		lastActivityAt int64
	}
	topicSummaries := map[string]topicSummary{}
	for _, dir := range a.knownSessionDirs() {
		infos, err := agent.ListSessions(dir)
		if err != nil {
			continue
		}
		for _, info := range infos {
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
	openTopics := map[string]struct {
		open    bool
		running bool
		status  string
	}{}
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab == nil || strings.TrimSpace(tab.TopicID) == "" {
			continue
		}
		key := topicSummaryKey(tab.Scope, tab.WorkspaceRoot, tab.TopicID)
		status := openTopics[key]
		status.open = true
		if tab.Ctrl != nil && tab.Ctrl.Running() {
			status.running = true
		}
		status.status = mergeTopicStatus(status.status, activityStatusForTab(tab))
		openTopics[key] = status
	}
	a.mu.RUnlock()

	// Global section.
	globalTitleMap := loadTopicTitles("")
	globalCreatedMap := loadTopicCreatedAts("")
	if len(globalTitleMap) > 0 || len(f.Projects) == 0 {
		globalTitle := strings.TrimSpace(f.GlobalTitle)
		if globalTitle == "" {
			globalTitle = "Global"
		}
		globalColor := normalizeProjectColor(f.GlobalColor)
		globalTopicIDs := orderedTopicIDs(f.GlobalTopics, globalTitleMap)
		children := make([]ProjectNode, 0, len(globalTopicIDs))
		for _, id := range globalTopicIDs {
			title := globalTitleMap[id]
			summary := topicSummaries[topicSummaryKey("global", "", id)]
			status := openTopics[topicSummaryKey("global", "", id)]
			children = append(children, ProjectNode{
				Key:            "global_topic_" + id,
				Kind:           "global_topic",
				Label:          title,
				TopicID:        id,
				ProjectColor:   globalColor,
				Turns:          summary.turns,
				CreatedAt:      globalCreatedMap[id],
				LastActivityAt: summary.lastActivityAt,
				Open:           status.open,
				Running:        status.running,
				Status:         status.status,
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
	for _, p := range f.Projects {
		title := p.Title
		if title == "" {
			title = workspaceName(p.Root)
		}
		node := ProjectNode{
			Key:  "project_" + p.Root,
			Kind: "project",
			Root: p.Root,
		}

		// Gather topics: explicit topic list + all known topic titles.
		titleMap := loadTopicTitles(p.Root)
		createdMap := loadTopicCreatedAts(p.Root)
		topicIDs := orderedTopicIDs(p.Topics, titleMap)

		children := make([]ProjectNode, 0, len(topicIDs))
		for _, tid := range topicIDs {
			topicTitle := strings.TrimSpace(titleMap[tid])
			if topicTitle == "" {
				topicTitle = topicTitleForTab("project", p.Root, tid)
			}
			summary := topicSummaries[topicSummaryKey("project", p.Root, tid)]
			status := openTopics[topicSummaryKey("project", p.Root, tid)]
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
				Open:           status.open,
				Running:        status.running,
				Status:         status.status,
			})
		}
		node.Label = title
		node.ProjectColor = p.Color
		node.Children = children
		out = append(out, node)
	}

	return applyProjectTreeOrder(out, f.SidebarOrder)
}

func topicSummaryKey(scope, workspaceRoot, topicID string) string {
	if scope == "global" {
		return "global::" + topicID
	}
	return "project:" + workspaceRoot + ":" + topicID
}

// ContextPanelInfo is the right-side panel's data for one tab.
type ContextPanelInfo struct {
	UsedTokens       int               `json:"usedTokens"`
	WindowTokens     int               `json:"windowTokens"`
	PromptTokens     int               `json:"promptTokens"`
	CompletionTokens int               `json:"completionTokens"`
	TotalTokens      int               `json:"totalTokens"`
	ReasoningTokens  int               `json:"reasoningTokens"`
	CacheHitTokens   int               `json:"cacheHitTokens"`
	CacheMissTokens  int               `json:"cacheMissTokens"`
	RequestCount     int               `json:"requestCount"`
	ElapsedMs        int64             `json:"elapsedMs"`
	SessionCost      float64           `json:"sessionCost"`
	SessionCurrency  string            `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64           `json:"sessionCostUsd,omitempty"`
	Mock             bool              `json:"mock,omitempty"`
	ReadFiles        []readFileRecord  `json:"readFiles"`
	ChangedFiles     []ChangedFileInfo `json:"changedFiles"`
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
	var ctrl *control.Controller
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
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" || dir == "" {
		return nil, "", false
	}
	path, _, err := validateSessionPath(dir, sessionPath)
	if err != nil {
		return nil, "", false
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
	return agent.SaveBranchMetaPreserveUpdated(path, m)
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
		add(desktopSessionDir(project.Root))
	}
	a.mu.RLock()
	for _, tab := range a.tabs {
		add(tabSessionDir(tab))
	}
	a.mu.RUnlock()
	return out
}

// findTopicSession scans the session directory for a .jsonl file whose .meta
// carries the given topicID. Returns the most recently updated match, or ""
// if no session exists for this topic.
func findTopicSession(dir, topicID string) string {
	if topicID == "" || dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var bestPath string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok {
			continue
		}
		if meta.TopicID != topicID {
			continue
		}
		if meta.UpdatedAt.After(bestTime) {
			bestTime = meta.UpdatedAt
			bestPath = path
		}
	}
	return bestPath
}

func (a *App) findKnownTopicSession(topicID string) (string, string) {
	for _, dir := range a.knownSessionDirs() {
		if path := findTopicSession(dir, topicID); path != "" {
			return path, dir
		}
	}
	return "", ""
}
