package control

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/goaleval"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const (
	staleDangerousPlan = "OLD PLAN:\n1. Delete production data\n2. Remove every backup"
	staleGoalComplete  = "OLD CLAIM: this goal is complete"
	currentSafePlan    = "CURRENT PLAN:\n1. Preserve production data\n2. Add a regression test"
	currentGoalResult  = "CURRENT GOAL RESULT: work is not complete"
)

// workflowDeepSeekProvider marks the ordinary scripted provider with the
// capability that enables DeepSeek's reasoning-only finish=stop behavior.
type workflowDeepSeekProvider struct{ *scriptedTurns }

func (*workflowDeepSeekProvider) RequiresToolCallReasoning() bool { return true }

func reasoningOnlyStop(text string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: text},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 42}},
		{Type: provider.ChunkDone},
	}
}

type planWorkflowCapture struct {
	approvals atomic.Int32
	mu        sync.Mutex
	seeds     []string
}

func (p *planWorkflowCapture) recordSeed(seed string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seeds = append(p.seeds, seed)
}

func (p *planWorkflowCapture) firstSeed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seeds) == 0 {
		return ""
	}
	return p.seeds[0]
}

func newPlanWorkflowController(t *testing.T, sess *agent.Session, turns [][]provider.Chunk) (*Controller, *scriptedTurns, *planWorkflowCapture) {
	t.Helper()
	base := &scriptedTurns{turns: turns}
	ag := agent.New(&workflowDeepSeekProvider{base}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	capture := &planWorkflowCapture{}
	var c *Controller
	c = New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.ApprovalRequest:
				capture.approvals.Add(1)
				go c.Approve(e.Approval.ID, true, false, false)
			case event.ToolResult:
				if e.Tool.ID == "plan-seed" && e.Tool.Name == "todo_write" && e.Tool.Err == "" {
					capture.recordSeed(e.Tool.Args)
				}
			}
		}),
	})
	c.SetPlanMode(true)
	return c, base, capture
}

func runPlanWorkflow(t *testing.T, c *Controller) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.runTurnWithRaw(ctx, "prepare a safe plan", "prepare a safe plan")
}

func assertSafePlanSeed(t *testing.T, seed string) {
	t.Helper()
	if !strings.Contains(seed, "Preserve production data") {
		t.Fatalf("approved todo seed does not contain the current plan: %s", seed)
	}
	if strings.Contains(seed, "Delete production data") || strings.Contains(seed, "Remove every backup") {
		t.Fatalf("approved todo seed reused a previous turn: %s", seed)
	}
}

func TestReasoningOnlyFirstPlanRecoversVisibleProposal(t *testing.T) {
	c, prov, capture := newPlanWorkflowController(t, agent.NewSession(""), [][]provider.Chunk{
		reasoningOnlyStop(currentSafePlan),
		textTurn(currentSafePlan),
		textTurn("Execution complete."),
	})

	if err := runPlanWorkflow(t, c); err != nil {
		t.Fatalf("run plan workflow: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want reasoning-only recovery + visible plan + execution (3)", prov.call)
	}
	if got := capture.approvals.Load(); got != 1 {
		t.Fatalf("approval requests = %d, want 1 after visible-plan recovery", got)
	}
	if c.PlanMode() {
		t.Fatal("Plan mode remained active after the recovered proposal was approved")
	}
	assertSafePlanSeed(t, capture.firstSeed())
}

func TestReasoningOnlyPlanNeverSeedsPreviousDangerousPlan(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous unrelated request"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: staleDangerousPlan})
	c, prov, capture := newPlanWorkflowController(t, sess, [][]provider.Chunk{
		reasoningOnlyStop(currentSafePlan),
		textTurn(currentSafePlan),
		textTurn("Execution complete."),
	})

	if err := runPlanWorkflow(t, c); err != nil {
		t.Fatalf("run plan workflow: %v", err)
	}
	assertSafePlanSeed(t, capture.firstSeed())
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want recovery before execution (3)", prov.call)
	}
}

type workflowGoalEvaluator struct {
	calls    int
	evidence []goaleval.GoalEvidence
}

