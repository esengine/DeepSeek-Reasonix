package agent

import (
	"testing"
	"time"
)

func TestThoughtClockRunsFromFirstReasoningToFirstAnswer(t *testing.T) {
	t0 := time.Unix(100, 0)
	var c thoughtClock
	c.answer(t0) // an answer before any reasoning is not a thought
	if c.ms() != 0 {
		t.Fatalf("no reasoning measured %d ms", c.ms())
	}
	c.reasoning(t0.Add(time.Second))
	c.reasoning(t0.Add(2 * time.Second))
	if got := c.ms(); got != 1000 {
		t.Fatalf("mid-thought = %d ms, want 1000 (to the last reasoning)", got)
	}
	c.answer(t0.Add(3500 * time.Millisecond))
	c.reasoning(t0.Add(9 * time.Second)) // reasoning after the answer began does not extend it
	c.answer(t0.Add(10 * time.Second))
	if got := c.ms(); got != 2500 {
		t.Fatalf("thought = %d ms, want 2500", got)
	}
}
