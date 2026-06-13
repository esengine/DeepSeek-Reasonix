package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type AgentStatus string

const (
	StatusIdle    AgentStatus = "ready"
	StatusRunning AgentStatus = "running"
	StatusDone    AgentStatus = "done"
)

type ManagedAgent struct {
	Name       string
	Ctrl       *control.Controller
	Sink       *SinkMultiplexer
	Inbox      *Inbox
	Config     config.OrchestratorAgentEntry

	mu         sync.Mutex
	status     AgentStatus
	lastTask   string
	lastResult string
	lastError  string
}

func NewManagedAgent(name string, ctrl *control.Controller, sink *SinkMultiplexer, cfg config.OrchestratorAgentEntry) *ManagedAgent {
	return &ManagedAgent{
		Name:   name,
		Ctrl:   ctrl,
		Sink:   sink,
		Inbox:  NewInbox(),
		Config: cfg,
		status: StatusIdle,
	}
}

func (a *ManagedAgent) Status() AgentStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *ManagedAgent) setStatus(s AgentStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = s
}

func (a *ManagedAgent) LastTask() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTask
}

func (a *ManagedAgent) setLastTask(t string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastTask = t
}

func (a *ManagedAgent) LastResult() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastResult
}

func (a *ManagedAgent) setLastResult(r string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastResult = r
}

func (a *ManagedAgent) LastError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastError
}

func (a *ManagedAgent) setLastError(err string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastError = err
}

func (a *ManagedAgent) Usage() *provider.Usage {
	return a.Ctrl.LastUsage()
}

func (a *ManagedAgent) SessionUsage() agent.SessionUsageMeta {
	return a.Ctrl.SessionUsage()
}

func (a *ManagedAgent) TurnCount() int {
	return a.Ctrl.Turn()
}

func (a *ManagedAgent) Run(ctx context.Context, task string) (string, error) {
	a.setStatus(StatusRunning)
	a.setLastTask(task)
	a.setLastResult("")
	a.setLastError("")

	a.Sink.Emit(event.Event{
		Kind: event.Notice,
		Text: fmt.Sprintf("%s started: %s", a.Name, task),
	})

	err := a.Ctrl.RunTurn(ctx, task)

	a.Sink.Emit(event.Event{
		Kind: event.Notice,
		Text: fmt.Sprintf("%s done", a.Name),
	})

	if a.Config.Persist && a.Ctrl.SessionPath() != "" {
		if snapErr := a.Ctrl.Snapshot(); snapErr != nil {
			slog.Warn("orchestrator: snapshot failed", "agent", a.Name, "error", snapErr)
		}
	}

	var result string
	if err == nil {
		hist := a.Ctrl.History()
		for i := len(hist) - 1; i >= 0; i-- {
			m := hist[i]
			if m.Role == provider.RoleAssistant && m.Content != "" {
				result = m.Content
				break
			}
		}
	}

	a.setStatus(StatusIdle)
	if err != nil {
		a.setLastError(err.Error())
		a.setLastResult(result)
		return result, err
	}
	a.setLastResult(result)
	return result, nil
}
