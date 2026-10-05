package delegation

import (
	"strings"
	"testing"

	"reasonix/internal/contract/event"
)

type idCapture struct{ ids []string }

func (c *idCapture) Emit(e event.Event) {
	if e.Kind == event.ToolDispatch {
		c.ids = append(c.ids, e.Tool.ID)
	}
}

// A sub-agent's tool ids are namespaced once, by the sink that knows which
// child it wraps. Wrapping twice prefixes the id twice, so it no longer starts
// with its parent's and every consumer matching on that prefix reads the call
// as top-level.
func TestNestedSinkNamespacesToolIDsExactlyOnce(t *testing.T) {
	const subID = "call_parent/sub-1"
	cap := &idCapture{}

	once := subSinkFor(subID, cap)
	once.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call_child", Name: "grep"}})

	twice := subSinkFor(subID, subSinkFor(subID, cap))
	twice.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call_child", Name: "grep"}})

	if len(cap.ids) != 2 {
		t.Fatalf("captured %d dispatches, want 2", len(cap.ids))
	}
	if want := subID + "/call_child"; cap.ids[0] != want {
		t.Fatalf("single wrap = %q, want %q", cap.ids[0], want)
	}
	if strings.Count(cap.ids[1], subID) != 1 {
		t.Fatalf("double wrap repeated the parent prefix: %q", cap.ids[1])
	}
}
