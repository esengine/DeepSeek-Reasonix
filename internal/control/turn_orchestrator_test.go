package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestTurnOrchestratorRunsForegroundUnit(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})
	c.SetPlanMode(true)

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "draft the plan", "draft the plan", ""); err != nil {
		t.Fatal(err)
	}

	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	if !strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
		t.Fatalf("orchestrator should compose plan marker before running, got %q", runner.inputs[0])
	}
}

func TestTurnOrchestratorTypedSyntheticTurnDoesNotDependOnPrefix(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})
	o := newTurnOrchestrator(c)

	turn := "Controller-created follow-up with a brand-new synthetic wording:\n- inspect\n- edit\n- verify"
	if IsSyntheticUserMessage(turn) {
		t.Fatalf("test setup: %q unexpectedly matched the legacy synthetic prefix list", turn)
	}
	if err := o.runSyntheticTurnWithRawDisplay(context.Background(), turn, turn, ""); err != nil {
		t.Fatal(err)
	}

	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	if strings.HasPrefix(runner.inputs[0], PlanModeMarker) {
		t.Fatalf("typed synthetic turn should remain a plain turn, got %q", runner.inputs[0])
	}
}

func TestTurnOrchestratorStopHookIgnoresCanceledTurnContext(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	var stopCalls int
	var stopErr error
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "record-stop"},
		Event:      hook.Stop,
		Scope:      hook.ScopeProject,
	}}, "", func(ctx context.Context, in hook.SpawnInput) hook.SpawnResult {
		stopCalls++
		stopErr = ctx.Err()
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{
		Runner: cancelingRunner{cancel: cancel},
		Hooks:  hooks,
	})

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(runCtx, "hello", "hello", ""); err != nil {
		t.Fatal(err)
	}

	if runCtx.Err() != context.Canceled {
		t.Fatalf("turn context err = %v, want %v", runCtx.Err(), context.Canceled)
	}
	if stopCalls != 1 {
		t.Fatalf("Stop hook calls = %d, want 1", stopCalls)
	}
	if stopErr != nil {
		t.Fatalf("Stop hook context err = %v, want nil", stopErr)
	}
}

type recordingSessionRunner struct {
	session *agent.Session
	inputs  []string
}

type deliveryScopeErrorRunner struct {
	scopes []agent.DeliveryExecutionScope
}

func (r *deliveryScopeErrorRunner) Run(ctx context.Context, _ string) error {
	if scope, ok := agent.DeliveryExecutionScopeFromContext(ctx); ok {
		r.scopes = append(r.scopes, scope)
	}
	return &agent.FinalReadinessError{Attempts: 3, Reason: "missing verification"}
}

func TestGoalReadinessFailureBlocksAndKeepsDeliveryScope(t *testing.T) {
	runner := &deliveryScopeErrorRunner{}
	c := New(Options{Runner: runner})
	c.SetGoal("ship the integration")

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "")
	var readiness *agent.FinalReadinessError
	if !errors.As(err, &readiness) {
		t.Fatalf("run err = %v, want FinalReadinessError", err)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus = %q, want blocked", got)
	}
	if len(runner.scopes) != 1 || runner.scopes[0].ID == "" || runner.scopes[0].TaskText != "ship the integration" {
		t.Fatalf("delivery scopes = %+v", runner.scopes)
	}
	if !c.ResumeGoal() || c.GoalStatus() != GoalStatusRunning {
		t.Fatal("blocked Goal should resume with its existing scope")
	}
	if id, task, ok := c.goals.deliveryScope(); !ok || id != runner.scopes[0].ID || task != "ship the integration" {
		t.Fatalf("resumed scope = (%q, %q, %v), want preserved id/task", id, task, ok)
	}
}

type recoveryPauseRunner struct {
	scopes []agent.DeliveryExecutionScope
	calls  int
}

func (r *recoveryPauseRunner) Run(ctx context.Context, _ string) error {
	r.calls++
	if scope, ok := agent.DeliveryExecutionScopeFromContext(ctx); ok {
		r.scopes = append(r.scopes, scope)
	}
	return &agent.RecoveryPauseError{
		Message:    "This automatic recovery turn paused to avoid repeated execution. Completed work is kept; send more requirements or reply continue.",
		StopReason: "episode_failures",
	}
}

