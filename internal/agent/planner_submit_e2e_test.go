package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type recordingPlanApprover struct {
	plan   string
	called bool
	allow  bool
}

func (r *recordingPlanApprover) RunWithPlannerApproval(ctx context.Context, plan string, run func(context.Context) error) error {
	r.called, r.plan = true, plan
	if !r.allow {
		return nil
	}
	return run(ctx)
}

// submitPlanCall includes an unhelpful acknowledgement round to prove the host
// stops as soon as the structured plan lands and never pays for that round.
func submitPlanCall(args string) [][]provider.Chunk {
	return [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "submit_plan", Arguments: args}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "I have submitted the plan above."},
			{Type: provider.ChunkDone},
		},
	}
}

func submitPlanCoordinator(t *testing.T, planner, exec *mockProvider, sink event.Sink) (*Coordinator, *Agent) {
	t.Helper()
	parentReg := tool.NewRegistry()
	parentReg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "contents"})
	parentReg.Add(NewAskTool())
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(planner, NewSession("planner-sys"), nil, PlannerToolRegistry(parentReg),
		Options{MaxSteps: 4}, executor, 0, sink, nil)
	return coord, executor
}

const e2ePlanArgs = `{
  "objective":"make the cache key model-aware",
  "steps":[
    {"id":"p1","title":"thread the model ref through","verified_files":["internal/provider/cache.go"]},
    {"id":"s1","parent_id":"p1","title":"extend cacheKey",
     "verification":[{"command":"go test ./internal/provider/","expect":"all green"}]}
  ]
}`

// The effect that matters: what the executor actually receives. A submitted plan
// must reach it as the rendered plan, not as whatever prose the planner ended on.
func TestSubmittedPlanReachesTheExecutorHandoff(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: submitPlanCall(e2ePlanArgs)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, event.Discard)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor never ran")
	}
	if got := len(planner.requests); got != 1 {
		t.Fatalf("planner requests = %d, want submit_plan to end the planner turn immediately", got)
	}
	handoff := lastUser(exec.requests[0])
	for _, want := range []string{
		"fix the cache key",
		"**Objective** — make the cache key model-aware",
		"1. thread the model ref through",
		"   - extend cacheKey",
		"verified: internal/provider/cache.go",
		"verify: go test ./internal/provider/ — all green",
	} {
		if !strings.Contains(handoff, want) {
			t.Errorf("executor handoff missing %q:\n%s", want, handoff)
		}
	}
	if strings.Contains(handoff, "I have submitted the plan above.") {
		t.Errorf("executor received the planner's prose instead of the plan:\n%s", handoff)
	}
}

// The user must see the plan itself, not the planner's acknowledgement of having
// submitted one — the host renders it because the plan is no longer prose.
func TestSubmittedPlanIsRenderedToTheSink(t *testing.T) {
	var texts []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text && e.Source == event.UsageSourcePlanner {
			texts = append(texts, e.Text)
		}
	})
	planner := &mockProvider{name: "planner", streams: submitPlanCall(e2ePlanArgs)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, sink)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "1. thread the model ref through") {
		t.Fatalf("the rendered plan never reached the sink:\n%s", joined)
	}
}

// requires_approval is a field now, so the gate fires on a plan whose prose says
// nothing about approval — the case the 24-phrase fallback cannot catch.
func TestSubmittedPlanGatesOnRequiresApprovalField(t *testing.T) {
	args := `{"objective":"drop the legacy table","requires_approval":true,
	          "steps":[{"title":"drop payments_v1"}]}`
	planner := &mockProvider{name: "planner", streams: submitPlanCall(args)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, event.Discard)
	approver := &recordingPlanApprover{allow: true}
	coord.SetPlannerPlanApprover(approver)

	if err := coord.Run(withNoClosedLoop(context.Background()), "drop the old table"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !approver.called {
		t.Fatal("requires_approval did not gate execution")
	}
	if !strings.Contains(approver.plan, "1. drop payments_v1") {
		t.Errorf("the approval card got %q, want the rendered plan", approver.plan)
	}
	if len(exec.requests) == 0 {
		t.Fatal("approval was granted but the executor never ran")
	}
}

func TestSubmittedPlanWithoutApprovalRunsStraightThrough(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: submitPlanCall(e2ePlanArgs)}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, event.Discard)
	approver := &recordingPlanApprover{allow: true}
	coord.SetPlannerPlanApprover(approver)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if approver.called {
		t.Fatal("a plan that did not request approval must not gate")
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor never ran")
	}
}

// A planner that ignores submit_plan must still work: the text path is the
// fallback, not a broken state.
func TestPlannerThatWritesProseStillReachesTheExecutor(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "1. edit the cache key\n2. run the tests"},
		{Type: provider.ChunkDone},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, event.Discard)

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the cache key"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor never ran on the prose fallback")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "edit the cache key") {
		t.Errorf("executor handoff = %q, want the planner's prose plan", got)
	}
}

// The planner asks with the real tool now, so a user-owned decision is settled
// while planning and the answer shapes the plan the executor receives — the
// prose-question path used to staple it onto a finished plan instead.
func TestPlannerAsksWithTheRealToolAndPlansFromTheAnswer(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "ask-1", Name: "ask", Arguments: `{"questions":[{"header":"Store","question":"Which database?","options":[{"label":"Keep going"},{"label":"postgres"}]}]}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "submit_plan", Arguments: `{"objective":"add the store","steps":[{"title":"wire the chosen database"}]}`}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "Submitted."},
			{Type: provider.ChunkDone},
		},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	coord, _ := submitPlanCoordinator(t, planner, exec, event.Discard)
	asker := &recordingAsker{}
	coord.SetAsker(asker)

	if err := coord.Run(withNoClosedLoop(context.Background()), "add a store"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(asker.questions) == 0 {
		t.Fatal("the planner's ask never reached the host")
	}
	if got := asker.questions[0].Prompt; got != "Which database?" {
		t.Errorf("question = %q, want the planner's own wording", got)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor never ran after the decision was settled")
	}
	if got := lastUser(exec.requests[0]); !strings.Contains(got, "wire the chosen database") {
		t.Errorf("executor handoff = %q, want the plan built after the answer", got)
	}
}

func TestPlannerRegistryCarriesAsk(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(NewAskTool())
	parent.Add(coordinatorTestTool{name: "read_file", readOnly: true})
	reg := PlannerToolRegistry(parent)
	if _, ok := reg.Get("ask"); !ok {
		t.Fatalf("planner registry lacks ask: %v", reg.Names())
	}
}
