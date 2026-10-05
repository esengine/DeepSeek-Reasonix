package coordinator

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trustedstate"
)

const planWithCheck = `{"objective":"fix the parser","steps":[{"id":"p1","title":"fix it",
 "acceptance":[{"text":"quoted fields parse"}],
 "verification":[{"command":"go test ./parser/"}]}]}`

const planWithoutCheck = `{"objective":"fix the parser","steps":[{"id":"p1","title":"fix it",
 "acceptance":[{"text":"quoted fields parse"}]}]}`

// A later plan that drops a check the task already accepted relaxes the
// contract. The planner cannot decide that, so the host puts the plan to the
// user even though neither the route nor the planner asked for approval.
func TestAPlanDroppingAnAcceptedCheckGoesToTheUser(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: append(submitPlanCall(planWithCheck), submitPlanCall(planWithoutCheck)...)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Done."}, {Type: provider.ChunkDone}}}
	executor := agent.New(exec, tool.NewRegistry(), sessionstore.NewSession("exec-sys"), agent.Options{
		EvidenceSeal: &agent.EvidenceSeal{Store: trustedstate.Open(t.TempDir(), nil), Stream: "ws"},
	}, event.Discard)
	parentReg := tool.NewRegistry()
	parentReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "contents"})
	coord := NewCoordinator(planner, sessionstore.NewSession("planner-sys"), nil, agent.PlannerToolRegistry(parentReg),
		agent.Options{MaxSteps: 4}, executor, 0, event.Discard, nil)
	approver := &recordingPlanApprover{allow: true}
	coord.SetPlannerPlanApprover(approver)
	ctx := agent.WithDeliveryExecutionScope(context.Background(), agent.DeliveryExecutionScope{ID: "goal-1", TaskText: "fix the parser"})

	if err := coord.Run(ctx, "fix the parser"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if approver.called {
		t.Fatal("the first plan was put to the user although nothing asked for it")
	}
	if err := coord.Run(ctx, "continue"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !approver.called {
		t.Fatal("a plan dropping the accepted check ran without the user being asked")
	}
}