func TestRecoveryPauseKeepsGoalRunningAndDeliveryScope(t *testing.T) {
	runner := &recoveryPauseRunner{}
	c := New(Options{Runner: runner})
	c.SetGoal("ship the integration")
	if id, task, ok := c.goals.deliveryScope(); !ok || task != "ship the integration" {
		t.Fatalf("initial scope = (%q, %q, %v)", id, task, ok)
	}
	scopeID, _, _ := c.goals.deliveryScope()

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "start", "start", "")
	var pause *agent.RecoveryPauseError
	if !errors.As(err, &pause) {
		t.Fatalf("run err = %v, want RecoveryPauseError", err)
	}
	// Recovery pause ends auto-continue only; Goal must stay running so the next
	// ordinary "continue" keeps the same Goal prompt and delivery scope.
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("GoalStatus = %q, want running after recovery pause", got)
	}
	if id, task, ok := c.goals.deliveryScope(); !ok || id != scopeID || task != "ship the integration" {
		t.Fatalf("scope after pause = (%q, %q, %v), want preserved running Goal", id, task, ok)
	}
	if len(runner.scopes) != 1 || runner.scopes[0].ID != scopeID {
		t.Fatalf("delivery scopes = %+v, want one call with scope %q", runner.scopes, scopeID)
	}

	// A follow-up ordinary Goal turn reuses the same delivery scope without ResumeGoal.
	err = newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "continue", "continue", "")
	if !errors.As(err, &pause) {
		t.Fatalf("follow-up err = %v, want RecoveryPauseError again", err)
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("GoalStatus after continue = %q, want running", got)
	}
	if id, task, ok := c.goals.deliveryScope(); !ok || id != scopeID || task != "ship the integration" {
		t.Fatalf("scope after continue = (%q, %q, %v), want same Goal", id, task, ok)
	}
	if runner.calls != 2 || len(runner.scopes) != 2 || runner.scopes[1].ID != scopeID {
		t.Fatalf("follow-up scopes = %+v calls=%d, want same delivery scope reused", runner.scopes, runner.calls)
	}
}

func (r *recordingSessionRunner) Run(ctx context.Context, input string) error {
	r.inputs = append(r.inputs, input)
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	return nil
}

func TestTurnOrchestratorGoalContinuationRunsStopPerUnit(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Started.\n\n[goal:continue]"),
		textTurn("Finished.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var stopEvents int
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "record-stop"},
		Event:      hook.Stop,
		Scope:      hook.ScopeProject,
	}}, "", func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		var p hook.Payload
		if err := json.Unmarshal([]byte(in.Stdin), &p); err != nil {
			t.Fatalf("hook payload: %v", err)
		}
		if p.Event == hook.Stop {
			stopEvents++
		}
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{Runner: ag, Executor: ag, Hooks: hooks})
	c.SetGoal("ship the refactor")

	o := newTurnOrchestrator(c)
	if err := o.runGoalLoopWithRawDisplay(context.Background(), "Start pursuing the active goal now.", "ship the refactor", ""); err != nil {
		t.Fatal(err)
	}

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want initial + continuation", prov.call)
	}
	if stopEvents != 2 {
		t.Fatalf("Stop hook events = %d, want one per goal-loop turn unit", stopEvents)
	}
}

