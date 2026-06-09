package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/fileref"
	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/i18n"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// eventChannel is the Wails runtime event name the frontend subscribes to for the
// agent's typed event stream. One channel carries every event kind; the payload's
// `kind` field discriminates — the desktop analogue of the serve transport's SSE
// `data:` frames.
const eventChannel = "agent:event"

// singleInstanceID is used by Wails to route a second desktop launch back to the
// running instance. Keep it stable across releases so launcher/Dock/taskbar
// reopen behavior remains predictable on every platform.
const singleInstanceID = "com.reasonix.desktop"

// App is the Wails-bound application object: the desktop frontend's command
// surface. Its exported methods (Submit/Cancel/Approve/…) are generated into JS
// bindings. The app manages multiple WorkspaceTabs — each with its own controller
// scoped to a project workspace — and routes commands to the active tab. Events
// flow the other way: each tab's controller emits to a tabEventSink that
// forwards events tagged with tabId to the webview via runtime.EventsEmit.
type App struct {
	ctx context.Context

	// mu protects the tab map, tabOrder, activeTabID, and per-tab fields that are read
	// from bound methods. All bound methods that touch a controller use activeCtrl().
	mu          sync.RWMutex
	tabs        map[string]*WorkspaceTab
	tabOrder    []string
	activeTabID string
	readyHook   func()

	forceQuit           atomic.Bool
	backgroundMaximised atomic.Bool
	trayReady           bool
	tray                *desktopTray

	mediaTokens *mediaTokenStore
}

// mediaTokenEntry holds metadata for a workspace media file served via temporary URL.
type mediaTokenEntry struct {
	absPath   string
	filename  string
	mime      string
	kind      string
	size      int64
	modTime   time.Time
	createdAt time.Time
	expiresAt time.Time
}

// mediaTokenStore manages temporary tokens that grant access to workspace files
// through the AssetServer middleware. Tokens expire after a fixed TTL and are
// capped at a maximum count; creating a new token evicts the oldest entry when
// the store is full.
type mediaTokenStore struct {
	mu    sync.Mutex
	byTok map[string]*mediaTokenEntry
	order []string // oldest first
	maxN  int
	ttl   time.Duration
}

const mediaTokenMax = 256

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{
		byTok: map[string]*mediaTokenEntry{},
		maxN:  mediaTokenMax,
		ttl:   10 * time.Minute,
	}
}

func (s *mediaTokenStore) cleanupLocked() {
	now := time.Now()
	for len(s.order) > 0 {
		tok := s.order[0]
		e := s.byTok[tok]
		if e == nil {
			s.order = s.order[1:]
			continue
		}
		if !now.Before(e.expiresAt) {
			delete(s.byTok, tok)
			s.order = s.order[1:]
			continue
		}
		break
	}
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
}

func (s *mediaTokenStore) create(absPath, filename, mime, kind string, size int64, modTime time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)

	now := time.Now()
	s.byTok[token] = &mediaTokenEntry{
		absPath:   absPath,
		filename:  filename,
		mime:      mime,
		kind:      kind,
		size:      size,
		modTime:   modTime,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	// Trim oldest if the new token pushed us over the limit.
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}

	return token
}

func (s *mediaTokenStore) get(token string) *mediaTokenEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byTok[token]
	if e == nil {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.byTok, token)
		return nil
	}
	return e
}

func (a *App) ensureMediaTokenStore() *mediaTokenStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mediaTokens == nil {
		a.mediaTokens = newMediaTokenStore()
	}
	return a.mediaTokens
}

// workspaceMediaMiddleware returns an HTTP middleware that intercepts
// /__reasonix_workspace_media/{token}/{filename} requests and serves the
// corresponding workspace file. All other paths pass through to the Wails
// default asset handler unchanged.
func (a *App) workspaceMediaMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			prefix := "/__reasonix_workspace_media/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			rest := strings.TrimPrefix(r.URL.Path, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 0 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			token := parts[0]

			entry := a.ensureMediaTokenStore().get(token)
			if entry == nil {
				http.NotFound(w, r)
				return
			}

			f, err := os.Open(entry.absPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()

			w.Header().Set("Content-Type", entry.mime)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": entry.filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=600")
			http.ServeContent(w, r, entry.filename, entry.modTime, f)
		})
	}
}

// NewApp constructs the bound object. Tabs are restored in startup from the
// last session's desktop-tabs.json.
func NewApp() *App {
	return &App{tabs: map[string]*WorkspaceTab{}, mediaTokens: newMediaTokenStore()}
}

func (a *App) bootContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// Platform exposes the native OS to the frontend so chrome/layout affordances can
// stay platform-scoped instead of relying on browser user-agent guesses.
func (a *App) Platform() string {
	return goruntime.GOOS
}

// startup runs once the webview process is up, before the frontend can issue any
// bound call. It captures the Wails context (needed for EventsEmit), then kicks
// off the initialization in a background goroutine so the webview loads immediately.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	installSystemQuitHook()
	a.startTray()

	go a.restoreOrBuildTabs()
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Swap(false) || consumeSystemQuitRequested() {
		return false
	}
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		cfg = config.LoadForEdit(config.UserConfigPath())
	}
	if cfg.DesktopCloseBehavior() == "background" {
		a.backgroundMaximised.Store(runtime.WindowIsMaximised(ctx))
		a.saveWindowStateSync()
		a.snapshotAllTabs()
		hideForBackground(ctx)
		return true
	}
	return false
}

func (a *App) showMainWindow() {
	if a.ctx != nil {
		showFromBackground(a.ctx, a.backgroundMaximised.Swap(false))
	}
}

func (a *App) secondInstanceLaunch() {
	a.showMainWindow()
}

func (a *App) quitApp() {
	if a.ctx == nil {
		return
	}
	a.forceQuit.Store(true)
	runtime.Quit(a.ctx)
}

func hideForBackground(ctx context.Context) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Hide(ctx)
		return
	}
	runtime.WindowHide(ctx)
}

func showFromBackground(ctx context.Context, wasMaximised bool) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Show(ctx)
	}
	plan := backgroundRestorePlanFor(goruntime.GOOS, wasMaximised)
	if plan.maximiseBeforeShow {
		runtime.WindowMaximise(ctx)
	}
	runtime.WindowShow(ctx)
	if plan.unminimiseAfterShow {
		runtime.WindowUnminimise(ctx)
	}
}

func backgroundCloseUsesApplicationHide(goos string) bool {
	return goos == "darwin"
}

type backgroundRestorePlan struct {
	maximiseBeforeShow  bool
	unminimiseAfterShow bool
}

func backgroundRestorePlanFor(goos string, wasMaximised bool) backgroundRestorePlan {
	if backgroundRestoreShouldMaximise(goos, wasMaximised) {
		return backgroundRestorePlan{maximiseBeforeShow: true}
	}
	return backgroundRestorePlan{unminimiseAfterShow: true}
}

func backgroundRestoreShouldMaximise(goos string, wasMaximised bool) bool {
	return wasMaximised && !backgroundCloseUsesApplicationHide(goos)
}

// restoreOrBuildTabs restores the tabs from the last session, or creates a
// default Global tab on first launch.
func (a *App) restoreOrBuildTabs() {
	ctx := a.ctx
	ensureWorkspace()

	// Load i18n from the first available config.
	if cfg, err := config.Load(); err == nil {
		i18n.DetectLanguage(cfg.Language)
	}

	f := loadTabsFile()
	if len(f.Tabs) > 0 {
		toBuild := make([]*WorkspaceTab, 0, len(f.Tabs))
		for _, entry := range f.Tabs {
			a.mu.Lock()
			id := a.restoredTabIDLocked(entry.ID)
			a.mu.Unlock()

			var tab *WorkspaceTab
			if entry.Scope == "project" {
				tab = a.createTabEntryWithID(entry.Scope, entry.WorkspaceRoot, entry.TopicID, id)
			} else {
				tab = a.createTabEntryWithID("global", globalTabWorkspaceRoot(), entry.TopicID, id)
			}
			tab.model = entry.Model
			tab.effort = cloneStringPtr(entry.Effort)
			tab.mode = persistedTabMode(entry.Mode)
			tab.SessionPath = strings.TrimSpace(entry.SessionPath)
			tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
			a.mu.Lock()
			a.tabs[tab.ID] = tab
			a.tabOrder = append(a.tabOrder, tab.ID)
			a.mu.Unlock()
			toBuild = append(toBuild, tab)
		}
		a.mu.Lock()
		if _, ok := a.tabs[f.ActiveTab]; ok {
			a.activeTabID = f.ActiveTab
		} else {
			ordered := a.orderedTabIDsLocked()
			if len(ordered) > 0 {
				a.activeTabID = ordered[0]
			}
		}
		a.mu.Unlock()
		for _, tab := range toBuild {
			a.startTabControllerBuild(tab)
		}
		return
	}

	// First launch: create a default Global tab.
	tab := a.createTabEntry("global", globalTabWorkspaceRoot(), "")
	tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
	tab.TopicTitle = "Global"
	a.mu.Lock()
	a.tabs[tab.ID] = tab
	a.tabOrder = append(a.tabOrder, tab.ID)
	a.activeTabID = tab.ID
	a.mu.Unlock()
	a.startTabControllerBuild(tab)
}

func (a *App) createTabEntry(scope, workspaceRoot, topicID string) *WorkspaceTab {
	return a.createTabEntryWithID(scope, workspaceRoot, topicID, newTabID())
}

func (a *App) createTabEntryWithID(scope, workspaceRoot, topicID, id string) *WorkspaceTab {
	return &WorkspaceTab{
		ID:            id,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitleForTab(scope, workspaceRoot, topicID),
		mode:          "normal",
		disabledMCP:   map[string]ServerView{},
	}
}

func (a *App) snapshotAllTabs() {
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
		}
	}
}

// shutdown snapshots all tabs, saves the final window geometry, and closes tabs.
func (a *App) shutdown(context.Context) {
	a.stopTray()
	// Save window geometry synchronously from Go so it's persisted even if the
	// frontend's beforeunload promise hasn't resolved yet.
	a.saveWindowStateSync()

	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
			t.Ctrl.Close()
		}
	}
}

// domReady is called (via OnDomReady) after the webview finishes loading its DOM
// but before the window is shown (StartHidden). It restores the saved window
// position and size, then calls WindowShow so the user never sees the default
// size/position flash.
func (a *App) domReady(_ context.Context) {
	state, ok := loadWindowState()
	if ok {
		// Validate saved position against current screens. Wails v2 doesn't
		// expose per-screen origin (x,y offsets) so we can only do a basic
		// sanity check: ensure the window origin falls within a generous
		// estimate of the screen area. If the user unplugged an external
		// display, negative or out-of-bounds coordinates are caught here.
		valid := state.X >= 0 && state.Y >= 0
		if valid {
			screens, err := runtime.ScreenGetAll(a.ctx)
			if err == nil && len(screens) > 0 {
				maxW, maxH := 0, 0
				for _, sc := range screens {
					if sc.Size.Width > maxW {
						maxW = sc.Size.Width
					}
					if sc.Size.Height > maxH {
						maxH = sc.Size.Height
					}
				}
				if state.X > maxW*2 || state.Y > maxH*2 {
					valid = false
				}
			}
		}
		if valid {
			runtime.WindowSetPosition(a.ctx, state.X, state.Y)
		} else {
			runtime.WindowCenter(a.ctx)
		}
	} else {
		runtime.WindowCenter(a.ctx)
	}

	if ok && state.Maximised {
		runtime.WindowMaximise(a.ctx)
	}

	runtime.WindowShow(a.ctx)
}

