package control

import (
	"testing"

	"reasonix/internal/contract/event"
)

// collectTools runs fn and returns the tool frames it emitted, in order.
func collectTools(fn func(*Controller)) []event.Tool {
	var got []event.Tool
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolDispatch || e.Kind == event.ToolResult {
			got = append(got, e.Tool)
		}
	})})
	defer c.Close()
	fn(c)
	return got
}

// A seeded task list is the host acting on an approved plan; the model never
// sent that todo_write. Counting it as a step credits the host's own
// bookkeeping to the model, which is the whole reason this axis exists.
func TestHostSeededPlanTodosAreIssuedByTheHost(t *testing.T) {
	frames := collectTools(func(c *Controller) {
		c.seedPlanTodos("# Plan\n\n- [ ] first\n- [ ] second\n")
	})
	if len(frames) == 0 {
		t.Fatal("seeding an approved plan emitted no tool frames")
	}
	for _, f := range frames {
		if f.Issuer != event.IssuedByHost {
			t.Errorf("seeded %q = issuer %q, want %q", f.ID, f.Issuer, event.IssuedByHost)
		}
		if f.Issuer.ModelWork() {
			t.Errorf("seeded %q counts as the model's work", f.ID)
		}
	}
}

// The destructive control on every gate that reads this field: with the issuer
// saying model, the same frame is counted. So it is the recorded provenance
// doing the work, not the call's shape, its id, or whether it had a dispatch.
func TestTheIssuerIsWhatDecidesWhetherAFrameCounts(t *testing.T) {
	frames := collectTools(func(c *Controller) {
		c.seedPlanTodos("# Plan\n\n- [ ] first\n")
	})
	if len(frames) == 0 {
		t.Fatal("seeding an approved plan emitted no tool frames")
	}
	count := func(in []event.Tool) int {
		n := 0
		for _, f := range in {
			if f.Issuer.ModelWork() {
				n++
			}
		}
		return n
	}
	if got := count(frames); got != 0 {
		t.Fatalf("host-issued frames counted as model work: %d of %d", got, len(frames))
	}
	flipped := append([]event.Tool(nil), frames...)
	for i := range flipped {
		flipped[i].Issuer = event.IssuedByModel
	}
	if got := count(flipped); got != len(flipped) {
		t.Fatalf("flipping the issuer to model changed nothing: %d of %d counted — "+
			"the gate is reading something other than the issuer", got, len(flipped))
	}
}
