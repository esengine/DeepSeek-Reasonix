package tool

import (
	"reflect"
	"strings"
	"testing"
)

// Withdrawing has to leave the provider-visible array byte-identical: the point
// of keeping the slot is that the cached prefix does not move.
func TestWithdrawKeepsTheSchemaAndFailsTheCall(t *testing.T) {
	r := NewRegistry()
	live := stubTool{name: "mcp__atlas__gone", server: "atlas", raw: "gone"}
	r.Add(live)
	before := r.Schemas()

	r.Add(Withdraw(live, `MCP tool "gone" not found on server "atlas"`))
	after := r.Schemas()

	if len(before) != 1 || len(after) != 1 || !reflect.DeepEqual(before[0], after[0]) {
		t.Fatalf("the schema moved when the tool was withdrawn:\n before: %+v\n  after: %+v", before, after)
	}
	held, _ := r.Get("mcp__atlas__gone")
	if !IsWithdrawn(held) {
		t.Fatal("the registry holds a live tool where a withdrawn slot was registered")
	}
	if _, err := held.Execute(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "not found on server") {
		t.Fatalf("calling a withdrawn tool returned %v, want its reason", err)
	}
	if !held.ReadOnly() {
		t.Fatal("a withdrawn tool reported a side effect it cannot have")
	}
}

// A slot is not a capability: anything that routes by what a tool can still do
// has to stop seeing this one, or a catalog keeps offering a dead call.
func TestWithdrawDropsTheMCPBinding(t *testing.T) {
	r := NewRegistry()
	live := stubTool{name: "mcp__atlas__gone", server: "atlas", raw: "gone"}
	r.Add(Withdraw(live, "gone"))

	if got := r.MCPBindings(); len(got) != 0 {
		t.Fatalf("a withdrawn tool still publishes %d MCP bindings, want none", len(got))
	}
	if IsWithdrawn(live) {
		t.Fatal("a live tool reported itself withdrawn")
	}
}

// Restating a reason must not wrap a slot inside another slot: the second
// reason is the current one, and the schema it holds open is still the
// original tool's.
func TestWithdrawRestatesRatherThanNests(t *testing.T) {
	once := Withdraw(stubTool{name: "mcp__atlas__gone"}, "first")
	twice := Withdraw(once, "second")

	if _, err := twice.Execute(t.Context(), nil); err == nil || err.Error() != "second" {
		t.Fatalf("reason = %v, want the restated one", err)
	}
	if twice.Name() != "mcp__atlas__gone" {
		t.Fatalf("name = %q, want the original tool's", twice.Name())
	}
}