// --- bound command surface (frontend → controller) ---
// Each method guards on a nil controller so a pre-startup or failed-build call is
// a no-op, never a panic.

// Submit runs raw user input as a turn; slash commands and @-references are
// resolved by the controller. Output arrives asynchronously on eventChannel.
func (a *App) Submit(input string) {
	a.SubmitToTab("", input)
}

func (a *App) SubmitToTab(tabID, input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		a.runEffortCommandForTab(tabID, trimmed)
		return
	}
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.SubmitDisplay(input, input)
	}
}

// RunShell executes a shell command directly (bypassing the model) and streams
// output as events on eventChannel.
func (a *App) RunShell(command string) {
	a.RunShellForTab("", command)
}

func (a *App) RunShellForTab(tabID, command string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.RunShell(command)
	}
}

// SubmitDisplay runs input as a turn while recording a shorter UI-only display
// string for the saved desktop transcript. The model still receives input.
func (a *App) SubmitDisplay(display, input string) {
	a.SubmitDisplayToTab("", display, input)
}

func (a *App) SubmitDisplayToTab(tabID, display, input string) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	ctrl.SubmitDisplay(display, input)
}

func (a *App) bindControllerDisplayRecorder(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.SetDisplayRecorder(func(content, display string) {
		dir := ctrl.SessionDir()
		if dir == "" {
			dir = config.SessionDir()
		}
		_ = recordSessionDisplay(dir, ctrl.SessionPath(), content, display)
	})
}

// Cancel aborts the in-flight turn.
func (a *App) Cancel() {
	a.CancelTab("")
}

func (a *App) CancelTab(tabID string) {
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Cancel()
	}
}

// Approve answers a pending approval_request by ID: allow runs the call, session
// also remembers the grant for the rest of the session.
func (a *App) Approve(id string, allow, session, persist bool) {
	a.ApproveWithScope(id, allow, session, persist, "")
}

func (a *App) ApproveWithScope(id string, allow, session, persist bool, scope string) {
	a.ApproveTabWithScope("", id, allow, session, persist, scope)
}

func (a *App) ApproveTab(tabID, id string, allow, session, persist bool) {
	a.ApproveTabWithScope(tabID, id, allow, session, persist, "")
}

func (a *App) ApproveTabWithScope(tabID, id string, allow, session, persist bool, scope string) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.ApproveWithScope(id, allow, session, persist, scope)
	}
}

// SetPlanMode toggles read-only plan mode.
func (a *App) SetPlanMode(on bool) {
	if on {
		a.SetModeForTab("", "plan")
		return
	}
	a.SetModeForTab("", "normal")
}

// SetMode applies a composer gating mode ("plan" | "yolo" | anything else =
// normal) in one call, so a turn submitted right after the switch can't race a
// half-applied SetPlanMode/SetBypass pair.
func (a *App) SetMode(mode string) {
	a.SetModeForTab("", mode)
}

func (a *App) SetModeForTab(tabID, mode string) {
	normalized := normalizeTabMode(mode)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.mode = normalized
	ctrl := tab.Ctrl
	tabIDForSave := tab.ID
	a.mu.Unlock()
	applyTabModeToController(ctrl, normalized)
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

func applyTabModeToController(ctrl *control.Controller, mode string) {
	if ctrl == nil {
		return
	}
	switch normalizeTabMode(mode) {
	case "plan":
		ctrl.SetMode(true, false)
	case "yolo":
		ctrl.SetMode(false, true)
	default:
		ctrl.SetMode(false, false)
	}
}

// QuestionAnswer is the frontend's reply to one question in an ask_request.
type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
}

// AnswerQuestion resolves a pending ask_request (the `ask` tool) by ID with the
// user's selections per question.
func (a *App) AnswerQuestion(id string, answers []QuestionAnswer) {
	a.AnswerQuestionForTab("", id, answers)
}

func (a *App) AnswerQuestionForTab(tabID, id string, answers []QuestionAnswer) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	out := make([]event.AskAnswer, len(answers))
	for i, an := range answers {
		out[i] = event.AskAnswer{QuestionID: an.QuestionID, Selected: an.Selected}
	}
	ctrl.AnswerQuestion(id, out)
}

// Compact runs one compaction pass on demand.
// Compact runs a plain compaction pass (the "compact now" button). Focus-guided
// compaction goes through Submit("/compact <focus>") instead.
func (a *App) Compact() error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.Compact(a.ctx, "")
}

// NewSession snapshots the current conversation and rotates to a fresh one.
func (a *App) NewSession() error {
	a.mu.RLock()
	tab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	if err := ctrl.NewSession(); err != nil {
		return err
	}
	a.persistTabSessionPath(tab, ctrl.SessionPath())
	return nil
}

// CheckpointMeta summarises one rewind point (a user turn) for the desktop.
type CheckpointMeta struct {
	Turn            int      `json:"turn"`
	Prompt          string   `json:"prompt"`
	Files           []string `json:"files"` // paths changed during the turn
	Time            int64    `json:"time"`  // unix milliseconds
	CanCode         bool     `json:"canCode"`
	CanConversation bool     `json:"canConversation"`
}

// Checkpoints lists the session's rewind points, oldest first, for the rewind UI.
func (a *App) Checkpoints() []CheckpointMeta {
	return a.CheckpointsForTab("")
}

func (a *App) CheckpointsForTab(tabID string) []CheckpointMeta {
	a.mu.RLock()
	var ctrl *control.Controller
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return []CheckpointMeta{}
	}
	metas := ctrl.Checkpoints()
	out := make([]CheckpointMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, CheckpointMeta{
			Turn:            m.Turn,
			Prompt:          m.Prompt,
			Files:           m.Paths,
			Time:            m.Time.UnixMilli(),
			CanCode:         len(m.Paths) > 0,
			CanConversation: ctrl.CheckpointHasBoundary(m.Turn),
		})
	}
	// RestoreCode(turn) reverts every file touched in this turn or any later one, so
	// a turn can rewind code even when it changed no files itself — as long as a
	// later turn did. Propagate CanCode backwards over the oldest-first list.
	hasCodeAfter := false
	for i := len(out) - 1; i >= 0; i-- {
		if len(out[i].Files) > 0 {
			hasCodeAfter = true
		}
		out[i].CanCode = hasCodeAfter
	}
	return out
}

// Rewind restores the session to the start of turn. scope is "code",
// "conversation", or "both" (anything else is treated as "both"). The frontend
// re-reads History after this resolves.
func (a *App) Rewind(turn int, scope string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	s := control.RewindBoth
	switch scope {
	case "code":
		s = control.RewindCode
	case "conversation":
		s = control.RewindConversation
	}
	return ctrl.Rewind(turn, s)
}

// Fork branches the conversation at the start of turn into a new session tab
// (preserving the current tab), keeping code intact, and switches to the new tab.
func (a *App) Fork(turn int) (TabMeta, error) {
	a.mu.RLock()
	sourceTab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	if sourceTab == nil || ctrl == nil {
		a.mu.RUnlock()
		return TabMeta{}, nil
	}
	scope := sourceTab.Scope
	workspaceRoot := sourceTab.WorkspaceRoot
	sourceTitle := sourceTab.TopicTitle
	model := sourceTab.model
	effort := cloneStringPtr(sourceTab.effort)
	mode := currentTabMode(sourceTab)
	disabledMCP := cloneServerViewMap(sourceTab.disabledMCP)
	mcpOrder := append([]string(nil), sourceTab.mcpOrder...)
	a.mu.RUnlock()

	newPath, err := ctrl.ForkSession(turn, "")
	if err != nil {
		return TabMeta{}, err
	}
	topicID := newTopicID()
	topicTitle := forkTopicTitle(sourceTitle)
	titleRoot := workspaceRoot
	if scope == "global" {
		titleRoot = ""
	}
	if err := setTopicTitle(titleRoot, topicID, topicTitle); err != nil {
		return TabMeta{}, err
	}
	m, _ := agent.EnsureBranchMeta(newPath)
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = topicID
	m.TopicTitle = topicTitle
	if err := agent.SaveBranchMeta(newPath, m); err != nil {
		return TabMeta{}, err
	}

	a.mu.Lock()
	tabID := a.newUniqueTabIDLocked()
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   newPath,
		model:         model,
		effort:        effort,
		mode:          mode,
		disabledMCP:   disabledMCP,
		mcpOrder:      mcpOrder,
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}
	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	meta := a.tabMeta(tab, true)
	a.mu.Unlock()

	a.emitProjectTreeChanged()
	a.startTabControllerBuild(tab)
	return meta, nil
}

// SummarizeFrom / SummarizeUpTo compress the conversation from / up to the start
// of turn into one summary (Claude Code's "summarize from/up to here"), keeping
// code intact. The frontend re-reads History after this resolves.
func (a *App) SummarizeFrom(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeFrom(a.ctx, turn)
}

func (a *App) SummarizeUpTo(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeUpTo(a.ctx, turn)
}

// SessionMeta summarises one saved session for the history panel.
type SessionMeta struct {
	Path           string `json:"path"`
	Preview        string `json:"preview"`         // first user message
	Title          string `json:"title,omitempty"` // user-chosen name, when set (overrides preview)
	Turns          int    `json:"turns"`
	CreatedAt      int64  `json:"createdAt"`      // unix milliseconds
	LastActivityAt int64  `json:"lastActivityAt"` // unix milliseconds
	ModTime        int64  `json:"modTime"`        // compatibility alias for lastActivityAt
	DeletedAt      int64  `json:"deletedAt,omitempty"`
	Current        bool   `json:"current"`
	Open           bool   `json:"open"`
	Scope          string `json:"scope,omitempty"`
	WorkspaceRoot  string `json:"workspaceRoot,omitempty"`
	TopicID        string `json:"topicId,omitempty"`
	TopicTitle     string `json:"topicTitle,omitempty"`
}

type WorkspaceMeta struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// ListSessions returns the saved sessions newest-first for the history panel,
// marking the one the current conversation is writing to and attaching any
// user-chosen titles.
func (a *App) ListSessions() []SessionMeta {
	dir := config.SessionDir()
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return []SessionMeta{}
	}
	titles := loadSessionTitles(dir)
	open := a.openSessionPaths(dir)
	active := a.activeSessionPath(dir)
	out := make([]SessionMeta, 0, len(infos))
	for _, s := range infos {
		_, isOpen := open[s.Path]
		out = append(out, sessionMetaFromInfo(s, titles[filepath.Base(s.Path)], s.Path == active, isOpen, 0))
	}
	return out
}

// ListTrashedSessions returns sessions that were moved to the local trash,
// newest-deleted first. These can be previewed, restored, or permanently purged.
func (a *App) ListTrashedSessions() []SessionMeta {
	dir := config.SessionDir()
	paths, err := listTrashedSessionFiles(dir)
	if err != nil {
		return []SessionMeta{}
	}
	titles := loadSessionTitles(dir)
	out := make([]SessionMeta, 0, len(paths))
	for _, path := range paths {
		infos, err := agent.ListSessions(filepath.Dir(path))
		if err != nil || len(infos) == 0 {
			continue
		}
		deletedAt := trashedSessionDeletedAt(path)
		out = append(out, sessionMetaFromInfo(infos[0], titles[filepath.Base(path)], false, false, deletedAt))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeletedAt == out[j].DeletedAt {
			return out[i].LastActivityAt > out[j].LastActivityAt
		}
		return out[i].DeletedAt > out[j].DeletedAt
	})
	return out
}