func (e *workflowGoalEvaluator) Evaluate(_ context.Context, evidence goaleval.GoalEvidence) (goaleval.Verdict, error) {
	e.calls++
	e.evidence = append(e.evidence, evidence)
	if strings.Contains(evidence.AssistantFinal, staleGoalComplete) {
		return goaleval.Verdict{Outcome: goaleval.OutcomeComplete, Reason: "assistant claimed completion"}, nil
	}
	return goaleval.Verdict{Outcome: goaleval.OutcomeBlocked, Reason: "current answer does not prove completion"}, nil
}

func newGoalWorkflowController(t *testing.T, turns [][]provider.Chunk) (*Controller, *scriptedTurns, *workflowGoalEvaluator) {
	t.Helper()
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous completed goal"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: staleGoalComplete})
	base := &scriptedTurns{turns: turns}
	ag := agent.New(&workflowDeepSeekProvider{base}, goalRegistry(), sess, agent.Options{}, event.Discard)
	evaluator := &workflowGoalEvaluator{}
	c := New(Options{Runner: ag, Executor: ag, GoalEvaluator: evaluator, Sink: event.Discard})
	c.SetGoal("new goal: validate the unfinished migration")
	return c, base, evaluator
}

func runGoalWorkflow(t *testing.T, c *Controller) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return newTurnOrchestrator(c).runGoalLoopWithRawDisplay(ctx, "inspect current work", "inspect current work", "")
}

func TestReasoningOnlyGoalNeverEvaluatesPreviousCompleteAnswer(t *testing.T) {
	c, prov, evaluator := newGoalWorkflowController(t, [][]provider.Chunk{
		reasoningOnlyStop("CURRENT GOAL REASONING: the migration is unfinished"),
		textTurn(currentGoalResult),
	})

	if err := runGoalWorkflow(t, c); err != nil {
		t.Fatalf("run Goal workflow: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want reasoning-only recovery + visible result (2)", prov.call)
	}
	if evaluator.calls != 1 || len(evaluator.evidence) != 1 {
		t.Fatalf("evaluator calls/evidence = %d/%d, want 1/1", evaluator.calls, len(evaluator.evidence))
	}
	if got := evaluator.evidence[0].AssistantFinal; got != currentGoalResult {
		t.Fatalf("evaluator AssistantFinal = %q, want current turn %q", got, currentGoalResult)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked from the current evidence", got)
	}
}

func TestVisibleWorkflowFinalControls(t *testing.T) {
	t.Run("Plan uses current visible proposal", func(t *testing.T) {
		sess := agent.NewSession("")
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous unrelated request"})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: staleDangerousPlan})
		c, prov, capture := newPlanWorkflowController(t, sess, [][]provider.Chunk{
			textTurn(currentSafePlan),
			textTurn("Execution complete."),
		})

		if err := runPlanWorkflow(t, c); err != nil {
			t.Fatalf("run plan workflow: %v", err)
		}
		assertSafePlanSeed(t, capture.firstSeed())
		if prov.call != 2 {
			t.Fatalf("provider calls = %d, want visible plan + execution (2)", prov.call)
		}
	})

	t.Run("Goal uses current visible result", func(t *testing.T) {
		c, prov, evaluator := newGoalWorkflowController(t, [][]provider.Chunk{textTurn(currentGoalResult)})
		if err := runGoalWorkflow(t, c); err != nil {
			t.Fatalf("run Goal workflow: %v", err)
		}
		if prov.call != 1 {
			t.Fatalf("provider calls = %d, want 1", prov.call)
		}
		if evaluator.calls != 1 || len(evaluator.evidence) != 1 {
			t.Fatalf("evaluator calls/evidence = %d/%d, want 1/1", evaluator.calls, len(evaluator.evidence))
		}
		if got := evaluator.evidence[0].AssistantFinal; got != currentGoalResult {
			t.Fatalf("evaluator AssistantFinal = %q, want %q", got, currentGoalResult)
		}
	})
}

