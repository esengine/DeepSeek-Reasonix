package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
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

// App is the Wails-bound application object: the desktop frontend's command
// surface. Its exported methods (Submit/Cancel/Approve/…) are generated into JS
// bindings. The app manages multiple WorkspaceTabs — each with its own controller
// scoped to a project workspace — and routes commands to the active tab. Events
// flow the other way: each tab's controller emits to a tabEventSink that
// forwards events tagged with tabId to the webview via runtime.EventsEmit.
type App struct {
	ctx context.Context

	// mu protects the tab map, activeTabID, and per-tab fields that are read
	// from bound methods. All bound methods that touch a controller use activeCtrl().
	mu          sync.RWMutex
	tabs        map[string]*WorkspaceTab
	activeTabID string
	readyHook   func()
}

// NewApp constructs the bound object. Tabs are restored in startup from the
// last session's desktop-tabs.json.
func NewApp() *App {
	return &App{tabs: map[string]*WorkspaceTab{}}
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

	go a.restoreOrBuildTabs()
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
		for _, entry := range f.Tabs {
			var tab *WorkspaceTab
			if entry.Scope == "project" {
				tab = a.createTabEntryWithID(entry.Scope, entry.WorkspaceRoot, entry.TopicID, entry.ID)
			} else {
				tab = a.createTabEntryWithID("global", globalTabWorkspaceRoot(), entry.TopicID, entry.ID)
			}
			tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
			a.mu.Lock()
			a.tabs[tab.ID] = tab
			a.mu.Unlock()
			go a.buildTabController(tab)
		}
		a.mu.Lock()
		if _, ok := a.tabs[f.ActiveTab]; ok {
			a.activeTabID = f.ActiveTab
		} else {
			for id := range a.tabs {
				a.activeTabID = id
				break
			}
		}
		a.mu.Unlock()
		return
	}

	// First launch: create a default Global tab.
	tab := a.createTabEntry("global", globalTabWorkspaceRoot(), "")
	tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
	tab.TopicTitle = "Global"
	a.mu.Lock()
	a.tabs[tab.ID] = tab
	a.activeTabID = tab.ID
	a.mu.Unlock()
	go a.buildTabController(tab)
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
		disabledMCP:   map[string]ServerView{},
	}
}

// shutdown snapshots all tabs and closes them.
func (a *App) shutdown(context.Context) {
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

// --- bound command surface (frontend → controller) ---
// Each method guards on a nil controller so a pre-startup or failed-build call is
// a no-op, never a panic.

// Submit runs raw user input as a turn; slash commands and @-references are
// resolved by the controller. Output arrives asynchronously on eventChannel.
func (a *App) Submit(input string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		a.runEffortCommand(trimmed)
		return
	}
	if ctrl := a.activeCtrl(); ctrl != nil {
		ctrl.Submit(input)
	}
}

// SubmitDisplay runs input as a turn while recording a shorter UI-only display
// string for the saved desktop transcript. The model still receives input.
func (a *App) SubmitDisplay(display, input string) {
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return
	}
	_ = recordSessionDisplay(config.SessionDir(), ctrl.SessionPath(), input, display)
	ctrl.Submit(input)
}

// Cancel aborts the in-flight turn.
func (a *App) Cancel() {
	if ctrl := a.activeCtrl(); ctrl != nil {
		ctrl.Cancel()
	}
}

// Approve answers a pending approval_request by ID: allow runs the call, session
// also remembers the grant for the rest of the session.
func (a *App) Approve(id string, allow, session, persist bool) {
	ctrl := a.activeCtrl()
	if ctrl != nil {
		ctrl.Approve(id, allow, session, persist)
	}
}

// SetPlanMode toggles read-only plan mode.
func (a *App) SetPlanMode(on bool) {
	ctrl := a.activeCtrl()
	if ctrl != nil {
		ctrl.SetPlanMode(on)
	}
}

