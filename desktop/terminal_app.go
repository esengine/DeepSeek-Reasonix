package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const terminalEventChannel = "terminal:event"

type TerminalStartRequest struct {
	TabID         string `json:"tabId,omitempty"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	Cols          int    `json:"cols,omitempty"`
	Rows          int    `json:"rows,omitempty"`
}

type TerminalInfo struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
}

type TerminalEvent struct {
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Data      string `json:"data,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Shell     string `json:"shell,omitempty"`
	Err       string `json:"err,omitempty"`
}

type terminalSession struct {
	id     string
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan struct{}
}

func (a *App) TerminalStart(req TerminalStartRequest) (TerminalInfo, error) {
	cwd := a.terminalCwd(req)
	spec, err := resolveTerminalShell()
	if err != nil {
		return TerminalInfo{}, err
	}
	if err := a.TerminalStop(""); err != nil {
		return TerminalInfo{}, err
	}

	ctx, cancel := context.WithCancel(a.bootContext())
	cmd := exec.CommandContext(ctx, spec.name, spec.args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return TerminalInfo{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return TerminalInfo{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return TerminalInfo{}, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return TerminalInfo{}, err
	}

	info := TerminalInfo{
		SessionID: fmt.Sprintf("terminal-%d", time.Now().UnixNano()),
		Cwd:       cwd,
		Shell:     spec.label,
	}
	session := &terminalSession{id: info.SessionID, cancel: cancel, cmd: cmd, stdin: stdin, done: make(chan struct{})}
	a.terminalMu.Lock()
	a.terminal = session
	a.terminalMu.Unlock()

	a.emitTerminal(TerminalEvent{SessionID: info.SessionID, Kind: "started", Cwd: cwd, Shell: spec.label})
	go a.forwardTerminalPipe(info.SessionID, stdout)
	go a.forwardTerminalPipe(info.SessionID, stderr)
	go a.waitTerminal(info.SessionID, cmd, session.done)
	return info, nil
}

func (a *App) TerminalWrite(sessionID, data string) error {
	a.terminalMu.Lock()
	session := a.terminal
	a.terminalMu.Unlock()
	if session == nil || (sessionID != "" && session.id != sessionID) {
		return errors.New("terminal session is not running")
	}
	_, err := io.WriteString(session.stdin, data)
	return err
}

func (a *App) TerminalResize(sessionID string, cols, rows int) error {
	// The current implementation binds a real shell process with pipes. Size is
	// accepted so the frontend can keep the same contract when ConPTY support is
	// added later.
	return nil
}

func (a *App) TerminalStop(sessionID string) error {
	a.terminalMu.Lock()
	session := a.terminal
	if session == nil || (sessionID != "" && session.id != sessionID) {
		a.terminalMu.Unlock()
		return nil
	}
	a.terminal = nil
	a.terminalMu.Unlock()

	_ = session.stdin.Close()
	session.cancel()
	if session.cmd != nil && session.cmd.Process != nil {
		_ = session.cmd.Process.Kill()
	}
	select {
	case <-session.done:
	case <-time.After(750 * time.Millisecond):
	}
	return nil
}

func (a *App) terminalCwd(req TerminalStartRequest) string {
	if req.WorkspaceRoot != "" {
		if st, err := os.Stat(req.WorkspaceRoot); err == nil && st.IsDir() {
			return req.WorkspaceRoot
		}
	}
	if req.TabID != "" {
		a.mu.RLock()
		tab := a.tabs[req.TabID]
		a.mu.RUnlock()
		if tab != nil && tab.WorkspaceRoot != "" {
			if st, err := os.Stat(tab.WorkspaceRoot); err == nil && st.IsDir() {
				return tab.WorkspaceRoot
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return filepath.Clean(".")
}

func (a *App) forwardTerminalPipe(sessionID string, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			a.emitTerminal(TerminalEvent{SessionID: sessionID, Kind: "output", Data: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func (a *App) waitTerminal(sessionID string, cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	close(done)

	a.terminalMu.Lock()
	if a.terminal != nil && a.terminal.id == sessionID {
		a.terminal = nil
	}
	a.terminalMu.Unlock()

	event := TerminalEvent{SessionID: sessionID, Kind: "exit"}
	if err != nil && !strings.Contains(err.Error(), "killed") {
		event.Err = err.Error()
	}
	a.emitTerminal(event)
}

func (a *App) emitTerminal(e TerminalEvent) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, terminalEventChannel, e)
	}
}

type terminalShellSpec struct {
	name  string
	args  []string
	label string
}

func resolveTerminalShell() (terminalShellSpec, error) {
	if runtime.GOOS == "windows" {
		for _, name := range []string{"pwsh.exe", "pwsh", "powershell.exe", "powershell"} {
			path, err := exec.LookPath(name)
			if err == nil {
				label := "PowerShell"
				if strings.Contains(strings.ToLower(name), "pwsh") {
					label = "PowerShell"
				} else {
					label = "Windows PowerShell"
				}
				return terminalShellSpec{name: path, args: []string{"-NoLogo"}, label: label}, nil
			}
		}
		return terminalShellSpec{}, errors.New("PowerShell was not found on PATH")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return terminalShellSpec{name: shell, label: filepath.Base(shell)}, nil
}
