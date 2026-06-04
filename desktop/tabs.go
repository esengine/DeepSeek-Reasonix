package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
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
	Ctrl          *control.Controller // nil while booting / on error
	Label         string              // model label (for the tab badge)
	Ready         bool                // true once boot.Build completes
	StartupErr    string              // build error, surfaced to the frontend
	sink          *tabEventSink       // routes events with this tab's ID

	// Per-turn autosave per tab.
	saveMu    sync.Mutex
	saving    bool
	saveAgain bool

	// readTelemetry tracks files read during this tab's session.
	readTelemetry []readFileRecord
	telemMu       sync.Mutex

	model       string // active model ref (for meta)
	disabledMCP map[string]ServerView
	mcpOrder    []string
}

type readFileRecord struct {
	Path      string `json:"path"`
	Turn      int    `json:"turn"`
	Time      int64  `json:"time"`
	Offset    int    `json:"offset,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (t *WorkspaceTab) recordReadFile(rec readFileRecord) {
	t.telemMu.Lock()
	t.readTelemetry = append(t.readTelemetry, rec)
	t.telemMu.Unlock()
}

func (t *WorkspaceTab) readTelemetrySnapshot() []readFileRecord {
	t.telemMu.Lock()
	defer t.telemMu.Unlock()
	out := make([]readFileRecord, len(t.readTelemetry))
	copy(out, t.readTelemetry)
	return out
}

// tabEventSink wraps a parent event.Sink and prepends a tabId to every wire
// event so the frontend can route it to the correct tab's reducer.
type tabEventSink struct {
	tabID string
	app   *App
	ctx   context.Context
}

func (s *tabEventSink) Emit(e event.Event) {
	if s.ctx != nil {
		runtime.EventsEmit(s.ctx, eventChannel, toWireTab(e, s.tabID))
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
		_ = saveTelemetry(sp+".telemetry.json", tab.readTelemetrySnapshot())
	}
}

// --- wire event with tab ----------------------------------------------------

func toWireTab(e event.Event, tabID string) wireEventTab {
	w := toWire(e)
	return wireEventTab{
		wireEvent:         w,
		TabID:             tabID,
		SessionHitTokens:  e.SessionHit,
		SessionMissTokens: e.SessionMiss,
		SessionCostUsd:    0, // filled by frontend accumulator per tab
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
	// SessionCostUsd is filled by the frontend's per-tab accumulator.
	SessionCostUsd float64 `json:"sessionCostUsd,omitempty"`
}

// --- Tab management on App --------------------------------------------------

// TabMeta is the frontend-facing shape of one tab.
type TabMeta struct {
	ID            string `json:"id"`
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	WorkspaceName string `json:"workspaceName"`
	TopicID       string `json:"topicId"`
	TopicTitle    string `json:"topicTitle"`
	Label         string `json:"label"`
	Ready         bool   `json:"ready"`
	Running       bool   `json:"running"`
	StartupErr    string `json:"startupErr,omitempty"`
	Active        bool   `json:"active"`
	Cwd           string `json:"cwd"`
}

func (a *App) tabMeta(tab *WorkspaceTab, active bool) TabMeta {
	m := TabMeta{
		ID:            tab.ID,
		Scope:         tab.Scope,
		WorkspaceRoot: tab.WorkspaceRoot,
		WorkspaceName: workspaceName(tab.WorkspaceRoot),
		TopicID:       tab.TopicID,
		TopicTitle:    tab.TopicTitle,
		Label:         tab.Label,
		Ready:         tab.Ready,
		StartupErr:    tab.StartupErr,
		Active:        active,
		Cwd:           tab.WorkspaceRoot,
	}
	if tab.Ctrl != nil {
		m.Running = tab.Ctrl.Running()
	}
	return m
}

// ListTabs returns every open tab's metadata for the frontend TabBar.
func (a *App) ListTabs() []TabMeta {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]TabMeta, 0, len(a.tabs))
	for _, tab := range a.tabs {
		out = append(out, a.tabMeta(tab, tab.ID == a.activeTabID))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].ID < out[j].ID
	})
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

	tabID := newTabID()
	topicTitle := loadTopicTitle(workspaceRoot, topicID)
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         "project",
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}

	a.tabs[tabID] = tab
	a.activeTabID = tabID
	a.saveTabsLocked()
	a.mu.Unlock()

	go a.buildTabController(tab)
	return a.tabMeta(tab, true), nil
}

// OpenGlobalTab opens a new global-scope tab (no project root). The global
// workspace root is the reasonix user config directory.
func (a *App) OpenGlobalTab(topicID string) (TabMeta, error) {
	globalRoot, err := ensureGlobalWorkspaceRoot()
	if err != nil {
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

	tabID := newTabID()
	topicTitle := loadTopicTitle("", topicID)
	if topicTitle == "" {
		topicTitle = "Global"
	}
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}

	a.tabs[tabID] = tab
	a.activeTabID = tabID
	a.saveTabsLocked()
	a.mu.Unlock()

	go a.buildTabController(tab)
	return a.tabMeta(tab, true), nil
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
	delete(a.tabs, tabID)
	wasActive := a.activeTabID == tabID
	if wasActive {
		// Pick another tab.
		for id := range a.tabs {
			a.activeTabID = id
			break
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
func (a *App) buildTabController(tab *WorkspaceTab) {
	ctx := a.ctx

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
		a.emitAgentReady(ctx)
		return
	}

	model := cfg.DefaultModel
	if e, ok := cfg.ResolveModel(model); ok {
		model = e.Name + "/" + e.Model
	}

	a.mu.Lock()
	tab.model = model
	tab.Label = model
	a.mu.Unlock()

	if tab.sink != nil {
		tab.sink.ctx = ctx
	}

	ctrl, err := boot.Build(ctx, boot.Options{
		Model:         model,
		RequireKey:    false,
		Sink:          tab.sink,
		WorkspaceRoot: root,
		Stderr:        io.Discard,
	})
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
		a.mu.Unlock()
		a.emitAgentReady(ctx)
		return
	}

	ctrl.EnableInteractiveApproval()

	if dir := ctrl.SessionDir(); dir != "" {
		var path string
		// When the tab has a TopicID, look for an existing session for this topic
		// so the user continues the conversation rather than starting fresh.
		if tab.TopicID != "" {
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
			m, _ := agent.EnsureBranchMeta(path)
			m.Scope = tab.Scope
			m.WorkspaceRoot = tab.WorkspaceRoot
			m.TopicID = tab.TopicID
			m.TopicTitle = tab.TopicTitle
			_ = agent.SaveBranchMeta(path, m)
			// Restore existing telemetry if resuming a session.
			telemetryPath := path + ".telemetry.json"
			if records := loadTelemetry(telemetryPath); len(records) > 0 {
				tab.telemMu.Lock()
				tab.readTelemetry = records
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
	a.emitAgentReady(ctx)
}

func (a *App) emitAgentReady(ctx context.Context) {
	if a.readyHook != nil {
		a.readyHook()
		return
	}
	if ctx != nil {
		runtime.EventsEmit(ctx, "agent:ready")
	}
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
			_ = ctrl.Snapshot()
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

// --- persistence: desktop-projects.json -------------------------------------

const desktopProjectsFile = "desktop-projects.json"
const tabsFileName = "desktop-tabs.json"

type desktopProject struct {
	Root   string   `json:"root"`
	Title  string   `json:"title,omitempty"`
	Topics []string `json:"topics"` // ordered topic IDs
}

type desktopProjectFile struct {
	Projects []desktopProject `json:"projects"`
}

type desktopTabEntry struct {
	ID            string `json:"id"`
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot"`
	TopicID       string `json:"topicId"`
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
	for _, tab := range a.tabs {
		entries = append(entries, desktopTabEntry{
			ID:            tab.ID,
			Scope:         tab.Scope,
			WorkspaceRoot: tab.WorkspaceRoot,
			TopicID:       tab.TopicID,
		})
	}
	f := desktopTabsFile{Tabs: entries, ActiveTab: a.activeTabID}
	b, _ := json.MarshalIndent(f, "", "  ")
	path := filepath.Join(dir, tabsFileName)
	tmp := path + ".tmp"
	os.WriteFile(tmp, b, 0o644)
	os.Rename(tmp, path)
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
	return f
}