func sessionMetaFromInfo(s agent.SessionInfo, title string, current, open bool, deletedAt int64) SessionMeta {
	return SessionMeta{
		Path:           s.Path,
		Preview:        s.Preview,
		Title:          title,
		Turns:          s.Turns,
		CreatedAt:      s.CreatedAt.UnixMilli(),
		LastActivityAt: s.LastActivityAt.UnixMilli(),
		ModTime:        s.LastActivityAt.UnixMilli(),
		DeletedAt:      deletedAt,
		Current:        current,
		Open:           open,
		Scope:          s.Scope,
		WorkspaceRoot:  s.WorkspaceRoot,
		TopicID:        s.TopicID,
		TopicTitle:     s.TopicTitle,
	}
}

// DeleteSession moves a saved session to the local trash. It refuses any open
// session because tab auto-save would recreate or append to the file later.
func (a *App) DeleteSession(path string) error {
	dir := config.SessionDir()
	sessionPath, key, err := validateSessionPath(dir, path)
	if err != nil {
		return err
	}
	if _, ok := a.openSessionPaths(dir)[sessionPath]; ok {
		return errActiveSession
	}
	if err := trashSessionArtifacts(dir, sessionPath, key); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) openSessionPaths(dir string) map[string]struct{} {
	a.mu.RLock()
	paths := make([]string, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != nil {
			paths = append(paths, tab.currentSessionPath())
		}
	}
	a.mu.RUnlock()

	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		currentPath, _, err := validateSessionPath(dir, path)
		if err == nil {
			out[currentPath] = struct{}{}
		}
	}
	return out
}

func (a *App) activeSessionPath(dir string) string {
	a.mu.RLock()
	var path string
	if tab := a.tabs[a.activeTabID]; tab != nil {
		path = tab.currentSessionPath()
	}
	a.mu.RUnlock()
	currentPath, _, err := validateSessionPath(dir, path)
	if err != nil {
		return ""
	}
	return currentPath
}

// RestoreSession moves a trashed session back into the saved-session list.
func (a *App) RestoreSession(path string) error {
	dir := config.SessionDir()
	_, key, _, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	if err := restoreTrashedSessionFile(dir, path); err != nil {
		return err
	}
	if err := restoreSessionTopicIndex(dir, filepath.Join(dir, key)); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// PurgeTrashedSession permanently removes a trashed session and its title/display
// sidecars.
func (a *App) PurgeTrashedSession(path string) error {
	return purgeTrashedSessionFile(config.SessionDir(), path)
}

// RenameSession sets a custom display name for a session (empty clears it back to
// the preview). It only affects the history panel; the file on disk is unchanged.
func (a *App) RenameSession(path, title string) error {
	return setSessionTitle(config.SessionDir(), path, title)
}

// ResumeSession snapshots the current conversation, then loads the session at
// path and continues it on the active tab. The model and working folder are
// unchanged; only the transcript is swapped. Returns the resumed messages for
// the frontend to render.
func (a *App) ResumeSession(path string) ([]HistoryMessage, error) {
	return a.ResumeSessionForTab("", path)
}

// ResumeSessionForTab is the tab-scoped form of ResumeSession. History rows
// carry scope/workspace/topic metadata, so callers that opened or selected a
// matching tab should resume on that exact controller instead of whichever tab is
// active by the time the async call reaches the backend.
func (a *App) ResumeSessionForTab(tabID, path string) ([]HistoryMessage, error) {
	tab := a.tabByID(tabID)
	if tab == nil || tab.Ctrl == nil {
		return []HistoryMessage{}, fmt.Errorf("tab is not ready")
	}
	ctrl := tab.Ctrl
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	_ = ctrl.Snapshot() // persist the current session before switching away
	ctrl.Resume(loaded, path)
	a.rememberTabSessionPath(tab, path)
	return a.HistoryForTab(tabID), nil
}

// PreviewSession reads a saved session for display only. It does not snapshot or
// swap the active controller, so the history drawer can call it while a turn runs.
func (a *App) PreviewSession(path string) ([]HistoryMessage, error) {
	return previewSessionMessages(config.SessionDir(), path)
}

// PickWorkspace opens a folder chooser and, on a pick, opens a new project tab
// scoped to that folder. Returns the chosen path ("" if cancelled).
func (a *App) PickWorkspace() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose working folder",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return a.SwitchWorkspace(dir)
}

func dialogDefaultDirectory(preferred string) string {
	if dir := nearestExistingDirectory(preferred); dir != "" {
		return dir
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := nearestExistingDirectory(cwd); dir != "" {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dir := nearestExistingDirectory(home); dir != "" {
			return dir
		}
	}
	return ""
}

func nearestExistingDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	for {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return path
			}
			path = filepath.Dir(path)
			continue
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func (a *App) ListWorkspaces() []WorkspaceMeta {
	migrateLegacyWorkspacesIntoProjects()
	activeRoot := ""
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		activeRoot = normalizeProjectRoot(tab.WorkspaceRoot)
	}
	a.mu.RUnlock()
	if activeRoot == "" {
		activeRoot = normalizeProjectRoot(cur)
	}
	projects := loadProjectsFile().Projects
	out := make([]WorkspaceMeta, 0, len(projects))
	for _, project := range projects {
		out = append(out, WorkspaceMeta{
			Path:    project.Root,
			Name:    projectDisplayName(project),
			Current: activeRoot != "" && project.Root == activeRoot,
		})
	}
	return out
}

func (a *App) RemoveWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("workspace path is required")
	}
	dir = normalizeProjectRoot(dir)
	forgetWorkspace(dir)
	if err := removeProject(dir); err != nil {
		return err
	}
	// If the removed workspace was the active one, clear the pointer
	// so we don't leave a stale reference to a deleted project.
	if loadWorkspace() == dir {
		if remaining := loadProjectsFile(); len(remaining.Projects) > 0 {
			// Fall back to the first remaining project
			saveWorkspace(remaining.Projects[0].Root)
		} else {
			// No projects left; clear the active pointer entirely
			clearWorkspace()
		}
	}
	a.emitProjectTreeChanged()
	return nil
}

func migrateLegacyWorkspacesIntoProjects() {
	legacy := loadWorkspaces()
	if len(legacy) == 0 {
		return
	}
	f := loadProjectsFile()
	seen := make(map[string]bool, len(f.Projects)+len(legacy))
	for _, p := range f.Projects {
		seen[p.Root] = true
	}
	changed := false
	for _, path := range legacy {
		root := normalizeProjectRoot(path)
		if root == "" || seen[root] {
			continue
		}
		f.Projects = append(f.Projects, desktopProject{Root: root})
		seen[root] = true
		changed = true
	}
	if changed {
		_ = saveProjectsFile(f)
	}
}

func workspaceName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return path
	}
	return name
}

func (a *App) SwitchWorkspace(dir string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = home
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	saveWorkspace(dir)

	// Open a registered topic so the new workspace appears in the project tree
	// immediately instead of only existing as an in-memory tab.
	topic, err := a.CreateTopic("project", dir, "")
	if err != nil {
		return "", err
	}
	meta, err := a.OpenProjectTab(dir, topic.ID)
	if err != nil {
		return "", err
	}
	return meta.WorkspaceRoot, nil
}

// HistoryMessage is one prior turn, for the frontend to repopulate its transcript
// after a reload.
type HistoryMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"`
	Level      string            `json:"level,omitempty"`
	ToolCalls  []HistoryToolCall `json:"toolCalls,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	Pending    bool              `json:"pending,omitempty"`
	Trigger    string            `json:"trigger,omitempty"`
	Messages   int               `json:"messages,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Archive    string            `json:"archive,omitempty"`
}

type HistoryToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// History returns the session's message log.
func (a *App) History() []HistoryMessage {
	return a.HistoryForTab("")
}

func (a *App) HistoryForTab(tabID string) []HistoryMessage {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return []HistoryMessage{}
	}
	msgs := ctrl.History()
	return historyMessages(msgs, sessionDisplayResolver(config.SessionDir(), ctrl.SessionPath()))
}

func historyMessages(msgs []provider.Message, resolveUserContent func(string) string) []HistoryMessage {
	out := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		content := m.Content
		if m.Role == provider.RoleUser {
			content = resolveUserContent(m.Content)
			if control.IsSyntheticUserMessage(content) {
				continue
			}
		}
		reasoning := ""
		if m.Role == provider.RoleAssistant {
			reasoning = m.ReasoningContent
		}
		hm := HistoryMessage{Role: string(m.Role), Content: content, Reasoning: reasoning}
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			hm.ToolCalls = make([]HistoryToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				hm.ToolCalls[i] = HistoryToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
			}
		}
		if m.Role == provider.RoleTool {
			hm.ToolCallID = m.ToolCallID
			hm.ToolName = m.Name
		}
		out = append(out, hm)
	}
	return out
}

func previewSessionMessages(sessionDir, path string) ([]HistoryMessage, error) {
	if out, ok, err := previewEventSessionMessages(path); ok || err != nil {
		return out, err
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	return historyMessages(loaded.Snapshot(), sessionDisplayResolver(sessionDir, path)), nil
}

type previewEventRecord struct {
	Kind             string             `json:"kind"`
	Type             string             `json:"type"`
	Role             string             `json:"role"`
	Text             string             `json:"text"`
	Content          string             `json:"content"`
	Reasoning        string             `json:"reasoning"`
	ReasoningContent string             `json:"reasoningContent"`
	Level            string             `json:"level"`
	ToolCalls        []previewToolCall  `json:"toolCalls"`
	CallID           string             `json:"callId"`
	ToolCallID       string             `json:"toolCallId"`
	ToolName         string             `json:"toolName"`
	Name             string             `json:"name"`
	Output           string             `json:"output"`
	Compaction       *previewCompaction `json:"compaction"`
	Trigger          string             `json:"trigger"`
	Messages         int                `json:"messages"`
	Summary          string             `json:"summary"`
	Archive          string             `json:"archive"`
}

type previewToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type previewCompaction struct {
	Trigger  string `json:"trigger"`
	Messages int    `json:"messages"`
	Summary  string `json:"summary"`
	Archive  string `json:"archive"`
}

func previewEventSessionMessages(path string) ([]HistoryMessage, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	out := []HistoryMessage{}
	toolName := map[string]string{}
	sawEvent := false
	for {
		var rec previewEventRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if sawEvent {
				return out, true, nil
			}
			return nil, false, nil
		}
		eventName := strings.TrimSpace(rec.Kind)
		if eventName == "" {
			eventName = strings.TrimSpace(rec.Type)
		}
		if eventName == "" {
			continue
		}
		sawEvent = true
		switch eventName {
		case "user.message":
			if rec.Text != "" {
				out = append(out, HistoryMessage{Role: "user", Content: rec.Text})
			}
		case "model.final":
			hm := HistoryMessage{Role: "assistant", Content: rec.Content, Reasoning: firstNonEmpty(rec.Reasoning, rec.ReasoningContent)}
			for _, tc := range rec.ToolCalls {
				id := tc.ID
				name := firstNonEmpty(tc.Name, tc.Function.Name)
				args := firstNonEmpty(tc.Arguments, tc.Function.Arguments)
				hm.ToolCalls = append(hm.ToolCalls, HistoryToolCall{ID: id, Name: name, Arguments: args})
				if id != "" {
					toolName[id] = name
				}
			}
			out = append(out, hm)
		case "tool.result":
			callID := firstNonEmpty(rec.CallID, rec.ToolCallID)
			out = append(out, HistoryMessage{
				Role:       "tool",
				ToolCallID: callID,
				ToolName:   firstNonEmpty(rec.ToolName, rec.Name, toolName[callID]),
				Content:    firstNonEmpty(rec.Output, rec.Content),
			})
		case "phase":
			out = append(out, HistoryMessage{Role: "phase", Content: firstNonEmpty(rec.Text, rec.Content)})
		case "notice":
			level := rec.Level
			if level != "warn" {
				level = "info"
			}
			out = append(out, HistoryMessage{Role: "notice", Level: level, Content: firstNonEmpty(rec.Text, rec.Content)})
		case "compaction_started":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{Role: "compaction", Pending: true, Trigger: c.Trigger})
		case "compaction_done":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{
				Role:     "compaction",
				Trigger:  c.Trigger,
				Messages: c.Messages,
				Summary:  c.Summary,
				Archive:  c.Archive,
			})
		}
	}
	return out, sawEvent, nil
}

