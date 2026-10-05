package control

import (
	"context"
	"fmt"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/runtime/coordinator"
)

// visibleTurnWork is the model's own output. A turn's identity has to exist
// before any of it: a start that arrives after the planner has already spoken
// names a turn whose first half nothing could be attributed to.
func visibleTurnWork(kind event.Kind) bool {
	switch kind {
	case event.Phase, event.Reasoning, event.Text, event.Message, event.ToolDispatch:
		return true
	}
	return false
}

type denyingPlanApprover struct{}

func (denyingPlanApprover) RunWithPlannerApproval(context.Context, string, func(context.Context) error) error {
	return nil
}

type boundaryRoute struct {
	name     string
	planner  []testutil.Turn
	tools    []tool.Tool
	approver agent.PlannerPlanApprover
	policy   agent.PlannerDecision
	// executes says whether this route reaches the executor at all. Without it
	// a route that quietly took another branch would still satisfy the matrix.
	executes bool
}

// twoModelTurn runs one user turn through the production topology — a
// Coordinator as the controller's runner — and returns the events it produced
// with the executor session it left behind.
func twoModelTurn(t *testing.T, route boundaryRoute, prompt string) ([]event.Event, []provider.Message) {
	t.Helper()
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(testutil.NewMock("exec", testutil.Turn{Text: "executor answered"}),
		tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	plannerTools := tool.NewRegistry()
	for _, tl := range route.tools {
		plannerTools.Add(tl)
	}
	sink, done, events := collectSink()
	coordinator := coordinator.NewCoordinatorWithPlannerPolicy(
		testutil.NewMock("planner", route.planner...), sessionstore.NewSession("planner sys"), nil,
		plannerTools, agent.Options{}, exec, 0, sink,
		func(context.Context, string) agent.PlannerDecision { return route.policy },
	)
	if route.approver != nil {
		coordinator.SetPlannerPlanApprover(route.approver)
	}
	c := New(Options{
		Runner: coordinator, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	defer c.autosaveWG.Wait()
	c.Submit(prompt)
	waitForDone(t, done)
	return events(), sess.Snapshot()
}

// TestEveryTopLevelRouteOpensOneNamedTurn is the route matrix: whichever model
// does the work, and whether or not any of it executes, a user turn gets one
// start, it carries the identity, it precedes the work, and the message it
// named is the authored turn it said.
func TestEveryTopLevelRouteOpensOneNamedTurn(t *testing.T) {
	planOnly := []testutil.Turn{{Text: "1. do the thing"}}
	planAndExecute := agent.PlannerDecision{Route: agent.PlannerRoutePlanAndExecute, Depth: agent.PlannerDepthFull, Reason: "matrix"}
	approvalPlan := `{"objective":"o","requires_approval":true,"steps":[{"title":"s1"}]}`

	for _, route := range []boundaryRoute{{
		name: "executor_only", executes: true,
		policy: agent.PlannerDecision{Route: agent.PlannerRouteExecutorOnly, Depth: agent.PlannerDepthNone, Reason: "matrix"},
	}, {
		name: "plan_and_execute", planner: planOnly, policy: planAndExecute, executes: true,
	}, {
		name: "conclude_no_changes", policy: planAndExecute,
		tools: []tool.Tool{agent.NewConcludeNoChangesTool()},
		planner: []testutil.Turn{
			{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "conclude_no_changes", Arguments: `{"reason":"already satisfied"}`}}},
			{Text: "nothing to do"},
		},
	}, {
		name: "approval_denied", policy: planAndExecute, approver: denyingPlanApprover{},
		tools: []tool.Tool{&agent.SubmitPlanTool{}},
		planner: []testutil.Turn{
			{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "submit_plan", Arguments: approvalPlan}}},
			{Text: "plan submitted"},
		},
	}, {
		name: "planner_failure_fallback", policy: planAndExecute, executes: true,
		planner: []testutil.Turn{{StreamError: fmt.Errorf("planner is down")}},
	}} {
		t.Run(route.name, func(t *testing.T) {
			evs, messages := twoModelTurn(t, route, "第一句用户输入")
			if got := executorRan(messages); got != route.executes {
				t.Fatalf("executor ran = %v, want %v: this route did not take the branch it names", got, route.executes)
			}
			requireOneNamedTurn(t, evs, messages)
		})
	}
}

func executorRan(messages []provider.Message) bool {
	for _, m := range messages {
		if m.Role == provider.RoleAssistant && m.Content == "executor answered" {
			return true
		}
	}
	return false
}