// SetMode applies a composer gating mode ("plan" | "yolo" | anything else =
// normal) in one call, so a turn submitted right after the switch can't race a
// half-applied SetPlanMode/SetBypass pair.
func (a *App) SetMode(mode string) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return
	}
	switch mode {
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
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
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
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.NewSession()
}

// CheckpointMeta summarises one rewind point (a user turn) for the desktop.
type CheckpointMeta struct {
	Turn   int      `json:"turn"`
	Prompt string   `json:"prompt"`
	Files  []string `json:"files"` // paths changed during the turn
	Time   int64    `json:"time"`  // unix milliseconds
}

// Checkpoints lists the session's rewind points, oldest first, for the rewind UI.
func (a *App) Checkpoints() []CheckpointMeta {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return []CheckpointMeta{}
	}
	metas := ctrl.Checkpoints()
	out := make([]CheckpointMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, CheckpointMeta{Turn: m.Turn, Prompt: m.Prompt, Files: m.Paths, Time: m.Time.UnixMilli()})
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

// Fork branches the conversation at the start of turn into a new session
// (preserving the current one), keeping code intact, and switches to the branch.
// The frontend re-reads History after this resolves.
func (a *App) Fork(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	_, err := ctrl.Fork(turn)
	return err
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
	Current        bool   `json:"current"`
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
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	cur := ""
	if ctrl != nil {
		cur = ctrl.SessionPath()
	}
	out := make([]SessionMeta, 0, len(infos))
	for _, s := range infos {
		out = append(out, SessionMeta{
			Path:           s.Path,
			Preview:        s.Preview,
			Title:          titles[filepath.Base(s.Path)],
			Turns:          s.Turns,
			CreatedAt:      s.CreatedAt.UnixMilli(),
			LastActivityAt: s.LastActivityAt.UnixMilli(),
			ModTime:        s.LastActivityAt.UnixMilli(),
			Current:        s.Path == cur,
			Scope:          s.Scope,
			WorkspaceRoot:  s.WorkspaceRoot,
			TopicID:        s.TopicID,
			TopicTitle:     s.TopicTitle,
		})
	}
	return out
}

// DeleteSession removes a saved session (and its title). It refuses the active
// session — that's the conversation on screen, and auto-save would recreate the
// file on the next turn; start a new session first to retire it.
func (a *App) DeleteSession(path string) error {
	dir := config.SessionDir()
	sessionPath, key, err := validateSessionPath(dir, path)
	if err != nil {
		return err
	}
	ctrl := a.activeCtrl()
	if ctrl != nil {
		currentPath, _, err := validateSessionPath(dir, ctrl.SessionPath())
		if err == nil && currentPath == sessionPath {
			return errActiveSession
		}
	}
	return removeSessionArtifacts(dir, sessionPath, key)
}

// RenameSession sets a custom display name for a session (empty clears it back to
// the preview). It only affects the history panel; the file on disk is unchanged.
func (a *App) RenameSession(path, title string) error {
	return setSessionTitle(config.SessionDir(), path, title)
}

// ResumeSession snapshots the current conversation, then loads the session at
// path and continues it — auto-save keeps appending to that file. The model and
// working folder are unchanged (same controller); only the transcript is swapped.
// Returns the resumed messages for the frontend to render.
func (a *App) ResumeSession(path string) ([]HistoryMessage, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return []HistoryMessage{}, nil
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	_ = ctrl.Snapshot() // persist the current session before switching away
	ctrl.Resume(loaded, path)
	return a.History(), nil
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
		DefaultDirectory: cur,
	})
	if err != nil || dir == "" {
		return "", err
	}
	return a.SwitchWorkspace(dir)
}

