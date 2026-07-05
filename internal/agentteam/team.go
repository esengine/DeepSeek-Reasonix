package agentteam

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
)

type TeamStatus string

const (
	TeamActive   TeamStatus = "active"
	TeamCleaning TeamStatus = "cleaning"
	TeamDone     TeamStatus = "done"
)

type TeamMember struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	AgentType string    `json:"agent_type"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	SessionID string    `json:"session_id"`
	JoinedAt  time.Time `json:"joined_at"`
}

type TeamConfig struct {
	Name        string       `json:"name"`
	LeadID      string       `json:"lead_id"`
	Status      TeamStatus   `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Members     []TeamMember `json:"members"`
	Workspace   string       `json:"workspace"`
	Description string       `json:"description"`
}

type Team struct {
	config TeamConfig
	dir    string
	mu     sync.RWMutex
}

func NewTeam(name, workspace string) *Team {
	now := time.Now().UTC()
	return &Team{
		config: TeamConfig{
			Name:      name,
			Status:    TeamActive,
			CreatedAt: now,
			UpdatedAt: now,
			Members:   []TeamMember{},
			Workspace: workspace,
		},
	}
}

func LoadTeam(dir string) (*Team, error) {
	configPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read team config: %w", err)
	}
	var cfg TeamConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse team config: %w", err)
	}
	return &Team{
		config: cfg,
		dir:    dir,
	}, nil
}

func (t *Team) Save(dir string) error {
	if strings.TrimSpace(dir) == "" {
		dir = t.dir
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("team directory is required")
	}
	t.dir = dir

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	t.mu.RLock()
	cfg := t.config
	cfg.UpdatedAt = time.Now().UTC()
	t.mu.RUnlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".team-config.*.tmp")
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
	return fileutil.ReplaceFile(tmpPath, filepath.Join(dir, "config.json"))
}

func (t *Team) Name() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.Name
}

func (t *Team) Status() TeamStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.Status
}

func (t *Team) SetStatus(status TeamStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.config.Status = status
	t.config.UpdatedAt = time.Now().UTC()
}

func (t *Team) LeadID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.LeadID
}

func (t *Team) SetLead(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.config.LeadID = id
	t.config.UpdatedAt = time.Now().UTC()
}

func (t *Team) AddMember(member TeamMember) {
	t.mu.Lock()
	defer t.mu.Unlock()
	member.JoinedAt = time.Now().UTC()
	for i, m := range t.config.Members {
		if m.ID == member.ID {
			t.config.Members[i] = member
			t.config.UpdatedAt = time.Now().UTC()
			return
		}
	}
	t.config.Members = append(t.config.Members, member)
	t.config.UpdatedAt = time.Now().UTC()
}

func (t *Team) RemoveMember(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	members := make([]TeamMember, 0, len(t.config.Members))
	for _, m := range t.config.Members {
		if m.ID != id {
			members = append(members, m)
		}
	}
	t.config.Members = members
	t.config.UpdatedAt = time.Now().UTC()
}

func (t *Team) Members() []TeamMember {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]TeamMember, len(t.config.Members))
	copy(out, t.config.Members)
	return out
}

func (t *Team) GetMember(id string) (TeamMember, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, m := range t.config.Members {
		if m.ID == id {
			return m, true
		}
	}
	return TeamMember{}, false
}

func (t *Team) UpdateMemberStatus(id, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, m := range t.config.Members {
		if m.ID == id {
			t.config.Members[i].Status = status
			t.config.UpdatedAt = time.Now().UTC()
			return
		}
	}
}

func (t *Team) Workspace() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.Workspace
}

func (t *Team) Description() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config.Description
}

func (t *Team) SetDescription(desc string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.config.Description = desc
	t.config.UpdatedAt = time.Now().UTC()
}

func (t *Team) Config() TeamConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config
}
