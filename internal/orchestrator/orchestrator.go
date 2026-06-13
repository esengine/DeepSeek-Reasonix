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
)

type Orchestrator struct {
	mainCtrl   *control.Controller
	agents     map[string]*ManagedAgent
	mainSink   event.Sink
	sessionDir string
	mu         sync.Mutex
}

func New(mainSink event.Sink) *Orchestrator {
	return &Orchestrator{
		agents:   make(map[string]*ManagedAgent),
		mainSink: mainSink,
	}
}

func (o *Orchestrator) SetMainCtrl(ctrl *control.Controller) {
	o.mainCtrl = ctrl
}

func (o *Orchestrator) MainCtrl() *control.Controller {
	return o.mainCtrl
}

func (o *Orchestrator) MainSink() event.Sink {
	return o.mainSink
}

func (o *Orchestrator) SetSessionDir(dir string) {
	o.sessionDir = dir
}

func (o *Orchestrator) SessionDir() string {
	return o.sessionDir
}

func (o *Orchestrator) AddAgent(name string, ctrl *control.Controller, cfg config.OrchestratorAgentEntry) {
	sink := NewSinkMultiplexer(o.mainSink, name)
	sink.SetVerbose(cfg.Verbose)
	agent := NewManagedAgent(name, ctrl, sink, cfg)
	o.mu.Lock()
	o.agents[name] = agent
	o.mu.Unlock()
}

func (o *Orchestrator) Agent(name string) (*ManagedAgent, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	a, ok := o.agents[name]
	return a, ok
}

func (o *Orchestrator) Agents() []*ManagedAgent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]*ManagedAgent, 0, len(o.agents))
	for _, a := range o.agents {
		out = append(out, a)
	}
	return out
}

func (o *Orchestrator) AgentNames() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, 0, len(o.agents))
	for name := range o.agents {
		out = append(out, name)
	}
	return out
}

func (o *Orchestrator) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, a := range o.agents {
		a.Ctrl.Close()
	}
}

func (o *Orchestrator) Stats(name string) string {
	agent, ok := o.Agent(name)
	if !ok {
		return fmt.Sprintf("agent %q not found", name)
	}

	usage := agent.SessionUsage()
	lastUsage := agent.Usage()
	status := agent.Status()

	s := fmt.Sprintf("%s: %s\n", name, status)
	s += fmt.Sprintf("  turns: %d\n", agent.TurnCount())
	s += fmt.Sprintf("  total tokens: %d (prompt %d + completion %d)\n",
		usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)
	s += fmt.Sprintf("  cache: %d hit / %d miss\n",
		usage.CacheHitTokens, usage.CacheMissTokens)
	if usage.Cost > 0 {
		s += fmt.Sprintf("  cost: %.4f %s\n", usage.Cost, usage.Currency)
	}
	if lastUsage != nil {
		s += fmt.Sprintf("  last turn: %d prompt + %d completion (%d total)\n",
			lastUsage.PromptTokens, lastUsage.CompletionTokens, lastUsage.TotalTokens)
	}
	if t := agent.LastTask(); t != "" {
		s += fmt.Sprintf("  last task: %s\n", t)
	}
	if r := agent.LastResult(); r != "" {
		maxLen := 200
		if len(r) > maxLen {
			r = r[:maxLen] + "..."
		}
		s += fmt.Sprintf("  last result: %s\n", r)
	}
	return s
}

func (o *Orchestrator) StatsAll() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var total agent.SessionUsageMeta
	s := ""

	for name, a := range o.agents {
		usage := a.SessionUsage()
		total.PromptTokens += usage.PromptTokens
		total.CompletionTokens += usage.CompletionTokens
		total.CacheHitTokens += usage.CacheHitTokens
		total.CacheMissTokens += usage.CacheMissTokens
		total.TotalTokens += usage.TotalTokens
		total.Cost += usage.Cost
		if usage.Currency != "" {
			total.Currency = usage.Currency
		}

		s += fmt.Sprintf("  %-12s %s  %d turns  %d tokens  $%.4f\n",
			name, a.Status(), a.TurnCount(), usage.TotalTokens, usage.Cost)
	}

	if len(o.agents) > 1 {
		s += fmt.Sprintf("  %-12s %s  %d turns  %d tokens  $%.4f\n",
			"── total ──", "", 0, total.TotalTokens, total.Cost)
	}

	return s
}

func (o *Orchestrator) SendMessage(ctx context.Context, to, message string) (string, error) {
	a, ok := o.Agent(to)
	if !ok {
		return "", fmt.Errorf("agent %q not found", to)
	}

	slog.Debug("orchestrator sending message", "to", to, "text", message)

	result, err := a.Run(ctx, message)
	if err != nil {
		return "", fmt.Errorf("agent %q failed: %w", to, err)
	}
	return result, nil
}
