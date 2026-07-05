package agentview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
)

type SessionState string

const (
	StateWorking   SessionState = "working"
	StateNeedsInput SessionState = "needs_input"
	StateIdle      SessionState = "idle"
	StateCompleted SessionState = "completed"
	StateFailed    SessionState = "failed"
	StateStopped   SessionState = "stopped"
	StateSleeping  SessionState = "sleeping"
)

type SessionInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	State        SessionState `json:"state"`
	Summary      string       `json:"summary"`
	LastActivity time.Time    `json:"last_activity"`
	CreatedAt    time.Time    `json:"created_at"`
	Workspace    string       `json:"workspace"`
	Model        string       `json:"model"`
	Pinned       bool         `json:"pinned"`
	PullRequests []string     `json:"pull_requests,omitempty"`
	Running      bool         `json:"running"`
	LoopCount    int          `json:"loop_count,omitempty"`
	NextWakeAt   *time.Time   `json:"next_wake_at,omitempty"`
}

type Manager struct {
	baseDir string
	mu      sync.RWMutex
	sessions map[string]*SessionInfo
	order   []string
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:  baseDir,
		sessions: map[string]*SessionInfo{},
		order:    []string{},
	}
}

func (m *Manager) sessionsDir() string {
	return filepath.Join(m.baseDir, "agent-view")
}

func (m *Manager) statePath(id string) string {
	return filepath.Join(m.sessionsDir(), id+".json")
}

func (m *Manager) Load() error {
	dir := m.sessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if id == "" {
			continue
		}
		info, err := loadSessionInfo(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		m.sessions[id] = info
		m.order = append(m.order, id)
	}

	sort.Slice(m.order, func(i, j int) bool {
		si := m.sessions[m.order[i]]
		sj := m.sessions[m.order[j]]
		return statePriority(si.State) < statePriority(sj.State) ||
			(statePriority(si.State) == statePriority(sj.State) && si.LastActivity.After(sj.LastActivity))
	})

	return nil
}

func loadSessionInfo(path string) (*SessionInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info SessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func statePriority(state SessionState) int {
	switch state {
	case StateNeedsInput:
		return 0
	case StateWorking:
		return 1
	case StateSleeping:
		return 2
	case StateIdle:
		return 3
	case StateCompleted:
		return 4
	case StateFailed:
		return 5
	case StateStopped:
		return 6
	default:
		return 7
	}
}

func (m *Manager) save(info *SessionInfo) error {
	dir := m.sessionsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := m.statePath(info.ID)
	tmp, err := os.CreateTemp(dir, ".agent-session-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func (m *Manager) Register(id, name, workspace, model string) (*SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	info := &SessionInfo{
		ID:           id,
		Name:         name,
		State:        StateWorking,
		Summary:      "Starting...",
		LastActivity: now,
		CreatedAt:    now,
		Workspace:    workspace,
		Model:        model,
		Running:      true,
	}

	m.sessions[id] = info
	m.order = append(m.order, id)

	if err := m.save(info); err != nil {
		return nil, err
	}

	return info, nil
}

func (m *Manager) UpdateState(id string, state SessionState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return
	}
	info.State = state
	info.LastActivity = time.Now().UTC()
	if state == StateCompleted || state == StateFailed || state == StateStopped {
		info.Running = false
	}
	_ = m.save(info)
}

func (m *Manager) UpdateSummary(id, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.sessions[id]
	if !ok {
		return
	}
	info.Summary = summary
	info.LastActivity = time.Now().UTC()
	_ = m.save(info)
}

func (m *Manager) Get(id string) (*SessionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	copy := *info
	return &copy, true
}

func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]SessionInfo, 0, len(m.order))
	for _, id := range m.order {
		if info, ok := m.sessions[id]; ok {
			out = append(out, *info)
		}
	}
	return out
}

func (m *Manager) ListByWorkspace(workspace string) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []SessionInfo
	for _, id := range m.order {
		if info, ok := m.sessions[id]; ok && info.Workspace == workspace {
			out = append(out, *info)
		}
	}
	return out
}

func (m *Manager) ByState(state SessionState) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []SessionInfo
	for _, id := range m.order {
		if info, ok := m.sessions[id]; ok && info.State == state {
			out = append(out, *info)
		}
	}
	return out
}

func (m *Manager) Pin(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[id]; ok {
		info.Pinned = true
		_ = m.save(info)
	}
}

func (m *Manager) Unpin(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[id]; ok {
		info.Pinned = false
		_ = m.save(info)
	}
}

func (m *Manager) Rename(id, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[id]; ok {
		info.Name = newName
		info.LastActivity = time.Now().UTC()
		_ = m.save(info)
	}
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[id]; !ok {
		return fmt.Errorf("session %q not found", id)
	}

	delete(m.sessions, id)
	for i, sid := range m.order {
		if sid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}

	path := m.statePath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (m *Manager) CountByState(state SessionState) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, info := range m.sessions {
		if info.State == state {
			count++
		}
	}
	return count
}

func (m *Manager) NeedsInputCount() int {
	return m.CountByState(StateNeedsInput)
}

func (m *Manager) AddPullRequest(id, pr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.sessions[id]; ok {
		info.PullRequests = append(info.PullRequests, pr)
		info.LastActivity = time.Now().UTC()
		_ = m.save(info)
	}
}
