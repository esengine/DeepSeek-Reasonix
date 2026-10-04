package control

import (
	"context"
	"reasonix/internal/base/testenv"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
)

func waitForDecisions(t *testing.T, c *Controller, want int) []Decision {
	t.Helper()
	deadline := time.Now().Add(testenv.Budget(t))
	for {
		got := c.Decisions()
		if len(got) == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("decisions = %+v, want %d", got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The projection names what waits on the user and who owns it. Kind is the part
// that matters: a frontend may draw one list, but "approve this transition" and
// "which option do you want" are answered by different calls with different
// rules, and flattening them into one Allow/Deny is how a plan starts looking
// like a permission prompt.
func TestDecisionsProjectBothOwnersWithStableIdentity(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()

	// Prompts are serialised by the host, so the two owners are exercised one
	// after the other rather than together.
	go c.requestApproval(context.Background(), approvalRequest{tool: planApprovalTool})
	planCard := waitForDecisions(t, c, 1)[0]
	if planCard.Kind != DecisionPlanApproval || planCard.ID == "" {
		t.Fatalf("plan projection = %+v, want an identified plan_approval", planCard)
	}
	if len(planCard.Questions) != 0 {
		t.Errorf("a plan approval is not a question: %+v", planCard)
	}

	// Repeated snapshots of unchanged state are identical, or a frontend rebuilds
	// cards nobody touched — and loses what the user typed into them.
	if again := c.Decisions(); len(again) != 1 || again[0].ID != planCard.ID || again[0].Kind != planCard.Kind {
		t.Fatalf("second snapshot = %+v, want the same as %+v", again, planCard)
	}
	if err := c.ResolvePlanDecision(planCard.ID, PlanDecisionExitPlan); err != nil {
		t.Fatalf("exit: %v", err)
	}
	waitForDecisions(t, c, 0)

	go c.Ask(context.Background(), []event.AskQuestion{{
		ID: "q1", Header: "Store", Prompt: "Delete or archive?", Reason: event.AskReasonUserDecision,
		Options: []event.AskOption{{Label: "Archive"}, {Label: "Delete"}},
	}})
	ask := waitForDecisions(t, c, 1)[0]
	if ask.Kind != DecisionAsk || ask.ID == "" {
		t.Fatalf("ask projection = %+v, want an identified ask", ask)
	}
	if len(ask.Questions) != 1 || ask.Questions[0].Reason != event.AskReasonUserDecision {
		t.Errorf("ask projection lost the question or its reason: %+v", ask)
	}
	if ask.ID == planCard.ID {
		t.Errorf("the two owners issued the same identity %q", ask.ID)
	}

	// Answering removes it from the projection, because the projection is
	// derived from the owner rather than tracked alongside it.
	c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Archive"}}})
	waitForDecisions(t, c, 0)
}

// The projection is what a frontend uses to tell an open prompt from one
// answered in another window, so a pending approval missing from it reads as
// already decided: the card seals, its buttons go, and the run it is blocking
// waits for an answer nobody can give any more. Ordinary tool permission was
// the kind left out.
func TestOrdinaryToolApprovalIsProjectedWhileItBlocksTheRun(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()

	go c.requestApproval(context.Background(), approvalRequest{tool: "bash", subject: "git branch -a"})
	card := waitForDecisions(t, c, 1)[0]
	if card.Kind != DecisionToolApproval || card.ID == "" {
		t.Fatalf("bash projection = %+v, want an identified tool_approval", card)
	}
	if len(card.Questions) != 0 {
		t.Errorf("an approval is not a question: %+v", card)
	}

	c.Approve(card.ID, true, false, false)
	waitForDecisions(t, c, 0)
}

// Three owners answer an approval — Approve, ResolvePlanDecision,
// ResolveRecovery — and the kind is how a frontend routes back to the right
// one. Reading it off the entry's shape keeps a recovery card from being
// offered the ordinary allow/deny pair, which resolves nothing.
func TestApprovalKindsNameTheCallThatAnswersThem(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()

	tool, _ := c.approval.registerDecisionKind("bash", "git branch -a", "", false, true, "", nil)
	plan, _ := c.approval.registerDecisionKind(planApprovalTool, "", "", true, false, "", nil)
	guard, _ := c.approval.registerDecisionKind("write_file", "src/main.go", "", false, true, "recovery",
		&event.RecoveryApproval{FailedTool: "bash", NextTool: "write_file"})

	want := map[string]DecisionKind{
		tool:  DecisionToolApproval,
		plan:  DecisionPlanApproval,
		guard: DecisionRecoveryApproval,
	}
	got := map[string]DecisionKind{}
	for _, d := range waitForDecisions(t, c, len(want)) {
		got[d.ID] = d.Kind
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("decision %s projected %q, want %q", id, got[id], kind)
		}
	}
}

// Reading the projection changes nothing. A client that never asks for it, asks
// twice, or ignores it entirely must leave the lifecycle exactly where it was —
// otherwise it is state wearing a projection's name.
func TestReadingDecisionsChangesNothing(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()
	c.SetPlanMode(true)

	before := c.plan().State()
	for range 3 {
		c.Decisions()
	}
	if got := c.plan().State(); got != before {
		t.Fatalf("reading the projection moved the lifecycle %+v → %+v", before, got)
	}
	if !c.PlanMode() || c.PlanPhase() != planmode.Planning {
		t.Fatalf("phase = %v plan = %v after reading decisions", c.PlanPhase(), c.PlanMode())
	}
}

// An identity that is no longer current answers nothing. The card the user
// clicked was about a state that has moved on, and the owner is the only party
// that can say so — which is why the action routes back to it instead of
// mutating the projection.
func TestActingOnAStaleDecisionResolvesNothing(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()

	go c.requestApproval(context.Background(), approvalRequest{tool: planApprovalTool})
	shown := waitForDecisions(t, c, 1)[0]

	if err := c.ResolvePlanDecision(shown.ID, PlanDecisionRevisePlan); err != nil {
		t.Fatalf("revise: %v", err)
	}
	if err := c.ResolvePlanDecision(shown.ID, PlanDecisionStartExecution); err == nil {
		t.Fatal("a decision answered twice started execution the second time")
	}
	if got := c.Decisions(); len(got) != 0 {
		t.Fatalf("decisions = %+v, want none once the owner resolved it", got)
	}
}

// A stale answer must not land on a newer question. Ask identities are issued
// per batch, so the second question is a different decision, not the first one
// updated in place.
func TestAStaleAskAnswerDoesNotAnswerANewerQuestion(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	c.EnableInteractiveApproval()

	question := []event.AskQuestion{{
		ID: "q1", Header: "Store", Prompt: "Which?",
		Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
	}}
	go c.Ask(context.Background(), question)
	first := waitForDecisions(t, c, 1)[0]
	c.AnswerQuestion(first.ID, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"A"}}})
	waitForDecisions(t, c, 0)

	go c.Ask(context.Background(), question)
	second := waitForDecisions(t, c, 1)[0]
	if second.ID == first.ID {
		t.Fatalf("a new question reused identity %q; a stale answer would land on it", second.ID)
	}
	c.AnswerQuestion(first.ID, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"B"}}})
	if got := c.Decisions(); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("the stale answer disturbed the open question: %+v", got)
	}
	c.AnswerQuestion(second.ID, []event.AskAnswer{{QuestionID: "q1", Selected: []string{"B"}}})
	waitForDecisions(t, c, 0)
}

