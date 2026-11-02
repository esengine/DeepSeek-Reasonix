package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/store"
)

// goalRuntimeController wires a controller whose goal turns carry no
// update_goal report, so the bounded evaluator decides every disposition. It
// returns the TurnDone/Notice channel for waiting.
func goalRuntimeController(t *testing.T, prov provider.Provider, eval goaleval.Evaluator) (*Controller, *agent.Agent, <-chan event.Event) {
	t.Helper()
	return goalRuntimeControllerWithTokenBudget(t, prov, eval, 0)
}

func goalRuntimeControllerWithTokenBudget(t *testing.T, prov provider.Provider, eval goaleval.Evaluator, tokens int) (*Controller, *agent.Agent, <-chan event.Event) {
	t.Helper()
	ag := agent.New(prov, goalRegistry(), sessionstore.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:          ag,
		Executor:        ag,
		GoalEvaluator:   eval,
		GoalTokenBudget: tokens,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})
	return c, ag, events
}

// waitGoalTurnDone drains notices until the goal loop's TurnDone.
func waitGoalTurnDone(t *testing.T, events <-chan event.Event) {
	t.Helper()
	for e := range events {
		if e.Kind == event.TurnDone {
			return
		}
	}
	t.Fatal("goal loop ended without TurnDone")
}

// TestSimpleGoalWithoutReportCompletesViaEvaluator pins the acceptance
// criterion: a simple Q&A goal whose model never calls update_goal still ends
// on the first turn when the evaluator says complete.
func TestSimpleGoalWithoutReportCompletesViaEvaluator(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("Here is the answer.")}}
	c, _, events := goalRuntimeController(t, prov, &fakeGoalEvaluator{outcome: goaleval.OutcomeComplete, reason: "the question is fully answered"})

	c.Submit("/goal explain the cache behavior")
	waitGoalTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (evaluator decides on the first turn, no second round)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
}

func TestGoalEvaluatorUsageCommitsBeforeFSMCompletion(t *testing.T) {
	sink := NewGoalUsageTee(event.Discard)
	mainProv := &scriptedTurns{turns: [][]provider.Chunk{textTurn("Here is the answer.")}}
	evalProv := &scriptedTurns{turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: `{"outcome":"complete","reason":"done"}`},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 60, CompletionTokens: 17, TotalTokens: 77}},
		{Type: provider.ChunkDone},
	}}}
	executor := agent.New(mainProv, goalRegistry(), sessionstore.NewSession(""), agent.Options{}, sink)
	evaluator := goaleval.NewSessionWithSink(evalProv, nil, "test/evaluator", sink)
	c := New(Options{Runner: executor, Executor: executor, GoalEvaluator: evaluator, Sink: sink})
	c.SetGoal("answer once")
	if err := newTurnOrchestrator(c).runTurnLoop(context.Background(), orchestratedTurn{input: "answer", raw: "answer"}); err != nil {
		t.Fatal(err)
	}
	if c.GoalStatus() != GoalStatusComplete {
		t.Fatalf("status = %q, want complete", c.GoalStatus())
	}
	if got := c.GoalRuntime().TokensUsed; got != 77 {
		t.Fatalf("evaluator usage = %d, want 77 committed before FSM completion", got)
	}
}

