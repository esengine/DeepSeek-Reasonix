package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/taskpolicy"
)

// A registered explore worker is the model's to call. The turn policy carries
// the plan-mode boundary, not a quota on how the model gathers context.
func TestTaskPolicyLeavesExploreSubagentAlone(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "explore", readOnly: true})
	a := New(&scriptedProvider{name: "p"}, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{})
	a.turn.policySet = true

	got := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "explore", Arguments: `{}`})
	if got.blocked {
		t.Fatalf("explore outcome = %+v, want no policy block", got)
	}
}

// Ordinary work carries no host-invented limits: the gate blocks a writer only
// under plan mode, and reaches for no reading of what the user wrote.
// One owner answers "may this run while planning". The turn policy carries the
// plan signal for the prompt and for diagnostics, and that is all it does — a
// second enforcement point here is the gap delegation fell through.
func TestTaskPolicyPlanSignalDoesNotEnforceThePhase(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	a := New(&scriptedProvider{name: "p"}, reg, sessionstore.NewSession("sys"), Options{CheckTargetAccess: allowTestFileAccess}, event.Discard)
	a.turn.policySet = true
	call := provider.ToolCall{Name: "write_file", Arguments: `{"path":"notes.txt","content":"x"}`}

	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{PlanMode: true})
	if got := a.executeOne(context.Background(), &a.turn, call); got.blocked {
		t.Fatalf("the turn policy blocked on its own: %+v", got)
	}

	a.SetPlanMode(true)
	got := a.executeOne(context.Background(), &a.turn, call)
	if !got.blocked || !strings.Contains(got.errMsg, "planning") {
		t.Fatalf("plan-phase outcome = %+v, want the phase gate to refuse", got)
	}
}
