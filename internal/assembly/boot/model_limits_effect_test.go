package boot

import (
	"testing"

	"reasonix/internal/contract/event"
)

// A model with its own context_window is compacted against that window, not
// the connection's: asserted as a summary request reaching the provider.
func foldsFor(t *testing.T, kind, model, overrides string) int {
	t.Helper()
	rec, run := budgetFixture(t, kind, model, overrides, event.Discard)
	for _, prompt := range []string{"start the task", "keep going", "keep going", "keep going", "keep going"} {
		run(prompt)
	}
	n := 0
	for _, req := range rec.requests() {
		if isSummaryRequest(req) {
			n++
		}
	}
	return n
}

func TestEffectModelContextWindowOverrideDrivesCompaction(t *testing.T) {
	if got := foldsFor(t, "boot-limits-small", "small,large", smallWindow); got == 0 {
		t.Fatal("the model with a 32000 override never compacted; the connection's window was used")
	}
	if got := foldsFor(t, "boot-limits-large", "large,small", smallWindow); got != 0 {
		t.Fatalf("a model without an override compacted %d times; it inherits 1000000", got)
	}
}
