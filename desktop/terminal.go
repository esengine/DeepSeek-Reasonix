package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/secrets"
)

const (
	terminalOutputChannel    = "terminal:output"
	terminalExitChannel      = "terminal:exit"
	maxTerminalsPerWorkspace = 10
	terminalCloseWait        = 2 * time.Second
	defaultTerminalColumns   = 80
	defaultTerminalRows      = 24
	maxTerminalColumns       = 1000
	maxTerminalRows          = 500
	maxTerminalSnapshotBytes = 128 * 1024
)

var (
	errTerminalStaleTab   = errors.New("terminal request is no longer for the active tab")
	errTerminalRemote     = errors.New("integrated terminal is unavailable for remote workspaces")
	errTerminalOutside    = errors.New("terminal directory is outside the workspace")
	errTerminalManagerOff = errors.New("terminal manager is not available")
)

// TerminalSessionView is the renderer-safe snapshot of an interactive shell.
type TerminalSessionView struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Shell     string `json:"shell"`
	Cwd       string `json:"cwd"`
	CreatedAt int64  `json:"createdAt"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Running   bool   `json:"running"`
}

// TerminalShellView is a backend-approved shell choice. The renderer sends the
// stable ID back; it never sends an executable path.
type TerminalShellView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// TerminalWorkspaceView describes terminal capability for the active tab. All
// slices are initialized so Wails serializes empty values as [] rather than null.
type TerminalWorkspaceView struct {
	Available bool                  `json:"available"`
	ReadOnly  bool                  `json:"readOnly"`
	Reason    string                `json:"reason,omitempty"`
	Sessions  []TerminalSessionView `json:"sessions"`
	Shells    []TerminalShellView   `json:"shells"`
}

type terminalTarget struct {
	tabID         string
	workspaceRoot string
	workspaceKey  string
	readOnly      bool
}

type terminalCommand struct {
	path  string
	args  []string
	label string
}

type terminalStartSpec struct {
	command terminalCommand
	dir     string
	env     []string
	cols    int
	rows    int
}

type terminalProcess interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
	Wait() (int, error)
}

type terminalSession struct {
	view         TerminalSessionView
	tabID        string
	workspaceKey string
	process      terminalProcess
	readDone     chan struct{}
	done         chan struct{}
	output       []byte
}

type terminalManager struct {
	app *App

	mu            sync.Mutex
	sessions      map[string]*terminalSession
	byWorkspace   map[string][]string
	starting      map[string]int
	tabGeneration map[string]uint64
	closedTabIDs  map[string]struct{}
	closed        bool
	start         func(terminalStartSpec) (terminalProcess, error)
}

func newTerminalManager(app *App) *terminalManager {
	return &terminalManager{
		app:           app,
		sessions:      make(map[string]*terminalSession),
		byWorkspace:   make(map[string][]string),
		starting:      make(map[string]int),
		tabGeneration: make(map[string]uint64),
		closedTabIDs:  make(map[string]struct{}),
		start:         startTerminalProcess,
	}
}

func emptyTerminalWorkspaceView() TerminalWorkspaceView {
	return TerminalWorkspaceView{
		Sessions: []TerminalSessionView{},
		Shells:   []TerminalShellView{},
	}
}

// TerminalWorkspaceForTab returns the terminal state for the currently active
// tab. The backend owns workspace resolution; renderer-supplied filesystem roots
// are never accepted.
func (a *App) TerminalWorkspaceForTab(tabID string) (TerminalWorkspaceView, error) {
	view := emptyTerminalWorkspaceView()
	target, err := a.terminalTargetForTab(tabID, false)
	if err != nil {
		if errors.Is(err, errTerminalRemote) {
			view.Reason = err.Error()
			return view, nil
		}
		return view, err
	}
	view.ReadOnly = target.readOnly
	available, reason := terminalPlatformAvailable()
	view.Available = available
	view.Reason = reason
	view.Shells = terminalShellOptions()
	if a.terminals != nil {
		view.Sessions = a.terminals.list(target.workspaceKey)
	}
	return view, nil
}

// TerminalOutputForTab returns a bounded snapshot of the selected session's
// output. It is an explicit user action for adding terminal context to chat;
// terminal output is never injected into provider prompts automatically.
func (a *App) TerminalOutputForTab(tabID, sessionID string) (string, error) {
	target, err := a.terminalTargetForTab(tabID, false)
	if err != nil {
		return "", err
	}
	if a.terminals == nil {
		return "", errTerminalManagerOff
	}
	return a.terminals.snapshot(target.workspaceKey, sessionID), nil
}

// CreateTerminalForTab starts an interactive shell at a workspace-relative
// file or directory. Files resolve to their parent directory after an os.Stat;
// symlinked directories are checked against the canonical workspace root.
func (a *App) CreateTerminalForTab(tabID, rel, shellID string) (TerminalSessionView, error) {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return TerminalSessionView{}, err
	}
	available, reason := terminalPlatformAvailable()
	if !available {
		return TerminalSessionView{}, errors.New(reason)
	}
	dir, err := resolveTerminalStartDir(target.workspaceRoot, rel)
	if err != nil {
		return TerminalSessionView{}, err
	}
	command, err := resolveTerminalCommand(target.workspaceRoot, shellID)
	if err != nil {
		return TerminalSessionView{}, err
	}
	if err := a.revalidateTerminalTarget(target, true); err != nil {
		return TerminalSessionView{}, err
	}
	if a.terminals == nil {
		return TerminalSessionView{}, errTerminalManagerOff
	}
	return a.terminals.create(target.tabID, target.workspaceKey, dir, command)
}

func (a *App) WriteTerminalForTab(tabID, sessionID, data string) error {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return err
	}
	if a.terminals == nil {
		return errTerminalManagerOff
	}
	return a.terminals.write(target.workspaceKey, sessionID, []byte(data))
}

func (a *App) ResizeTerminalForTab(tabID, sessionID string, cols, rows int) error {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return err
	}
	if a.terminals == nil {
		return errTerminalManagerOff
	}
	return a.terminals.resize(target.workspaceKey, sessionID, cols, rows)
}

func (a *App) CloseTerminalForTab(tabID, sessionID string) error {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return err
	}
	if a.terminals == nil {
		return errTerminalManagerOff
	}
	return a.terminals.closeTerminal(target.workspaceKey, sessionID)
}

func (a *App) RenameTerminalForTab(tabID, sessionID, title string) error {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return err
	}
	if a.terminals == nil {
		return errTerminalManagerOff
	}
	return a.terminals.rename(target.workspaceKey, sessionID, title)
}

func (a *App) terminalTargetForTab(tabID string, requireWritable bool) (terminalTarget, error) {
	tabID = strings.TrimSpace(tabID)
	a.mu.RLock()
	activeID := a.activeTabID
	if tabID == "" {
		tabID = activeID
	}
	if tabID == "" || tabID != activeID {
		a.mu.RUnlock()
		return terminalTarget{}, errTerminalStaleTab
	}
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.RUnlock()
		return terminalTarget{}, errTerminalStaleTab
	}
	root := tab.WorkspaceRoot
	readOnly := tab.ReadOnly
	a.mu.RUnlock()

	if a.activeWorkbenchTargetIsRemote() {
		return terminalTarget{}, errTerminalRemote
	}
	if requireWritable && readOnly {
		return terminalTarget{}, readOnlyChannelErr()
	}
	base, err := workspaceBaseFromRoot(root)
	if err != nil {
		return terminalTarget{}, err
	}
	base, err = canonicalDirectory(base)
	if err != nil {
		return terminalTarget{}, fmt.Errorf("resolve terminal workspace: %w", err)
	}
	return terminalTarget{
		tabID:         tabID,
		workspaceRoot: base,
		workspaceKey:  tabID + "\x00" + filepath.Clean(base),
		readOnly:      readOnly,
	}, nil
}

func (a *App) revalidateTerminalTarget(target terminalTarget, requireWritable bool) error {
	a.mu.RLock()
	tab := a.tabByIDLocked(target.tabID)
	valid := tab != nil && a.activeTabID == target.tabID
	readOnly := valid && tab.ReadOnly
	root := ""
	if valid {
		root = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	if !valid || a.activeWorkbenchTargetIsRemote() {
		return errTerminalStaleTab
	}
	if requireWritable && readOnly {
		return readOnlyChannelErr()
	}
	base, err := workspaceBaseFromRoot(root)
	if err != nil {
		return err
	}
	base, err = canonicalDirectory(base)
	if err != nil || filepath.Clean(base) != target.workspaceRoot {
		return errTerminalStaleTab
	}
	return nil
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", real)
	}
	return filepath.Clean(real), nil
}

func resolveTerminalStartDir(workspaceRoot, rel string) (string, error) {
	workspaceRoot, err := canonicalDirectory(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", errTerminalOutside
	}
	target, ok, err := workspacePathForBase(workspaceRoot, rel)
	if err != nil || !ok {
		return "", errTerminalOutside
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("resolve terminal directory: %w", err)
	}
	if !info.IsDir() {
		target = filepath.Dir(target)
	}
	target, err = canonicalDirectory(target)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(workspaceRoot, target)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", errTerminalOutside
	}
	return target, nil
}

func terminalShellOptions() []TerminalShellView {
	options := []TerminalShellView{{ID: "default", Label: "Default shell"}}
	seen := map[string]bool{"default": true}
	add := func(id, label, binary string) {
		if seen[id] {
			return
		}
		if _, err := exec.LookPath(binary); err == nil {
			seen[id] = true
			options = append(options, TerminalShellView{ID: id, Label: label})
		}
	}
	if runtime.GOOS == "windows" {
		add("powershell", "PowerShell", "pwsh.exe")
		add("windows-powershell", "Windows PowerShell", "powershell.exe")
		add("cmd", "Command Prompt", "cmd.exe")
		add("bash", "Bash", "bash.exe")
		return options
	}
	add("zsh", "zsh", "zsh")
	add("bash", "bash", "bash")
	add("fish", "fish", "fish")
	add("sh", "sh", "sh")
	return options
}

func resolveTerminalCommand(_ string, shellID string) (terminalCommand, error) {
	shellID = strings.ToLower(strings.TrimSpace(shellID))
	if shellID == "" || shellID == "auto" {
		shellID = "default"
	}
	if shellID == "default" {
		if cfg, err := config.LoadUserConfigReadOnly(); err == nil {
			if command, ok := terminalCommandFromConfig(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path); ok {
				return command, nil
			}
		}
		return defaultTerminalCommand()
	}
	return namedTerminalCommand(shellID)
}

func terminalCommandFromConfig(prefer, configuredPath string) (terminalCommand, bool) {
	prefer = strings.ToLower(strings.TrimSpace(prefer))
	configuredPath = strings.TrimSpace(configuredPath)
	if prefer == "" || prefer == "auto" {
		return terminalCommand{}, false
	}
	if prefer != "bash" && prefer != "powershell" && prefer != "pwsh" {
		return terminalCommand{}, false
	}
	if configuredPath != "" {
		if path, err := exec.LookPath(configuredPath); err == nil {
			label := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			return commandForShellPath(path, label), true
		}
	}
	command, err := namedTerminalCommand(prefer)
	return command, err == nil
}

func defaultTerminalCommand() (terminalCommand, error) {
	if runtime.GOOS != "windows" {
		if path := strings.TrimSpace(os.Getenv("SHELL")); path != "" {
			if resolved, err := exec.LookPath(path); err == nil {
				return commandForShellPath(resolved, filepath.Base(resolved)), nil
			}
		}
		for _, id := range []string{"zsh", "bash", "fish", "sh"} {
			if command, err := namedTerminalCommand(id); err == nil {
				return command, nil
			}
		}
		return terminalCommand{}, errors.New("no interactive shell was found")
	}
	for _, id := range []string{"powershell", "windows-powershell", "cmd", "bash"} {
		if command, err := namedTerminalCommand(id); err == nil {
			return command, nil
		}
	}
	return terminalCommand{}, errors.New("no interactive shell was found")
}

func namedTerminalCommand(shellID string) (terminalCommand, error) {
	var binary, label string
	switch shellID {
	case "bash":
		binary, label = "bash", "bash"
	case "zsh":
		binary, label = "zsh", "zsh"
	case "fish":
		binary, label = "fish", "fish"
	case "sh":
		binary, label = "sh", "sh"
	case "powershell", "pwsh":
		binary, label = "pwsh", "PowerShell"
		if runtime.GOOS == "windows" {
			binary = "pwsh.exe"
		}
	case "windows-powershell":
		binary, label = "powershell.exe", "Windows PowerShell"
	case "cmd":
		binary, label = "cmd.exe", "Command Prompt"
	default:
		return terminalCommand{}, fmt.Errorf("unsupported terminal shell %q", shellID)
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return terminalCommand{}, fmt.Errorf("terminal shell %q is not installed", shellID)
	}
	return commandForShellPath(path, label), nil
}

func commandForShellPath(path, label string) terminalCommand {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	args := []string{}
	switch base {
	case "bash", "zsh", "sh", "ksh", "fish":
		args = []string{"-l"}
	case "pwsh", "powershell":
		args = []string{"-NoLogo"}
	case "cmd":
		args = []string{"/Q"}
	}
	return terminalCommand{path: path, args: args, label: label}
}

func terminalEnvironment(base []string) []string {
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if ok && (strings.EqualFold(key, "TERM") || strings.EqualFold(key, "COLORTERM")) {
			continue
		}
		env = append(env, item)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func (m *terminalManager) create(tabID, workspaceKey, dir string, command terminalCommand) (TerminalSessionView, error) {
	tabID = strings.TrimSpace(tabID)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TerminalSessionView{}, errTerminalManagerOff
	}
	if tabID == "" {
		m.mu.Unlock()
		return TerminalSessionView{}, errTerminalStaleTab
	}
	if _, closed := m.closedTabIDs[tabID]; closed {
		m.mu.Unlock()
		return TerminalSessionView{}, errTerminalStaleTab
	}
	generation := m.tabGeneration[tabID]
	if len(m.byWorkspace[workspaceKey])+m.starting[workspaceKey] >= maxTerminalsPerWorkspace {
		m.mu.Unlock()
		return TerminalSessionView{}, fmt.Errorf("terminal session limit reached (%d)", maxTerminalsPerWorkspace)
	}
	m.starting[workspaceKey]++
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.starting[workspaceKey]--
		if m.starting[workspaceKey] == 0 {
			delete(m.starting, workspaceKey)
		}
		m.mu.Unlock()
	}()

	id, err := newTerminalID()
	if err != nil {
		return TerminalSessionView{}, err
	}
	proc, err := m.start(terminalStartSpec{
		command: command,
		dir:     dir,
		env:     terminalEnvironment(secrets.ProcessEnv()),
		cols:    defaultTerminalColumns,
		rows:    defaultTerminalRows,
	})
	if err != nil {
		return TerminalSessionView{}, fmt.Errorf("start terminal: %w", err)
	}

	session := &terminalSession{
		view: TerminalSessionView{
			ID:        id,
			Title:     command.label,
			Shell:     command.label,
			Cwd:       dir,
			CreatedAt: time.Now().UnixMilli(),
			Running:   true,
		},
		tabID:        tabID,
		workspaceKey: workspaceKey,
		process:      proc,
		readDone:     make(chan struct{}),
		done:         make(chan struct{}),
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = proc.Close()
		return TerminalSessionView{}, errTerminalManagerOff
	}
	if _, closed := m.closedTabIDs[tabID]; closed {
		m.mu.Unlock()
		_ = proc.Close()
		return TerminalSessionView{}, errTerminalStaleTab
	}
	if m.tabGeneration[tabID] != generation {
		m.mu.Unlock()
		_ = proc.Close()
		return TerminalSessionView{}, errTerminalStaleTab
	}
	m.sessions[id] = session
	m.byWorkspace[workspaceKey] = append(m.byWorkspace[workspaceKey], id)
	view := session.view
	m.mu.Unlock()

	go m.readLoop(session)
	go m.waitLoop(session)
	return view, nil
}

func (m *terminalManager) snapshot(workspaceKey, sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[strings.TrimSpace(sessionID)]
	if session == nil || session.workspaceKey != workspaceKey {
		return ""
	}
	return string(session.output)
}

func appendTerminalSnapshot(current, data []byte) []byte {
	if len(data) >= maxTerminalSnapshotBytes {
		return append([]byte(nil), data[len(data)-maxTerminalSnapshotBytes:]...)
	}
	if over := len(current) + len(data) - maxTerminalSnapshotBytes; over > 0 {
		if over >= len(current) {
			current = current[:0]
		} else {
			current = append([]byte(nil), current[over:]...)
		}
	}
	return append(current, data...)
}

func (m *terminalManager) list(workspaceKey string) []TerminalSessionView {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.byWorkspace[workspaceKey]
	out := make([]TerminalSessionView, 0, len(ids))
	for _, id := range ids {
		if session := m.sessions[id]; session != nil {
			out = append(out, session.view)
		}
	}
	return out
}

func (m *terminalManager) sessionLocked(workspaceKey, sessionID string) (*terminalSession, error) {
	session := m.sessions[strings.TrimSpace(sessionID)]
	if session == nil || session.workspaceKey != workspaceKey {
		return nil, errors.New("terminal session not found in the active workspace")
	}
	return session, nil
}

func (m *terminalManager) write(workspaceKey, sessionID string, data []byte) error {
	m.mu.Lock()
	session, err := m.sessionLocked(workspaceKey, sessionID)
	if err == nil && !session.view.Running {
		err = errors.New("terminal session has exited")
	}
	var proc terminalProcess
	if err == nil {
		proc = session.process
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = proc.Write(data)
	return err
}

func (m *terminalManager) resize(workspaceKey, sessionID string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	if cols > maxTerminalColumns {
		cols = maxTerminalColumns
	}
	if rows > maxTerminalRows {
		rows = maxTerminalRows
	}
	m.mu.Lock()
	session, err := m.sessionLocked(workspaceKey, sessionID)
	var proc terminalProcess
	if err == nil && session.view.Running {
		proc = session.process
	}
	m.mu.Unlock()
	if err != nil || proc == nil {
		return err
	}
	return proc.Resize(cols, rows)
}

func (m *terminalManager) rename(workspaceKey, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("terminal title is required")
	}
	if len([]rune(title)) > 80 {
		return errors.New("terminal title is too long")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session, err := m.sessionLocked(workspaceKey, sessionID)
	if err != nil {
		return err
	}
	session.view.Title = title
	return nil
}

func (m *terminalManager) closeTerminal(workspaceKey, sessionID string) error {
	m.mu.Lock()
	session, err := m.sessionLocked(workspaceKey, sessionID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.removeSessionLocked(session)
	m.mu.Unlock()

	_ = session.process.Close()
	select {
	case <-session.done:
	case <-time.After(terminalCloseWait):
	}
	return nil
}

func (m *terminalManager) closeForTab(tabID string) {
	m.closeSessions(m.detachForTab(tabID))
}

// detachForTab closes the creation gate and removes every registered session
// without waiting on process I/O. Callers can use it while serializing an App
// capability transition, then close the returned processes after releasing
// App.mu.
func (m *terminalManager) detachForTab(tabID string) []*terminalSession {
	if m == nil {
		return nil
	}
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return nil
	}
	m.mu.Lock()
	m.closedTabIDs[tabID] = struct{}{}
	m.tabGeneration[tabID]++
	sessions := make([]*terminalSession, 0)
	for _, session := range m.sessions {
		if session.tabID != tabID {
			continue
		}
		m.removeSessionLocked(session)
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	return sessions
}

func (m *terminalManager) closeSessions(sessions []*terminalSession) {
	if m == nil || len(sessions) == 0 {
		return
	}
	for _, session := range sessions {
		_ = session.process.Close()
	}
	deadline := time.NewTimer(terminalCloseWait)
	defer deadline.Stop()
	for _, session := range sessions {
		select {
		case <-session.done:
		case <-deadline.C:
			return
		}
	}
}

func (m *terminalManager) reopenForTab(tabID string) {
	if m == nil {
		return
	}
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return
	}
	m.mu.Lock()
	if !m.closed {
		delete(m.closedTabIDs, tabID)
	}
	m.mu.Unlock()
}

func (m *terminalManager) removeSessionLocked(session *terminalSession) {
	delete(m.sessions, session.view.ID)
	ids := m.byWorkspace[session.workspaceKey]
	filtered := ids[:0]
	for _, id := range ids {
		if id != session.view.ID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		delete(m.byWorkspace, session.workspaceKey)
	} else {
		m.byWorkspace[session.workspaceKey] = filtered
	}
}

func (m *terminalManager) closeAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	sessions := make([]*terminalSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*terminalSession)
	m.byWorkspace = make(map[string][]string)
	m.mu.Unlock()

	for _, session := range sessions {
		_ = session.process.Close()
	}
	deadline := time.NewTimer(terminalCloseWait)
	defer deadline.Stop()
	for _, session := range sessions {
		select {
		case <-session.done:
		case <-deadline.C:
			return
		}
	}
}

func (m *terminalManager) readLoop(session *terminalSession) {
	defer close(session.readDone)
	buf := make([]byte, 8*1024)
	for {
		n, err := session.process.Read(buf)
		if n > 0 {
			active := false
			m.mu.Lock()
			if current := m.sessions[session.view.ID]; current == session {
				session.output = appendTerminalSnapshot(session.output, buf[:n])
				active = true
			}
			m.mu.Unlock()
			if active {
				m.emitOutput(session.view.ID, buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *terminalManager) waitLoop(session *terminalSession) {
	exitCode, waitErr := session.process.Wait()
	select {
	case <-session.readDone:
	case <-time.After(terminalCloseWait):
	}
	_ = session.process.Close()
	if waitErr != nil && exitCode == 0 {
		exitCode = -1
	}
	m.mu.Lock()
	removed := true
	if current := m.sessions[session.view.ID]; current == session {
		current.view.Running = false
		current.view.ExitCode = &exitCode
		removed = false
	}
	m.mu.Unlock()
	close(session.done)
	m.emitExit(session.view.ID, exitCode, removed)
}

func (m *terminalManager) emitOutput(id string, data []byte) {
	if m.app == nil || len(data) == 0 {
		return
	}
	m.app.emitRuntimeEvent(terminalOutputChannel, map[string]any{
		"id":   id,
		"data": base64.StdEncoding.EncodeToString(data),
	})
}

func (m *terminalManager) emitExit(id string, exitCode int, removed bool) {
	if m.app == nil {
		return
	}
	m.app.emitRuntimeEvent(terminalExitChannel, map[string]any{
		"id":       id,
		"exitCode": exitCode,
		"removed":  removed,
	})
}

func newTerminalID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "term-" + hex.EncodeToString(raw[:]), nil
}