func saveProjectsFile(f desktopProjectFile) error {
	dir := desktopConfigDir()
	os.MkdirAll(dir, 0o755)
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

func addProject(root, title string) error {
	f := loadProjectsFile()
	for _, p := range f.Projects {
		if p.Root == root {
			if title != "" {
				p.Title = title
			}
			return saveProjectsFile(f)
		}
	}
	f.Projects = append(f.Projects, desktopProject{Root: root, Title: title})
	return saveProjectsFile(f)
}

func projectTitle(root string) string {
	for _, p := range loadProjectsFile().Projects {
		if p.Root == root {
			if p.Title != "" {
				return p.Title
			}
			return workspaceName(root)
		}
	}
	return workspaceName(root)
}

// --- topic helpers ----------------------------------------------------------

const topicTitlesFile = "desktop-topic-titles.json"

func topicTitlesPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitlesFile)
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

func loadTopicTitle(workspaceRoot, topicID string) string {
	return loadTopicTitles(workspaceRoot)[topicID]
}

func setTopicTitle(workspaceRoot, topicID, title string) error {
	m := loadTopicTitles(workspaceRoot)
	if strings.TrimSpace(title) == "" {
		delete(m, topicID)
	} else {
		m[topicID] = strings.TrimSpace(title)
	}
	return saveTopicTitles(workspaceRoot, m)
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

func saveTelemetry(path string, records []readFileRecord) error {
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadTelemetry(path string) []readFileRecord {
	b, err := os.ReadFile(path)
	if err != nil {
		return []readFileRecord{}
	}
	var records []readFileRecord
	json.Unmarshal(b, &records)
	if records == nil {
		return []readFileRecord{}
	}
	return records
}

// --- project tree -----------------------------------------------------------

// ProjectNode is one node in the sidebar project tree (a project folder or a
// topic leaf).
type ProjectNode struct {
	Key      string        `json:"key"`  // stable key for React
	Kind     string        `json:"kind"` // "project" | "topic" | "global_folder" | "global_topic"
	Label    string        `json:"label"`
	Root     string        `json:"root,omitempty"` // project workspace root
	TopicID  string        `json:"topicId,omitempty"`
	Children []ProjectNode `json:"children,omitempty"`
}

// TopicMeta describes a topic for the project tree.
type TopicMeta struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
}

// CreateTopic creates a new topic under a project workspace and returns its metadata.
func (a *App) CreateTopic(scope, workspaceRoot, title string) (TopicMeta, error) {
	if strings.TrimSpace(title) == "" {
		return TopicMeta{}, fmt.Errorf("title is required")
	}
	topicID := newTopicID()
	if scope == "global" {
		workspaceRoot = ""
	}
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
		_ = addProject(workspaceRoot, "")
	}
	if err := setTopicTitle(workspaceRoot, topicID, title); err != nil {
		return TopicMeta{}, err
	}
	// Append to project's topic list.
	f := loadProjectsFile()
	for i, p := range f.Projects {
		if p.Root == workspaceRoot {
			f.Projects[i].Topics = append(f.Projects[i].Topics, topicID)
			_ = saveProjectsFile(f)
			break
		}
	}
	return TopicMeta{ID: topicID, Title: title, CreatedAt: time.Now().UnixMilli()}, nil
}

