package control

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestClearSessionRefusesWhileTurnRuns(t *testing.T) {
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec})
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	err := c.ClearSession()
	if err == nil || !IsSessionRotationBusy(err) || !strings.Contains(err.Error(), "cannot clear while a turn is running") {
		t.Fatalf("ClearSession while running = %v, want classified busy error", err)
	}
}
