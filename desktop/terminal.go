package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

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

var errTerminalOutsideWorkspace = errors.New("outside workspace")

// resolveTerminalStartDir maps a workspace-relative path to the directory where
// an embedded terminal should start. Files resolve to their parent directory.
func resolveTerminalStartDir(base, rel string) (string, error) {
	path, ok, err := workspacePathForBase(base, rel)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return "", errTerminalOutsideWorkspace
		}
		return "", err
	}
	if !ok {
		return "", errTerminalOutsideWorkspace
	}
	if ext := filepath.Ext(path); ext != "" {
		path = filepath.Dir(path)
	}
	return path, nil
}

// StartTerminalAt launches a shell PTY at a workspace-relative path.
func (a *App) StartTerminalAt(rel string) string {
	return a.startTerminalRelative(rel)
}

// startTerminalRelative resolves a workspace-relative path and starts a terminal there.
func (a *App) startTerminalRelative(rel string) string {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "no workspace: " + err.Error()
	}
	path, err := resolveTerminalStartDir(base, rel)
	if err != nil {
		if errors.Is(err, errTerminalOutsideWorkspace) {
			return "invalid path (outside workspace)"
		}
		return "invalid path: " + err.Error()
	}
	// Start the terminal in a goroutine so the Wails RPC returns immediately.
	type result struct {
		id  string
		err string
	}
	ch := make(chan result, 1)
	go func() {
		id, err := a.startTerminalAt(path)
		if err != nil {
			ch <- result{err: "failed: " + err.Error()}
		} else {
			ch <- result{id: id}
		}
	}()
	select {
	case r := <-ch:
		return r.id
	case <-time.After(5 * time.Second):
		return "timeout: terminal did not start within 5s"
	}
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
