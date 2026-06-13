package main

import (
	"context"
	"encoding/json"
	"io"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// terminalSession holds one PTY shell instance.
type terminalSession struct {
	id     string
	cmd    *exec.Cmd
	pty    *os.File
	cancel context.CancelFunc
}

// terminalSessions manages multiple PTY sessions.
type terminalSessions struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	nextID   int
}

// terminalDataEvent is the Wails event name for PTY output.
const terminalDataEvent = "terminal:output"

// terminalOutput is the JSON payload sent via events.
type terminalOutput struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

func newTerminalSessions() *terminalSessions {
	return &terminalSessions{sessions: map[string]*terminalSession{}}
}

func (ts *terminalSessions) add(s *terminalSession) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	id := fmt.Sprintf("term-%d", ts.nextID)
	ts.nextID++
	s.id = id
	ts.sessions[id] = s
	return id
}

func (ts *terminalSessions) get(id string) *terminalSession {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.sessions[id]
}

func (ts *terminalSessions) remove(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.sessions, id)
}

func (ts *terminalSessions) stop(id string) {
	s := ts.get(id)
	if s != nil && s.cancel != nil {
		s.cancel()
	}
	ts.remove(id)
}

func (ts *terminalSessions) stopAll() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, s := range ts.sessions {
		if s.cancel != nil {
			s.cancel()
		}
	}
	ts.sessions = map[string]*terminalSession{}
}

// startTerminalAt launches a shell PTY at the given directory and returns its session ID.
func (a *App) startTerminalAt(dir string) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Verify directory before starting PTY.
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("directory not accessible: %w", err)
	}

	ctx, cancel := context.WithCancel(a.bootContext())
	cmd := exec.CommandContext(ctx, shell, "-i")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		cancel()
		return "", err
	}

	s := &terminalSession{cmd: cmd, pty: f, cancel: cancel}
	id := a.terms.add(s)

	// Stream PTY output to frontend.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				payload, _ := json.Marshal(terminalOutput{SessionID: id, Data: string(buf[:n])})
				runtime.EventsEmit(a.bootContext(), terminalDataEvent, string(payload))
			}
			if readErr != nil {
				if readErr != io.EOF {
					msg, _ := json.Marshal(terminalOutput{SessionID: id, Data: "\r\n[terminal closed: " + readErr.Error() + "]\r\n"})
					runtime.EventsEmit(a.bootContext(), terminalDataEvent, string(msg))
				}
				return
			}
		}
	}()

	// Wait for command exit.
	go func() {
		_ = cmd.Wait()
		a.terms.stop(id)
		msg, _ := json.Marshal(terminalOutput{SessionID: id, Data: "\r\n[process exited]\r\n"})
		runtime.EventsEmit(a.bootContext(), terminalDataEvent, string(msg))
	}()

	return id, nil
}

// StartTerminal launches a shell PTY at the workspace root. Returns session ID or error string.
func (a *App) StartTerminal() string {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "no workspace: " + err.Error()
	}
	id, err := a.startTerminalAt(base)
	if err != nil {
		return "failed: " + err.Error()
	}
	return id
}

// StartTerminalAt launches a shell PTY at a workspace-relative path.
func (a *App) StartTerminalAt(rel string) string {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "no workspace: " + err.Error()
	}
	path, ok, err := workspacePathForBase(base, rel)
	if err != nil || !ok {
		if err != nil {
			return "invalid path: " + err.Error()
		}
		return "invalid path (outside workspace)"
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	id, err := a.startTerminalAt(path)
	if err != nil {
		return "failed: " + err.Error()
	}
	return id
}

// TerminalInput writes data to a PTY session.
func (a *App) TerminalInput(sessionID string, data string) {
	s := a.terms.get(sessionID)
	if s == nil || s.pty == nil {
		return
	}
	_, _ = s.pty.Write([]byte(data))
}

// StopTerminal kills a PTY session.
func (a *App) StopTerminal(sessionID string) {
	a.terms.stop(sessionID)
}

// TerminalResize sets terminal size.
func (a *App) TerminalResize(sessionID string, cols int, rows int) {
	s := a.terms.get(sessionID)
	if s == nil || s.pty == nil {
		return
	}
	_ = pty.Setsize(s.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