func TestTurnOrchestratorApprovedPlanSharesOneStopHook(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Make the change\n2. Verify it"),
		textTurn("Done."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	approvalID := make(chan string, 1)
	var promptSubmitEvents, stopEvents int
	hooks := hook.NewRunner([]hook.ResolvedHook{
		{
			HookConfig: hook.HookConfig{Command: "record-submit"},
			Event:      hook.UserPromptSubmit,
			Scope:      hook.ScopeProject,
		},
		{
			HookConfig: hook.HookConfig{Command: "record-stop"},
			Event:      hook.Stop,
			Scope:      hook.ScopeProject,
		},
	}, "", func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		var p hook.Payload
		if err := json.Unmarshal([]byte(in.Stdin), &p); err != nil {
			t.Fatalf("hook payload: %v", err)
		}
		switch p.Event {
		case hook.UserPromptSubmit:
			promptSubmitEvents++
		case hook.Stop:
			stopEvents++
		}
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Hooks:    hooks,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalID <- e.Approval.ID
			}
		}),
	})
	c.SetPlanMode(true)
	go func() { c.Approve(<-approvalID, true, false, false) }()

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "plan this change", "plan this change", ""); err != nil {
		t.Fatal(err)
	}

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want plan + approved execution", prov.call)
	}
	if promptSubmitEvents != 1 {
		t.Fatalf("UserPromptSubmit events = %d, want one for plan + approved execution unit", promptSubmitEvents)
	}
	if stopEvents != 1 {
		t.Fatalf("Stop hook events = %d, want one for plan + approved execution unit", stopEvents)
	}
}

func TestTurnOrchestratorRefTurnRecordsVisibleDisplay(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("referenced evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	events := make(chan event.Event, 4)
	c := New(Options{
		WorkspaceRoot: root,
		Runner:        runner,
		Executor:      exec,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})
	var gotContent, gotDisplay string
	c.SetDisplayRecorder(func(content, display string) {
		gotContent = content
		gotDisplay = display
	})

	const visible = "explain @notes.txt"
	c.runRefTurn(visible, visible)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	if !strings.Contains(runner.inputs[0], "Referenced context:") || !strings.Contains(runner.inputs[0], "referenced evidence") {
		t.Fatalf("model input should include resolved reference context, got %q", runner.inputs[0])
	}
	if gotDisplay != visible {
		t.Fatalf("display recorder display = %q, want visible prompt %q", gotDisplay, visible)
	}
	if gotContent != runner.inputs[0] {
		t.Fatalf("display recorder content = %q, want persisted model input %q", gotContent, runner.inputs[0])
	}
}

func TestTurnOrchestratorAutoReasoningLanguageUsesRawPromptForRefTurns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package main\nfunc AuthHandler() error { return errors.New(\"not authorized\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeTurnRunner{}
	events := make(chan event.Event, 4)
	c := New(Options{
		WorkspaceRoot: root,
		Runner:        runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})

	const visible = "解释 @auth.go 的报错"
	c.runRefTurn(visible, visible)
	waitForTurnDone(t, events)

	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %d, want 1", len(runner.inputs))
	}
	got := runner.inputs[0]
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") {
		t.Fatalf("auto reasoning language should anchor Chinese before referenced context, got %q", got)
	}
	if !strings.Contains(got, "Referenced context:") || !strings.Contains(got, "AuthHandler") {
		t.Fatalf("ref context missing from model input: %q", got)
	}
	if strings.Contains(got, "use English") {
		t.Fatalf("English referenced file content should not win over raw Chinese prompt:\n%s", got)
	}
}

func TestTurnOrchestratorCheckpointBoundaryPrecedesUserMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	c := New(Options{
		Runner:      runner,
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "write the test", "write the test", ""); err != nil {
		t.Fatal(err)
	}

	if !c.CheckpointHasBoundary(0) {
		t.Fatal("checkpoint boundary should be available for the orchestrated turn")
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "write the test" {
		t.Fatalf("session messages after turn = %+v, want system + user", sess.Messages)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("saved messages = %d, want system + user", len(loaded.Messages))
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.UpdatedAt.IsZero() {
		t.Fatal("activity meta should be marked after transcript changes")
	}
	if err := c.Rewind(0, RewindConversation); err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("session messages after rewind = %d, want boundary before user message", len(sess.Messages))
	}
}

func TestTurnOrchestratorSyntheticTurnDoesNotCreateCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	c := New(Options{
		Runner:      runner,
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})
	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "real prompt", "real prompt", ""); err != nil {
		t.Fatal(err)
	}
	if err := o.runSyntheticTurnWithRawDisplay(context.Background(), "hidden follow-up", "hidden follow-up", ""); err != nil {
		t.Fatal(err)
	}

	cps := c.Checkpoints()
	if len(cps) != 1 {
		t.Fatalf("checkpoints = %+v, want exactly the visible user turn", cps)
	}
	if cps[0].Turn != 0 || cps[0].Prompt != "real prompt" {
		t.Fatalf("checkpoint = %+v, want turn 0 real prompt", cps[0])
	}
	turns := c.CheckpointTurnsByMessageIndex()
	if len(turns) != 1 || turns[1] != 0 {
		t.Fatalf("checkpoint turns by message index = %v, want {1:0}", turns)
	}
}

