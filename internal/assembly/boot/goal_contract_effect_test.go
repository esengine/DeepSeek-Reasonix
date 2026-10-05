package boot

import (
	"context"
	"strings"
	"testing"

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

// TestEffectGoalContractIsStatedOnceAndNeverLost is the safety property the
// contract split and the fold supersede have to hold together: a turn may be
// reminded of a contract only while the model can still see one. The reminder
// points at the contract, so a request carrying the pointer and not the target
// leaves the model following a reference into nothing.
func TestEffectGoalContractIsStatedOnceAndNeverLost(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &compactionEffectProvider{bulk: strings.Repeat("work output line with detail. ", 400)}
	provider.Register("boot-goal-contract-effect", func(provider.Config) (provider.Provider, error) {
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
kind = "boot-goal-contract-effect"
model = "x"
context_window = 32000
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
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
	const reminderMark = "under the task contract stated earlier"

	contracts, reminders, folded, seenGoal := 0, 0, false, false
	for i, req := range rec.requests() {
		if isSummarizerRequest(req) {
			folded = true
			continue
		}
		text := userTextOf(req)
		hasContract := strings.Contains(text, contractMark)
		hasReminder := strings.Contains(text, reminderMark)
		if !hasContract && !hasReminder {
			continue // a request from before the goal was set
		}
		seenGoal = true
		if hasReminder && !hasContract {
			t.Fatalf("request %d reminds the model of a contract it can no longer see", i)
		}
		if hasContract {
			contracts++
		}
		if hasReminder {
			reminders++
		}
	}
	if !folded {
		t.Fatal("the fixture never compacted, so the post-fold restatement was never exercised")
	}
	if !seenGoal {
		t.Fatal("no request carried a goal block; the goal never reached the model")
	}
	if reminders == 0 {
		t.Fatal("every turn restated the full contract; the split never took effect")
	}
	t.Logf("agent requests with a contract=%d with a reminder=%d", contracts, reminders)
}

// The contract is stated once and superseded thereafter: at most the retained
// copy plus one fresh restatement, never one per turn the goal ever ran.
func TestEffectGoalContractDoesNotAccumulate(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

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
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
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
		if isSummarizerRequest(req) {
			continue
		}
		if n := strings.Count(userTextOf(req), contractMark); n > 2 {
			t.Fatalf("request %d carried the full contract %d times; copies are piling up", i, n)
		}
	}
}
