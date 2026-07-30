package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type recordingRecoveryGate struct {
	observation RecoveryObservation
	proposals   []RecoveryProposal
	decision    RecoveryDecision
}

func TestRecoveryPlanTransitionDetectsOnlyStructuralRewriteOfActivePlan(t *testing.T) {
	a := &Agent{}
	initial := json.RawMessage(`{"todos":[{"content":"Implement parser","status":"in_progress"}]}`)
	if changed, _, _ := a.recoveryPlanTransition("todo_write", initial); changed {
		t.Fatal("initial plan must stay on the fast path")
	}

	a.setTodoState([]evidence.TodoItem{
		{Content: "Implement parser", Status: "in_progress"},
		{Content: "Run tests", Status: "pending"},
	})
	progressOnly := json.RawMessage(`{"todos":[{"content":"Implement parser","status":"completed"},{"content":"Run tests","status":"in_progress"}]}`)
	if changed, _, _ := a.recoveryPlanTransition("todo_write", progressOnly); changed {
		t.Fatal("progress-only update must not invoke the plan reviewer")
	}

	replacement := json.RawMessage(`{"todos":[{"content":"Replace parser architecture","status":"in_progress"},{"content":"Run tests","status":"pending"}]}`)
	changed, before, after := a.recoveryPlanTransition("todo_write", replacement)
	if !changed {
		t.Fatal("structural rewrite of active plan was not detected")
	}
	if !strings.Contains(before, "Implement parser") || !strings.Contains(after, "Replace parser architecture") {
		t.Fatalf("plan evidence before=%q after=%q", before, after)
	}
}

func TestRecoveryPlanTransitionIgnoresCompletedPriorPlan(t *testing.T) {
	a := &Agent{}
	a.setTodoState([]evidence.TodoItem{{Content: "Old task", Status: "completed"}})
	next := json.RawMessage(`{"todos":[{"content":"New user task","status":"in_progress"}]}`)
	if changed, _, _ := a.recoveryPlanTransition("todo_write", next); changed {
		t.Fatal("a new task after a completed plan is not a mid-plan transition")
	}
}

func (g *recordingRecoveryGate) ObserveResult(_ context.Context, observation RecoveryObservation) string {
	g.observation = observation
	return ""
}

func (g *recordingRecoveryGate) BeforeMutation(_ context.Context, proposal RecoveryProposal) (RecoveryDecision, error) {
	g.proposals = append(g.proposals, proposal)
	decision := g.decision
	if decision == (RecoveryDecision{}) {
		decision.Allow = true
	}
	return decision, nil
}

func TestAuthorizedRecoveryPlanTransitionCanReplaceCurrentTodo(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "todo_write"))
	gate := &recordingRecoveryGate{decision: RecoveryDecision{
		Allow: true, AuthorizePlanReplacement: true,
	}}
	a := New(nil, reg, NewSession(""), Options{RecoveryGate: gate}, event.Discard)
	a.SeedTodoState([]evidence.TodoItem{
		{Content: "Inspect environment", Status: "completed"},
		{Content: "Implement parser", Status: "in_progress"},
		{Content: "Run tests", Status: "pending"},
	})

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID:   "replace-plan",
		Name: "todo_write",
		Arguments: `{"todos":[
			{"content":"Inspect environment","status":"completed"},
			{"content":"Replace parser architecture","status":"in_progress"},
			{"content":"Run tests","status":"pending"}
		]}`,
	})
	if out.errMsg != "" {
		t.Fatalf("authorized plan replacement was blocked: %+v", out)
	}
	if len(gate.proposals) != 1 || !gate.proposals[0].PlanTransition {
		t.Fatalf("recovery proposals = %+v, want one plan transition", gate.proposals)
	}
	got := a.CanonicalTodoState()
	if len(got) != 3 || got[0].Status != "completed" || got[1].Content != "Replace parser architecture" {
		t.Fatalf("canonical todo state = %+v, want preserved history plus replacement", got)
	}
}

func TestPlanTransitionNeedsDedicatedReplacementAuthorization(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "todo_write"))
	gate := &recordingRecoveryGate{decision: RecoveryDecision{Allow: true}}
	a := New(nil, reg, NewSession(""), Options{RecoveryGate: gate}, event.Discard)
	a.SeedTodoState([]evidence.TodoItem{{Content: "Implement parser", Status: "in_progress"}})

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID:        "replace-plan-without-authorization",
		Name:      "todo_write",
		Arguments: `{"todos":[{"content":"Replace parser architecture","status":"in_progress"}]}`,
	})
	if out.errMsg == "" || !strings.Contains(out.output, "cannot be removed or replaced") {
		t.Fatalf("plain allow unexpectedly replaced current todo: %+v", out)
	}
}

func TestObserveRecoveryResultMarksCancellation(t *testing.T) {
	gate := &recordingRecoveryGate{}
	a := &Agent{recoveryGate: gate}
	a.observeRecoveryResult(
		context.Background(),
		"write_file",
		json.RawMessage(`{"path":"a.go"}`),
		false,
		true,
		"cancelled",
		context.Canceled,
		false,
		false,
		0,
	)
	if !gate.observation.Cancelled {
		t.Fatalf("observation = %+v, want cancellation marked", gate.observation)
	}
	if gate.observation.TaskScopeID == "" {
		t.Fatalf("observation = %+v, want a host-owned recovery scope", gate.observation)
	}
}

func TestObserveRecoveryResultKeepsToolOwnedDeadlineAsFailure(t *testing.T) {
	gate := &recordingRecoveryGate{}
	a := &Agent{recoveryGate: gate}
	a.observeRecoveryResult(
		context.Background(),
		"mcp__server__write",
		json.RawMessage(`{"value":"x"}`),
		false,
		true,
		"",
		fmt.Errorf("MCP tool timed out after 30s: %w", context.DeadlineExceeded),
		false,
		false,
		0,
	)
	if gate.observation.Cancelled {
		t.Fatalf("observation = %+v, tool-owned deadline must remain a qualifying transient failure", gate.observation)
	}
	if !strings.Contains(gate.observation.ErrSummary, "timed out") {
		t.Fatalf("observation = %+v, want timeout evidence preserved", gate.observation)
	}
}

func TestObserveRecoveryResultMarksParentDeadlineCancellation(t *testing.T) {
	gate := &recordingRecoveryGate{}
	a := &Agent{recoveryGate: gate}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	a.observeRecoveryResult(
		ctx,
		"mcp__server__write",
		json.RawMessage(`{"value":"x"}`),
		false,
		true,
		"",
		context.DeadlineExceeded,
		false,
		false,
		0,
	)
	if !gate.observation.Cancelled {
		t.Fatalf("observation = %+v, parent deadline must remain a cancellation", gate.observation)
	}
}

func TestRecoveryBlockSurfacesConcreteReason(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinTool(t, "write_file"))
	gate := &recordingRecoveryGate{decision: RecoveryDecision{
		Blocked: true,
		Message: "blocked: Auto stopped repeating this operation after 3 consecutive failures: write a.go. Other operations remain available.",
	}}
	a := New(nil, reg, NewSession(""), Options{RecoveryGate: gate}, event.Discard)
	out := a.executeOne(context.Background(), provider.ToolCall{
		ID: "blocked-write", Name: "write_file", Arguments: `{"path":"a.go","content":"x"}`,
	})
	if !out.blocked || !strings.Contains(out.errMsg, "stopped repeating this operation") || strings.Contains(out.errMsg, "Auto Guard") {
		t.Fatalf("recovery failure card = %+v", out)
	}
}
