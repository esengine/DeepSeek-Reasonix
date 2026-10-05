package hook

import (
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// A PreToolUse hook runs before every tool call, so a hook that is broken on
// this host reports the same failure forever. The user needs to see it once.
func TestRepeatedHookWarningSurfacesOnce(t *testing.T) {
	var notices []string
	r := NewRunner(nil, testenv.TempDir(t), nil, func(n Notice) { notices = append(notices, n.Text+" "+n.Detail) })
	broken := Outcome{
		Hook:     ResolvedHook{HookConfig: HookConfig{Command: reportedGuardHook}, Event: PreToolUse, Scope: ScopeGlobal},
		Decision: DecisionWarn,
		Stderr:   "'grep' is not recognized as an internal or external command",
	}
	for range 5 {
		r.handle(Report{Outcomes: []Outcome{broken}})
	}
	if len(notices) != 1 {
		t.Fatalf("identical warning surfaced %d times, want 1", len(notices))
	}

	// Something new from the same hook is still worth showing.
	changed := broken
	changed.Stderr = "permission denied"
	r.handle(Report{Outcomes: []Outcome{changed}})
	if len(notices) != 2 {
		t.Fatalf("changed message surfaced %d notices, want 2", len(notices))
	}

	// Blocks always surface: the message is how the user learns why a turn
	// stopped, and the second block is a second stopped turn.
	blocked := broken
	blocked.Decision = DecisionBlock
	for range 3 {
		r.handle(Report{Outcomes: []Outcome{blocked}, Blocked: true})
	}
	if len(notices) != 5 {
		t.Fatalf("blocks surfaced %d notices total, want 5", len(notices))
	}
}

// Two different hooks failing the same way are two separate problems.
func TestRepeatSuppressionIsPerHook(t *testing.T) {
	var notices []string
	r := NewRunner(nil, testenv.TempDir(t), nil, func(n Notice) { notices = append(notices, n.Text+" "+n.Detail) })
	for _, command := range []string{"first.sh", "second.sh"} {
		r.handle(Report{Outcomes: []Outcome{{
			Hook:     ResolvedHook{HookConfig: HookConfig{Command: command}, Event: PreToolUse, Scope: ScopeGlobal},
			Decision: DecisionWarn,
			Stderr:   "boom",
		}}})
	}
	if len(notices) != 2 {
		t.Fatalf("two hooks produced %d notices, want 2", len(notices))
	}
	for _, want := range []string{"first.sh", "second.sh"} {
		if !strings.Contains(strings.Join(notices, "\n"), want) {
			t.Fatalf("notice for %s missing from %v", want, notices)
		}
	}
}

// Editing hooks clears the suppression: a rule the user just changed reports
// against the new configuration, not the old one's history.
func TestReplaceClearsRepeatSuppression(t *testing.T) {
	var notices []string
	r := NewRunner(nil, testenv.TempDir(t), nil, func(n Notice) { notices = append(notices, n.Text+" "+n.Detail) })
	outcome := Outcome{
		Hook:     ResolvedHook{HookConfig: HookConfig{Command: "guard.sh"}, Event: PreToolUse, Scope: ScopeGlobal},
		Decision: DecisionWarn,
		Stderr:   "boom",
	}
	r.handle(Report{Outcomes: []Outcome{outcome}})
	r.handle(Report{Outcomes: []Outcome{outcome}})
	r.Replace(nil)
	r.handle(Report{Outcomes: []Outcome{outcome}})
	if len(notices) != 2 {
		t.Fatalf("notices = %d, want 2 (once before the edit, once after)", len(notices))
	}
}
