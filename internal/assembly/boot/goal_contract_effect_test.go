package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func userTextOf(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// The contract is stated once and superseded thereafter: at most the retained
// copy plus one fresh restatement, never one per turn the goal ever ran.
func TestEffectGoalContractDoesNotAccumulate(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-goal-accum-effect", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.5
recent_keep = 2

[[providers]]
name = "test-model"
kind = "boot-goal-accum-effect"
model = "x"
context_window = 32000
`)
	approveWorkspaceAt(t, home, dir)

	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// Converse first: a goal set on turn one lands in the pinned first user
	// turn, which no fold can reach, and the fixture would prove nothing.
	for _, prompt := range []string{"orient me", "and again"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}
	ctrl.SetGoal("ship the parser")
	for _, prompt := range []string{"start", "keep going", "keep going", "keep going",
		"keep going", "keep going", "keep going", "keep going"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatalf("Run(%q): %v", prompt, err)
		}
	}

	const contractMark = "Treat the user's goal as a task contract"
	for i, req := range rec.requests() {
		if n := strings.Count(userTextOf(req), contractMark); n > 2 {
			t.Fatalf("request %d carried the full contract %d times; copies are piling up", i, n)
		}
	}
}