func (a *App) ListWorkspaces() []WorkspaceMeta {
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	seen := map[string]bool{}
	paths := make([]string, 0, 8)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if seen[path] {
			return
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	add(cur)
	for _, path := range loadWorkspaces() {
		add(path)
	}
	out := make([]WorkspaceMeta, 0, len(paths))
	for _, path := range paths {
		out = append(out, WorkspaceMeta{
			Path:    path,
			Name:    workspaceName(path),
			Current: false,
		})
	}
	return out
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
	_ = addProject(dir, "")

	// Open a new project tab for this workspace.
	meta, err := a.OpenProjectTab(dir, newTopicID())
	if err != nil {
		return "", err
	}
	return meta.WorkspaceRoot, nil
}

// HistoryMessage is one prior turn, for the frontend to repopulate its transcript
// after a reload.
type HistoryMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
}

// History returns the session's message log.
func (a *App) History() []HistoryMessage {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
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
		}
		reasoning := ""
		if m.Role == provider.RoleAssistant {
			reasoning = m.ReasoningContent
		}
		out = append(out, HistoryMessage{Role: string(m.Role), Content: content, Reasoning: reasoning})
	}
	return out
}

func previewSessionMessages(sessionDir, path string) ([]HistoryMessage, error) {
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	return historyMessages(loaded.Snapshot(), sessionDisplayResolver(sessionDir, path)), nil
}

// ContextInfo is the prompt-vs-window gauge payload. Both zero means no data yet.
type ContextInfo struct {
	Used   int `json:"used"`
	Window int `json:"window"`
}