func requireOneNamedTurn(t *testing.T, evs []event.Event, messages []provider.Message) {
	t.Helper()
	named, bare, dones, namedAt, firstWorkAt := 0, 0, 0, -1, -1
	var identity event.Event
	for i, e := range evs {
		switch {
		case e.Kind == event.TurnStarted && e.AuthoredTurn != nil && e.MsgIndex != nil:
			named++
			namedAt, identity = i, e
		case e.Kind == event.TurnStarted:
			bare++
		case e.Kind == event.TurnDone:
			dones++
		}
		if firstWorkAt < 0 && visibleTurnWork(e.Kind) {
			firstWorkAt = i
		}
	}
	if named != 1 || bare != 0 {
		t.Fatalf("turn starts: %d named, %d bare; want exactly one named and no bare", named, bare)
	}
	if dones != 1 {
		t.Fatalf("turn ends: %d; want exactly one", dones)
	}
	if firstWorkAt >= 0 && namedAt > firstWorkAt {
		t.Fatalf("the turn was named at event %d, after visible work at %d (%v): identity must precede work",
			namedAt, firstWorkAt, evs[firstWorkAt].Kind)
	}
	index, authored := *identity.MsgIndex, *identity.AuthoredTurn
	if index < 0 || index >= len(messages) {
		t.Fatalf("named message %d outside a session of %d", index, len(messages))
	}
	class := sessionstore.ClassifyTurn(messages[index], agent.PriorAuthoredTurn(messages[:index]))
	if messages[index].Role != provider.RoleUser || !class.StartsTurn || class.AuthoredTurn != authored {
		t.Fatalf("named turn %d at message %d; that message is %s authored=%d starts=%v",
			authored, index, messages[index].Role, class.AuthoredTurn, class.StartsTurn)
	}
}

// TestSyntheticContinuationOpensNoTurnOfItsOwn is the negative control. Approved
// plan execution is more work inside the turn the user started, so it must not
// come with a boundary of its own — at either end. A start it never opens and a
// done it still emits would only move the asymmetry to the end of the turn.
func TestSyntheticContinuationOpensNoTurnOfItsOwn(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(testutil.NewMock("exec",
		testutil.Turn{Text: "here is the plan"},
		testutil.Turn{Text: "executed the approved plan"},
	), tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	var c *Controller
	sink, done, events := collectSink()
	approving := event.FuncSink(func(e event.Event) {
		sink.Emit(e)
		if e.Kind == event.ApprovalRequest && e.Approval.Tool == planApprovalTool {
			go c.Approve(e.Approval.ID, true, false, false)
		}
	})
	c = New(Options{
		Runner: exec, Executor: exec, Sink: approving,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.Close()
	defer c.autosaveWG.Wait()
	c.EnableInteractiveApproval()
	c.SetPlanMode(true)
	c.Submit("规划一下这件事")
	waitForDone(t, done)

	starts, dones := 0, 0
	for _, e := range events() {
		switch e.Kind {
		case event.TurnStarted:
			starts++
		case event.TurnDone:
			dones++
		}
	}
	if starts != 1 || dones != 1 {
		t.Fatalf("plan mode ran %d starts and %d ends for one user turn; the approved execution must not open its own",
			starts, dones)
	}
	if !hasSyntheticContinuation(sess.Snapshot()) {
		t.Fatal("no synthetic continuation ran; this test needs the approved execution to have happened")
	}
}

func hasSyntheticContinuation(messages []provider.Message) bool {
	for _, m := range messages {
		if m.Role == provider.RoleUser && sessionstore.IsSyntheticUserText(sessionstore.UserMessageText(m)) {
			return true
		}
	}
	return false
}

// TestSlashInvokedSubagentSkillOpensOneNamedTurn covers the top-level route
// that runs no model in this session at all: a runAs=subagent skill writes the
// user's line into the transcript from the host side. It is still a turn the
// user authored, so it is named like every other one — and the message it names
// lands through the same seam an executed turn's does.
func TestSlashInvokedSubagentSkillOpensOneNamedTurn(t *testing.T) {
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	exec := agent.New(testutil.NewMock("exec", testutil.Turn{Text: "unused"}),
		tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, events := collectSink()
	c := New(Options{
		Runner: exec, Executor: exec, Sink: sink,
		SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"),
		SkillRunner: func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
			return "the child answered", nil
		},
	})
	defer c.Close()
	defer c.autosaveWG.Wait()

	c.runSubagentSkillSlash(skill.Skill{Name: "probe", Path: "(builtin)"}, "跑一下这个技能", "/probe 跑一下这个技能", "")
	waitForDone(t, done)
	requireOneNamedTurn(t, events(), sess.Snapshot())
}