// TestEvaluatorOutcomesDriveFSM covers the evaluator verdict matrix.
func TestEvaluatorOutcomesDriveFSM(t *testing.T) {
	cases := []struct {
		name       string
		outcome    goaleval.Outcome
		wantStatus string
		wantCause  string
	}{
		{"complete", goaleval.OutcomeComplete, GoalStatusComplete, ""},
		{"blocked", goaleval.OutcomeBlocked, GoalStatusBlocked, ""},
		{"uncertain fails closed", goaleval.OutcomeUncertain, GoalStatusBlocked, stopCauseEvaluator},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			turn := textTurn("done.")
			// A continue verdict loops forever unless a budget is configured.
			budget := 0
			if tc.wantStatus == GoalStatusRunning {
				budget = 1
				turn = []provider.Chunk{
					{Type: provider.ChunkText, Text: "done."},
					{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, RequestCount: 1}},
					{Type: provider.ChunkDone},
				}
			}
			prov := &scriptedTurns{turns: [][]provider.Chunk{turn}}
			c, _, events := goalRuntimeControllerWithTokenBudget(t, prov, &fakeGoalEvaluator{outcome: tc.outcome, reason: "verdict"}, budget)
			c.Submit("/goal assess the impact")
			waitGoalTurnDone(t, events)
			if got := c.GoalStatus(); got != tc.wantStatus {
				t.Fatalf("GoalStatus() = %q, want %q", got, tc.wantStatus)
			}
			if rt := c.GoalRuntime(); rt.StopCause != tc.wantCause {
				t.Fatalf("StopCause = %q, want %q", rt.StopCause, tc.wantCause)
			}
		})
	}
}

// TestEvaluatorErrorPausesFirstTurn pins fail-closed: an erroring evaluator
// pauses the goal on the first turn without looping to a fixed cap.
func TestEvaluatorErrorPausesFirstTurn(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done.")}}
	c, _, events := goalRuntimeController(t, prov, &fakeGoalEvaluator{err: context.DeadlineExceeded})
	c.Submit("/goal evaluate this")
	waitGoalTurnDone(t, events)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked (fail closed)", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != stopCauseEvaluator || rt.TurnsUsed != 1 {
		t.Fatalf("runtime = %+v, want evaluator pause after 1 turn", rt)
	}
}

// TestEvaluatorUnavailablePausesFirstTurn pins the no-evaluator configuration.
func TestEvaluatorUnavailablePausesFirstTurn(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done.")}}
	c, _, events := goalRuntimeController(t, prov, nil)
	c.Submit("/goal evaluate this")
	waitGoalTurnDone(t, events)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked (evaluator unavailable)", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != stopCauseEvaluator {
		t.Fatalf("StopCause = %q, want %q", rt.StopCause, stopCauseEvaluator)
	}
}

// TestEvaluatorCompleteStillGatedByReadiness: the evaluator's complete claim
// must pass host readiness — seeded incomplete todos keep the goal going.
func TestEvaluatorCompleteStillGatedByReadiness(t *testing.T) {
	g := &goalMachine{goal: "fix everything", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	res := g.advance(goalAdvanceInput{
		evaluator: &goalEvaluatorVerdict{outcome: goaleval.OutcomeComplete, reason: "all done"},
		todos:     []evidence.TodoItem{{Content: "Fix the parser", Status: "in_progress"}},
	})
	if !res.cont || g.status != GoalStatusRunning || g.stopCause != "" {
		t.Fatalf("readiness-rejected complete should continue: result=%+v runtime=%+v", res, g.runtimeView())
	}
}

// TestGoalUsageTeeAttributesScopedBillableCallsAndExcludesTitle covers the
// observational token accounting surface: executor/subagent-style usage counts,
// title generation does not.
func TestGoalUsageTeeAttributesScopedBillableCallsAndExcludesTitle(t *testing.T) {
	tee := NewGoalUsageTee(event.Discard).(*goalUsageTee)
	g := &goalMachine{goal: "ship it", status: GoalStatusRunning}
	g.budgetClass = budgetClassWrite
	g.turnsLimit = unlimitedGoalTurns
	g.tokensLimit = 0
	g.noProgressLimit = 0
	g.scopeID = newGoalScopeID()
	rec := g.newTurnRecorder(g.scopeID, g.continuationEpoch)
	tee.setActiveRecorder(rec)

	usage := func(tokens int) *provider.Usage { return &provider.Usage{TotalTokens: tokens, RequestCount: 1} }
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(100), UsageSource: event.UsageSourceExecutor})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(200), UsageSource: event.UsageSourcePlanner})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(300), UsageSource: event.UsageSourceSubagent})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(400), UsageSource: event.UsageSourceCompaction})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(500), UsageSource: event.UsageSourceRecoveryReviewer})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(600), UsageSource: event.UsageSourceGoalEvaluator})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(700), UsageSource: event.UsageSourceCapabilityRouter})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(800), UsageSource: event.UsageSourceClassifier})
	// Title generation and unrelated background calls never count.
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(900), UsageSource: event.UsageSourceTitle})
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(1000), UsageSource: event.UsageSourceTitle})

	if rec.usageTokens() != 100+200+300+400+500+600+700+800 {
		t.Fatalf("usageTokens = %d, want 3600", rec.usageTokens())
	}
	if g.tokensUsed != 3600 {
		t.Fatalf("live goal tokens = %d, want 3600", g.tokensUsed)
	}
	if g.requestsUsed != 8 || rec.requestsUsed != 8 {
		t.Fatalf("requests = goal:%d recorder:%d, want 8", g.requestsUsed, rec.requestsUsed)
	}

	// No active goal turn → nothing folds.
	tee.setActiveRecorder(nil)
	tee.Emit(event.Event{Kind: event.Usage, Usage: usage(50), UsageSource: event.UsageSourceExecutor})
	if rec.usageTokens() != 3600 {
		t.Fatalf("usageTokens after span close = %d, want 3600", rec.usageTokens())
	}
}

