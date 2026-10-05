package boot

import (
	"testing"

	"reasonix/internal/ext/skill"
)

// A run's own cap only ever tightens the default budget, and zero on either
// side means that side sets no limit.
func TestSubagentStepsTakeTheTighterCap(t *testing.T) {
	cases := []struct{ parent, run, want int }{
		{parent: 40, run: 0, want: 20},
		{parent: 40, run: 4, want: 4},
		{parent: 6, run: 9, want: 5},
		{parent: 0, run: 0, want: 0},
		{parent: 0, run: 4, want: 4},
	}
	for _, c := range cases {
		r := &skillSubagents{maxSteps: c.parent}
		if got := r.stepsFor(skill.SubagentRunOptions{MaxSteps: c.run}); got != c.want {
			t.Errorf("parent %d, run %d: steps %d, want %d", c.parent, c.run, got, c.want)
		}
	}
}
