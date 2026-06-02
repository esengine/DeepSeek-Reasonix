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
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/i18n"
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
// bindings and call straight through to one transport-agnostic control.Controller
// — the same controller the chat TUI and the HTTP/SSE server drive, assembled by
// the shared internal/boot. Events flow the other way: the controller emits to an
// eventSink that forwards each one to the webview via runtime.EventsEmit.
type App struct {
	ctx  context.Context
	sink *eventSink
	ctrl *control.Controller

	// mu protects ctrl, label, model, startupErr, and ready during the async
	// boot sequence. startup() spawns a goroutine for boot.Build(); all methods
	// that touch the controller acquire the lock.
	mu          sync.RWMutex
	startupErr  string
	label       string
	model       string // active provider name (for the bottom model switcher)
	ready       bool   // true once boot.Build completes (success or failure)
	disabledMCP map[string]ServerView
	mcpOrder    []string
}

// NewApp constructs the bound object. The controller is built later, in startup,
// once the Wails context exists.
func NewApp() *App { return &App{sink: &eventSink{}, disabledMCP: map[string]ServerView{}} }

// startup runs once the webview process is up, before the frontend can issue any
// bound call. It captures the Wails context (needed for EventsEmit), points the
// sink at it, then kicks off the entire initialization (workspace, config, build)
// in a background goroutine so the webview loads immediately. The frontend polls
// Meta() and sees Ready flip to true once the controller is assembled. RequireKey
// is false so a missing API key opens the window in a "set your key" state rather
// than failing to launch; a build error is surfaced through Meta instead of
// crashing the window.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.sink.ctx = ctx

	// Everything else — workspace resolution, config loading, i18n setup, and
	// boot.Build — runs in the background so the webview appears instantly.
	// During this window Meta().Ready is false and the frontend shows a loading
	// state; bound calls are no-ops (ctrl is nil).
	go a.buildController()
}

// buildController runs the full initialization sequence in a background goroutine:
// workspace resolution, config loading, i18n setup, and boot.Build. On success it
// wires up the controller and flips ready; on failure it stores the error so
// Meta().StartupErr surfaces it.
func (a *App) buildController() {
	ctx := a.ctx // captured by startup before this goroutine starts

	// A GUI launch starts in "/" (read-only); move into a real, writable working
	// folder (the remembered one, else home) before anything reads/writes config,
	// .env, memory, or skills relative to cwd.
	ensureWorkspace()

	// Resolve the active model to its canonical "provider/model" ref up front so
	// the switcher can mark it current.
	model := ""
	if cfg, err := config.Load(); err == nil {
		// Drive the Go-side catalogue (i18n.M) from the configured language so the
		// backend-provided slash UI — command descriptions, sub-command hints,
		// listing notices — comes through localized, matching the frontend.
		i18n.DetectLanguage(cfg.Language)
		model = cfg.DefaultModel
		if e, ok := cfg.ResolveModel(cfg.DefaultModel); ok {
			model = e.Name + "/" + e.Model
		}
	}

	a.mu.Lock()
	a.model = model
	a.mu.Unlock()

	ctrl, err := boot.Build(ctx, boot.Options{Model: model, RequireKey: false, Sink: a.sink})
	if err != nil {
		a.mu.Lock()
		a.startupErr = err.Error()
		a.ready = true
		a.mu.Unlock()
		runtime.EventsEmit(ctx, "agent:ready")
		return
	}

	a.mu.Lock()
	a.ctrl = ctrl
	a.label = ctrl.Label()
	a.ready = true
	a.mu.Unlock()

	// Desktop is interactive: route "ask" gate decisions to the frontend as
	// approval_request events, answered via Approve.
	ctrl.EnableInteractiveApproval()

	// Land auto-save in a fresh session file (same as a fresh chat/serve start).
	if dir := ctrl.SessionDir(); dir != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(dir, ctrl.Label()))
	}

	// Notify the frontend that the controller is ready — it re-fetches Meta,
	// ContextUsage, and History.
	runtime.EventsEmit(ctx, "agent:ready")
}

// shutdown snapshots the conversation and stops plugin subprocesses on close.
func (a *App) shutdown(context.Context) {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil {
		_ = ctrl.Snapshot()
		ctrl.Close()
	}
}

