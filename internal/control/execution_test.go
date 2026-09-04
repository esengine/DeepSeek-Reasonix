package control

import (
	"strings"
	"testing"

	"reasonix/internal/execjournal"
)

// TestInterruptedExecutionBlockNamesEveryEntry: a block that says work was cut
// without saying which leaves the model to guess at scope, and a guess about
// half-finished work is worse than the silence it replaced.
func TestInterruptedExecutionBlockNamesEveryEntry(t *testing.T) {
	block := interruptedExecutionBlock([]execjournal.Entry{
		{ID: "call/fleet-1", Name: "research", Grant: "read"},
		{ID: "call/fleet-2", Name: "implement", Grant: "write"},
	})
	for _, want := range []string{"call/fleet-1", "call/fleet-2", "research", "implement", "read", "write"} {
		if !strings.Contains(block, want) {
			t.Errorf("block does not name %q:\n%s", want, block)
		}
	}
	if !strings.HasPrefix(block, "<interrupted-execution>") || !strings.HasSuffix(block, "</interrupted-execution>") {
		t.Errorf("block is not a closed element:\n%s", block)
	}
}

// TestNoSessionIsNotAnError: a run that persists nothing has no journal to read
// and must report nothing rather than failing. The delegation tools degrade the
// same way, so a headless run stays usable.
func TestNoSessionIsNotAnError(t *testing.T) {
	var c *Controller
	if got := c.InterruptedExecutions(); got != nil {
		t.Fatalf("nil controller returned %+v, want nil", got)
	}
}
