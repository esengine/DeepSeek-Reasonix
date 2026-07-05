package agentteam

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager 管理多个团队及其相关资源。
type Manager struct {
	baseDir string
	mu      sync.RWMutex
	teams   map[string]*Team
}

// NewManager 创建一个新的团队管理器。
func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
		teams:   map[string]*Team{},
	}
}

func (m *Manager) teamDir(name string) string {
	return filepath.Join(m.baseDir, "teams", sanitizeName(name))
}

func (m *Manager) taskDir(teamName string) string {
	return filepath.Join(m.baseDir, "tasks", sanitizeName(teamName))
}

func (m *Manager) mailboxDir(teamName string) string {
	return filepath.Join(m.baseDir, "mailboxes", sanitizeName(teamName))
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	if result == "" {
		result = "unnamed"
	}
	return result
}

// CreateTeam 创建一个新的团队。
func (m *Manager) CreateTeam(name, workspace string) (*Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("team name is required")
	}

	teamDir := m.teamDir(name)
	if _, err := os.Stat(teamDir); err == nil {
		return nil, fmt.Errorf("team %q already exists", name)
	}

	team := NewTeam(name, workspace)
	if err := team.Save(teamDir); err != nil {
		return nil, fmt.Errorf("save team: %w", err)
	}

	m.teams[strings.ToLower(name)] = team
	return team, nil
}

// GetTeam 根据名称获取团队。
func (m *Manager) GetTeam(name string) (*Team, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	team, ok := m.teams[strings.ToLower(name)]
	if ok {
		return team, true
	}

	teamDir := m.teamDir(name)
	if _, err := os.Stat(teamDir); os.IsNotExist(err) {
		return nil, false
	}

	team, err := LoadTeam(teamDir)
	if err != nil {
		return nil, false
	}
	m.teams[strings.ToLower(name)] = team
	return team, true
}

// ListTeams 列出所有团队的名称。
func (m *Manager) ListTeams() []string {
	teamsDir := filepath.Join(m.baseDir, "teams")
	entries, err := os.ReadDir(teamsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// DeleteTeam 删除指定名称的团队及其所有资源。
func (m *Manager) DeleteTeam(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.teams, strings.ToLower(name))

	teamDir := m.teamDir(name)
	taskDir := m.taskDir(name)
	mailboxDir := m.mailboxDir(name)

	for _, dir := range []string{teamDir, taskDir, mailboxDir} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove team dir %q: %w", dir, err)
		}
	}

	return nil
}

// GetTaskList 获取指定团队的任务列表。
func (m *Manager) GetTaskList(teamName string) (*TaskList, error) {
	taskDir := m.taskDir(teamName)
	return LoadTaskList(taskDir)
}

// GetMailbox 获取指定团队的邮箱。
func (m *Manager) GetMailbox(teamName string) (*Mailbox, error) {
	mboxDir := m.mailboxDir(teamName)
	return LoadMailbox(mboxDir)
}

// SaveTaskList 保存指定团队的所有任务。
func (m *Manager) SaveTaskList(teamName string, tl *TaskList) error {
	for _, task := range tl.List() {
		t := task
		if err := tl.SaveTask(&t); err != nil {
			return fmt.Errorf("save task %q: %w", task.ID, err)
		}
	}
	return nil
}