func (r previewEventRecord) compactionPayload() previewCompaction {
	if r.Compaction != nil {
		return *r.Compaction
	}
	return previewCompaction{Trigger: r.Trigger, Messages: r.Messages, Summary: r.Summary, Archive: r.Archive}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ContextInfo is the prompt-vs-window gauge payload. Both zero means no data yet.
type ContextInfo struct {
	Used         int     `json:"used"`
	Window       int     `json:"window"`
	CompactRatio float64 `json:"compactRatio,omitempty"`
}

// ContextUsage returns the latest context-window gauge numbers.
func (a *App) ContextUsage() ContextInfo {
	return a.ContextUsageForTab("")
}

func (a *App) ContextUsageForTab(tabID string) ContextInfo {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return ContextInfo{}
	}
	used, window := ctrl.ContextSnapshot()
	return ContextInfo{Used: used, Window: window, CompactRatio: ctrl.CompactRatio()}
}

// BalanceInfo is the wallet-balance readout for the status bar. Available is true
// only when a balance was fetched; Display is the formatted amount (e.g. "¥110.00")
// and is "" when the active provider declares no balance_url — the frontend then
// omits the readout. Err carries a fetch failure for an optional tooltip.
type BalanceInfo struct {
	Available bool   `json:"available"`
	Display   string `json:"display"`
	Err       string `json:"err,omitempty"`
}

// Balance queries the active provider's wallet balance (a network call). It
// returns an empty (unavailable) readout when no provider balance_url is set, the
// controller is down, or the fetch fails — so the status bar simply shows nothing
// rather than an error.
func (a *App) Balance() BalanceInfo {
	return a.BalanceForTab("")
}

func (a *App) BalanceForTab(tabID string) BalanceInfo {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return BalanceInfo{}
	}
	b, err := ctrl.Balance(a.ctx)
	if err != nil {
		return BalanceInfo{Err: err.Error()}
	}
	if b == nil {
		return BalanceInfo{} // provider declares no balance endpoint
	}
	return BalanceInfo{Available: true, Display: b.Display()}
}

// JobView is one running background job (bash/task started with
// run_in_background) for the status-bar indicator.
type JobView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

// Jobs returns the still-running background jobs for the status bar. It refreshes
// on demand (mount, turn end, and on each notice the frontend receives).
func (a *App) Jobs() []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID("")
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) JobsForTab(tabID string) []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID(tabID)
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) jobsForCtrl(ctrl *control.Controller, out []JobView) []JobView {
	if ctrl == nil {
		return out
	}
	for _, v := range ctrl.Jobs() {
		out = append(out, JobView{ID: v.ID, Kind: v.Kind, Label: v.Label, Status: v.Status, StartedAt: v.StartedAt})
	}
	return out
}

// Meta describes the session for the frontend's header and status line.
type Meta struct {
	Label        string `json:"label"`
	Ready        bool   `json:"ready"`
	StartupErr   string `json:"startupErr,omitempty"`
	EventChannel string `json:"eventChannel"`
	Cwd          string `json:"cwd"`
	Bypass       bool   `json:"bypass"` // YOLO mode on (auto-approve every tool call)
}

// Meta reports the model label, readiness, any startup error, the working
// directory (for the status line), and the runtime event channel the frontend
// subscribes to.
func (a *App) Meta() Meta {
	return a.MetaForTab("")
}

func (a *App) MetaForTab(tabID string) Meta {
	tab := a.tabByID(tabID)
	if tab == nil {
		return Meta{EventChannel: eventChannel}
	}
	cwd := tab.WorkspaceRoot
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return Meta{
		Label:        tab.Label,
		Ready:        tab.Ready,
		StartupErr:   tab.StartupErr,
		EventChannel: eventChannel,
		Cwd:          cwd,
		Bypass:       tab.Ctrl != nil && tab.Ctrl.Bypass(),
	}
}

// SetBypass toggles YOLO mode for the session: auto-approve every tool call
// (writers and bash run without asking). Deny rules still apply. Runtime-only —
// not written to config, so it resets on relaunch.
func (a *App) SetBypass(on bool) {
	if on {
		a.SetModeForTab("", "yolo")
		return
	}
	a.SetModeForTab("", "normal")
}

// CommandInfo describes one available slash command for the composer's "/" menu.
type CommandInfo struct {
	Name        string `json:"name"` // without the leading slash
	Description string `json:"description"`
	Hint        string `json:"hint,omitempty"` // argument hint, if any
	Kind        string `json:"kind"`           // "builtin" | "custom" | "mcp"
}

// Commands lists the slash commands available this session — built-in actions,
// custom commands (.reasonix/commands), and MCP prompts — for the composer's "/"
// autocomplete menu.
func (a *App) Commands() []CommandInfo {
	out := []CommandInfo{
		{Name: "new", Description: i18n.M.CmdNew, Kind: "builtin"},
		{Name: "compact", Description: i18n.M.CmdCompact, Kind: "builtin"},
		{Name: "model", Description: i18n.M.CmdModel, Kind: "builtin"},
		{Name: "effort", Description: i18n.M.CmdEffort, Kind: "builtin"},
		{Name: "memory", Description: i18n.M.CmdMemory, Kind: "builtin"},
		{Name: "remember", Description: i18n.M.CmdRemember, Kind: "builtin"},
		{Name: "mcp", Description: i18n.M.CmdMcp, Kind: "builtin"},
		{Name: "hooks", Description: i18n.M.CmdHooks, Kind: "builtin"},
		{Name: "theme", Description: i18n.M.CmdTheme, Kind: "builtin"},
		{Name: "skill", Description: i18n.M.CmdSkill, Kind: "builtin"},
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return out
	}
	// Skills are invocable as /<name> (the model runs inline ones; subagent ones
	// run isolated). Listing them here is what surfaces /init, /explore, … in the
	// composer's slash menu; selecting one submits "/<name>", which the controller
	// resolves via RunSkill.
	for _, s := range ctrl.Skills() {
		out = append(out, CommandInfo{Name: s.Name, Description: s.Description, Kind: "skill"})
	}
	for _, c := range ctrl.Commands() {
		out = append(out, CommandInfo{Name: c.Name, Description: c.Description, Hint: c.ArgHint, Kind: "custom"})
	}
	if h := ctrl.Host(); h != nil {
		for _, p := range h.Prompts() {
			out = append(out, CommandInfo{Name: p.Name, Description: p.Description, Kind: "mcp"})
		}
	}
	return out
}

// SlashArgItem is one sub-command / argument suggestion for the composer's slash
// menu (the part after the command word). Mirrors the CLI's arg completion via
// the shared control.SlashArgItems, so desktop and CLI offer the same hints.
type SlashArgItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint"`
	Descend bool   `json:"descend"`
}

// SlashArgsResult carries the suggestions plus the byte offset in the input where
// the current token begins, so the composer replaces just that token.
type SlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from"`
}

// SlashArgs completes the arguments of a management slash command (/mcp, /model,
// /skill, /hooks) for the composer — the same logic the chat TUI uses. Empty
// Items means the input has no structured arguments to complete.
func (a *App) SlashArgs(input string) SlashArgsResult {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	model := ""
	if tab := a.activeTabLocked(); tab != nil {
		model = tab.model
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return SlashArgsResult{Items: []SlashArgItem{}}
	}
	data := control.ArgData{
		Skills:          ctrl.Skills(),
		DisabledSkills:  ctrl.DisabledSkills(),
		ConfiguredMCP:   ctrl.ConfiguredMCPNames(),
		DisconnectedMCP: ctrl.DisconnectedMCPNames(),
		CurrentModel:    model,
	}
	for _, m := range a.Models() {
		data.ModelRefs = append(data.ModelRefs, m.Ref)
	}
	if h := ctrl.Host(); h != nil {
		data.ServerNames = h.ServerNames()
	}
	items, from := control.SlashArgItems(input, data)
	// Non-nil so it serializes as a JSON array, never null — the frontend filters
	// over it directly.
	out := SlashArgsResult{Items: []SlashArgItem{}, From: from}
	for _, it := range items {
		out.Items = append(out.Items, SlashArgItem{Label: it.Label, Insert: it.Insert, Hint: it.Hint, Descend: it.Descend})
	}
	return out
}

// CapabilitiesView is the MCP & Skills drawer's data: connected/failed MCP
// servers and the discoverable skills, the GUI counterpart to `/mcp` + `/skill`.
type CapabilitiesView struct {
	Servers    []ServerView    `json:"servers"`
	Skills     []SkillView     `json:"skills"`
	SkillRoots []SkillRootView `json:"skillRoots"`
}

// ServerView is one MCP server for the drawer. Status is "connected" (with
// tool/prompt/resource counts), "deferred" (lazy/on-demand startup enabled),
// "failed" (with the connection error), "initializing" (background startup in
// progress), or "disabled".
type ServerView struct {
	Name           string     `json:"name"`
	Transport      string     `json:"transport"`
	Status         string     `json:"status"`
	BuiltIn        bool       `json:"builtIn,omitempty"`
	Configured     bool       `json:"configured,omitempty"`
	AutoStart      bool       `json:"autoStart"`
	Tier           string     `json:"tier,omitempty"`
	Command        string     `json:"command,omitempty"`
	Args           []string   `json:"args,omitempty"`
	URL            string     `json:"url,omitempty"`
	EnvKeys        []string   `json:"envKeys,omitempty"`
	Tools          int        `json:"tools"`
	Prompts        int        `json:"prompts"`
	Resources      int        `json:"resources"`
	Error          string     `json:"error,omitempty"`
	ToolList       []ToolView `json:"toolList,omitempty"`
	AuthStatus     string     `json:"authStatus,omitempty"`
	AuthURL        string     `json:"authUrl,omitempty"`
	AuthConfigured bool       `json:"authConfigured,omitempty"`
}

type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillView is one discoverable skill for the drawer.
type SkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
	Enabled     bool   `json:"enabled"`
}

type SkillRootSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
}

// SkillRootView is one skill discovery root for the drawer's Sources section.
type SkillRootView struct {
	Dir        string               `json:"dir"`
	Scope      string               `json:"scope"`
	Priority   int                  `json:"priority"`
	Status     string               `json:"status"`
	Configured bool                 `json:"configured"`
	Removable  bool                 `json:"removable"`
	Skills     int                  `json:"skills"`
	SkillItems []SkillRootSkillView `json:"skillItems,omitempty"`
	Warning    string               `json:"warning,omitempty"`
}

