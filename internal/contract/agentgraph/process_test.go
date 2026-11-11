package agentgraph

import (
	"testing"

	"reasonix/internal/contract/process"
	"reasonix/internal/contract/process/proctest"
)

func TestNodeStateMapsOntoProcessStates(t *testing.T) {
	proctest.Total(t, ".", "NodeState", func(v string) (process.Class, bool) { return NodeState(v).ProcessClass("") })
}

func TestQueuedNodeCarriesItsWaitCause(t *testing.T) {
	for w, want := range map[WaitCause]process.Cause{WaitSlots: process.CauseSlots, WaitWriters: process.CauseWriters, WaitClaim: process.CauseClaim} {
		got, _ := StateQueued.ProcessClass(w)
		if got.State != process.Queued || got.Cause != want {
			t.Errorf("queued for %q: got %+v", w, got)
		}
	}
}
