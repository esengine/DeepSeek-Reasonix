package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// A gate that only reports the failure leaves the model to guess: the observed
// behavior is a retry, which burns the repair budget in run_loop and can end the
// turn with no visible answer. complete_step already names the alternative in
// this same switch; update_goal now does too.
func TestUpdateGoalGateTellsAPlainSessionWhatToDoInstead(t *testing.T) {
	goal, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	outcome, blocked := contextualToolGateOutcome(context.Background(), goal, "update_goal")
	if !blocked {
		t.Fatal("update_goal was not gated outside an active goal turn")
	}
	if !strings.Contains(outcome.output, "only available while an active goal turn") {
		t.Fatalf("gate message lost its established prefix: %s", outcome.output)
	}
	for _, want := range []string{"do not retry it", "end the turn with your answer instead"} {
		if !strings.Contains(outcome.output, want) {
			t.Fatalf("gate message does not tell the model what to do instead (%q missing): %s", want, outcome.output)
		}
	}
}