// Capabilities projects the session's MCP servers (connected + failed) and skills
// for the MCP & Skills drawer. Non-nil slices so the frontend can map over them.
func (a *App) Capabilities() CapabilitiesView {
	out := CapabilitiesView{Servers: []ServerView{}, Skills: []SkillView{}, SkillRoots: []SkillRootView{}}
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil {
		return out
	}
	ctrl := tab.Ctrl
	disabled := make(map[string]ServerView, len(tab.disabledMCP))
	for name, s := range tab.disabledMCP {
		disabled[name] = s
	}
	order := append([]string(nil), tab.mcpOrder...)
	if ctrl == nil {
		return out
	}
	seen := map[string]bool{}
	connected := map[string]bool{}
	retainedDisabled := map[string]ServerView{}
	var loadedCfg *config.Config
	configured := map[string]config.PluginEntry{}
	var configuredEntries []config.PluginEntry
	if cfg, err := config.LoadForRoot(tab.WorkspaceRoot); err == nil {
		loadedCfg = cfg
		configuredEntries = append(configuredEntries, cfg.Plugins...)
		for _, p := range configuredEntries {
			configured[p.Name] = p
		}
	}
	if h := ctrl.Host(); h != nil {
		for _, s := range h.Servers() {
			seen[s.Name] = true
			connected[s.Name] = true
			view := ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				BuiltIn: s.Name == "codegraph",
				Tools:   s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}
			if p, ok := configured[s.Name]; ok {
				view = withPluginConfig(view, p)
			} else if s.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
		for _, f := range h.Failures() {
			seen[f.Name] = true
			view := ServerView{
				Name: f.Name, Transport: f.Transport, Status: "failed", BuiltIn: f.Name == "codegraph", Error: f.Error,
			}
			if p, ok := configured[f.Name]; ok {
				view = withPluginConfig(view, p)
			} else if f.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
	}
	// Configured servers that are neither connected nor failed are either lazy
	// (deferred), background/eager (initializing), or toggled off this session.
	if len(configuredEntries) > 0 || loadedCfg != nil {
		for _, p := range configuredEntries {
			if seen[p.Name] {
				continue
			}
			if s, ok := disabled[p.Name]; ok {
				s.Status = "disabled"
				s = withPluginConfig(s, p)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled[p.Name] = s
				seen[p.Name] = true
				delete(disabled, p.Name)
				continue
			}
			status := "disabled"
			if p.ShouldAutoStart() {
				switch p.ResolvedTier() {
				case "background", "eager":
					status = "initializing"
				default:
					status = "deferred"
				}
			}
			out.Servers = append(out.Servers, withPluginConfig(ServerView{Name: p.Name, Status: status}, p))
			seen[p.Name] = true
		}
		if loadedCfg != nil && !seen["codegraph"] {
			status := "disabled"
			if loadedCfg.Codegraph.Enabled {
				status = "initializing"
			}
			if s, ok := disabled["codegraph"]; ok {
				s.Status = "disabled"
				s.Transport = "stdio"
				s.BuiltIn = true
				s = withCodegraphConfig(s, loadedCfg.Codegraph)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled["codegraph"] = s
				delete(disabled, "codegraph")
			} else {
				out.Servers = append(out.Servers, withCodegraphConfig(ServerView{Name: "codegraph", Status: status}, loadedCfg.Codegraph))
			}
			seen["codegraph"] = true
		}
	}
	out.Servers = orderServerViews(out.Servers, order)

	a.mu.Lock()
	for name := range connected {
		delete(retainedDisabled, name)
	}
	tab.disabledMCP = retainedDisabled
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, out.Servers)
	a.mu.Unlock()

	for _, s := range ctrl.AllSkills() {
		out.Skills = append(out.Skills, SkillView{
			Name: s.Name, Description: s.Description,
			Scope: string(s.Scope), RunAs: string(s.RunAs),
			Enabled: ctrl.SkillEnabled(s.Name),
		})
	}
	out.SkillRoots = skillRootsView()
	return out
}

func withPluginConfig(v ServerView, p config.PluginEntry) ServerView {
	tt := p.Type
	if tt == "" {
		tt = "stdio"
	}
	v.Transport = tt
	v.Configured = true
	v.AutoStart = p.ShouldAutoStart()
	v.Tier = p.ResolvedTier()
	v.Command = p.Command
	v.Args = append([]string(nil), p.Args...)
	v.URL = p.URL
	v.AuthConfigured = mcpdiag.HasAuthConfig(p.Headers, p.Env, p.URL)
	if len(p.Env) > 0 {
		v.EnvKeys = make([]string, 0, len(p.Env))
		for k := range p.Env {
			v.EnvKeys = append(v.EnvKeys, k)
		}
		sort.Strings(v.EnvKeys)
	}
	auth := mcpdiag.DiagnoseAuth(v.Transport, v.Status, v.Error, v.URL, v.AuthConfigured)
	v.AuthStatus = auth.Status
	v.AuthURL = auth.URL
	return v
}

func withCodegraphConfig(v ServerView, c config.CodegraphConfig) ServerView {
	v.Name = "codegraph"
	v.Transport = "stdio"
	v.BuiltIn = true
	v.Configured = true
	v.AutoStart = c.ShouldAutoStart()
	v.Tier = c.ResolvedTier()
	v.AuthStatus = mcpdiag.AuthNone
	return v
}

func skillRootsView() []SkillRootView {
	cwd, _ := os.Getwd()
	cfg, _ := config.Load()
	userCfg := config.LoadForEdit(config.UserConfigPath())
	var custom []string
	var excluded []string
	maxDepth := 3
	if cfg != nil {
		custom = cfg.SkillCustomPaths()
		excluded = cfg.SkillExcludedPaths()
		maxDepth = cfg.SkillMaxDepth()
	}
	st := skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom, ExcludedPaths: excluded, MaxDepth: maxDepth, DisableBuiltins: true, Stderr: io.Discard})
	counts := map[string]int{}
	skillItems := map[string][]SkillRootSkillView{}
	roots := st.Roots()
	for _, sk := range st.List() {
		root := skillDisplayRoot(sk, roots)
		counts[root]++
		skillItems[root] = append(skillItems[root], SkillRootSkillView{
			Name:        sk.Name,
			Description: sk.Description,
			Scope:       string(sk.Scope),
			RunAs:       string(sk.RunAs),
		})
	}
	for root := range skillItems {
		sort.Slice(skillItems[root], func(i, j int) bool {
			return skillItems[root][i].Name < skillItems[root][j].Name
		})
	}
	userConfigured := map[string]bool{}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			userConfigured[config.CanonicalSkillPath(p)] = true
		}
	}
	out := []SkillRootView{}
	for _, r := range roots {
		dir := config.CanonicalSkillPath(r.Dir)
		view := SkillRootView{
			Dir:        r.Dir,
			Scope:      string(r.Scope),
			Priority:   r.Priority + 1,
			Status:     string(r.Status),
			Configured: r.Scope == skill.ScopeCustom && userConfigured[dir],
			Removable:  true,
			Skills:     counts[dir],
			SkillItems: skillItems[dir],
		}
		out = append(out, view)
	}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			if rootActive(out, p) {
				continue
			}
			out = append(out, SkillRootView{
				Dir:        p,
				Scope:      string(skill.ScopeCustom),
				Status:     "inactive",
				Configured: true,
				Removable:  true,
				Warning:    "configured in user config but not active in this workspace; project [skills].paths may override it",
			})
		}
	}
	return out
}

func rootActive(roots []SkillRootView, path string) bool {
	want := config.CanonicalSkillPath(path)
	for _, r := range roots {
		if r.Scope == string(skill.ScopeCustom) && config.CanonicalSkillPath(r.Dir) == want {
			return true
		}
	}
	return false
}

// PickSkillFolder opens a directory picker for adding custom skill roots. It only
// returns a path; AddSkillPath performs normalization and writes config.
func (a *App) PickSkillFolder() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose skills folder",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return normalizeSkillPath(dir), nil
}

// AddSkillPath adds a custom skill root to the user config and rebuilds the
// controller so the skills index and slash menu reflect it immediately.
func (a *App) AddSkillPath(path string) error {
	path = normalizeSkillPath(path)
	workspaceRoot := a.activeWorkspaceRoot()
	return a.applyConfigChange(func(c *config.Config) error {
		if isConventionSkillRoot(path, workspaceRoot) {
			return c.RestoreSkillPath(path)
		}
		return c.AddSkillPath(path)
	})
}

// RemoveSkillPath removes a skill source from the user config and rebuilds. For
// convention roots, it records a pseudo-delete in excluded_paths.
func (a *App) RemoveSkillPath(path string) error {
	path = normalizeSkillPath(path)
	return a.applyConfigChange(func(c *config.Config) error {
		removed, err := c.RemoveSkillPath(path)
		if err != nil || removed {
			return err
		}
		return c.ExcludeSkillPath(path)
	})
}

// RefreshSkills rebuilds the controller without changing config, reloading skill
// discovery, the system prompt index, and slash completions.
func (a *App) RefreshSkills() error {
	return a.rebuild()
}

// SetSkillEnabled persists a skill toggle and rebuilds the controller so the
// prompt index, slash menu, and skill tools reflect it immediately.
func (a *App) SetSkillEnabled(name string, enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetSkillEnabled(name, enabled)
	})
}

func normalizeSkillPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if info.Mode().IsRegular() {
		if filepath.Base(path) == skill.SkillFile {
			return filepath.Clean(filepath.Dir(filepath.Dir(path)))
		}
		return filepath.Clean(filepath.Dir(path))
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(path, skill.SkillFile)); err == nil {
			return filepath.Clean(filepath.Dir(path))
		}
	}
	return filepath.Clean(path)
}

func isConventionSkillRoot(path, workspaceRoot string) bool {
	want := config.CanonicalSkillPath(path)
	if want == "" {
		return false
	}
	bases := []string{workspaceRoot}
	if home, err := os.UserHomeDir(); err == nil {
		bases = append(bases, home)
	}
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		for _, dir := range config.ConventionDirs {
			if want == config.CanonicalSkillPath(filepath.Join(base, dir, skill.SkillsDirname)) {
				return true
			}
		}
	}
	return false
}

func skillRootPath(path string) string {
	if filepath.Base(path) == skill.SkillFile {
		return filepath.Dir(path)
	}
	return path
}

func skillDisplayRoot(sk skill.Skill, roots []skill.Root) string {
	cleanPath := filepath.Clean(sk.Path)
	for _, r := range roots {
		if r.Scope != sk.Scope {
			continue
		}
		cleanRoot := filepath.Clean(r.Dir)
		prefix := cleanRoot + string(filepath.Separator)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, prefix) {
			return config.CanonicalSkillPath(r.Dir)
		}
	}
	return config.CanonicalSkillPath(filepath.Dir(skillRootPath(sk.Path)))
}

// MCPServerInput is the drawer's "add server" form. Transport is "stdio" (Command
// + Args + Env) or "http"/"sse" (URL). Mirrors config.PluginEntry's writable shape.
type MCPServerInput struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	URL       string            `json:"url"`
	Env       map[string]string `json:"env"`
}

// AddMCPServer connects a server live and persists it to config (Customize → MCP →
// Add). Returns the number of tools it exposed.
func (a *App) AddMCPServer(in MCPServerInput) (int, error) {
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	entry := config.PluginEntry{
		Name:    in.Name,
		Type:    normalizeMCPTransport(in.Transport),
		Command: in.Command,
		Args:    in.Args,
		URL:     in.URL,
		Env:     in.Env,
	}
	entry, _ = config.NormalizePluginCommandLine(entry)
	if err := a.saveDesktopMCPServer(entry); err != nil {
		return 0, err
	}
	return ctrl.ConnectMCPServer(entry)
}

