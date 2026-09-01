package builtin

import (
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// The provider tool contract is intentionally cache-stable: provider requests
// use Registry.Schemas, not SchemasForContext, so update_goal ships in every
// session's schema whether or not a goal turn is armed. That makes the
// description the only place a plain-session model is told not to call it —
// guard the precondition so it cannot be edited away silently.
func TestUpdateGoalDescriptionStatesTheActiveGoalPrecondition(t *testing.T) {
	goal, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	desc := goal.Description()
	for _, want := range []string{
		"Only valid during an active goal turn",
		"answer normally instead",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("update_goal description no longer states the precondition (%q missing): %s", want, desc)
		}
	}
}
