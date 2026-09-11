package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
)

func TestGoalLegacyBudgetFlagNoticesExactlyOnce(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	t.Cleanup(m.ctrl.Close)

	m.runGoalSubcommand("/goal --research investigate the failure")

	joined := ansi.Strip(strings.Join(*m.pendingCommit, "\n"))
	if got := strings.Count(joined, control.GoalBudgetFlagDeprecatedNotice); got != 1 {
		t.Fatalf("deprecated budget notices = %d, want 1:\n%s", got, joined)
	}
}

func TestMissingLegacyGoalCommandDoesNotStartTUITurn(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{WorkspaceRoot: t.TempDir()})
	t.Cleanup(m.ctrl.Close)

	if cmd := m.runGoalSubcommand("/goal resume .reasonix/autoresearch/missing-task/"); cmd != nil {
		t.Fatal("missing legacy archive returned a provider turn command")
	}
	if got := m.ctrl.GoalStatus(); got != control.GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
}
