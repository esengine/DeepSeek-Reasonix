package pty

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
)

// DefaultSessionID is used when no session_id is explicitly provided.
const DefaultSessionID = "default"

type ctxKey struct{}
type noManager struct{}

// WithManager stamps ctx with the PTY manager so tools can reach it via FromContext.
func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// WithoutManager shadows an ancestor manager for isolated child runs.
func WithoutManager(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, noManager{})
}

// FromContext returns the PTY manager set on ctx, if any.
func FromContext(ctx context.Context) (*Manager, bool) {
	m, ok := ctx.Value(ctxKey{}).(*Manager)
	return m, ok && m != nil
}

// Manager coordinates all active persistent PTY sessions for a session/controller.
type Manager struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	defaultCwd string
	spec       sandbox.Spec
	closed     bool
}

// NewManager creates a new PTY session manager with the specified default working directory and optional sandbox spec.
func NewManager(defaultCwd string, spec ...sandbox.Spec) *Manager {
	if defaultCwd == "" {
		defaultCwd, _ = os.Getwd()
	}
	var sb sandbox.Spec
	if len(spec) > 0 {
		sb = spec[0]
	}
	return &Manager{
		sessions:   make(map[string]*Session),
		defaultCwd: defaultCwd,
		spec:       sb,
	}
}

// Start launches a new persistent PTY session according to opts.
func (m *Manager) Start(ctx context.Context, opts StartOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrSessionClosed
	}

	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = DefaultSessionID
	}

	if existing, ok := m.sessions[id]; ok && existing.IsRunning() {
		return nil, fmt.Errorf("%w: %q is already running (pid %d)", ErrSessionExists, id, existing.PID())
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" {
		cwd = m.defaultCwd
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(m.defaultCwd, cwd)
	}

	cmdPath := strings.TrimSpace(opts.Command)
	var args []string
	if cmdPath == "" {
		cmdPath = defaultShellPath()
	} else {
		fields := strings.Fields(cmdPath)
		cmdPath = fields[0]
		if len(fields) > 1 {
			args = append(fields[1:], opts.Args...)
		} else {
			args = opts.Args
		}
	}

	cols := opts.Cols
	if cols == 0 {
		cols = DefaultTerminalCols
	}
	rows := opts.Rows
	if rows == 0 {
		rows = DefaultTerminalRows
	}

	// Prepare safe base environment (redacting credentials if configured)
	baseEnv := secrets.ProcessEnv()
	envMap := make(map[string]string)
	for _, envStr := range baseEnv {
		parts := strings.SplitN(envStr, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	// Terminal capability overrides
	envMap["TERM"] = "xterm-256color"
	envMap["COLORTERM"] = "truecolor"
	for k, v := range opts.Env {
		envMap[k] = v
	}
	cmdEnv := make([]string, 0, len(envMap))
	for k, v := range envMap {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}

	var cmd *exec.Cmd
	if m.spec.Enforce() {
		sh := sandbox.ResolveShell("", "", nil)
		shellCmd := cmdPath
		if len(args) > 0 {
			shellCmd += " " + strings.Join(args, " ")
		}
		argv, wrapped := sandbox.Command(m.spec, sh, shellCmd)
		if wrapped && len(argv) > 0 {
			cmd = exec.Command(argv[0], argv[1:]...)
		} else {
			cmd = exec.Command(cmdPath, args...)
		}
	} else {
		cmd = exec.Command(cmdPath, args...)
	}
	cmd.Dir = cwd
	cmd.Env = cmdEnv

	lowPTY, err := spawnOSPTY(cmd, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("pty spawn failed: %w", err)
	}

	now := time.Now()
	sess := &Session{
		id:         id,
		command:    cmdPath,
		cwd:        cwd,
		startedAt:  now,
		lastActive: now,
		cols:       cols,
		rows:       rows,
		cmd:        cmd,
		lowPTY:     lowPTY,
		buffer:     NewRingBuffer(DefaultBufferSize),
		done:       make(chan struct{}),
		onOutput:   make(chan struct{}, 64),
	}
	sess.running.Store(true)

	go sess.startOutputPump()
	go sess.monitorProcess()

	m.sessions[id] = sess
	return sess, nil
}

// Get returns the session matching id, or ErrSessionNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id == "" {
		id = DefaultSessionID
	}
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	return sess, nil
}

// GetOrCreate returns an existing running session or starts a new default one.
func (m *Manager) GetOrCreate(ctx context.Context, id string, cwd string) (*Session, error) {
	if id == "" {
		id = DefaultSessionID
	}

	m.mu.RLock()
	sess, ok := m.sessions[id]
	running := ok && sess.IsRunning()
	m.mu.RUnlock()

	if running {
		return sess, nil
	}

	return m.Start(ctx, StartOptions{
		ID:  id,
		Cwd: cwd,
	})
}

// List returns snapshots of all managed sessions sorted by startedAt.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]SessionInfo, 0, len(m.sessions))
	for _, sess := range m.sessions {
		result = append(result, sess.Info())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// Write delivers input to the target session and optionally waits for output.
func (m *Manager) Write(ctx context.Context, id string, input string, waitBudget time.Duration) (string, error) {
	sess, err := m.Get(id)
	if err != nil {
		return "", err
	}
	return sess.Write(ctx, input, waitBudget)
}

// Read returns unread output from the target session.
func (m *Manager) Read(id string, maxBytes int) (string, error) {
	sess, err := m.Get(id)
	if err != nil {
		return "", err
	}
	return sess.Read(maxBytes), nil
}

// Close terminates a single managed session.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "" {
		id = DefaultSessionID
	}
	sess, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	delete(m.sessions, id)
	return sess.Close()
}

// CloseAll terminates all managed PTY processes during teardown.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	var errs []error
	for id, sess := range m.sessions {
		if err := sess.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing pty session %q: %w", id, err))
		}
		delete(m.sessions, id)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing pty sessions: %v", errs)
	}
	return nil
}

// Resize resizes the terminal window of the specified session.
func (m *Manager) Resize(id string, cols, rows uint16) error {
	sess, err := m.Get(id)
	if err != nil {
		return err
	}
	return sess.Resize(cols, rows)
}

// PendingInput returns any partially typed unsubmitted command line in the specified session.
func (m *Manager) PendingInput(id string) string {
	if id == "" {
		id = DefaultSessionID
	}
	m.mu.RLock()
	sess := m.sessions[id]
	m.mu.RUnlock()
	if sess != nil {
		return sess.PendingInput()
	}
	return ""
}

// ResetPendingInput discards any pending input and optionally cancels the line in the specified session.
func (m *Manager) ResetPendingInput(id string, sendCancel bool) {
	if id == "" {
		id = DefaultSessionID
	}
	m.mu.RLock()
	sess := m.sessions[id]
	m.mu.RUnlock()
	if sess != nil {
		sess.ResetPendingInput(sendCancel)
	}
}