func TestMaxRunWorkDurationTakesOnlyNewAssistantMaximum(t *testing.T) {
	messages := []provider.Message{
		{Role: provider.RoleAssistant, WorkDurationMs: 99_000},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, WorkDurationMs: 5_000},
		{Role: provider.RoleTool, WorkDurationMs: 50_000},
		{Role: provider.RoleAssistant, WorkDurationMs: 24_000},
	}
	if got := maxRunWorkDuration(messages, 1); got != 24_000 {
		t.Fatalf("max Run work duration = %d, want 24000", got)
	}
}
func TestGoalLegacyBudgetTokensSidecarAutoResumes(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	// Old sidecar: paused solely because of the removed token hard limit.
	state := goalState{
		Goal:             "应用打开设置时崩溃",
		Status:           GoalStatusBlocked,
		StopCause:        stopCauseBudgetTokens,
		Block:            "token budget exhausted (0/200000 tokens used)",
		BudgetClass:      budgetClassWrite,
		TurnsUsed:        1,
		TurnsLimit:       20,
		TokensUsed:       214_000,
		TokensLimit:      200_000,
		BudgetExtensions: 0,
		NoProgressLimit:  0,
		Todos: []evidence.TodoItem{{
			Content: "verify the repaired model mapping", Status: "in_progress",
		}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionGoalState(path), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	migPath, migData, migrated, _ := g.restoreFromState(path)
	if !migrated {
		t.Fatal("legacy budget_tokens pause must migrate")
	}
	if g.status != GoalStatusRunning || g.stopCause != "" {
		t.Fatalf("status/stopCause = %q/%q, want running/empty", g.status, g.stopCause)
	}
	if g.block != "" {
		t.Fatalf("block = %q, want empty after legacy token pause migration", g.block)
	}
	if g.tokensUsed != 214_000 {
		t.Fatalf("tokensUsed = %d, want preserved 214000", g.tokensUsed)
	}
	if g.tokensLimit != 0 {
		t.Fatalf("tokensLimit = %d, want 0", g.tokensLimit)
	}
	if g.turnsUsed != 1 || g.turnsLimit != unlimitedGoalTurns {
		t.Fatalf("turns = %d/%d, want 1/unlimited", g.turnsUsed, g.turnsLimit)
	}
	if err := g.writeStateErr(migPath, migData); err != nil {
		t.Fatal(err)
	}
	var migratedState goalState
	if err := json.Unmarshal(migData, &migratedState); err != nil {
		t.Fatal(err)
	}
	if len(migratedState.Todos) != 1 || migratedState.Todos[0].Content != "verify the repaired model mapping" {
		t.Fatalf("migration lost persisted todos: %+v", migratedState.Todos)
	}
	// Second load must stay running without re-entering the legacy pause.
	g2 := &goalMachine{}
	if _, _, migrated2, _ := g2.restoreFromState(path); migrated2 {
		t.Fatal("normalized sidecar migrated a second time")
	}
	if g2.status != GoalStatusRunning || g2.stopCause != "" {
		t.Fatalf("second load = %q/%q, want running/empty", g2.status, g2.stopCause)
	}
}

func TestGoalLargeTokenUsageDoesNotExhaustBudget(t *testing.T) {
	g := &goalMachine{
		goal: "ship", status: GoalStatusRunning,
		budgetClass: budgetClassSimple, turnsLimit: unlimitedGoalTurns, tokensUsed: 900_000, tokensLimit: 0,
		noProgressLimit: 0,
	}
	res := g.advance(goalAdvanceInput{
		report:           &goalTurnReport{status: GoalStatusRunning, reason: "progress"},
		progressEvidence: []string{"new-evidence"},
	})
	if !res.cont {
		t.Fatal("goal with large tokensUsed must continue while turns remain")
	}
}

// TestGoalUsageTotalTokensFallback checks the prompt+completion fallback when
// TotalTokens is missing (never double-counting cache hit/miss).
func TestGoalUsageTotalTokensFallback(t *testing.T) {
	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 90}
	if got := usageTotalTokens(u); got != 120 {
		t.Fatalf("fallback = %d, want 120 (prompt+completion, no cache double count)", got)
	}
	u.TotalTokens = 200
	if got := usageTotalTokens(u); got != 200 {
		t.Fatalf("TotalTokens preferred = %d, want 200", got)
	}
}

// TestGoalSidecarCompatRestoresOldAndNewFields pins the compatibility contract:
// an old sidecar without the budget fields restores with re-derived defaults,
// and a new sidecar's pause (blocked + stopCause) survives a controller rebuild
// without failing open.
func TestGoalSidecarCompatRestoresOldAndNewFields(t *testing.T) {
	t.Run("old sidecar restores with defaults", func(t *testing.T) {
		dir := testenv.TempDir(t)
		path := filepath.Join(dir, "session.jsonl")
		// Old sidecar: only goal/status/turns — no budget fields.
		data := []byte(`{"goal":"legacy goal","status":"running","turns":3}`)
		if err := os.WriteFile(store.SessionGoalState(path), data, 0o600); err != nil {
			t.Fatal(err)
		}
		exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
		c.Resume(sessionstore.NewSession("sys"), path)
		rt := c.GoalRuntime()
		if rt.TurnsUsed != 3 {
			t.Fatalf("TurnsUsed = %d, want 3 (legacy Turns carried over)", rt.TurnsUsed)
		}
		if rt.TokensUsed != 0 {
			t.Fatalf("TokensUsed = %d, want 0 (no legacy token record)", rt.TokensUsed)
		}
		if rt.TurnsLimit != 0 || rt.NoProgressLimit != 0 {
			t.Fatalf("removed limits resurfaced: %+v", rt)
		}
		if rt.TokensLimit != 0 {
			t.Fatalf("TokensLimit = %d, want 0 when no budget is configured", rt.TokensLimit)
		}
	})

	t.Run("removed numeric pause auto-migrates on rebuild", func(t *testing.T) {
		dir := testenv.TempDir(t)
		path := filepath.Join(dir, "session.jsonl")
		exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
		c.SetGoal("ship the release")
		c.goals.pauseFor(stopCauseBudgetTurns, "turn budget exhausted", nil)
		statePath, data, ok := c.goals.buildStateLocked(nil)
		if !ok {
			t.Fatal("no persisted state")
		}
		if err := os.WriteFile(statePath, data, 0o600); err != nil {
			t.Fatal(err)
		}

		freshExec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
		fresh := New(Options{Executor: freshExec, SessionDir: dir, Label: "fresh"})
		fresh.Resume(sessionstore.NewSession("sys"), path)
		if fresh.GoalStatus() != GoalStatusRunning {
			t.Fatalf("restored status = %q, want running after numeric pause migration", fresh.GoalStatus())
		}
		if rt := fresh.GoalRuntime(); rt.StopCause != "" || rt.TurnsLimit != 0 {
			t.Fatalf("restored runtime = %+v, want continuous Goal", rt)
		}
	})
}

// TestGoalPauseResumeCommands covers the /goal pause and /goal resume CLI
// surface plus the runtime view.
func TestGoalPauseResumeCommands(t *testing.T) {
	cmd, ok := ParseGoalCommand("/goal pause")
	if !ok || cmd.Action != GoalCommandPause {
		t.Fatalf("ParseGoalCommand(/goal pause) = %+v", cmd)
	}
	cmd, ok = ParseGoalCommand("/goal resume")
	if !ok || cmd.Action != GoalCommandResume {
		t.Fatalf("ParseGoalCommand(/goal resume) = %+v", cmd)
	}
	cmd, ok = ParseGoalCommand("/goal")
	if !ok || cmd.Action != GoalCommandStatus {
		t.Fatalf("ParseGoalCommand(/goal) = %+v", cmd)
	}

	c := New(Options{Sink: event.Discard})
	if c.PauseGoal() {
		t.Fatal("PauseGoal without a goal must return false")
	}
	c.SetGoal("long-running research")
	if !c.PauseGoal() {
		t.Fatal("PauseGoal on a running goal must return true")
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != stopCauseManual {
		t.Fatalf("StopCause = %q, want manual", rt.StopCause)
	}
	// The goal text and budget survive the pause.
	if got := c.Goal(); got != "long-running research" {
		t.Fatalf("Goal() = %q, want preserved", got)
	}
	if !c.ResumeGoal() {
		t.Fatal("ResumeGoal on a manually paused goal must return true")
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("GoalStatus() after resume = %q, want running", got)
	}
	if rt := c.GoalRuntime(); rt.StopCause != "" {
		t.Fatalf("StopCause after resume = %q, want cleared", rt.StopCause)
	}
}

// TestGoalRuntimeViewPopulatesFromController covers the runtime view surface
// the CLI and desktop read.
func TestGoalRuntimeViewPopulatesFromController(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	c.SetGoal("finish the migration")
	rt := c.GoalRuntime()
	if rt.TurnsUsed != 0 || rt.TurnsLimit != 0 || rt.NoProgressLimit != 0 {
		t.Fatalf("runtime view = %+v, want continuous defaults", rt)
	}
	if rt.TokensLimit != 0 {
		t.Fatalf("TokensLimit = %d, want 0 (no hard token limit)", rt.TokensLimit)
	}
}

// TestFooterTextDoesNotDriveGoalState pins the acceptance criterion: a
// historical [goal:complete] footer in the latest answer never influences the
// FSM — only the structured tool report does.
func TestFooterTextDoesNotDriveGoalState(t *testing.T) {
	g := &goalMachine{goal: "migrate the storage", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	res := g.advance(goalAdvanceInput{evaluator: &goalEvaluatorVerdict{outcome: goaleval.OutcomeContinue, reason: "work is ongoing"}})
	if !res.cont || g.status != GoalStatusRunning {
		t.Fatalf("plain footer-equivalent text changed Goal state: result=%+v runtime=%+v", res, g.runtimeView())
	}
}

// minimalFakeTool is a no-op tool for delivery-flow tests.
type minimalFakeTool struct {
	name     string
	readOnly bool
}

func (f minimalFakeTool) Name() string            { return f.name }
func (f minimalFakeTool) Description() string     { return "" }
func (f minimalFakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f minimalFakeTool) ReadOnly() bool          { return f.readOnly }
func (f minimalFakeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.name + " done", nil
}
