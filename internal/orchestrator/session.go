package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
)

func (o *Orchestrator) SaveSessions(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	o.mu.Lock()
	agents := make([]*ManagedAgent, 0, len(o.agents))
	for _, a := range o.agents {
		agents = append(agents, a)
	}
	o.mu.Unlock()

	for _, a := range agents {
		if !a.Config.Persist {
			continue
		}
		if err := a.Ctrl.Snapshot(); err != nil {
			return fmt.Errorf("save %s session: %w", a.Name, err)
		}
	}
	return nil
}

func (o *Orchestrator) LoadSessions(dir string) error {
	if dir == "" {
		return nil
	}

	o.mu.Lock()
	agents := make([]*ManagedAgent, 0, len(o.agents))
	for _, a := range o.agents {
		agents = append(agents, a)
	}
	o.mu.Unlock()

	for _, a := range agents {
		if !a.Config.Persist {
			continue
		}
		path := filepath.Join(dir, a.Name+".jsonl")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		a.Ctrl.Resume(nil, path)
	}
	return nil
}
