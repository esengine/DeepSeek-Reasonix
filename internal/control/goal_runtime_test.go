package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/goaleval"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
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
	ag := agent.New(prov, goalRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
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
	executor := agent.New(mainProv, goalRegistry(), agent.NewSession(""), agent.Options{}, sink)
	evaluator := goaleval.NewSessionWithSink(evalProv, nil, "test/evaluator", sink)
	c := New(Options{Runner: executor, Executor: executor, GoalEvaluator: evaluator, Sink: sink})
	c.SetGoal("answer once")
	if err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "answer", "answer", ""); err != nil {
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

// TestTurnTokenNoProgressPausesAndResumeExtendsBudget covers the outer turn
// budget, observational no-progress state, and the resume extension contract.
// Token hard limits no longer pause goals.
// TestGoalTurnRecorderProtocol covers idempotency, upgrades, terminal
// conflicts, and stale-epoch rejection.
func TestGoalTurnRecorderProtocol(t *testing.T) {
	newRec := func(t *testing.T) (*goalMachine, *goalTurnRecorder) {
		t.Helper()
		g := &goalMachine{goal: "fix it", status: GoalStatusRunning}
		g.scopeID = newGoalScopeID()
		rec := g.newTurnRecorder(g.scopeID, g.continuationEpoch)
		return g, rec
	}
	report := func(status, reason string) tool.GoalReport {
		return tool.GoalReport{Status: status, Reason: reason, NextAction: ""}
	}

	t.Run("idempotent same value", func(t *testing.T) {
		_, rec := newRec(t)
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "working")); err != nil {
			t.Fatal(err)
		}
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "working")); err != nil {
			t.Fatalf("identical repeat must be idempotent: %v", err)
		}
		if got := rec.validReport(rec.epoch); got == nil || got.status != GoalStatusRunning {
			t.Fatalf("validReport = %+v", got)
		}
	})

	t.Run("wire continue maps to the running FSM state", func(t *testing.T) {
		_, rec := newRec(t)
		got, err := rec.RecordGoalReport(report("continue", "working"))
		if err != nil {
			t.Fatal(err)
		}
		if got != "update_goal: continue recorded for this turn." {
			t.Fatalf("tool result = %q", got)
		}
		if got := rec.validReport(rec.epoch); got == nil || got.status != GoalStatusRunning {
			t.Fatalf("validReport = %+v, want internal running status", got)
		}
	})

	t.Run("continue upgrades to complete", func(t *testing.T) {
		_, rec := newRec(t)
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "working")); err != nil {
			t.Fatal(err)
		}
		if _, err := rec.RecordGoalReport(report(GoalStatusComplete, "")); err != nil {
			t.Fatalf("continue → complete upgrade must be allowed: %v", err)
		}
		if got := rec.validReport(rec.epoch); got == nil || got.status != GoalStatusComplete {
			t.Fatalf("validReport = %+v, want complete", got)
		}
	})

	t.Run("terminal conflicts rejected", func(t *testing.T) {
		_, rec := newRec(t)
		if _, err := rec.RecordGoalReport(report(GoalStatusComplete, "")); err != nil {
			t.Fatal(err)
		}
		if _, err := rec.RecordGoalReport(report(GoalStatusBlocked, "actually stuck")); err == nil {
			t.Fatal("terminal complete must reject a later blocked report")
		}
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "just kidding")); err == nil {
			t.Fatal("terminal complete must reject a later continue report")
		}
	})

	t.Run("conflicting non-terminal rejected", func(t *testing.T) {
		_, rec := newRec(t)
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "doing A")); err != nil {
			t.Fatal(err)
		}
		if _, err := rec.RecordGoalReport(report(GoalStatusRunning, "doing B")); err == nil {
			t.Fatal("conflicting continue reports must be rejected")
		}
	})

	t.Run("stale epoch invalidates report", func(t *testing.T) {
		g, rec := newRec(t)
		if _, err := rec.RecordGoalReport(report(GoalStatusComplete, "")); err != nil {
			t.Fatal(err)
		}
		// The goal is replaced: epoch bumps, scope rotates.
		g.set("replacement", "", nil)
		if got := rec.validReport(rec.epoch); got != nil {
			t.Fatalf("stale recorder report = %+v, want nil", got)
		}
	})

	t.Run("late record after replacement rejected", func(t *testing.T) {
		g, rec := newRec(t)
		g.set("replacement", "", nil)
		if _, err := rec.RecordGoalReport(report(GoalStatusComplete, "")); err == nil {
			t.Fatal("late record on a replaced goal must be rejected")
		}
	})

	t.Run("usage folds only for matching lifecycle", func(t *testing.T) {
		g, rec := newRec(t)
		rec.addUsage(150)
		if g.tokensUsed != 150 {
			t.Fatalf("tokensUsed = %d, want 150", g.tokensUsed)
		}
		g.set("replacement", "", nil)
		rec.addUsage(50)
		if g.tokensUsed != 0 {
			t.Fatalf("stale usage folded into replacement goal: %d", g.tokensUsed)
		}
	})
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