// UpdateMCPServer edits a persisted external MCP server. The name is the stable
// identity; callers must remove + add if they want to rename a server.
func (a *App) UpdateMCPServer(name string, in MCPServerInput) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; configure it with [codegraph]")
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if strings.TrimSpace(in.Name) != "" && strings.TrimSpace(in.Name) != name {
		return fmt.Errorf("renaming MCP servers is not supported; remove and add a new server")
	}
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Type = normalizeMCPTransport(in.Transport)
	updated.Command = strings.TrimSpace(in.Command)
	updated.Args = append([]string(nil), in.Args...)
	updated.URL = strings.TrimSpace(in.URL)
	updated.Tier = ""
	if in.Env != nil {
		updated.Env = in.Env
	}
	updated, _ = config.NormalizePluginCommandLine(updated)
	if updated.Type == "stdio" {
		updated.URL = ""
	} else {
		updated.Command = ""
		updated.Args = nil
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}

	a.mu.RLock()
	tab := a.activeTabLocked()
	sessionDisabled := false
	if tab != nil {
		_, sessionDisabled = tab.disabledMCP[name]
	}
	a.mu.RUnlock()
	wasConnected := mcpConnected(ctrl, name)
	wasFailed := mcpFailed(ctrl, name)
	if wasConnected {
		ctrl.DisconnectMCPServer(name)
	}
	if !sessionDisabled && (wasConnected || wasFailed || updated.ResolvedTier() != "lazy") {
		if _, err := ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(ctrl, updated, err)
			return nil
		}
	}
	return nil
}

// RemoveMCPServer disconnects a live server and drops it from config (the row's ✕).
func (a *App) RemoveMCPServer(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it cannot be removed")
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	disconnected := tab.Ctrl.DisconnectMCPServer(name)
	removed, err := a.removeDesktopMCPServer(name)
	if err != nil {
		return err
	}
	if disconnected || removed {
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		tab.mcpOrder = removeServerOrder(tab.mcpOrder, name)
		a.mu.Unlock()
		return nil
	}
	return fmt.Errorf("no MCP server named %q", name)
}

// ReconnectMCPServer disconnects the server if it is already connected (to force
// a fresh handshake and tool re-registration), then reconnects.  Failures are
// recorded on the Host so the UI can render them.
func (a *App) ReconnectMCPServer(name string) error {
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if mcpConnected(tab.Ctrl, name) {
		tab.Ctrl.DisconnectMCPServer(name)
	}
	_, err := a.connectConfiguredMCPServerForTab(tab, name)
	if err != nil {
		recordMCPFailure(tab.Ctrl, config.PluginEntry{Name: name}, err)
		return err
	}
	a.mu.Lock()
	delete(tab.disabledMCP, name)
	a.mu.Unlock()
	return nil
}

// ClearMCPServerAuthentication removes local auth-like config for one MCP and
// clears the current session's cached connection failure. It does not remove the
// server itself or try to sign the user out of the third-party browser session.
func (a *App) ClearMCPServerAuthentication(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it has no stored MCP authentication")
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if _, _, _, err := config.ClearPluginAuthenticationInSource(name); err != nil {
		return err
	}
	ctrl.DisconnectMCPServer(name)
	if h := ctrl.Host(); h != nil {
		h.ClearFailure(name)
	}
	return nil
}

// SetMCPServerEnabled is the connector toggle: on reconnects a configured server
// for this session, off disconnects it (config untouched either way — like Claude
// Code's per-conversation enable/disable, it resets on the next session start).
func (a *App) SetMCPServerEnabled(name string, enabled bool) error {
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if name == "codegraph" {
		return a.setCodegraphEnabled(enabled)
	}
	if enabled {
		_, err := a.connectConfiguredMCPServerForTab(tab, name)
		if err == nil {
			a.mu.Lock()
			delete(tab.disabledMCP, name)
			a.mu.Unlock()
		}
		return err
	}
	if s, ok := findMCPServerView(tab.Ctrl, name); ok {
		s.Status = "disabled"
		s.Error = ""
		a.mu.Lock()
		if tab.disabledMCP == nil {
			tab.disabledMCP = map[string]ServerView{}
		}
		tab.disabledMCP[name] = s
		tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
		a.mu.Unlock()
	}
	tab.Ctrl.DisconnectMCPServer(name)
	return nil
}

func (a *App) connectConfiguredMCPServerForTab(tab *WorkspaceTab, name string) (int, error) {
	if tab == nil || tab.Ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	cfg, err := config.LoadForRoot(tab.WorkspaceRoot)
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return tab.Ctrl.ConnectMCPServer(p)
		}
	}
	if name == "codegraph" {
		return tab.Ctrl.ConnectCodegraphMCPServer(cfg)
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// SetMCPServerTier is kept for old desktop bindings. New config writes drop the
// retired tier field; for CodeGraph this now means "enable and start in the
// background".
func (a *App) SetMCPServerTier(name, tier string) error {
	if name == "codegraph" {
		return a.setCodegraphTier(tier)
	}
	tier = normalizeMCPTier(tier)
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Tier = tier
	if !updated.ShouldAutoStart() {
		on := true
		updated.AutoStart = &on
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}
	tab := a.activeTab()
	if tier != "lazy" && tab != nil && tab.Ctrl != nil && !mcpConnected(tab.Ctrl, name) {
		if _, err := tab.Ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(tab.Ctrl, updated, err)
			return nil
		}
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		a.mu.Unlock()
	}
	return nil
}

func (a *App) setCodegraphEnabled(enabled bool) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	cfg.Codegraph.Enabled = enabled
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	if enabled {
		a.mu.Lock()
		delete(tab.disabledMCP, "codegraph")
		a.mu.Unlock()
		if _, err := tab.Ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
			recordCodegraphFailure(tab.Ctrl, cfg.Codegraph, err)
			return nil
		}
		return nil
	}
	if h := tab.Ctrl.Host(); h != nil {
		h.ClearFailure("codegraph")
	}
	tab.Ctrl.DisconnectMCPServer("codegraph")
	s := withCodegraphConfig(ServerView{Name: "codegraph", Status: "disabled"}, cfg.Codegraph)
	a.mu.Lock()
	if tab.disabledMCP == nil {
		tab.disabledMCP = map[string]ServerView{}
	}
	tab.disabledMCP["codegraph"] = s
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
	a.mu.Unlock()
	return nil
}

func (a *App) setCodegraphTier(_ string) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	cfg.Codegraph.Enabled = true
	cfg.Codegraph.Tier = ""
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return nil
	}
	a.mu.Lock()
	delete(tab.disabledMCP, "codegraph")
	a.mu.Unlock()
	if !mcpConnected(tab.Ctrl, "codegraph") {
		if _, err := tab.Ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
			recordCodegraphFailure(tab.Ctrl, cfg.Codegraph, err)
			return nil
		}
	}
	return nil
}

func (a *App) desktopMCPServerForEdit(name string) (config.PluginEntry, bool, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return config.PluginEntry{}, false, err
	}
	if p, ok := findPluginEntry(cfg.Plugins, name); ok {
		return p, true, nil
	}
	if merged, err := config.LoadForRoot(a.activeWorkspaceRoot()); err == nil {
		if p, ok := findPluginEntry(merged.Plugins, name); ok {
			return p, true, nil
		}
	}
	return config.PluginEntry{}, false, nil
}

func (a *App) saveDesktopMCPServer(entry config.PluginEntry) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := cfg.UpsertPlugin(entry); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	_, err = a.removeProjectMCPOverride(entry.Name)
	return err
}

func (a *App) removeDesktopMCPServer(name string) (bool, error) {
	removed := false
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return false, err
	}
	if cfg.RemovePlugin(name) {
		removed = true
		if err := cfg.SaveTo(path); err != nil {
			return false, err
		}
	}
	projectRemoved, err := a.removeProjectMCPOverride(name)
	if err != nil {
		return removed, err
	}
	return removed || projectRemoved, nil
}

func (a *App) removeProjectMCPOverride(name string) (bool, error) {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	cfg := config.LoadForEdit(path)
	if !cfg.RemovePlugin(name) {
		return false, nil
	}
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) syncProjectCodegraphOverride(c config.CodegraphConfig) error {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := config.LoadForEdit(path)
	cfg.Codegraph = c
	return cfg.SaveTo(path)
}

func findPluginEntry(entries []config.PluginEntry, name string) (config.PluginEntry, bool) {
	for _, p := range entries {
		if p.Name == name {
			return p, true
		}
	}
	return config.PluginEntry{}, false
}

func normalizeMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	case "":
		return "background"
	default:
		return "lazy"
	}
}

func normalizeMCPTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return "stdio"
	}
}

func mcpConnected(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return true
		}
	}
	return false
}

func mcpFailed(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return true
		}
	}
	return false
}

func recordMCPFailure(ctrl *control.Controller, e config.PluginEntry, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	exp := e.ExpandedPlugin()
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    exp.Name,
		Type:    exp.Type,
		Command: exp.Command,
		Args:    exp.Args,
		Env:     exp.Env,
		URL:     exp.URL,
		Headers: exp.Headers,
	}, err)
}

func recordCodegraphFailure(ctrl *control.Controller, c config.CodegraphConfig, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	cmd := strings.TrimSpace(c.Path)
	if cmd == "" {
		cmd = "codegraph"
	}
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    "codegraph",
		Type:    "stdio",
		Command: cmd,
		Args:    []string{"serve", "--mcp"},
	}, err)
}

func findMCPServerView(ctrl *control.Controller, name string) (ServerView, bool) {
	if ctrl == nil || ctrl.Host() == nil {
		return ServerView{}, false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				Tools: s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}, true
		}
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return ServerView{Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error}, true
		}
	}
	return ServerView{}, false
}

func pluginToolsToView(tools []plugin.ToolInfo) []ToolView {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ToolView, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolView{Name: t.Name, Description: t.Description})
	}
	return out
}

func orderServerViews(servers []ServerView, order []string) []ServerView {
	pos := make(map[string]int, len(order))
	for i, name := range order {
		pos[name] = i
	}
	sort.SliceStable(servers, func(i, j int) bool {
		pi, iok := pos[servers[i].Name]
		pj, jok := pos[servers[j].Name]
		switch {
		case iok && jok:
			return pi < pj
		case iok:
			return true
		case jok:
			return false
		default:
			return false
		}
	})
	return servers
}

func mergeServerOrder(order []string, servers []ServerView) []string {
	seen := make(map[string]bool, len(order)+len(servers))
	next := make([]string, 0, len(order)+len(servers))
	for _, name := range order {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	for _, s := range servers {
		if s.Name == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		next = append(next, s.Name)
	}
	return next
}

func removeServerOrder(order []string, name string) []string {
	if name == "" || len(order) == 0 {
		return order
	}
	next := order[:0]
	for _, n := range order {
		if n != name {
			next = append(next, n)
		}
	}
	return next
}

// ModelInfo is one (provider, model) the bottom switcher can pick. Ref ("provider/
// model") is what SetModel takes; Provider/Model are for display.
type ModelInfo struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Current  bool   `json:"current"`
}

type EffortInfo struct {
	Supported bool     `json:"supported"`
	Current   string   `json:"current"`
	Default   string   `json:"default"`
	Levels    []string `json:"levels"`
}

// Models flattens the configured providers into their (provider, model) pairs —
// the switcher's options — marking the active one. A vendor with a `models` list
// yields one entry per model, all sharing the same endpoint/key. Unconfigured
// providers are skipped. Result is non-nil: the frontend reads .length, so a nil
// slice (JSON null) would crash the switcher on an empty list.
func (a *App) Models() []ModelInfo {
	return a.ModelsForTab("")
}