func TestReasoningOnlyPlanAndGoalUseExecutionFinal(t *testing.T) {
	c, prov, capture := newPlanWorkflowController(t, agent.NewSession(""), [][]provider.Chunk{
		reasoningOnlyStop(currentSafePlan),
		textTurn(currentSafePlan),
		textTurn(currentGoalResult),
	})
	evaluator := &workflowGoalEvaluator{}
	c.evaluator = evaluator
	c.SetGoal("ship the safe plan")

	if err := runGoalWorkflow(t, c); err != nil {
		t.Fatalf("run Plan + Goal workflow: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want repaired proposal plus execution (3)", prov.call)
	}
	if capture.approvals.Load() != 1 {
		t.Fatalf("approval requests = %d, want 1", capture.approvals.Load())
	}
	assertSafePlanSeed(t, capture.firstSeed())
	if evaluator.calls != 1 || len(evaluator.evidence) != 1 {
		t.Fatalf("evaluator calls/evidence = %d/%d, want 1/1", evaluator.calls, len(evaluator.evidence))
	}
	if got := evaluator.evidence[0].AssistantFinal; got != currentGoalResult {
		t.Fatalf("evaluator AssistantFinal = %q, want execution final %q", got, currentGoalResult)
	}
}

func TestGoalVisibleFinalRepairNeverRepeatsUpdateGoal(t *testing.T) {
	c, prov, evaluator := newGoalWorkflowController(t, [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "the goal is verified"}, toolCallChunk("goal-report", "update_goal", `{"status":"complete","reason":"verified"}`), {Type: provider.ChunkDone}},
		reasoningOnlyStop("the structured goal report is complete"),
		{{Type: provider.ChunkReasoning, Text: "I should repeat the report"}, toolCallChunk("repair-repeat", "update_goal", `{"status":"blocked","reason":"must not replace the committed report"}`), {Type: provider.ChunkDone}},
		textTurn("Verified work is complete."),
	})

	if err := runGoalWorkflow(t, c); err != nil {
		t.Fatalf("run Goal workflow: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want work + reasoning stop + blocked repair + visible repair (4)", prov.call)
	}
	if evaluator.calls != 0 {
		t.Fatalf("evaluator calls = %d, want structured update_goal report to decide", evaluator.calls)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want original complete report preserved", got)
	}
	var blocked string
	for _, msg := range c.executor.Session().Snapshot() {
		if msg.Role == provider.RoleTool && msg.ToolCallID == "repair-repeat" {
			blocked = msg.Content
			break
		}
	}
	if !strings.Contains(blocked, "finalization-only") {
		t.Fatalf("repair update_goal result = %q, want host-side block", blocked)
	}
}

func repeatedReasoningOnlyStops() [][]provider.Chunk {
	return [][]provider.Chunk{
		reasoningOnlyStop("empty attempt one"),
		reasoningOnlyStop("empty attempt two"),
		reasoningOnlyStop("empty attempt three"),
		reasoningOnlyStop("must never be called"),
	}
}