func TestGoalWorkDurationUsesPerRunMaximumAndRejectsStaleRuns(t *testing.T) {
	g := &goalMachine{goal: "ship", status: GoalStatusRunning, scopeID: newGoalScopeID(), turnsLimit: unlimitedGoalTurns}
	firstEpoch := g.continuationEpoch
	first := g.newTurnRecorder(g.scopeID, firstEpoch)
	first.addWorkDuration(24_000)
	first.addWorkDuration(5_000) // one recorder commits at most once
	if g.workDurationMs != 24_000 {
		t.Fatalf("first Run duration = %d, want 24000", g.workDurationMs)
	}

	g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning}})
	second := g.newTurnRecorder(g.scopeID, g.continuationEpoch)
	second.addWorkDuration(3_000)
	if g.workDurationMs != 27_000 {
		t.Fatalf("cumulative work duration = %d, want 27000", g.workDurationMs)
	}

	g.mu.Lock()
	g.installGoalLocked("replacement", budgetClassSimple)
	g.mu.Unlock()
	second.addWorkDuration(9_000)
	if g.workDurationMs != 0 {
		t.Fatalf("stale Run polluted replacement Goal: %d", g.workDurationMs)
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

func TestBudgetClassForBareFaultIsWrite(t *testing.T) {
	// User-reported Chinese bare fault keeps its legacy compatibility class.
	class := budgetClassForLegacyMode("数据模型管理器又出现历史 BUG 了……", GoalResearchAuto)
	if class != budgetClassWrite {
		t.Fatalf("budget class = %q, want write", class)
	}
	// Consultative / diagnostic fault statements stay simple.
	for _, goal := range []string{
		"为什么会出现这个 BUG？",
		"只分析原因，不要修改代码。",
		"诊断数据库连接失败原因。",
		"复现并定位问题，但不要修复。",
	} {
		if got := budgetClassForLegacyMode(goal, GoalResearchAuto); got != budgetClassSimple {
			t.Errorf("budgetClassFor(%q) = %q, want simple", goal, got)
		}
	}
	// Explicit mutation verbs remain write.
	if got := budgetClassForLegacyMode("fix the crash in settings", GoalResearchAuto); got != budgetClassWrite {
		t.Fatalf("explicit fix class = %q, want write", got)
	}
}

func TestGoalLegacyBudgetTokensSidecarAutoResumes(t *testing.T) {
	dir := t.TempDir()
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
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		// Old sidecar: only goal/status/turns — no budget fields.
		data := []byte(`{"goal":"legacy goal","status":"running","turns":3}`)
		if err := os.WriteFile(store.SessionGoalState(path), data, 0o600); err != nil {
			t.Fatal(err)
		}
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
		c.Resume(agent.NewSession("sys"), path)
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
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
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

		freshExec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
		fresh := New(Options{Executor: freshExec, SessionDir: dir, Label: "fresh"})
		fresh.Resume(agent.NewSession("sys"), path)
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

// TestGoalDeliveryWorkflowCompletesAfterVerifiedSignoff covers the
// Goal + Delivery combination: the model works (edit → verify → review →
// complete_step), reports complete via update_goal, and the goal completes —
// no user-facing recovery card.
func TestGoalDeliveryWorkflowCompletesAfterVerifiedSignoff(t *testing.T) {
	todoWrite, _ := tool.LookupBuiltin("todo_write")
	completeStep, _ := tool.LookupBuiltin("complete_step")
	reg := goalRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)
	reg.Add(minimalFakeTool{name: "write_file"})
	reg.Add(minimalFakeTool{name: "read_file", readOnly: true})
	reg.Add(minimalFakeTool{name: "bash"})

	prov := &scriptedTurns{turns: flattenTurns(
		[][]provider.Chunk{
			{toolCallChunk("t0", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
			{toolCallChunk("w1", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
			{toolCallChunk("rv", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
			{toolCallChunk("vf", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
			{toolCallChunk("sg", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
			{toolCallChunk("ug", "update_goal", `{"status":"complete","reason":""}`), {Type: provider.ChunkDone}},
			textTurn("Ship main delivered."),
		},
	)}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{DeliveryProfile: true}, event.Discard)
	done := make(chan event.Event, 1)
	var doneReadiness *event.FinalReadiness
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				doneReadiness = e.Readiness
				done <- e
			}
		}),
	})
	c.Submit("/goal implement main")
	<-done

	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete after verified sign-off", got)
	}
	if doneReadiness != nil {
		t.Fatalf("TurnDone.Readiness = %+v, want nil (Goal absorbs readiness; no recovery card)", doneReadiness)
	}
	if got := c.Goal(); got != "" {
		t.Fatalf("completed goal should be cleared, got %q", got)
	}
}

// TestPlainDeliveryReadinessFailureSurfacesRecoveryCardWithoutRetries covers
// the plain (non-Goal) Delivery combination: readiness failure ends the run on
// the first final answer, surfaces the recovery card, and never auto-continues.
func TestPlainDeliveryReadinessFailureSurfacesRecoveryCardWithoutRetries(t *testing.T) {
	todoWrite, _ := tool.LookupBuiltin("todo_write")
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(minimalFakeTool{name: "write_file"})
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		{toolCallChunk("w1", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("t0", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		textTurn("premature final"),
		textTurn("extra turn that must never run"),
	}}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{DeliveryProfile: true}, event.Discard)
	done := make(chan event.Event, 1)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				done <- e
			}
		}),
	})

	c.Submit("implement main")
	ev := <-done
	if ev.Readiness == nil || len(ev.Readiness.Missing) == 0 {
		t.Fatalf("TurnDone.Readiness = %+v, want missing requirements for the recovery card", ev.Readiness)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 (work turn + final answer, no readiness retries)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusStopped {
		t.Fatalf("GoalStatus() = %q, want stopped (no goal involved)", got)
	}
}