// --- bound command surface (frontend → controller) ---
// Each method guards on a nil controller so a pre-startup or failed-build call is
// a no-op, never a panic.

// Submit runs raw user input as a turn; slash commands and @-references are
// resolved by the controller. Output arrives asynchronously on eventChannel.
func (a *App) Submit(input string) {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil {
		ctrl.Submit(input)
	}
}

// SubmitDisplay runs input as a turn while recording a shorter UI-only display
// string for the saved desktop transcript. The model still receives input.
func (a *App) SubmitDisplay(display, input string) {
	if a.ctrl == nil {
		return
	}
	_ = recordSessionDisplay(config.SessionDir(), a.ctrl.SessionPath(), input, display)
	a.ctrl.Submit(input)
}

// Cancel aborts the in-flight turn.
func (a *App) Cancel() {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil {
		ctrl.Cancel()
	}
}

// Approve answers a pending approval_request by ID: allow runs the call, session
// also remembers the grant for the rest of the session.
func (a *App) Approve(id string, allow, session bool) {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil {
		ctrl.Approve(id, allow, session)
	}
}

// SetPlanMode toggles read-only plan mode.
func (a *App) SetPlanMode(on bool) {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil {
		ctrl.SetPlanMode(on)
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.Compact(a.ctx, "")
}

// NewSession snapshots the current conversation and rotates to a fresh one.
func (a *App) NewSession() error {
	a.mu.RLock()
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeFrom(a.ctx, turn)
}

func (a *App) SummarizeUpTo(turn int) error {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeUpTo(a.ctx, turn)
}

// SessionMeta summarises one saved session for the history panel.
type SessionMeta struct {
	Path    string `json:"path"`
	Preview string `json:"preview"`         // first user message
	Title   string `json:"title,omitempty"` // user-chosen name, when set (overrides preview)
	Turns   int    `json:"turns"`
	ModTime int64  `json:"modTime"` // unix milliseconds, for the frontend to group/format
	Current bool   `json:"current"`
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
	ctrl := a.ctrl
	a.mu.RUnlock()
	cur := ""
	if ctrl != nil {
		cur = ctrl.SessionPath()
	}
	out := make([]SessionMeta, 0, len(infos))
	for _, s := range infos {
		out = append(out, SessionMeta{
			Path:    s.Path,
			Preview: s.Preview,
			Title:   titles[filepath.Base(s.Path)],
			Turns:   s.Turns,
			ModTime: s.ModTime.UnixMilli(),
			Current: s.Path == cur,
		})
	}
	return out
}

// DeleteSession removes a saved session (and its title). It refuses the active
// session — that's the conversation on screen, and auto-save would recreate the
// file on the next turn; start a new session first to retire it.
func (a *App) DeleteSession(path string) error {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl != nil && ctrl.SessionPath() == path {
		return errActiveSession
	}
	return deleteSessionFile(config.SessionDir(), path)
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
	ctrl := a.ctrl
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

// PickWorkspace opens a folder chooser and, on a pick, switches the agent to that
// project: it re-roots the process there, rebuilds the controller from that
// folder's reasonix.toml + REASONIX.md, and starts a fresh session — the desktop
// analogue of opening a different project. The new controller is built before the
// old one is torn down, so a folder whose config can't load leaves the current
// session untouched. Returns the chosen path ("" if cancelled).
func (a *App) PickWorkspace() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose working folder",
		DefaultDirectory: cur,
	})
	if err != nil || dir == "" {
		return "", err // cancelled or error → no change
	}
	return a.SwitchWorkspace(dir)
}

