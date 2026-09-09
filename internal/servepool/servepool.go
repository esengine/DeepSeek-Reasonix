// Package servepool manages a pool of per-project `reasonix serve`
// sub-processes behind a single-entry HTTP gateway for remote clients
// (#8983). Serves are spawned lazily (first request for a stopped project),
// bind 127.0.0.1 with random ports, and are reclaimed after an idle window.
package servepool

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config controls the pool.
type Config struct {
	// ReasonixBin is the binary used to spawn serve sub-processes. Defaults
	// to os.Executable() (the desktop process is reasonix itself).
	ReasonixBin string
	// PortFileDir is where per-project serve.port / serve.token files are
	// written; defaults to the project's .reasonix directory.
	PortFileDir string
	// IdleTimeout is how long a project serve may sit without traffic before
	// being reclaimed. Default 15 minutes.
	IdleTimeout time.Duration
	// SpawnTimeout bounds waiting for a spawned serve to become ready.
	// Default 8 seconds.
	SpawnTimeout time.Duration
	// ProjectRoots is the initial project list (desktop-projects.json roots
	// or CLI config); RefreshProjects can update it later.
	ProjectRoots []string
}

// ProjectState mirrors the manifest entry a remote client sees.
type ProjectState struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Root    string `json:"root"`
	State   string `json:"state"` // stopped | starting | running | degraded | failed
	Sessions int   `json:"sessions,omitempty"`
	Err     string `json:"err,omitempty"`
}

// Manager owns the pool. All methods are safe for concurrent use.
type Manager struct {
	cfg     Config
	mu      sync.Mutex
	bin     string
	projects map[string]*project // keyed by project id (workspace slug)
	stop    chan struct{}
	done    chan struct{}
}

type project struct {
	state    string
	root     string
	id       string
	cmd      *exec.Cmd
	port     int
	token    string
	lastUse  time.Time
	failures int
	degradedUntil time.Time
	err      string
}

// NewManager builds a pool manager with the given config.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 15 * time.Minute
	}
	if cfg.SpawnTimeout == 0 {
		cfg.SpawnTimeout = 8 * time.Second
	}
	bin := cfg.ReasonixBin
	if bin == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("servepool: resolve own binary: %w", err)
		}
		bin = self
	}
	m := &Manager{
		cfg:      cfg,
		bin:      bin,
		projects: map[string]*project{},
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, root := range cfg.ProjectRoots {
		m.addProjectLocked(root)
	}
	go m.loop()
	return m, nil
}

// WorkspaceSlug mirrors config.WorkspaceSlug: the flat directory name used
// as the stable project id (avoids importing internal/config into the pool).
func WorkspaceSlug(root string) string {
	root = filepath.Clean(root)
	base := filepath.Base(root)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "workspace"
	}
	return base
}

func (m *Manager) addProjectLocked(root string) {
	root = filepath.Clean(root)
	if root == "" {
		return
	}
	id := WorkspaceSlug(root)
	if _, ok := m.projects[id]; ok {
		return
	}
	m.projects[id] = &project{state: "stopped", root: root, id: id}
}

// RefreshProjects replaces the project list, preserving running instances
// and marking newly removed projects for stop on next reclaim.
func (m *Manager) RefreshProjects(roots []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range roots {
		r = filepath.Clean(r)
		if r == "" {
			continue
		}
		seen[WorkspaceSlug(r)] = true
		m.addProjectLocked(r)
	}
	for id, p := range m.projects {
		if !seen[id] && p.state == "stopped" {
			delete(m.projects, id)
		}
	}
}

// Projects returns the manifest snapshot.
func (m *Manager) Projects() []ProjectState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ProjectState, 0, len(m.projects))
	for _, p := range m.projects {
		ps := ProjectState{ID: p.id, Root: p.root, State: p.state, Err: p.err}
		if p.port > 0 {
			ps.Name = filepath.Base(p.root)
		} else {
			ps.Name = filepath.Base(p.root)
		}
		out = append(out, ps)
	}
	return out
}

