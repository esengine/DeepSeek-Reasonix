package agentteam

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

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
)

type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Assignee    string     `json:"assignee"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Dependencies []string  `json:"dependencies"`
	Output      string     `json:"output,omitempty"`
	Priority    int        `json:"priority"`
	Tags        []string   `json:"tags,omitempty"`
	lockFile    string     `json:"-"`
}

type TaskList struct {
	dir  string
	mu   sync.RWMutex
	tasks map[string]*Task
	order []string
}

func NewTaskList(dir string) *TaskList {
	return &TaskList{
		dir:   dir,
		tasks: map[string]*Task{},
		order: []string{},
	}
}

func LoadTaskList(dir string) (*TaskList, error) {
	tl := NewTaskList(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return tl, nil
		}
		return nil, fmt.Errorf("read task directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if id == "" {
			continue
		}
		task, err := loadTask(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		tl.tasks[id] = task
		tl.order = append(tl.order, id)
	}
	sort.Slice(tl.order, func(i, j int) bool {
		ti := tl.tasks[tl.order[i]]
		tj := tl.tasks[tl.order[j]]
		if ti.Priority != tj.Priority {
			return ti.Priority > tj.Priority
		}
		return ti.CreatedAt.Before(tj.CreatedAt)
	})
	return tl, nil
}

func loadTask(path string) (*Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (tl *TaskList) SaveTask(t *Task) error {
	if strings.TrimSpace(tl.dir) == "" {
		return nil
	}
	if err := os.MkdirAll(tl.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(tl.dir, t.ID+".json")
	tmp, err := os.CreateTemp(tl.dir, ".task-*.tmp")
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

func (tl *TaskList) Create(task Task) (string, error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	if strings.TrimSpace(task.ID) == "" {
		task.ID = fmt.Sprintf("task_%d", time.Now().UnixNano())
	}
	if task.Status == "" {
		task.Status = TaskPending
	}
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now

	if _, exists := tl.tasks[task.ID]; exists {
		return "", fmt.Errorf("task %q already exists", task.ID)
	}

	tl.tasks[task.ID] = &task
	tl.order = append(tl.order, task.ID)

	if err := tl.SaveTask(&task); err != nil {
		return "", err
	}

	return task.ID, nil
}

func (tl *TaskList) Get(id string) (*Task, bool) {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	t, ok := tl.tasks[id]
	if !ok {
		return nil, false
	}
	copy := *t
	return &copy, true
}

func (tl *TaskList) List() []Task {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	out := make([]Task, 0, len(tl.order))
	for _, id := range tl.order {
		if t, ok := tl.tasks[id]; ok {
			out = append(out, *t)
		}
	}
	return out
}

func (tl *TaskList) Update(id string, updates Task) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	t, ok := tl.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}

	if updates.Title != "" {
		t.Title = updates.Title
	}
	if updates.Description != "" {
		t.Description = updates.Description
	}
	if updates.Status != "" {
		t.Status = updates.Status
		if updates.Status == TaskCompleted {
			now := time.Now().UTC()
			t.CompletedAt = &now
		}
	}
	if updates.Assignee != "" {
		t.Assignee = updates.Assignee
	}
	if updates.Output != "" {
		t.Output = updates.Output
	}
	if len(updates.Dependencies) > 0 {
		t.Dependencies = updates.Dependencies
	}
	if updates.Priority != 0 {
		t.Priority = updates.Priority
	}
	if len(updates.Tags) > 0 {
		t.Tags = updates.Tags
	}
	t.UpdatedAt = time.Now().UTC()

	return tl.SaveTask(t)
}

func (tl *TaskList) Delete(id string) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	if _, ok := tl.tasks[id]; !ok {
		return fmt.Errorf("task %q not found", id)
	}

	delete(tl.tasks, id)
	for i, tid := range tl.order {
		if tid == id {
			tl.order = append(tl.order[:i], tl.order[i+1:]...)
			break
		}
	}

	if strings.TrimSpace(tl.dir) != "" {
		path := filepath.Join(tl.dir, id+".json")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (tl *TaskList) Claim(memberID string) (*Task, bool) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	for _, id := range tl.order {
		t, ok := tl.tasks[id]
		if !ok || t.Status != TaskPending {
			continue
		}
		if !tl.dependenciesSatisfied(t) {
			continue
		}
		t.Status = TaskInProgress
		t.Assignee = memberID
		t.UpdatedAt = time.Now().UTC()
		if err := tl.SaveTask(t); err != nil {
			return nil, false
		}
		copy := *t
		return &copy, true
	}
	return nil, false
}

func (tl *TaskList) dependenciesSatisfied(t *Task) bool {
	for _, depID := range t.Dependencies {
		dep, ok := tl.tasks[depID]
		if !ok || dep.Status != TaskCompleted {
			return false
		}
	}
	return true
}

func (tl *TaskList) ByStatus(status TaskStatus) []Task {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	var out []Task
	for _, id := range tl.order {
		if t, ok := tl.tasks[id]; ok && t.Status == status {
			out = append(out, *t)
		}
	}
	return out
}

func (tl *TaskList) ByAssignee(memberID string) []Task {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	var out []Task
	for _, id := range tl.order {
		if t, ok := tl.tasks[id]; ok && t.Assignee == memberID {
			out = append(out, *t)
		}
	}
	return out
}

func (tl *TaskList) CountByStatus(status TaskStatus) int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	count := 0
	for _, t := range tl.tasks {
		if t.Status == status {
			count++
		}
	}
	return count
}

func (tl *TaskList) Len() int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return len(tl.tasks)
}
