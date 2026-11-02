package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestProfileBudgetPreparationUsesTurnAdmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	parent := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("parent"), agent.Options{}, event.Discard)
	c := New(Options{Runner: parent, Executor: parent, Skills: []skill.Skill{{Name: "probe", RunAs: skill.RunSubagent}}, SkillRunner: func(ctx context.Context, _ skill.Skill, _ string, _ skill.SubagentRunOptions) (string, error) {
		if calls.Add(1) != 1 {
			return "unexpected concurrent run", nil
		}
		close(entered)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "released", nil
		}
	}})
	t.Cleanup(c.Close)
	defer close(release)
	done := make(chan error, 1)
	go func() { _, err := c.RunSubagentProfile(t.Context(), "probe", "first", false); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not enter")
	}
	_, second := c.RunSubagentProfile(t.Context(), "probe", "second", false)
	if !errors.Is(second, ErrTurnRunning) || calls.Load() != 1 {
		t.Errorf("second=%v calls=%d", second, calls.Load())
	}
	c.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("controller cancellation did not reach profile")
	}
}