// RenameTopic updates a topic's display title.
func (a *App) RenameTopic(topicID, title string) error {
	// Find which workspace this topic belongs to by scanning all project topic titles.
	f := loadProjectsFile()
	for _, p := range f.Projects {
		m := loadTopicTitles(p.Root)
		if _, ok := m[topicID]; ok {
			return setTopicTitle(p.Root, topicID, title)
		}
	}
	// Check global.
	m := loadTopicTitles("")
	if _, ok := m[topicID]; ok {
		return setTopicTitle("", topicID, title)
	}
	return fmt.Errorf("topic %q not found", topicID)
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
			found = true
			break
		}
	}
	if !found {
		m := loadTopicTitles("")
		if _, ok := m[topicID]; ok {
			delete(m, topicID)
			_ = saveTopicTitles("", m)
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
	return nil
}

// ListProjectTree builds the sidebar tree: project folders each containing
// their topics, plus a Global section.
func (a *App) ListProjectTree() []ProjectNode {
	f := loadProjectsFile()
	out := []ProjectNode{}

	// Global section.
	globalTopics := loadTopicTitles("")
	if len(globalTopics) > 0 {
		children := make([]ProjectNode, 0, len(globalTopics))
		for id, title := range globalTopics {
			children = append(children, ProjectNode{
				Key:     "global_topic_" + id,
				Kind:    "global_topic",
				Label:   title,
				TopicID: id,
			})
		}
		sort.Slice(children, func(i, j int) bool {
			return children[i].Label < children[j].Label
		})
		out = append(out, ProjectNode{
			Key:      "global_folder",
			Kind:     "global_folder",
			Label:    "Global",
			Children: children,
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
		seen := map[string]bool{}
		var topicIDs []string
		for _, tid := range p.Topics {
			if !seen[tid] {
				seen[tid] = true
				topicIDs = append(topicIDs, tid)
			}
		}
		for tid := range titleMap {
			if !seen[tid] {
				seen[tid] = true
				topicIDs = append(topicIDs, tid)
			}
		}
		sort.Strings(topicIDs)

		children := make([]ProjectNode, 0, len(topicIDs))
		for _, tid := range topicIDs {
			topicTitle := titleMap[tid]
			if topicTitle == "" {
				topicTitle = tid
			}
			// Check if this topic is already open in a tab.
			opened := false
			a.mu.RLock()
			for _, tab := range a.tabs {
				if tab.TopicID == tid && tab.WorkspaceRoot == p.Root {
					opened = true
					break
				}
			}
			a.mu.RUnlock()
			label := topicTitle
			if opened {
				label = "● " + topicTitle
			}
			children = append(children, ProjectNode{
				Key:     "topic_" + tid,
				Kind:    "topic",
				Label:   label,
				Root:    p.Root,
				TopicID: tid,
			})
		}
		node.Label = title
		node.Children = children
		out = append(out, node)
	}

	// Open projects that don't yet have topics still show up.
	// Also add any workspace that has open tabs but isn't in the project list.
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab.Scope != "project" || tab.WorkspaceRoot == "" {
			continue
		}
		found := false
		for _, n := range out {
			if n.Root == tab.WorkspaceRoot {
				found = true
				break
			}
		}
		if !found {
			out = append(out, ProjectNode{
				Key:   "project_" + tab.WorkspaceRoot,
				Kind:  "project",
				Root:  tab.WorkspaceRoot,
				Label: workspaceName(tab.WorkspaceRoot),
				Children: []ProjectNode{{
					Key:     "topic_" + tab.TopicID,
					Kind:    "topic",
					Label:   "● " + tab.TopicTitle,
					Root:    tab.WorkspaceRoot,
					TopicID: tab.TopicID,
				}},
			})
		}
	}
	a.mu.RUnlock()

	return out
}

// ContextPanelInfo is the right-side panel's data for one tab.
type ContextPanelInfo struct {
	UsedTokens       int               `json:"usedTokens"`
	WindowTokens     int               `json:"windowTokens"`
	PromptTokens     int               `json:"promptTokens"`
	CompletionTokens int               `json:"completionTokens"`
	ReasoningTokens  int               `json:"reasoningTokens"`
	CacheHitTokens   int               `json:"cacheHitTokens"`
	CacheMissTokens  int               `json:"cacheMissTokens"`
	SessionCostUsd   float64           `json:"sessionCostUsd"`
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
	}

	if records := tab.readTelemetrySnapshot(); records != nil {
		info.ReadFiles = records
	}

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

func newTabID() string {
	var b [8]byte
	rand.Read(b[:])
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
