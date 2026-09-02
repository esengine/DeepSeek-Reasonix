package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/taskcontract"
	"reasonix/internal/tool"
)

func standardTodoTestAgent(t *testing.T, turns [][]provider.Chunk) (*Agent, *scriptedProvider) {
	t.Helper()
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: turns}
	return New(prov, reg, NewSession("stable-system-prefix"), Options{}, event.Discard), prov
}

func standardTodoContext() context.Context {
	return WithStandardTodoContinuation(context.Background(), StandardTodoContinuationPolicy{ExecutionExpected: true})
}

func TestStandardTodoContinuationCompletesInsideSameRun(t *testing.T) {
	a, prov := standardTodoTestAgent(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "I will edit it next."}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("write-1", "write_file", `{"path":"main.go"}`),
			toolCallChunk("todo-2", "todo_write", `{"todos":[{"content":"Edit code","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "Implemented."}, {Type: provider.ChunkDone}},
	})

	if err := a.Run(standardTodoContext(), "实施这个修改"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4", prov.call)
	}
	foundSynthetic := false
	for _, message := range a.Session().Snapshot() {
		if message.Role == provider.RoleUser && IsSyntheticUserText(message.Content) {
			foundSynthetic = true
		}
	}
	if !foundSynthetic {
		t.Fatal("same-Run continuation was not persisted as a hidden synthetic turn")
	}
}

func TestStandardTodoContinuationStopsAfterOneStalledNudge(t *testing.T) {
	a, prov := standardTodoTestAgent(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "I will do it."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Still planning to do it."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "must not be consumed"}, {Type: provider.ChunkDone}},
	})

	if err := a.Run(standardTodoContext(), "实施这个修改"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 (one nudge, then stop on no progress)", prov.call)
	}
}

func TestStandardTodoContinuationAllowsOneProgressGatedSecondNudge(t *testing.T) {
	a, prov := standardTodoTestAgent(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "I will do it."}, {Type: provider.ChunkDone}},
		{toolCallChunk("write-1", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "There is more."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Stopped after bounded repair."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "must not be consumed"}, {Type: provider.ChunkDone}},
	})

	if err := a.Run(standardTodoContext(), "实施这个修改"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 5 {
		t.Fatalf("provider calls = %d, want 5 (hard maximum of two nudges)", prov.call)
	}
}

func TestStandardTodoContinuationRequiresTrustedExecutionIntent(t *testing.T) {
	a, prov := standardTodoTestAgent(t, [][]provider.Chunk{
		{toolCallChunk("todo-1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Stopping here."}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "must not be consumed"}, {Type: provider.ChunkDone}},
	})

	if err := a.Run(context.Background(), "实施这个修改"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want historical two-call behavior without host policy", prov.call)
	}
}

func TestStandardTodoContinuationExcludesClosedLoopAndReadOnlyBoundaries(t *testing.T) {
	newEligibleAgent := func() *Agent {
		ledger := evidence.NewLedger()
		ledger.Record(evidence.Receipt{
			ToolName: "todo_write",
			Success:  true,
			Todos:    []evidence.TodoItem{{Content: "Edit code", Status: "in_progress"}},
		})
		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "write_file", readOnly: false})
		return &Agent{
			svc:  agentServices{tools: reg},
			task: taskRuntime{ledger: ledger},
			turn: turnRuntime{engine: runtimepolicy.NewEngine(runtimepolicy.Constraints{})},
		}
	}

	if !newEligibleAgent().standardTodoContinuationEligible(&turnRuntime{}) {
		t.Fatal("baseline Standard writer turn should be eligible")
	}
	tests := []struct {
		name  string
		apply func(*Agent, *turnRuntime)
	}{
		{name: "subagent", apply: func(a *Agent, _ *turnRuntime) { a.subagentDepth = 1 }},
		{name: "read-only executor", apply: func(a *Agent, _ *turnRuntime) { a.readOnlyExecution = true }},
		{name: "plan mode", apply: func(a *Agent, _ *turnRuntime) { a.planMode.Store(true) }},
		{name: "forbid mutation", apply: func(a *Agent, _ *turnRuntime) { a.turn.constraints.ForbidMutation = true }},
		{name: "delivery floor", apply: func(a *Agent, _ *turnRuntime) { a.turn.constraints.PolicyFloor = taskcontract.PolicyFloorDelivery }},
		{name: "goal scope", apply: func(a *Agent, _ *turnRuntime) { a.turn.deliveryScopeActive = true }},
		{name: "loop guard", apply: func(a *Agent, _ *turnRuntime) { a.turn.loopGuardArmed = true }},
		{name: "grace boundary", apply: func(_ *Agent, state *turnRuntime) { state.graceRound = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newEligibleAgent()
			state := &turnRuntime{}
			tc.apply(a, state)
			if a.standardTodoContinuationEligible(state) {
				t.Fatalf("%s boundary must not auto-continue", tc.name)
			}
		})
	}

	noWriter := newEligibleAgent()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	noWriter.svc.tools = reg
	if noWriter.standardTodoContinuationEligible(&turnRuntime{}) {
		t.Fatal("read-only registry must not auto-continue")
	}
}
