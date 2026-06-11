package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
)

func sessionPath(dir, name string) string {
	return filepath.Join(dir, "orchestrator_"+name+".jsonl")
}

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
		path := sessionPath(dir, a.Name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		a.Ctrl.Resume(nil, path)
		a.Ctrl.SetSessionPath(path)
	}
	return nil
}

func (o *Orchestrator) SessionPath(dir, name string) string {
	return sessionPath(dir, name)
}