func TestReasoningOnlyWorkflowExhaustionFailsClosed(t *testing.T) {
	t.Run("Plan does not approve or seed stale text", func(t *testing.T) {
		sess := agent.NewSession("")
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous unrelated request"})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: staleDangerousPlan})
		c, prov, capture := newPlanWorkflowController(t, sess, repeatedReasoningOnlyStops())

		err := runPlanWorkflow(t, c)
		if err == nil || !strings.Contains(err.Error(), "visible final answer") {
			t.Fatalf("error = %v, want bounded visible-final failure", err)
		}
		if prov.call != 3 {
			t.Fatalf("provider calls = %d, want exactly 3 bounded attempts", prov.call)
		}
		if got := capture.approvals.Load(); got != 0 {
			t.Fatalf("approval requests = %d, want 0 after recovery exhaustion", got)
		}
		if seed := capture.firstSeed(); seed != "" {
			t.Fatalf("recovery exhaustion seeded stale Plan todos: %s", seed)
		}
		if !c.PlanMode() {
			t.Fatal("Plan mode was disabled despite visible-final recovery failure")
		}
	})

	t.Run("Goal does not invoke evaluator with stale text", func(t *testing.T) {
		c, prov, evaluator := newGoalWorkflowController(t, repeatedReasoningOnlyStops())

		err := runGoalWorkflow(t, c)
		if err == nil || !strings.Contains(err.Error(), "visible final answer") {
			t.Fatalf("error = %v, want bounded visible-final failure", err)
		}
		if prov.call != 3 {
			t.Fatalf("provider calls = %d, want exactly 3 bounded attempts", prov.call)
		}
		if evaluator.calls != 0 || len(evaluator.evidence) != 0 {
			t.Fatalf("evaluator consumed stale evidence after recovery exhaustion: calls/evidence=%d/%d", evaluator.calls, len(evaluator.evidence))
		}
		if got := c.GoalStatus(); got == GoalStatusComplete {
			t.Fatalf("GoalStatus() = %q after visible-final recovery exhaustion", got)
		}
	})

	t.Run("Goal does not commit a report without a visible result", func(t *testing.T) {
		turns := [][]provider.Chunk{
			{{Type: provider.ChunkReasoning, Text: "the goal is verified"}, toolCallChunk("goal-report-exhausted", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
			reasoningOnlyStop("report submitted"),
			reasoningOnlyStop("repair still hidden"),
			reasoningOnlyStop("final repair still hidden"),
		}
		c, prov, evaluator := newGoalWorkflowController(t, turns)

		err := runGoalWorkflow(t, c)
		if err == nil || !strings.Contains(err.Error(), "visible final answer") {
			t.Fatalf("error = %v, want bounded visible-final failure", err)
		}
		if prov.call != 4 {
			t.Fatalf("provider calls = %d, want report + original stop + two repairs (4)", prov.call)
		}
		if evaluator.calls != 0 {
			t.Fatalf("evaluator calls = %d, want none after runner failure", evaluator.calls)
		}
		if got := c.GoalStatus(); got != GoalStatusRunning {
			t.Fatalf("GoalStatus() = %q, want fail-closed running", got)
		}
	})
}

func captureStopHookRunner(t *testing.T) (*hook.Runner, *[]hook.Payload) {
	t.Helper()
	payloads := []hook.Payload{}
	spawner := func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		var payload hook.Payload
		if err := json.Unmarshal([]byte(in.Stdin), &payload); err != nil {
			t.Errorf("decode Stop hook payload: %v", err)
			return hook.SpawnResult{ExitCode: 1}
		}
		payloads = append(payloads, payload)
		return hook.SpawnResult{ExitCode: 0}
	}
	runner := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "capture-stop"},
		Event:      hook.Stop,
	}}, t.TempDir(), spawner, nil)
	return runner, &payloads
}

func TestStopHookNeverReusesPreviousAssistantText(t *testing.T) {
	tests := []struct {
		name string
		turn []provider.Chunk
		want string
	}{
		{name: "reasoning-only current turn reports no visible final", turn: reasoningOnlyStop("current private reasoning"), want: ""},
		{name: "visible current turn reports current final", turn: textTurn("CURRENT visible answer"), want: "CURRENT visible answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := agent.NewSession("")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous question"})
			sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "OLD visible answer"})
			base := &scriptedTurns{turns: [][]provider.Chunk{tt.turn}}
			ag := agent.New(&workflowDeepSeekProvider{base}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
			hooks, payloads := captureStopHookRunner(t)
			c := New(Options{Runner: ag, Executor: ag, Hooks: hooks, Sink: event.Discard})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.runTurnWithRaw(ctx, "current question", "current question"); err != nil {
				t.Fatalf("run current turn: %v", err)
			}
			if base.call != 1 {
				t.Fatalf("provider calls = %d, want ordinary chat behavior unchanged (1)", base.call)
			}
			if len(*payloads) != 1 {
				t.Fatalf("Stop hook payloads = %d, want 1", len(*payloads))
			}
			if got := (*payloads)[0].LastAssistant; got != tt.want {
				t.Fatalf("Stop hook lastAssistantText = %q, want current-turn value %q", got, tt.want)
			}
		})
	}
}