func (a *App) ModelsForTab(tabID string) []ModelInfo {
	a.mu.RLock()
	curModel := ""
	workspaceRoot := ""
	if tab := a.tabByIDLocked(tabID); tab != nil {
		curModel = tab.model
		workspaceRoot = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return []ModelInfo{}
	}
	if entry, ok := cfg.ResolveModel(curModel); ok {
		curModel = entry.Name + "/" + entry.Model
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	out := []ModelInfo{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !modelProviderAccessAllowed(access, p.Name) || !p.Configured() {
			continue
		}
		for _, m := range p.ChatModelList() {
			ref := p.Name + "/" + m
			out = append(out, ModelInfo{Ref: ref, Provider: p.Name, Model: m, Current: ref == curModel})
		}
	}
	return out
}

func modelProviderAccessAllowed(access map[string]bool, name string) bool {
	if len(access) == 0 {
		return true
	}
	return access[strings.TrimSpace(name)]
}

// SetModel switches the active model and carries the current conversation into the
// new model's session, so the chat continues seamlessly and subsequent turns use
// the new model. No-op if name is already active or the controller is down.
func (a *App) SetModel(name string) error {
	return a.SetModelForTab("", name)
}

func (a *App) SetModelForTab(tabID, name string) error {
	if a.ctx == nil || name == "" {
		return nil
	}
	tab := a.tabByID(tabID)
	if tab == nil {
		return nil
	}
	if name == tab.model {
		return nil
	}
	if tab.Ctrl != nil && tab.Ctrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing model")
	}
	cfg, err := config.LoadForRoot(tab.WorkspaceRoot)
	if err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(name)
	if !ok {
		return fmt.Errorf("unknown model %q", name)
	}
	if !modelProviderAccessAllowed(providerAccessSet(cfg.Desktop.ProviderAccess), entry.Name) {
		return fmt.Errorf("model %q is not available because provider %q is not added", name, entry.Name)
	}
	name = entry.Name + "/" + entry.Model
	effortOverride := cloneStringPtr(tab.effort)
	if effortOverride != nil {
		normalized, err := config.NormalizeEffort(entry, config.EffortDisplay(&config.ProviderEntry{Effort: *effortOverride}))
		if err != nil {
			effortOverride = nil
		} else {
			effortOverride = &normalized
		}
	}

	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}

	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          name,
		RequireKey:     false,
		Sink:           tab.sink,
		WorkspaceRoot:  tab.WorkspaceRoot,
		EffortOverride: cloneStringPtr(effortOverride),
	})
	if err != nil {
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.mu.Lock()
	tab.Ctrl = newCtrl
	tab.model = name
	tab.effort = cloneStringPtr(effortOverride)
	tab.Label = newCtrl.Label()
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)

	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func (a *App) Effort() EffortInfo {
	return a.EffortForTab("")
}

func (a *App) EffortForTab(tabID string) EffortInfo {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return EffortInfo{Current: "auto", Levels: []string{}}
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		return EffortInfo{Supported: false, Current: "auto", Default: cap.Default, Levels: []string{}}
	}
	levels := cap.Levels
	if levels == nil {
		levels = []string{}
	}
	return EffortInfo{Supported: true, Current: config.EffortDisplay(entry), Default: cap.Default, Levels: levels}
}

func (a *App) SetEffort(level string) error {
	return a.SetEffortForTab("", level)
}

func (a *App) SetEffortForTab(tabID, level string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		if strings.TrimSpace(tabID) == "" {
			entry, err := a.currentProviderEntryForTab("")
			if err != nil {
				return err
			}
			effort, err := config.NormalizeEffort(entry, level)
			if err != nil {
				return err
			}
			return a.applyProviderEffortConfig(entry, effort)
		}
		return fmt.Errorf("tab %q not found", tabID)
	}
	ctrl := tab.Ctrl
	if ctrl != nil && ctrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing effort")
	}
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return err
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}
	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          tab.model,
		RequireKey:     false,
		Sink:           tab.sink,
		WorkspaceRoot:  tab.WorkspaceRoot,
		EffortOverride: &effort,
	})
	if err != nil {
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.mu.Lock()
	tab.Ctrl = newCtrl
	tab.effort = &effort
	tab.Label = newCtrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)
	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func (a *App) applyProviderEffortConfig(entry *config.ProviderEntry, effort string) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		if _, ok := cfg.Provider(entry.Name); !ok {
			if err := cfg.UpsertProvider(*entry); err != nil {
				return err
			}
		}
		if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
			if err := cfg.SetProviderThinking(entry.Name, "adaptive"); err != nil {
				return err
			}
		}
		for _, name := range providerEffortTargetNames(cfg, entry) {
			if err := cfg.SetProviderEffort(name, effort); err != nil {
				return err
			}
		}
		return nil
	})
}

func providerEffortTargetNames(cfg *config.Config, entry *config.ProviderEntry) []string {
	if cfg == nil || entry == nil {
		return nil
	}
	out := []string{entry.Name}
	seen := map[string]bool{entry.Name: true}
	kind := officialProviderKindFromEntry(*entry)
	if kind == "" {
		return out
	}
	var family []string
	switch kind {
	case "deepseek":
		family = []string{"deepseek", "deepseek-flash", "deepseek-pro"}
	case "mimo-token-plan":
		family = []string{"mimo-token-plan", "mimo-pro", "mimo-flash"}
	case "mimo-api":
		family = []string{"mimo-api"}
	}
	for _, name := range family {
		if seen[name] {
			continue
		}
		p, ok := cfg.Provider(name)
		if !ok || officialProviderKindFromEntry(*p) != kind {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// DirEntry is one entry in the "@" file-reference menu.
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// FilePreview is a bounded, read-only file payload for the workspace side panel.
type FilePreview struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Kind      string `json:"kind,omitempty"`
	Mime      string `json:"mime,omitempty"`
	URL       string `json:"url,omitempty"`
	Err       string `json:"err,omitempty"`
}

type WorkspaceChangeView struct {
	Path         string   `json:"path"`
	OldPath      string   `json:"oldPath,omitempty"`
	Sources      []string `json:"sources"`
	GitStatus    string   `json:"gitStatus,omitempty"`
	Turns        []int    `json:"turns,omitempty"`
	LatestPrompt string   `json:"latestPrompt,omitempty"`
	LatestTime   int64    `json:"latestTime,omitempty"`
}

type WorkspaceChangesView struct {
	Files        []WorkspaceChangeView `json:"files"`
	GitAvailable bool                  `json:"gitAvailable"`
	GitErr       string                `json:"gitErr,omitempty"`
	GitBranch    string                `json:"gitBranch,omitempty"`
}

// workspaceNoiseNames are local cache/vendor entries hidden from the file tree
// and "@" menu regardless of where they appear.
var workspaceNoiseNames = map[string]bool{
	".codex":       true,
	".codegraph":   true,
	".DS_Store":    true,
	".git":         true,
	".npm":         true,
	".pnpm-store":  true,
	"node_modules": true,
	"Thumbs.db":    true,
}

var workspaceNoiseDirs = map[string]bool{
	"bin":                      true,
	"desktop/build":            true,
	"desktop/frontend/dist":    true,
	"desktop/frontend/wailsjs": true,
	"dist":                     true,
	"npm/.stage":               true,
	"site/.astro":              true,
	"site/dist":                true,
	"stage":                    true,
	"tmp":                      true,
}

const filePreviewLimit = 256 * 1024
const fileRefSearchLimit = 20

var previewMediaMIMEs = map[string]string{
	".bmp":  "image/bmp",
	".gif":  "image/gif",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

func trimUTF8PartialSuffix(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	for i := len(data) - 1; i >= 0 && len(data)-i <= utf8.UTFMax; i-- {
		if !utf8.RuneStart(data[i]) {
			continue
		}
		if !utf8.Valid(data[:i]) || utf8.FullRune(data[i:]) {
			return data
		}
		return data[:i]
	}
	return data
}

func previewMediaKind(path string) (kind string, mime string) {
	mime = previewMediaMIMEs[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		return "", ""
	}
	if strings.HasPrefix(mime, "image/") {
		return "image", mime
	}
	if mime == "application/pdf" {
		return "pdf", mime
	}
	return "", ""
}

func workspaceEntryRel(rel, name string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return name
	}
	return rel + "/" + name
}

func skipWorkspaceEntry(rel, name string, isDir bool) bool {
	if workspaceNoiseNames[name] {
		return true
	}
	return isDir && workspaceNoiseDirs[workspaceEntryRel(rel, name)]
}

func (a *App) activeWorkspaceBase() (string, error) {
	root := a.activeWorkspaceRoot()
	if strings.TrimSpace(root) == "" || root == "." {
		return os.Getwd()
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root), nil
}

func (a *App) workspacePath(rel string) (string, bool, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", false, err
	}
	return workspacePathForBase(base, rel)
}

func workspacePath(rel string) (string, bool, error) {
	base, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	return workspacePathForBase(base, rel)
}

func workspacePathForBase(base, rel string) (string, bool, error) {
	base = filepath.Clean(base)
	if rel == "" {
		return "", false, os.ErrInvalid
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, rel)
	}
	path = filepath.Clean(path)
	r, err := filepath.Rel(base, path)
	if err != nil {
		return "", false, err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", false, os.ErrPermission
	}
	return path, true, nil
}

// ListDir lists one directory level (directories first, then files, each
// alphabetical) for the "@" file-reference menu. rel resolves against the active
// tab workspace. The menu navigates one level at a time, never recursively —
// bounded for huge trees.
func (a *App) ListDir(rel string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return []DirEntry{}
	}
	dir := base
	if rel != "" {
		path, ok, err := workspacePathForBase(base, rel)
		if err != nil || !ok {
			return []DirEntry{}
		}
		dir = path
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return []DirEntry{}
	}
	dirs, files := []DirEntry{}, []DirEntry{}
	for _, e := range es {
		name := e.Name()
		if skipWorkspaceEntry(rel, name, e.IsDir()) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, DirEntry{Name: name, IsDir: true})
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, DirEntry{Name: name, IsDir: false})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return append(dirs, files...)
}

// SearchFileRefs finds workspace files by basename for bare "@token" completion.
func (a *App) SearchFileRefs(query string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	paths := fileref.Search(base, query, fileRefSearchLimit)
	out := make([]DirEntry, 0, len(paths))
	for _, path := range paths {
		out = append(out, DirEntry{Name: path, IsDir: false})
	}
	return out
}

// ReadFile returns a small text preview for a file under the current workspace.
func (a *App) ReadFile(rel string) FilePreview {
	out := FilePreview{Path: rel}
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		out.Err = "invalid path"
		return out
	}
	info, err := os.Stat(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if info.IsDir() {
		out.Err = "path is a directory"
		return out
	}
	if !info.Mode().IsRegular() {
		out.Err = "path is not a regular file"
		return out
	}
	out.Size = info.Size()
	if kind, mime := previewMediaKind(path); kind != "" {
		token := a.ensureMediaTokenStore().create(path, info.Name(), mime, kind, info.Size(), info.ModTime())
		out.Kind = kind
		out.Mime = mime
		out.URL = "/__reasonix_workspace_media/" + token + "/" + url.PathEscape(info.Name())
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	defer f.Close()

	buf := make([]byte, filePreviewLimit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		out.Err = err.Error()
		return out
	}
	data := buf[:n]
	if len(data) > filePreviewLimit {
		data = data[:filePreviewLimit]
		out.Truncated = true
	}

	// Check for BOM first (just the first 2-3 bytes — always complete
	// even at a truncation boundary). BOM-prefixed files skip the NUL
	// check since UTF-16 normally contains 0x00 for ASCII characters.
	bomKind := fileenc.DetectQuick(data)
	if bomKind != fileenc.UTF8 {
		enc, _ := fileenc.Detect(data)
		if enc == fileenc.LossyUTF8 {
			out.Binary = true
			return out
		}
		decoded := fileenc.Decode(data, enc)
		out.Body = string(decoded)
		return out
	}

	// No BOM — NUL in raw bytes is a binary signal.
	if bytes.Contains(data, []byte{0}) {
		out.Binary = true
		return out
	}

	// Trim any partial multi-byte rune at the truncation boundary BEFORE
	// encoding detection. Without this, a large UTF-8 file truncated
	// mid-character would fail utf8.Valid and be misdetected as GB18030
	// or LossyUTF8, producing mojibake or a false binary classification.
	if out.Truncated {
		data = trimUTF8PartialSuffix(data)
	}
	enc, _ := fileenc.Detect(data)
	if enc == fileenc.LossyUTF8 {
		out.Binary = true
		return out
	}
	out.Body = string(fileenc.Decode(data, enc))
	return out
}

// OpenWorkspacePath opens a file or folder from the workspace in the OS default app.
func (a *App) OpenWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return openWorkspacePath(path)
}

