package agent

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type rosterTool struct{ recordingTool }

func (*rosterTool) WritesHostStateOnly() bool { return true }

// A call whose every effect is host-owned state — a team roster change — is
// recorded as changing nothing in the work product, so the turn's receipt does
// not report it as an unverified change; an opaque tool still does.
func TestAHostStateCallIsNoChangeToTheWorkProduct(t *testing.T) {
	roster := &rosterTool{recordingTool{name: "create_agent"}}
	opaque := &recordingTool{name: "apply_thing"}
	reg := tool.NewRegistry()
	reg.Add(roster)
	reg.Add(opaque)
	a := New(nil, reg, sessionstore.NewSession(""), Options{Gate: &stubGate{}}, event.Discard)
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "c1", Name: "create_agent", Arguments: `{"name":"Ada"}`})
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "c2", Name: "apply_thing", Arguments: `{}`})
	got := map[string]bool{}
	for _, r := range a.task.ledger.Receipts() {
		got[r.ToolName] = r.Mutation
	}
	if got["create_agent"] || !got["apply_thing"] {
		t.Fatalf("mutations by tool = %v", got)
	}
}