func (a *App) ListWorkspaces() []WorkspaceMeta {
	cur, _ := os.Getwd()
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
			Current: path == cur,
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
	cur, _ := os.Getwd()
	if dir == cur {
		saveWorkspace(dir)
		return dir, nil
	}
	if err := os.Chdir(dir); err != nil {
		return "", err
	}
	// Resolve the new folder's default model from its own config.
	model := ""
	if cfg, cerr := config.Load(); cerr == nil {
		model = cfg.DefaultModel
		if e, ok := cfg.ResolveModel(cfg.DefaultModel); ok {
			model = e.Name + "/" + e.Model
		}
	}
	ctrl, err := boot.Build(a.ctx, boot.Options{Model: model, RequireKey: false, Sink: a.sink})
	if err != nil {
		_ = os.Chdir(cur) // roll back; the current session stays intact
		return "", err
	}
	saveWorkspace(dir) // remember it so the next launch reopens here
	// Commit the switch: save and tear down the old session, then swap in the new
	// project's controller with a fresh session file.
	a.mu.Lock()
	if a.ctrl != nil {
		_ = a.ctrl.Snapshot()
		a.ctrl.Close()
	}
	a.ctrl = ctrl
	a.model = model
	a.label = ctrl.Label()
	a.startupErr = ""
	a.mu.Unlock()
	ctrl.EnableInteractiveApproval()
	if d := ctrl.SessionDir(); d != "" {
		ctrl.SetSessionPath(agent.NewSessionPath(d, ctrl.Label()))
	}
	return dir, nil
}

// HistoryMessage is one prior turn, for the frontend to repopulate its transcript
// after a reload.
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// History returns the session's message log.
func (a *App) History() []HistoryMessage {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	msgs := ctrl.History()
	resolve := sessionDisplayResolver(config.SessionDir(), ctrl.SessionPath())
	out := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		content := m.Content
		if m.Role == provider.RoleUser {
			content = resolve(m.Content)
		}
		out = append(out, HistoryMessage{Role: string(m.Role), Content: content})
	}
	return out
}

// ContextInfo is the prompt-vs-window gauge payload. Both zero means no data yet.
type ContextInfo struct {
	Used   int `json:"used"`
	Window int `json:"window"`
}

// ContextUsage returns the latest context-window gauge numbers.
func (a *App) ContextUsage() ContextInfo {
	a.mu.RLock()
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	label := a.label
	startupErr := a.startupErr
	ready := a.ready
	ctrl := a.ctrl
	a.mu.RUnlock()
	cwd, _ := os.Getwd()
	return Meta{
		Label:        label,
		Ready:        ready,
		StartupErr:   startupErr,
		EventChannel: eventChannel,
		Cwd:          cwd,
		Bypass:       ctrl != nil && ctrl.Bypass(),
	}
}