// Open ensures the project's serve is running and returns its id. It blocks
// until the serve is ready or the spawn times out.
func (m *Manager) Open(id string) error {
	m.mu.Lock()
	p, ok := m.projects[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("servepool: unknown project %q", id)
	}
	switch p.state {
	case "running", "starting":
		p.lastUse = time.Now()
		m.mu.Unlock()
		return nil
	case "degraded":
		if time.Now().Before(p.degradedUntil) {
			m.mu.Unlock()
			return fmt.Errorf("servepool: project %q is degraded (rapid crashes); retry later", id)
		}
		p.state = "stopped"
		p.failures = 0
	}
	p.state = "starting"
	p.err = ""
	m.mu.Unlock()
	return m.spawn(p)
}

// Touch records activity for the project (idle-reclaim accounting).
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok {
		p.lastUse = time.Now()
	}
}

// Port returns the bound port of a running project serve (0 when not running).
func (m *Manager) Port(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok && p.state == "running" {
		return p.port
	}
	return 0
}

// Token returns the per-project auth token (empty when not running).
func (m *Manager) Token(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok {
		return p.token
	}
	return ""
}

// Close stops every running serve and the manager loop.
func (m *Manager) Close() {
	close(m.stop)
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.projects {
		m.stopLocked(p)
	}
}

func (m *Manager) stopLocked(p *project) {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	p.cmd = nil
	p.port = 0
	p.token = ""
	if p.state != "degraded" {
		p.state = "stopped"
	}
	p.err = ""
}

// spawn starts the project's serve and waits for readiness.
func (m *Manager) spawn(p *project) error {
	portFile := filepath.Join(m.portFileDir(p.root), "serve.port")
	tokenFile := filepath.Join(m.portFileDir(p.root), "serve.token")
	if err := os.MkdirAll(filepath.Dir(portFile), 0o755); err != nil {
		m.markFailed(p, fmt.Errorf("mkdir port dir: %w", err))
		return err
	}
	token := newToken()
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		m.markFailed(p, fmt.Errorf("write token file: %w", err))
		return err
	}
	cmd := exec.Command(m.bin,
		"serve",
		"--addr", "127.0.0.1:0",
		"--port-file", portFile,
		"--auth", "token",
		"--token", token,
	)
	cmd.Dir = p.root
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		m.markFailed(p, fmt.Errorf("spawn serve: %w", err))
		return err
	}
	p.cmd = cmd
	p.token = token
	deadline := time.Now().Add(m.cfg.SpawnTimeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(portFile); err == nil {
			var port int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &port); err == nil && port > 0 {
				m.mu.Lock()
				p.port = port
				p.state = "running"
				p.lastUse = time.Now()
				p.failures = 0
				p.err = ""
				m.mu.Unlock()
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	p.cmd = nil
	m.markFailed(p, fmt.Errorf("serve did not become ready within %s", m.cfg.SpawnTimeout))
	return errors.New("servepool: " + p.err)
}

func (m *Manager) markFailed(p *project, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.err = err.Error()
	p.failures++
	if p.failures >= 3 {
		p.state = "degraded"
		p.degradedUntil = time.Now().Add(5 * time.Minute)
	} else {
		p.state = "stopped"
	}
}

func (m *Manager) portFileDir(root string) string {
	if m.cfg.PortFileDir != "" {
		return m.cfg.PortFileDir
	}
	return filepath.Join(root, ".reasonix")
}

// loop ticks health checks and idle reclamation.
func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

func (m *Manager) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, p := range m.projects {
		if p.state == "running" && now.Sub(p.lastUse) > m.cfg.IdleTimeout {
			m.stopLocked(p)
		}
	}
}

// RandomToken returns a 32-byte hex gateway/per-project token.
func RandomToken() string { return newToken() }

// newToken returns a 32-byte hex gateway/per-project token.
func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b)
}