// ContextUsage returns the latest context-window gauge numbers.
func (a *App) ContextUsage() ContextInfo {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return ContextInfo{}
	}
	used, window := ctrl.ContextSnapshot()
	return ContextInfo{Used: used, Window: window}
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
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
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
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
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
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
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
	ctrl := a.activeCtrl()
	if ctrl != nil {
		ctrl.SetBypass(on)
	}
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
	if cfg, err := config.Load(); err == nil {
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
				switch loadedCfg.Codegraph.ResolvedTier() {
				case "background", "eager":
					status = "initializing"
				default:
					status = "deferred"
				}
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
	if cfg != nil {
		custom = cfg.SkillCustomPaths()
	}
	st := skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom, DisableBuiltins: true, Stderr: io.Discard})
	counts := map[string]int{}
	skillItems := map[string][]SkillRootSkillView{}
	for _, sk := range st.List() {
		root := config.CanonicalSkillPath(filepath.Dir(skillRootPath(sk.Path)))
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
	for _, r := range st.Roots() {
		dir := config.CanonicalSkillPath(r.Dir)
		view := SkillRootView{
			Dir:        r.Dir,
			Scope:      string(r.Scope),
			Priority:   r.Priority + 1,
			Status:     string(r.Status),
			Configured: r.Scope == skill.ScopeCustom && userConfigured[dir],
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
		DefaultDirectory: cur,
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
	return a.applyConfigChange(func(c *config.Config) error {
		return c.AddSkillPath(path)
	})
}

// RemoveSkillPath removes a custom skill root from the user config and rebuilds.
func (a *App) RemoveSkillPath(path string) error {
	path = normalizeSkillPath(path)
	return a.applyConfigChange(func(c *config.Config) error {
		_, err := c.RemoveSkillPath(path)
		return err
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

func skillRootPath(path string) string {
	if filepath.Base(path) == skill.SkillFile {
		return filepath.Dir(path)
	}
	return path
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
	Tier      string            `json:"tier"`
}

// AddMCPServer connects a server live and persists it to config (Customize → MCP →
// Add). Returns the number of tools it exposed.
func (a *App) AddMCPServer(in MCPServerInput) (int, error) {
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	return ctrl.AddMCPServer(config.PluginEntry{
		Name:    in.Name,
		Type:    normalizeMCPTransport(in.Transport),
		Command: in.Command,
		Args:    in.Args,
		URL:     in.URL,
		Env:     in.Env,
		Tier:    normalizeMCPTier(in.Tier),
	})
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	found := false
	var updated config.PluginEntry
	for _, p := range cfg.Plugins {
		if p.Name != name {
			continue
		}
		updated = p
		updated.Type = normalizeMCPTransport(in.Transport)
		updated.Command = strings.TrimSpace(in.Command)
		updated.Args = append([]string(nil), in.Args...)
		updated.URL = strings.TrimSpace(in.URL)
		updated.Tier = normalizeMCPTier(in.Tier)
		if in.Env != nil {
			updated.Env = in.Env
		}
		if updated.Type == "stdio" {
			updated.URL = ""
		} else {
			updated.Command = ""
			updated.Args = nil
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	if err := cfg.UpsertPlugin(updated); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
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
		if _, err := ctrl.ConnectConfiguredMCPServer(name); err != nil {
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
	_, err := tab.Ctrl.RemoveMCPServer(name)
	if err == nil {
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		tab.mcpOrder = removeServerOrder(tab.mcpOrder, name)
		a.mu.Unlock()
	}
	return err
}

// RetryMCPServer reconnects a configured server that failed or was disconnected,
// without touching config (the failed row's retry button).
func (a *App) RetryMCPServer(name string) error {
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	_, err := ctrl.ConnectConfiguredMCPServer(name)
	return err
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
		_, err := tab.Ctrl.ConnectConfiguredMCPServer(name)
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

// SetMCPServerTier persists how a configured MCP server should start on future
// sessions. It does not tear down a connected server; the per-session toggle and
// "connect now" remain separate controls.
func (a *App) SetMCPServerTier(name, tier string) error {
	if name == "codegraph" {
		return a.setCodegraphTier(tier)
	}
	tier = normalizeMCPTier(tier)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	found := false
	var updated config.PluginEntry
	for i := range cfg.Plugins {
		if cfg.Plugins[i].Name == name {
			cfg.Plugins[i].Tier = tier
			if !cfg.Plugins[i].ShouldAutoStart() {
				on := true
				cfg.Plugins[i].AutoStart = &on
			}
			updated = cfg.Plugins[i]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	tab := a.activeTab()
	if tier != "lazy" && tab != nil && tab.Ctrl != nil && !mcpConnected(tab.Ctrl, name) {
		if _, err := tab.Ctrl.ConnectConfiguredMCPServer(name); err != nil {
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Codegraph.Enabled = enabled
	if err := cfg.Save(); err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if enabled {
		a.mu.Lock()
		delete(tab.disabledMCP, "codegraph")
		a.mu.Unlock()
		if _, err := tab.Ctrl.ConnectConfiguredMCPServer("codegraph"); err != nil {
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

func (a *App) setCodegraphTier(tier string) error {
	tier = normalizeMCPTier(tier)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Codegraph.Enabled = true
	cfg.Codegraph.Tier = tier
	if err := cfg.Save(); err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return nil
	}
	a.mu.Lock()
	delete(tab.disabledMCP, "codegraph")
	a.mu.Unlock()
	if tier != "lazy" && !mcpConnected(tab.Ctrl, "codegraph") {
		if _, err := tab.Ctrl.ConnectConfiguredMCPServer("codegraph"); err != nil {
			recordCodegraphFailure(tab.Ctrl, cfg.Codegraph, err)
			return nil
		}
	}
	return nil
}

func normalizeMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
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
	a.mu.RLock()
	curModel := ""
	workspaceRoot := ""
	if tab := a.activeTabLocked(); tab != nil {
		curModel = tab.model
		workspaceRoot = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return []ModelInfo{}
	}
	out := []ModelInfo{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, m := range p.ModelList() {
			ref := p.Name + "/" + m
			out = append(out, ModelInfo{Ref: ref, Provider: p.Name, Model: m, Current: ref == curModel})
		}
	}
	return out
}

// SetModel switches the active model and carries the current conversation into the
// new model's session, so the chat continues seamlessly and subsequent turns use
// the new model. No-op if name is already active or the controller is down.
func (a *App) SetModel(name string) error {
	if a.ctx == nil || name == "" {
		return nil
	}
	tab := a.activeTab()
	if tab == nil {
		return nil
	}
	if name == tab.model {
		return nil
	}

	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
	}

	newCtrl, err := boot.Build(a.ctx, boot.Options{
		Model:         name,
		RequireKey:    false,
		Sink:          tab.sink,
		WorkspaceRoot: tab.WorkspaceRoot,
		Stderr:        io.Discard,
	})
	if err != nil {
		return err
	}
	old := tab.Ctrl
	a.mu.Lock()
	tab.Ctrl = newCtrl
	tab.model = name
	tab.Label = newCtrl.Label()
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	newCtrl.EnableInteractiveApproval()

	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	return nil
}

func (a *App) Effort() EffortInfo {
	entry, err := a.currentProviderEntry()
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
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl != nil && ctrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing effort")
	}
	entry, err := a.currentProviderEntry()
	if err != nil {
		return err
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
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
		return cfg.SetProviderEffort(entry.Name, effort)
	})
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
}

// atSkip are entries the "@" menu hides as noise.
var atSkip = map[string]bool{".git": true, "node_modules": true, ".DS_Store": true}

const filePreviewLimit = 256 * 1024
const fileRefSearchLimit = 20

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

func workspacePath(rel string) (string, bool, error) {
	base, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
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
// alphabetical) for the "@" file-reference menu. rel resolves against the process
// cwd; "" lists the cwd. The menu navigates one level at a time, never
// recursively — bounded for huge trees.
func (a *App) ListDir(rel string) []DirEntry {
	base, err := os.Getwd()
	if err != nil {
		return []DirEntry{}
	}
	dir := base
	if rel != "" {
		if filepath.IsAbs(rel) {
			dir = filepath.Clean(rel)
		} else {
			dir = filepath.Join(base, rel)
		}
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return []DirEntry{}
	}
	dirs, files := []DirEntry{}, []DirEntry{}
	for _, e := range es {
		name := e.Name()
		if atSkip[name] {
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
	base, err := os.Getwd()
	if err != nil {
		return []DirEntry{}
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
	path, ok, err := workspacePath(rel)
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
	path, ok, err := workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return openWorkspacePath(path)
}

// RevealWorkspacePath shows a workspace file in the native file manager.
func (a *App) RevealWorkspacePath(rel string) error {
	path, ok, err := workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
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
	if a.activeSink() != nil {
		a.activeSink().Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
	}
}

func (a *App) runEffortCommand(input string) {
	entry, err := a.currentProviderEntry()
	if err != nil {
		a.notice("effort: " + err.Error())
		return
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		a.notice(fmt.Sprintf("effort is not configurable for %s", entry.Name))
		return
	}
	args := strings.Fields(input)
	if len(args) < 2 {
		a.notice(fmt.Sprintf("effort for %s: %s (default: %s; options: %s)", entry.Name, config.EffortDisplay(entry), cap.Default, strings.Join(cap.Levels, "|")))
		return
	}
	if len(args) > 2 {
		a.notice("usage: /effort " + strings.Join(cap.Levels, "|"))
		return
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		a.notice(err.Error())
		return
	}
	if err := a.SetEffort(args[1]); err != nil {
		a.notice("effort: " + err.Error())
		return
	}
	display := effort
	if display == "" {
		display = "auto"
	}
	a.notice(fmt.Sprintf("effort for %s set to %s", entry.Name, display))
}

func (a *App) currentProviderEntry() (*config.ProviderEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	a.mu.RLock()
	ref := ""
	if tab := a.activeTabLocked(); tab != nil {
		ref = tab.model
	}
	a.mu.RUnlock()
	if strings.TrimSpace(ref) == "" {
		ref = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", ref)
	}
	return entry, nil
}

// SavePastedImage stores a browser clipboard image data URL under
// .reasonix/attachments and returns the relative @-reference path.
func (a *App) SavePastedImage(dataURL string) (string, error) {
	return control.SaveImageDataURL(dataURL)
}

// SavePastedFile stores a dropped non-image file (the browser exposes its bytes
// as a data URL but not a real path) under .reasonix/attachments and returns the
// relative @-reference path.
func (a *App) SavePastedFile(name, dataURL string) (string, error) {
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