func TestTurnOrchestratorStopFailureHookCancelledContext(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var stopCalls int
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "stop"},
		Event:      hook.StopFailure,
		Scope:      hook.ScopeProject,
	}}, "", func(ctx context.Context, in hook.SpawnInput) hook.SpawnResult {
		if ctx.Err() != nil {
			t.Errorf("Stop hook spawner ctx.Err()=%v; want nil", ctx.Err())
		}
		var p hook.Payload
		json.Unmarshal([]byte(in.Stdin), &p)
		if p.Event == hook.StopFailure {
			if p.Error == "" || !p.IsInterrupt {
				t.Errorf("failure payload = %+v", p)
			}
			stopCalls++
		}
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{Runner: ag, Executor: ag, Hooks: hooks})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(ctx, "test", "test", ""); err != nil && err != context.Canceled {
		t.Fatal(err)
	}
	if stopCalls != 1 {
		t.Fatalf("StopFailure hooks called = %d; want 1", stopCalls)
	}
}

// TestTurnOrchestratorCancelPreservesVisibleUserPrompt verifies that when the
// user explicitly cancels a visible turn (Ctrl+C), the real user prompt and
// fully paired tool work remain in the session while unsafe fragments become
// provider-excluded display history.
func TestTurnOrchestratorCancelPreservesVisibleUserPrompt(t *testing.T) {
	sess := agent.NewSession("you are a helpful agent")
	// Pre-populate with a few messages from an earlier turn.
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous work"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	preCount := len(sess.Messages)

	// runner that simulates a cancelled turn: it adds the user message plus
	// some tool-call garbage the real agent would leave behind, then returns
	// context.Canceled.
	runner := &cancelStrippingRunner{
		session: sess,
		add: []provider.Message{
			{Role: provider.RoleAssistant, Content: "let me do that", ToolCalls: []provider.ToolCall{
				{ID: "c1", Name: "todo_write", Arguments: `{"todos":[{"content":"add abc","status":"in_progress"}]}`},
			}},
			{Role: provider.RoleTool, Content: "Todos updated: 1 total — 0 completed, 1 in_progress, 0 pending.", ToolCallID: "c1", Name: "todo_write"},
		},
		err: context.Canceled,
	}

	ex := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: runner, Executor: ex})
	c.SetPlanMode(true)
	// Simulate a user-initiated cancel: set the cancelling flag.
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	// Pre-seed todoState as if a successful todo_write from the cancelled turn
	// had already updated it — this is the state the runner leaves behind before
	// returning context.Canceled, and what RebuildTodoState must clear.
	ex.ReplaceTodoState([]evidence.TodoItem{{Content: "add abc", Status: "in_progress"}})

	o := newTurnOrchestrator(c)
	err := o.runTurnWithRawDisplay(context.Background(), "add config file abc", "add config file abc", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// The visible user prompt and completed tool pair stay, followed by a durable
	// provider-excluded recovery record.
	msgs := sess.Messages
	if len(msgs) != preCount+4 {
		t.Fatalf("session messages after cancel = %d, want user + tool pair + recovery %d: %+v", len(msgs), preCount+4, msgs)
	}
	user := msgs[preCount]
	if user.Role != provider.RoleUser || user.Content != "add config file abc" {
		t.Fatalf("cancelled user message = %+v, want prefix-free prompt", user)
	}
	if msgs[preCount+1].Role != provider.RoleAssistant || msgs[preCount+2].Role != provider.RoleTool {
		t.Fatalf("completed tool pair was not retained: %+v", msgs[preCount+1:])
	}
	last := msgs[len(msgs)-1]
	if !last.LocalOnly || last.InterruptedTurn == nil || !last.InterruptedTurn.Pending || len(last.InterruptedTurn.CompletedTools) != 1 {
		t.Fatalf("pending recovery metadata missing: %+v", last)
	}

	// The completed todo_write result is canonical, so its state remains visible
	// and the next model turn can inspect rather than blindly repeat it.
	if todos := c.Todos(); len(todos) != 1 || todos[0].Status != "in_progress" {
		t.Fatalf("Todos() after cancel = %v, want retained completed todo_write state", todos)
	}
}

