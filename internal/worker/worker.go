// Package worker manages background Reasonix worker instances spawned as
// subprocesses from within the agent loop. Workers run in full isolation
// and communicate results back through the manager.
package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Status string

const (
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
	Killed  Status = "killed"
)

type Job struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Status    Status    `json:"status"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`

	cmd    *exec.Cmd
	cancel context.CancelFunc
	mu     sync.RWMutex
}

func (j *Job) Snapshot() Job {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return Job{
		ID:        j.ID,
		Prompt:    j.Prompt,
		Status:    j.Status,
		Output:    j.Output,
		Error:     j.Error,
		StartedAt: j.StartedAt,
		EndedAt:   j.EndedAt,
	}
}

type Manager struct {
	mu       sync.RWMutex
	jobs     map[string]*Job
	seq      int64
	reasonix string
}

func NewManager(reasonixPath string) *Manager {
	if reasonixPath == "" {
		reasonixPath = "reasonix"
	}
	return &Manager{
		jobs:     map[string]*Job{},
		reasonix: reasonixPath,
	}
}

func (m *Manager) Spawn(prompt, cwd, model string, maxSteps int) string {
	id := fmt.Sprintf("wrk_%d", atomic.AddInt64(&m.seq, 1))
	if model == "" {
		model = "deepseek-flash"
	}
	if maxSteps <= 0 {
		maxSteps = 50
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{
		"run", "--model", model,
		"--max-steps", fmt.Sprintf("%d", maxSteps),
		prompt,
	}
	cmd := exec.CommandContext(ctx, m.reasonix, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	job := &Job{
		ID: id, Prompt: prompt, Status: Running,
		StartedAt: time.Now(), cmd: cmd, cancel: cancel,
	}
	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()
	go m.run(job)
	return id
}

func (m *Manager) run(j *Job) {
	output, err := j.cmd.CombinedOutput()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.EndedAt = time.Now()
	j.Output = string(output)
	if err != nil {
		if j.cmd.ProcessState != nil && !j.cmd.ProcessState.Exited() {
			j.Status = Killed
		} else {
			j.Status = Failed
			j.Error = err.Error()
		}
	} else {
		j.Status = Done
	}
}

func (m *Manager) Result(id string) (Job, bool) {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return Job{}, false
	}
	return j.Snapshot(), true
}

func (m *Manager) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j.Snapshot())
	}
	return out
}

func (m *Manager) Kill(id string) bool {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	j.mu.RLock()
	alive := j.Status == Running
	j.mu.RUnlock()
	if !alive {
		return false
	}
	j.cancel()
	return true
}

func (m *Manager) Cleanup(age time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-age)
	for id, j := range m.jobs {
		j.mu.RLock()
		status := j.Status
		ended := j.EndedAt
		j.mu.RUnlock()
		if status == Running {
			j.cancel()
		}
		if status != Running && (age == 0 || ended.Before(cutoff)) {
			delete(m.jobs, id)
		}
	}
}

func (m *Manager) Shutdown() { m.Cleanup(0) }

func ResolveReasonix() string {
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs
		}
	}
	return "reasonix"
}
