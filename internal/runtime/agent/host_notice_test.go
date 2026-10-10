package agent

import (
	"fmt"
	"slices"
	"testing"
)

// A fact queued between turns still has to be delivered: the state it describes
// moved while nothing was running, and the next turn is the first one that can
// act on it. That is what separates this from a steer.
func TestHostFactsWaitForTheNextRound(t *testing.T) {
	a := &Agent{}
	a.NoteHostFact("the live server withdrew mcp__live__beta")
	a.NoteHostFact("  the live server withdrew mcp__live__beta  ")
	a.NoteHostFact("")

	got := a.takeHostFacts()
	if !slices.Equal(got, []string{"the live server withdrew mcp__live__beta"}) {
		t.Fatalf("pending facts = %v, want the one fact stated once", got)
	}
	if got := a.takeHostFacts(); len(got) != 0 {
		t.Fatalf("facts were delivered twice: %v", got)
	}
}

// A host that keeps changing its mind must not be able to grow one turn's tail
// without bound.
func TestHostFactsAreBounded(t *testing.T) {
	a := &Agent{}
	for i := range maxPendingHostNotices + 5 {
		a.NoteHostFact(fmt.Sprintf("fact %d", i))
	}
	if got := len(a.takeHostFacts()); got != maxPendingHostNotices {
		t.Fatalf("queued %d facts, want the ceiling of %d", got, maxPendingHostNotices)
	}
}
