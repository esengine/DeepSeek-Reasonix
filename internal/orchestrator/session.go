package orchestrator

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"reasonix/internal/agent"
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
		a.Ctrl.SetSessionPath(path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		loaded, loadErr := agent.LoadSession(path)
		if loadErr != nil {
			slog.Warn("orchestrator: failed to load session for agent", "agent", a.Name, "path", path, "err", loadErr)
			continue
		}
		a.Ctrl.Resume(loaded, path)
	}
	return nil
}

func (o *Orchestrator) SessionPath(dir, name string) string {
	return sessionPath(dir, name)
}
