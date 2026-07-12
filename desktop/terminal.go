package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ptylib "github.com/aymanbagabas/go-pty"

	"reasonix/internal/config"
	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
)

const (
	terminalDataEvent      = "terminal:data"
	terminalExitEvent      = "terminal:exit"
	terminalHistoryMax     = 2 << 20
	terminalOutputBatchMax = 64 << 10
	terminalOutputFlush    = 16 * time.Millisecond
)

type TerminalSessionView struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Shell     string `json:"shell"`
	Snapshot  string `json:"snapshot,omitempty"`
	Sequence  uint64 `json:"sequence"`
}

type terminalDataPayload struct {
	TabID     string `json:"tabId"`
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
	Sequence  uint64 `json:"sequence"`
}

type terminalExitPayload struct {
	TabID     string `json:"tabId"`
	SessionID string `json:"sessionId"`
	Error     string `json:"error,omitempty"`
	Expected  bool   `json:"expected"`
}

type terminalManager struct {
	app      *App
	mu       sync.Mutex
	sessions map[string]*terminalSession
	nextID   atomic.Uint64
}

type terminalSession struct {
	manager   *terminalManager
	tabID     string
	id        string
	cwd       string
	shell     string
	pty       ptylib.Pty
	cmd       *ptylib.Cmd
	cancel    context.CancelFunc
	writeMu   sync.Mutex
	historyMu sync.Mutex
	history   []byte
	sequence  uint64
	closed    atomic.Bool
	expected  atomic.Bool
}

func newTerminalManager(app *App) *terminalManager {
	return &terminalManager{app: app, sessions: map[string]*terminalSession{}}
}

func clampTerminalSize(cols, rows int) (int, int) {
	if cols < 2 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	if cols > 1000 {
		cols = 1000
	}
	if rows > 500 {
		rows = 500
	}
	return cols, rows
}

func terminalShellArgs(shell sandbox.Shell) []string {
	if shell.Kind == sandbox.ShellPowerShell {
		return []string{"-NoLogo"}
	}
	return []string{"-l"}
}

func terminalEnvironment() []string {
	env := append([]string(nil), secrets.ProcessEnv()...)
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=Reasonix",
	)
	return env
}

func (a *App) terminalManager() *terminalManager {
	if a.terminals == nil {
		a.terminals = newTerminalManager(a)
	}
	return a.terminals
}

// TerminalStart starts or reattaches to the terminal owned by tabID. The cwd is
// always resolved from the backend tab binding; callers cannot inject a path.
func (a *App) TerminalStart(tabID string, cols, rows int) (TerminalSessionView, error) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return TerminalSessionView{}, errors.New("terminal tab id is required")
	}
	base, err := a.workspaceBaseForTab(tabID)
	if err != nil {
		return TerminalSessionView{}, err
	}
	cfg, err := config.LoadForRoot(base)
	if err != nil {
		return TerminalSessionView{}, fmt.Errorf("load terminal shell settings: %w", err)
	}
	shell := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, nil)
	return a.terminalManager().start(tabID, base, shell, cols, rows)
}

func (a *App) TerminalWrite(tabID, sessionID, data string) error {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode terminal input: %w", err)
	}
	return a.terminalManager().write(strings.TrimSpace(tabID), strings.TrimSpace(sessionID), raw)
}

func (a *App) TerminalResize(tabID, sessionID string, cols, rows int) error {
	return a.terminalManager().resize(strings.TrimSpace(tabID), strings.TrimSpace(sessionID), cols, rows)
}

func (a *App) TerminalClose(tabID, sessionID string) error {
	return a.terminalManager().close(strings.TrimSpace(tabID), strings.TrimSpace(sessionID))
}

func (m *terminalManager) start(tabID, cwd string, shell sandbox.Shell, cols, rows int) (TerminalSessionView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.sessions[tabID]; current != nil && !current.closed.Load() {
		current.resize(cols, rows)
		return current.view(), nil
	}

	pt, err := ptylib.New()
	if err != nil {
		return TerminalSessionView{}, fmt.Errorf("create terminal: %w", err)
	}
	cols, rows = clampTerminalSize(cols, rows)
	if err := pt.Resize(cols, rows); err != nil {
		_ = pt.Close()
		return TerminalSessionView{}, fmt.Errorf("resize terminal: %w", err)
	}
	ctx, cancel := context.WithCancel(m.app.bootContext())
	cmd := pt.CommandContext(ctx, shell.Path, terminalShellArgs(shell)...)
	cmd.Dir = cwd
	cmd.Env = terminalEnvironment()
	if err := cmd.Start(); err != nil {
		cancel()
		_ = pt.Close()
		return TerminalSessionView{}, fmt.Errorf("start terminal shell %q: %w", shell.Path, err)
	}

	session := &terminalSession{
		manager: m,
		tabID:   tabID,
		id:      fmt.Sprintf("terminal-%d-%d", time.Now().UnixMilli(), m.nextID.Add(1)),
		cwd:     cwd,
		shell:   shell.Path,
		pty:     pt,
		cmd:     cmd,
		cancel:  cancel,
	}
	m.sessions[tabID] = session
	go session.run()
	return session.view(), nil
}