// The two status fields together, because the pair is the compatibility
// contract: an old client reads `plan` and behaves as it always did, a new one
// reads `plan_phase` and can finally say "executing an approved plan".
func TestPlanPhaseAndLegacyFlagProjectTogether(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	defer c.Close()
	for _, tc := range []struct {
		action planmode.Action
		phase  planmode.Phase
		legacy bool
		wire   string
	}{
		{planmode.Enter, planmode.Planning, true, "planning"},
		{planmode.Submit, planmode.AwaitingApproval, true, "awaiting_approval"},
		{planmode.Start, planmode.Executing, false, "executing"},
		{planmode.Exit, planmode.Inactive, false, ""},
	} {
		if _, ok := c.plan().Apply(tc.action); !ok {
			t.Fatalf("%v refused", tc.action)
		}
		if got := c.PlanMode(); got != tc.legacy {
			t.Errorf("%v: plan = %v, want %v", tc.phase, got, tc.legacy)
		}
		if got := c.PlanPhase(); got != tc.phase {
			t.Errorf("phase = %v, want %v", got, tc.phase)
		}
		// Inactive is absent on the wire rather than a fourth string: a phase
		// present at all means the run belongs to a plan lifecycle.
		wire := ""
		if p := c.PlanPhase(); p != planmode.Inactive {
			wire = p.String()
		}
		if wire != tc.wire {
			t.Errorf("%v projects plan_phase %q, want %q", tc.phase, wire, tc.wire)
		}
	}
}