// SetBypass toggles YOLO mode for the session: auto-approve every tool call
// (writers and bash run without asking). Deny rules still apply. Runtime-only —
// not written to config, so it resets on relaunch.
func (a *App) SetBypass(on bool) {
	a.mu.RLock()
	ctrl := a.ctrl
	a.mu.RUnlock()
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
		{Name: "memory", Description: i18n.M.CmdMemory, Kind: "builtin"},
		{Name: "mcp", Description: i18n.M.CmdMcp, Kind: "builtin"},
		{Name: "hooks", Description: i18n.M.CmdHooks, Kind: "builtin"},
		{Name: "skill", Description: i18n.M.CmdSkill, Kind: "builtin"},
	}
	a.mu.RLock()
	ctrl := a.ctrl
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
	ctrl := a.ctrl
	model := a.model
	a.mu.RUnlock()
	if ctrl == nil {
		return SlashArgsResult{}
	}
	data := control.ArgData{
		Skills:       ctrl.Skills(),
		CurrentModel: model,
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
// tool/prompt/resource counts) or "failed" (with the connection error).
type ServerView struct {
	Name      string     `json:"name"`
	Transport string     `json:"transport"`
	Status    string     `json:"status"`
	Tools     int        `json:"tools"`
	Prompts   int        `json:"prompts"`
	Resources int        `json:"resources"`
	Error     string     `json:"error,omitempty"`
	ToolList  []ToolView `json:"toolList,omitempty"`
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
}

// SkillRootView is one skill discovery root for the drawer's Sources section.
type SkillRootView struct {
	Dir        string `json:"dir"`
	Scope      string `json:"scope"`
	Priority   int    `json:"priority"`
	Status     string `json:"status"`
	Configured bool   `json:"configured"`
	Skills     int    `json:"skills"`
	Warning    string `json:"warning,omitempty"`
}

// Capabilities projects the session's MCP servers (connected + failed) and skills
// for the MCP & Skills drawer. Non-nil slices so the frontend can map over them.
func (a *App) Capabilities() CapabilitiesView {
	out := CapabilitiesView{Servers: []ServerView{}, Skills: []SkillView{}, SkillRoots: []SkillRootView{}}
	a.mu.RLock()
	ctrl := a.ctrl
	disabled := make(map[string]ServerView, len(a.disabledMCP))
	for name, s := range a.disabledMCP {
		disabled[name] = s
	}
	order := append([]string(nil), a.mcpOrder...)
	a.mu.RUnlock()
	if ctrl == nil {
		return out
	}
	seen := map[string]bool{}
	connected := map[string]bool{}
	retainedDisabled := map[string]ServerView{}
	codegraphConfigured := false
	if h := ctrl.Host(); h != nil {
		for _, s := range h.Servers() {
			seen[s.Name] = true
			connected[s.Name] = true
			out.Servers = append(out.Servers, ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				Tools: s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			})
		}
		for _, f := range h.Failures() {
			seen[f.Name] = true
			out.Servers = append(out.Servers, ServerView{
				Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error,
			})
		}
	}
	// Configured servers that are neither connected nor failed are toggled off
	// (disconnected this session, or auto_start=false) — shown with an off switch.
	if cfg, err := config.Load(); err == nil {
		codegraphConfigured = cfg.Codegraph.Enabled
		for _, p := range cfg.Plugins {
			if seen[p.Name] {
				continue
			}
			tt := p.Type
			if tt == "" {
				tt = "stdio"
			}
			if s, ok := disabled[p.Name]; ok {
				s.Status = "disabled"
				s.Transport = tt
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled[p.Name] = s
				seen[p.Name] = true
				delete(disabled, p.Name)
				continue
			}
			out.Servers = append(out.Servers, ServerView{Name: p.Name, Transport: tt, Status: "disabled"})
			seen[p.Name] = true
		}
	}
	for name, s := range disabled {
		if seen[name] {
			continue
		}
		if name != "codegraph" || !codegraphConfigured {
			continue
		}
		s.Status = "disabled"
		s.Error = ""
		out.Servers = append(out.Servers, s)
		retainedDisabled[name] = s
	}
	out.Servers = orderServerViews(out.Servers, order)

	a.mu.Lock()
	for name := range connected {
		delete(retainedDisabled, name)
	}
	a.disabledMCP = retainedDisabled
	a.mcpOrder = mergeServerOrder(a.mcpOrder, out.Servers)
	a.mu.Unlock()

	for _, s := range ctrl.Skills() {
		out.Skills = append(out.Skills, SkillView{
			Name: s.Name, Description: s.Description,
			Scope: string(s.Scope), RunAs: string(s.RunAs),
		})
	}
	out.SkillRoots = skillRootsView()
	return out
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
	for _, sk := range st.List() {
		counts[config.CanonicalSkillPath(filepath.Dir(skillRootPath(sk.Path)))]++
	}
	userConfigured := map[string]bool{}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			userConfigured[config.CanonicalSkillPath(p)] = true
		}
	}
	var out []SkillRootView
	for _, r := range st.Roots() {
		dir := config.CanonicalSkillPath(r.Dir)
		view := SkillRootView{
			Dir:        r.Dir,
			Scope:      string(r.Scope),
			Priority:   r.Priority + 1,
			Status:     string(r.Status),
			Configured: r.Scope == skill.ScopeCustom && userConfigured[dir],
			Skills:     counts[dir],
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
}

// AddMCPServer connects a server live and persists it to config (Customize → MCP →
// Add). Returns the number of tools it exposed.
func (a *App) AddMCPServer(in MCPServerInput) (int, error) {
	if a.ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	return a.ctrl.AddMCPServer(config.PluginEntry{
		Name:    in.Name,
		Type:    in.Transport,
		Command: in.Command,
		Args:    in.Args,
		URL:     in.URL,
		Env:     in.Env,
	})
}

// RemoveMCPServer disconnects a live server and drops it from config (the row's ✕).
func (a *App) RemoveMCPServer(name string) error {
	if a.ctrl == nil {
		return fmt.Errorf("no active session")
	}
	_, err := a.ctrl.RemoveMCPServer(name)
	if err == nil {
		a.mu.Lock()
		delete(a.disabledMCP, name)
		a.mcpOrder = removeServerOrder(a.mcpOrder, name)
		a.mu.Unlock()
	}
	return err
}

// RetryMCPServer reconnects a configured server that failed or was disconnected,
// without touching config (the failed row's retry button).
func (a *App) RetryMCPServer(name string) error {
	if a.ctrl == nil {
		return fmt.Errorf("no active session")
	}
	_, err := a.ctrl.ConnectConfiguredMCPServer(name)
	return err
}

// SetMCPServerEnabled is the connector toggle: on reconnects a configured server
// for this session, off disconnects it (config untouched either way — like Claude
// Code's per-conversation enable/disable, it resets on the next session start).
func (a *App) SetMCPServerEnabled(name string, enabled bool) error {
	if a.ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if enabled {
		_, err := a.ctrl.ConnectConfiguredMCPServer(name)
		if err == nil {
			a.mu.Lock()
			delete(a.disabledMCP, name)
			a.mu.Unlock()
		}
		return err
	}
	if s, ok := findMCPServerView(a.ctrl, name); ok {
		s.Status = "disabled"
		s.Error = ""
		a.mu.Lock()
		if a.disabledMCP == nil {
			a.disabledMCP = map[string]ServerView{}
		}
		a.disabledMCP[name] = s
		a.mcpOrder = mergeServerOrder(a.mcpOrder, []ServerView{s})
		a.mu.Unlock()
	}
	a.ctrl.DisconnectMCPServer(name)
	return nil
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

// Models flattens the configured providers into their (provider, model) pairs —
// the switcher's options — marking the active one. A vendor with a `models` list
// yields one entry per model, all sharing the same endpoint/key. Unconfigured
// providers are skipped. Result is non-nil: the frontend reads .length, so a nil
// slice (JSON null) would crash the switcher on an empty list.
func (a *App) Models() []ModelInfo {
	a.mu.RLock()
	curModel := a.model
	a.mu.RUnlock()
	cfg, err := config.Load()
	if err != nil {
		return nil
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
// the new model. (Switching models necessarily resets the prompt cache; that's the
// cost of the switch.) No-op if name is already active or the controller is down.
func (a *App) SetModel(name string) error {
	if a.ctx == nil || name == "" {
		return nil
	}
	a.mu.RLock()
	curModel := a.model
	ctrl := a.ctrl
	a.mu.RUnlock()
	if name == curModel {
		return nil
	}

	var carried []provider.Message
	if ctrl != nil {
		_ = ctrl.Snapshot()
		carried = ctrl.History()
		ctrl.Close()
	}

	newCtrl, err := boot.Build(a.ctx, boot.Options{Model: name, RequireKey: false, Sink: a.sink})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.ctrl = newCtrl
	a.model = name
	a.label = newCtrl.Label()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()

	path := ""
	if dir := newCtrl.SessionDir(); dir != "" {
		path = agent.NewSessionPath(dir, newCtrl.Label())
	}
	// Carry the prior conversation (full provider.Message log, incl. the system
	// prompt) into the new session so history is preserved across the switch.
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	return nil
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

// atSkip are entries the "@" menu hides as noise.
var atSkip = map[string]bool{".git": true, "node_modules": true, ".DS_Store": true}

const filePreviewLimit = 256 * 1024

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
		return nil
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
		return nil
	}
	var dirs, files []DirEntry
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
	ctrl := a.ctrl
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
// panel's explicit "remember" action, equivalent to typing "#<note>". An unknown
// scope falls back to project. Returns the file written.
func (a *App) Remember(scope, note string) (string, error) {
	a.mu.RLock()
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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
	ctrl := a.ctrl
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

// eventSink is the controller's event.Sink in desktop mode: it forwards every
// agent event to the webview as one runtime event, JSON-shaped by toWire. It is a
// type distinct from App so App's bound method set stays the clean command surface
// — Emit must not be exposed to JS. Emit runs on the agent goroutine;
// runtime.EventsEmit is goroutine-safe, and the ctx guard covers the brief window
// before startup assigns it.
type eventSink struct{ ctx context.Context }

func (s *eventSink) Emit(e event.Event) {
	if s.ctx == nil {
		return
	}
	runtime.EventsEmit(s.ctx, eventChannel, toWire(e))
}