func TestTurnOrchestratorProviderErrorPreservesCompletedPairAndLocalPartial(t *testing.T) {
	sess := agent.NewSession("system")
	apiErr := errors.New("provider connection reset")
	runner := &cancelStrippingRunner{
		session: sess,
		add: []provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"ok"}`, Added: 1}}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "write_file", Content: "wrote a.txt"},
			{
				Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
				LocalOnly: true, Content: "partial final answer", ReasoningContent: "partial reasoning",
				InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, DroppedPartialText: true, DroppedPartialReasoning: true},
			},
		},
		err: apiErr,
	}
	ex := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: runner, Executor: ex})

	err := newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), "update a.txt", "update a.txt", "")
	if !errors.Is(err, apiErr) {
		t.Fatalf("run error = %v, want %v", err, apiErr)
	}
	msgs := sess.Snapshot()
	if len(msgs) != 5 || msgs[2].Role != provider.RoleAssistant || msgs[3].Role != provider.RoleTool || !msgs[4].LocalOnly {
		t.Fatalf("provider-error recovery transcript = %+v", msgs)
	}
	recovery := msgs[4].InterruptedTurn
	if recovery == nil || !recovery.Pending || len(recovery.CompletedTools) != 1 || len(recovery.CompletedTools[0].Files) != 1 || recovery.CompletedTools[0].Files[0] != "a.txt" {
		t.Fatalf("provider-error recovery metadata = %+v", recovery)
	}
	if msgs[4].Content != "partial final answer" || msgs[4].ReasoningContent != "partial reasoning" {
		t.Fatalf("provider-error display output was not retained: %+v", msgs[4])
	}
}

func TestTurnOrchestratorInterruptedAfterCompactionRelocatesVisibleTurn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		cancel bool
	}{
		{name: "cancel", err: context.Canceled, cancel: true},
		{name: "provider error", err: errors.New("provider connection reset")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := agent.NewSession("system")
			for i := 0; i < 3; i++ {
				sess.Add(provider.Message{Role: provider.RoleUser, Content: "old task"})
				sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "old answer"})
			}
			start := sess.Len()
			runner := &compactingErrorRunner{session: sess, err: tc.err}
			c := New(Options{Runner: runner, Executor: agent.New(nil, nil, sess, agent.Options{}, event.Discard)})
			if tc.cancel {
				c.mu.Lock()
				c.canceling = true
				c.mu.Unlock()
			}

			err := newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), "update a.txt", "update a.txt", "")
			if !errors.Is(err, tc.err) {
				t.Fatalf("run error = %v, want %v", err, tc.err)
			}
			msgs := sess.Snapshot()
			if start <= len(msgs) {
				t.Fatalf("test setup did not shrink transcript below stale boundary: start=%d len=%d", start, len(msgs))
			}
			userCount := 0
			for _, m := range msgs {
				if m.Role == provider.RoleUser && StripComposePrefixes(m.Content) == "update a.txt" {
					userCount++
				}
			}
			if userCount != 1 {
				t.Fatalf("current user occurrences = %d, want 1: %+v", userCount, msgs)
			}
			if len(msgs) != 6 || !agent.IsCompactionSummary(msgs[1]) || msgs[3].Role != provider.RoleAssistant || msgs[4].Role != provider.RoleTool || !msgs[5].LocalOnly {
				t.Fatalf("recovered compacted transcript = %+v", msgs)
			}
			recovery := msgs[5].InterruptedTurn
			if recovery == nil || !recovery.Pending || len(recovery.CompletedTools) != 1 || recovery.CompletedTools[0].Name != "write_file" {
				t.Fatalf("recovery metadata = %+v", recovery)
			}
		})
	}
}

func TestTurnOrchestratorCancelClassifiesCancelledToolResultAsInterrupted(t *testing.T) {
	sess := agent.NewSession("system")
	runner := &cancelStrippingRunner{
		session: sess,
		add: []provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "error: context canceled"},
		},
		err: context.Canceled,
	}
	c := New(Options{Runner: runner, Executor: agent.New(nil, nil, sess, agent.Options{}, event.Discard)})
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	err := newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), "run tests", "run tests", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want cancellation", err)
	}
	msgs := sess.Snapshot()
	recovery := msgs[len(msgs)-1].InterruptedTurn
	if recovery == nil || len(recovery.CompletedTools) != 0 || len(recovery.InterruptedTools) != 1 || recovery.InterruptedTools[0] != "bash" {
		t.Fatalf("cancelled tool result was misclassified: %+v", recovery)
	}
	if msgs[len(msgs)-3].Role != provider.RoleAssistant || msgs[len(msgs)-2].Role != provider.RoleTool {
		t.Fatalf("paired cancelled call/result should remain canonical: %+v", msgs)
	}
}

func TestTurnOrchestratorCancelBeforeRunnerAddsUserPreservesVisiblePrompt(t *testing.T) {
	workspace := t.TempDir()
	writeVisionTestConfig(t, workspace)
	imagePath := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(imagePath, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("system")
	ex := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{
		Runner:        cancelBeforeUserRunner{},
		Executor:      ex,
		WorkspaceRoot: workspace,
		ModelRef:      "custom/vision-pro",
	})
	c.SetPlanMode(true)
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	err := newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), "inspect @diagram.png", "inspect @diagram.png", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	msgs := sess.Snapshot()
	if len(msgs) != 3 || msgs[1].Role != provider.RoleUser || msgs[1].Content != "inspect @diagram.png" || !msgs[2].LocalOnly {
		t.Fatalf("session after pre-executor cancel = %+v, want user plus recovery marker", msgs)
	}
	if len(msgs[1].Images) != 1 || !strings.HasPrefix(msgs[1].Images[0], "data:image/png;base64,") {
		t.Fatalf("session after pre-executor cancel lost user image: %+v", msgs[1].Images)
	}
}

// TestTurnOrchestratorCancelFlushesCleanTranscriptToDisk verifies that after a
// user-cancel strip the cleaned transcript is written to disk, so a restart or
// session resume does not reload the partial turn from a stale mid-turn
// autosave.  See #5286.
func TestTurnOrchestratorCancelFlushesCleanTranscriptToDisk(t *testing.T) {
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "earlier turn"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	// Count only non-system messages; the system prompt is not written to the
	// .jsonl by Session.Save (it is reconstructed from the session options).
	wantNonSystem := 0
	for _, m := range sess.Messages {
		if m.Role != provider.RoleSystem {
			wantNonSystem++
		}
	}
	wantNonSystem += 4 // visible user + complete assistant/tool pair + recovery

	runner := &cancelStrippingRunner{
		session: sess,
		add: []provider.Message{
			{Role: provider.RoleAssistant, Content: "working…", ToolCalls: []provider.ToolCall{
				{ID: "d1", Name: "todo_write", Arguments: `{"todos":[{"content":"task","status":"in_progress"}]}`},
			}},
			{Role: provider.RoleTool, Content: "Todos updated.", ToolCallID: "d1", Name: "todo_write"},
		},
		err: context.Canceled,
	}

	sessionPath := agent.NewSessionPath(t.TempDir(), "test-model")
	c := New(Options{
		Runner:      runner,
		Executor:    agent.New(nil, nil, sess, agent.Options{}, event.Discard),
		SessionPath: sessionPath,
	})
	c.SetPlanMode(true)
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "do something", "do something", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Load the session file written after cleanup and verify the complete pair and
	// provider-excluded recovery marker survive restart.
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	nonSystem := 0
	var last provider.Message
	for _, m := range loaded.Messages {
		if m.Role != provider.RoleSystem {
			nonSystem++
			last = m
		}
	}
	if nonSystem != wantNonSystem {
		t.Fatalf("on-disk message count (non-system) = %d, want %d — stale partial turn still on disk", nonSystem, wantNonSystem)
	}
	if !last.LocalOnly || last.InterruptedTurn == nil || !last.InterruptedTurn.Pending {
		t.Fatalf("last on-disk message = %+v, want pending local recovery", last)
	}
}

func TestResumeRecoversStaleVisibleInFlightTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale-visible.jsonl")
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous work"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	start := len(sess.Messages)
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue work"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "working", ToolCalls: []provider.ToolCall{
		{ID: "todo-1", Name: "todo_write", Arguments: `{"todos":[{"content":"continue work","status":"in_progress"}]}`},
	}})
	sess.Add(provider.Message{Role: provider.RoleTool, Content: "Todos updated.", ToolCallID: "todo-1", Name: "todo_write"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, start, true); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path})
	c.Resume(loaded, path)

	msgs := exec.Session().Snapshot()
	if len(msgs) != start+4 {
		t.Fatalf("resumed messages = %d, want user + completed pair + recovery %d: %+v", len(msgs), start+4, msgs)
	}
	last := msgs[len(msgs)-1]
	if !last.LocalOnly || last.InterruptedTurn == nil || !last.InterruptedTurn.Pending {
		t.Fatalf("last resumed message = %+v, want provider-excluded recovery", last)
	}
	if todos := c.Todos(); len(todos) != 1 || todos[0].Status != "in_progress" {
		t.Fatalf("Todos() after stale in-flight recovery = %+v, want retained completed todo_write", todos)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != start+4 {
		t.Fatalf("persisted messages = %d, want recovered count %d: %+v", len(reloaded.Messages), start+4, reloaded.Messages)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatalf("stale in-flight marker survived resume: %+v", meta.InFlightTurn)
	}
}

func TestResumeClearsStaleSyntheticInFlightTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale-synthetic.jsonl")
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "ship it"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "Started.\n\n[goal:continue]"})
	start := len(sess.Messages)
	sess.Add(provider.Message{Role: provider.RoleUser, Content: goalContinueTurn})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "hidden continuation partial"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, start, false); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path})
	c.Resume(loaded, path)

	msgs := exec.Session().Snapshot()
	if len(msgs) != start {
		t.Fatalf("resumed messages = %d, want synthetic turn stripped to %d: %+v", len(msgs), start, msgs)
	}
	if last := msgs[len(msgs)-1]; last.Role != provider.RoleAssistant || !strings.Contains(last.Content, "[goal:continue]") {
		t.Fatalf("last resumed message = %+v, want completed visible turn preserved", last)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatalf("stale in-flight marker survived resume: %+v", meta.InFlightTurn)
	}
}

// cancelStrippingRunner adds messages to a session then returns a fixed error,
// simulating an agent that was interrupted mid-turn.
type cancelStrippingRunner struct {
	session *agent.Session
	add     []provider.Message
	err     error
}

type compactingErrorRunner struct {
	session *agent.Session
	err     error
}

type cancelBeforeUserRunner struct{}

func (cancelBeforeUserRunner) Run(context.Context, string) error {
	return context.Canceled
}

func (r *cancelStrippingRunner) Run(ctx context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	for _, m := range r.add {
		r.session.Add(m)
	}
	return r.err
}

func (r *compactingErrorRunner) Run(_ context.Context, input string) error {
	r.session.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "<compaction-summary>\nold work\n</compaction-summary>"},
		{Role: provider.RoleUser, Content: input, CreatedAt: time.Now().UnixMilli()},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "write-1", Name: "write_file", Arguments: `{"path":"a.txt","content":"ok"}`}}},
		{Role: provider.RoleTool, ToolCallID: "write-1", Name: "write_file", Content: "wrote a.txt"},
		{
			Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
			LocalOnly: true, Content: "partial final answer", ReasoningContent: "private partial reasoning",
			InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true},
		},
	})
	return r.err
}