// RevealWorkspacePath shows a workspace file in the native file manager.
func (a *App) RevealWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return revealPath(path)
}

// RevealPath shows an arbitrary absolute path in the native file manager.
func (a *App) RevealPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.ErrInvalid
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return revealPath(path)
}

func revealPath(path string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		dir := path
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func (a *App) notice(text string) {
	a.noticeForTab("", text)
}

func (a *App) noticeForTab(tabID, text string) {
	tab := a.tabByID(tabID)
	if tab != nil && tab.sink != nil {
		tab.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
	}
}

func (a *App) runEffortCommand(input string) {
	a.runEffortCommandForTab("", input)
}

func (a *App) runEffortCommandForTab(tabID, input string) {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		a.noticeForTab(tabID, fmt.Sprintf("effort is not configurable for %s", entry.Name))
		return
	}
	args := strings.Fields(input)
	if len(args) < 2 {
		a.noticeForTab(tabID, fmt.Sprintf("effort for %s: %s (default: %s; options: %s)", entry.Name, config.EffortDisplay(entry), cap.Default, strings.Join(cap.Levels, "|")))
		return
	}
	if len(args) > 2 {
		a.noticeForTab(tabID, "usage: /effort "+strings.Join(cap.Levels, "|"))
		return
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		a.noticeForTab(tabID, err.Error())
		return
	}
	if err := a.SetEffortForTab(tabID, args[1]); err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	display := effort
	if display == "" {
		display = "auto"
	}
	a.noticeForTab(tabID, fmt.Sprintf("effort for %s set to %s", entry.Name, display))
}

func (a *App) currentProviderEntry() (*config.ProviderEntry, error) {
	return a.currentProviderEntryForTab("")
}

func (a *App) currentProviderEntryForTab(tabID string) (*config.ProviderEntry, error) {
	a.mu.RLock()
	ref := ""
	workspaceRoot := ""
	effortOverride := (*string)(nil)
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ref = tab.model
		workspaceRoot = tab.WorkspaceRoot
		effortOverride = cloneStringPtr(tab.effort)
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		ref = cfg.DefaultModel
	}
	resolved, _, ok := cfg.ResolveModelWithFallback(ref)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", ref)
	}
	entry, ok := cfg.ResolveModel(resolved)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", resolved)
	}
	if effortOverride != nil {
		entry.Effort = *effortOverride
	}
	return entry, nil
}

// SavePastedImage stores a browser clipboard image data URL under the active
// tab's workspace .reasonix/attachments and returns the relative @-reference path.
func (a *App) SavePastedImage(dataURL string) (string, error) {
	root := a.activeWorkspaceRoot()
	if root != "" && root != "." {
		if prev, err := os.Getwd(); err == nil {
			if err := os.Chdir(root); err == nil {
				defer func() { _ = os.Chdir(prev) }()
			}
		}
	}
	return control.SaveImageDataURL(dataURL)
}

// SavePastedFile stores a dropped non-image file (the browser exposes its bytes
// as a data URL but not a real path) under the active tab's workspace
// .reasonix/attachments and returns the relative @-reference path.
func (a *App) SavePastedFile(name, dataURL string) (string, error) {
	root := a.activeWorkspaceRoot()
	if root != "" && root != "." {
		if prev, err := os.Getwd(); err == nil {
			if err := os.Chdir(root); err == nil {
				defer func() { _ = os.Chdir(prev) }()
			}
		}
	}
	return control.SaveAttachmentDataURL(name, dataURL)
}

// AttachmentDataURL returns a safe data URL for a stored image attachment.
func (a *App) AttachmentDataURL(path string) (string, error) {
	return control.ImageDataURL(path)
}

// DroppedItem is one OS-dropped file resolved into a composer context entry: an
// in-tree file becomes a workspace @reference (read in place, no copy), while an
// image or out-of-tree file is copied into .reasonix/attachments.
type DroppedItem struct {
	Kind       string `json:"kind"` // "workspace" | "attachment"
	Path       string `json:"path"`
	IsDir      bool   `json:"isDir,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

// AttachDropped turns an absolute path from the native file-drop bridge into a
// composer context entry. Images are stored as attachments so the chip shows a
// thumbnail; other in-workspace files are referenced relatively (no copy); files
// outside the workspace are copied into .reasonix/attachments.
func (a *App) AttachDropped(path string) (DroppedItem, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return DroppedItem{}, err
	}
	if isImageExt(path) {
		if rel, err := control.SaveImageFile(path); err == nil {
			preview, _ := control.ImageDataURL(rel)
			return DroppedItem{Kind: "attachment", Path: rel, PreviewURL: preview}, nil
		}
	}
	if rel, ok := workspaceRelative(path); ok {
		return DroppedItem{Kind: "workspace", Path: rel, IsDir: info.IsDir()}, nil
	}
	if info.IsDir() {
		return DroppedItem{}, fmt.Errorf("can only attach files from outside the workspace")
	}
	rel, err := control.SaveAttachmentFile(path)
	if err != nil {
		return DroppedItem{}, err
	}
	return DroppedItem{Kind: "attachment", Path: rel}, nil
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func workspaceRelative(path string) (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// --- memory panel (frontend ⇄ controller) ---

// MemoryDoc is one loaded doc-memory file for the panel: path, scope, and body.
type MemoryDoc struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Body  string `json:"body"`
}

// MemoryFact is one saved auto-memory, surfaced read-only in the panel.
type MemoryFact struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Body        string `json:"body"`
}

// MemoryScope is one writable quick-add target (scope id + the file it writes to).
type MemoryScope struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

// MemoryView is the whole memory panel payload: hierarchical docs, saved facts,
// and the writable scopes for the quick-add selector.
type MemoryView struct {
	Docs      []MemoryDoc   `json:"docs"`
	Facts     []MemoryFact  `json:"facts"`
	Scopes    []MemoryScope `json:"scopes"`
	StoreDir  string        `json:"storeDir"`
	Available bool          `json:"available"`
}

// writableScopes are the quick-add targets the panel offers, broad → specific.
var writableScopes = []memory.Scope{memory.ScopeUser, memory.ScopeProject, memory.ScopeLocal}

// Memory returns the loaded memory for the panel: the REASONIX.md hierarchy, the
// saved auto-memories, and the writable scopes. Read-only; mutations go through
// Remember / SaveDoc.
func (a *App) Memory() MemoryView {
	// Always return non-nil slices: a nil Go slice marshals to JSON `null`, which
	// would crash the panel's `view.facts.length` / `.map`.
	view := MemoryView{Docs: []MemoryDoc{}, Facts: []MemoryFact{}, Scopes: []MemoryScope{}}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return view
	}
	set := ctrl.Memory()
	if set == nil {
		return view
	}
	view.StoreDir = set.Store.Dir
	view.Available = true
	for _, d := range set.Docs {
		view.Docs = append(view.Docs, MemoryDoc{Path: d.Path, Scope: string(d.Scope), Body: d.Body})
	}
	for _, f := range set.Store.List() {
		view.Facts = append(view.Facts, MemoryFact{
			Name: f.Name, Title: f.Title, Description: f.Description, Type: string(f.Type), Body: f.Body,
		})
	}
	for _, sc := range writableScopes {
		if p := set.DocPath(sc); p != "" { // user scope yields "" when no config dir
			view.Scopes = append(view.Scopes, MemoryScope{Scope: string(sc), Path: p})
		}
	}
	return view
}

// Remember quick-adds a one-line note to the doc-memory file for scope — the
// panel's explicit "remember" action, equivalent to typing "/remember <note>".
// An unknown scope falls back to project. Returns the file written.
func (a *App) Remember(scope, note string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.QuickAdd(parseScope(scope), note)
}

// Forget deletes a saved auto-memory by name — the panel's delete action for a
// fact the model owns. A no-op when no controller is attached.
func (a *App) Forget(name string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.ForgetMemory(name)
}

// SaveDoc overwrites a memory doc with the panel editor's contents. The controller
// validates path against the recognized memory files. Returns the file written.
func (a *App) SaveDoc(path, body string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.SaveDoc(path, body)
}

// parseScope maps a frontend scope id to a memory.Scope, defaulting to project.
func parseScope(s string) memory.Scope {
	switch memory.Scope(s) {
	case memory.ScopeUser:
		return memory.ScopeUser
	case memory.ScopeLocal:
		return memory.ScopeLocal
	default:
		return memory.ScopeProject
	}
}

// onboardingKeyEnv is the default provider (deepseek) key from config.Default().
const onboardingKeyEnv = "DEEPSEEK_API_KEY"

// onboardingBalanceURL doubles as a zero-token connectivity + auth probe:
// billing.FetchWithClient surfaces 401/403 for a bad key.
const onboardingBalanceURL = "https://api.deepseek.com/user/balance"

// NativeConfirmRequest is the payload for ConfirmAction — a native OS confirmation
// dialog that replaces web-style confirm() for destructive or important actions.
type NativeConfirmRequest struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	Detail       string `json:"detail"`
	ConfirmLabel string `json:"confirmLabel"`
	CancelLabel  string `json:"cancelLabel"`
	Destructive  bool   `json:"destructive"`
}

// ConfirmAction shows a native confirmation dialog and returns true when the user
// clicks the confirm button. For destructive actions the dialog type is Warning so
// the platform can apply its danger styling (red tint on macOS, etc.).
func (a *App) ConfirmAction(req NativeConfirmRequest) (bool, error) {
	if a.ctx == nil {
		return false, nil
	}
	dialogType := runtime.QuestionDialog
	if req.Destructive {
		dialogType = runtime.WarningDialog
	}
	confirm := req.ConfirmLabel
	if confirm == "" {
		confirm = "OK"
	}
	cancel := req.CancelLabel
	if cancel == "" {
		cancel = "Cancel"
	}
	title := req.Title
	if title == "" {
		title = req.Message
	}
	body := req.Message
	if req.Detail != "" {
		if body != "" {
			body += "\n\n" + req.Detail
		} else {
			body = req.Detail
		}
	}
	defaultBtn := confirm
	if req.Destructive {
		// On destructive actions, make cancel the default so Enter / Space
		// does NOT accidentally confirm. ESC always maps to CancelButton.
		defaultBtn = cancel
	}
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          dialogType,
		Title:         title,
		Message:       body,
		Buttons:       []string{confirm, cancel},
		DefaultButton: defaultBtn,
		CancelButton:  cancel,
	})
	if err != nil {
		return false, err
	}
	return result == confirm, nil
}

func (a *App) NeedsOnboarding() bool {
	return strings.TrimSpace(os.Getenv(onboardingKeyEnv)) == ""
}

// ConnectKey validates apiKey against the balance endpoint, persists it to the
// global credentials file, and rebuilds the controller so the new key takes effect.
func (a *App) ConnectKey(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("key is required")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	if _, err := billing.FetchWithClient(ctx, nil, onboardingBalanceURL, apiKey); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if err := upsertDotEnv(onboardingKeyEnv, apiKey); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	if err := a.rebuild(); err != nil {
		// Key is persisted; surface the failure but let the next rebuild load it.
		a.mu.Lock()
		if tab := a.activeTabLocked(); tab != nil {
			tab.StartupErr = err.Error()
		}
		a.mu.Unlock()
	}
	return nil
}