func (m *terminalManager) session(tabID, sessionID string) (*terminalSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[tabID]
	if session == nil || session.closed.Load() {
		return nil, errors.New("terminal session is not running")
	}
	if sessionID != "" && session.id != sessionID {
		return nil, errors.New("terminal session changed")
	}
	return session, nil
}

func (m *terminalManager) write(tabID, sessionID string, data []byte) error {
	session, err := m.session(tabID, sessionID)
	if err != nil {
		return err
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	_, err = session.pty.Write(data)
	return err
}

func (m *terminalManager) resize(tabID, sessionID string, cols, rows int) error {
	session, err := m.session(tabID, sessionID)
	if err != nil {
		return err
	}
	return session.resize(cols, rows)
}

func (m *terminalManager) close(tabID, sessionID string) error {
	m.mu.Lock()
	session := m.sessions[tabID]
	if session == nil || (sessionID != "" && session.id != sessionID) {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, tabID)
	m.mu.Unlock()
	session.stop(true)
	return nil
}

func (m *terminalManager) closeTab(tabID string) {
	_ = m.close(tabID, "")
}

func (m *terminalManager) closeAll() {
	m.mu.Lock()
	sessions := make([]*terminalSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	clear(m.sessions)
	m.mu.Unlock()
	for _, session := range sessions {
		session.stop(true)
	}
}

func (s *terminalSession) view() TerminalSessionView {
	s.historyMu.Lock()
	snapshot := append([]byte(nil), s.history...)
	sequence := s.sequence
	s.historyMu.Unlock()
	return TerminalSessionView{
		SessionID: s.id,
		CWD:       s.cwd,
		Shell:     s.shell,
		Snapshot:  base64.StdEncoding.EncodeToString(snapshot),
		Sequence:  sequence,
	}
}

func (s *terminalSession) resize(cols, rows int) error {
	cols, rows = clampTerminalSize(cols, rows)
	return s.pty.Resize(cols, rows)
}

func (s *terminalSession) stop(expected bool) {
	if expected {
		s.expected.Store(true)
	}
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		_ = s.pty.Close()
	}
}

func (s *terminalSession) run() {
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		s.pumpOutput()
	}()
	waitErr := s.cmd.Wait()
	s.stop(s.expected.Load())
	<-outputDone

	s.manager.mu.Lock()
	if s.manager.sessions[s.tabID] == s {
		delete(s.manager.sessions, s.tabID)
	}
	s.manager.mu.Unlock()

	errorText := ""
	if waitErr != nil && !s.expected.Load() {
		errorText = waitErr.Error()
	}
	s.manager.emit(terminalExitEvent, terminalExitPayload{
		TabID:     s.tabID,
		SessionID: s.id,
		Error:     errorText,
		Expected:  s.expected.Load(),
	})
}

func (s *terminalSession) pumpOutput() {
	chunks := make(chan []byte, 8)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32<<10)
		for {
			n, err := s.pty.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				chunks <- chunk
			}
			if err != nil {
				return
			}
		}
	}()

	var batch []byte
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		sequence := s.appendHistory(batch)
		s.manager.emit(terminalDataEvent, terminalDataPayload{
			TabID:     s.tabID,
			SessionID: s.id,
			Data:      base64.StdEncoding.EncodeToString(batch),
			Sequence:  sequence,
		})
		batch = batch[:0]
	}

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				flush()
				return
			}
			batch = append(batch, chunk...)
			if len(batch) >= terminalOutputBatchMax {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				continue
			}
			if len(batch) == len(chunk) {
				timer.Reset(terminalOutputFlush)
			}
		case <-timer.C:
			flush()
		}
	}
}

func (s *terminalSession) appendHistory(data []byte) uint64 {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.sequence++
	if len(data) >= terminalHistoryMax {
		s.history = append(s.history[:0], data[len(data)-terminalHistoryMax:]...)
		return s.sequence
	}
	overflow := len(s.history) + len(data) - terminalHistoryMax
	if overflow > 0 {
		copy(s.history, s.history[overflow:])
		s.history = s.history[:len(s.history)-overflow]
	}
	s.history = append(s.history, data...)
	return s.sequence
}

func (m *terminalManager) emit(name string, payload any) {
	if m == nil || m.app == nil {
		return
	}
	m.app.runtimeEvents.Emit(m.app.bootContext(), name, payload)
}
