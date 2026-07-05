package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestTurnOrchestratorSkipsMemoryCompilerForSyntheticTurns(t *testing.T) {
	// A genuine user turn supplies a Memory v5 source and is not skipped; a
	// synthetic controller-injected turn (the goal-loop continuation) is marked
	// to bypass compilation so its contract can't be re-injected and loop
	// (#5342, #5329).
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})
	o := newTurnOrchestrator(c)

	real := "fix the login bug"
	if err := o.runTurnWithRawDisplay(context.Background(), real, real, ""); err != nil {
		t.Fatal(err)
	}
	if err := o.runTurnWithRawDisplay(context.Background(), goalContinueTurn, goalContinueTurn, ""); err != nil {
		t.Fatal(err)
	}

	if len(runner.memoryCompilerSkips) != 2 {
		t.Fatalf("runs = %d, want 2", len(runner.memoryCompilerSkips))
	}
	if runner.memoryCompilerSkips[0] {
		t.Fatalf("genuine user turn was marked skip-compile")
	}
	if !runner.memoryCompilerSkips[1] {
		t.Fatalf("synthetic goal-continuation turn was NOT marked skip-compile")
	}
	// The genuine turn supplies a source; the synthetic one must not.
	if len(runner.memoryCompilerInputs) != 1 || runner.memoryCompilerInputs[0] != real {
		t.Fatalf("memory compiler sources = %v, want exactly [%q]", runner.memoryCompilerInputs, real)
	}
}

func TestTurnOrchestratorTypedSyntheticTurnDoesNotDependOnPrefix(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{AutoPlan: "on", Runner: runner})
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
		t.Fatalf("typed synthetic turn should not be auto-planned, got %q", runner.inputs[0])
	}
	if len(runner.memoryCompilerSkips) != 1 || !runner.memoryCompilerSkips[0] {
		t.Fatalf("typed synthetic turn was not marked skip-compile: %+v", runner.memoryCompilerSkips)
	}
	if len(runner.memoryCompilerInputs) != 0 {
		t.Fatalf("typed synthetic turn supplied Memory v5 source input: %+v", runner.memoryCompilerInputs)
	}
}

func TestTurnOrchestratorLegacySyntheticPrefixStillSkipsMemoryCompiler(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})
	o := newTurnOrchestrator(c)

	if err := o.runTurnWithRawDisplay(context.Background(), goalContinueTurn, goalContinueTurn, ""); err != nil {
		t.Fatal(err)
	}
	if len(runner.memoryCompilerSkips) != 1 || !runner.memoryCompilerSkips[0] {
		t.Fatalf("legacy synthetic prefix was not marked skip-compile: %+v", runner.memoryCompilerSkips)
	}
	if len(runner.memoryCompilerInputs) != 0 {
		t.Fatalf("legacy synthetic prefix supplied Memory v5 source input: %+v", runner.memoryCompilerInputs)
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
	session              *agent.Session
	inputs               []string
	memoryCompilerInputs []string
}

func (r *recordingSessionRunner) Run(ctx context.Context, input string) error {
	r.inputs = append(r.inputs, input)
	if source, ok := agent.MemoryCompilerSourceInputFromContext(ctx); ok {
		r.memoryCompilerInputs = append(r.memoryCompilerInputs, source)
	}
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
	if len(runner.memoryCompilerInputs) != 1 || runner.memoryCompilerInputs[0] != visible {
		t.Fatalf("memory compiler source input = %+v, want %q", runner.memoryCompilerInputs, visible)
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
		AutoPlan:      "off",
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

func TestTurnOrchestratorStopHookCancelledContext(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done")}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	var stopCalls int
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "stop"},
		Event:      hook.Stop,
		Scope:      hook.ScopeProject,
	}}, "", func(ctx context.Context, in hook.SpawnInput) hook.SpawnResult {
		if ctx.Err() != nil {
			t.Errorf("Stop hook spawner ctx.Err()=%v; want nil", ctx.Err())
		}
		var p hook.Payload
		json.Unmarshal([]byte(in.Stdin), &p)
		if p.Event == hook.Stop {
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
		t.Fatalf("Stop hooks called = %d; want 1", stopCalls)
	}
}

// TestTurnOrchestratorCancelPreservesVisibleUserPrompt verifies that when the
// user explicitly cancels a visible turn (Ctrl+C), the real user prompt remains
// in the session while incomplete assistant/tool remnants are stripped. Without
// this, the next user message can lose the just-submitted context (#5499); if
// the remnants remain, the model can re-execute interrupted work (#5286).
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

	// The visible user prompt must stay, while assistant/tool remnants from the
	// cancelled turn must be stripped.
	msgs := sess.Messages
	if len(msgs) != preCount+1 {
		t.Fatalf("session messages after cancel = %d, want pre-turn + user prompt %d: %+v", len(msgs), preCount+1, msgs)
	}
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleUser || last.Content != "add config file abc" {
		t.Fatalf("last message after cancel = %+v, want preserved visible user prompt", last)
	}
	for _, m := range msgs[preCount+1:] {
		if m.Role == provider.RoleAssistant || m.Role == provider.RoleTool {
			t.Fatalf("cancelled turn remnant survived: %+v", m)
		}
	}

	// todoState must also be reset: the in_progress todo written by the
	// cancelled turn must not survive the strip.
	if todos := c.Todos(); len(todos) != 0 {
		t.Fatalf("Todos() after cancel = %v, want empty — cancelled todo_write leaked into canonical state", todos)
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
	wantNonSystem++ // the cancelled visible turn's user prompt is preserved

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
	c.mu.Lock()
	c.canceling = true
	c.mu.Unlock()

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "do something", "do something", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Load the session file written after the strip and verify it contains the
	// pre-cancel messages plus the visible user prompt — not the partial
	// assistant/tool messages.
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
	if last.Role != provider.RoleUser || last.Content != "do something" {
		t.Fatalf("last on-disk message = %+v, want preserved visible user prompt", last)
	}
}

func TestResumeClearsStaleVisibleInFlightTurn(t *testing.T) {
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
	if len(msgs) != start+1 {
		t.Fatalf("resumed messages = %d, want pre-turn + user prompt %d: %+v", len(msgs), start+1, msgs)
	}
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleUser || last.Content != "continue work" {
		t.Fatalf("last resumed message = %+v, want preserved visible user prompt", last)
	}
	if todos := c.Todos(); len(todos) != 0 {
		t.Fatalf("Todos() after stale in-flight recovery = %+v, want empty", todos)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Messages) != start+1 {
		t.Fatalf("persisted messages = %d, want cleaned count %d: %+v", len(reloaded.Messages), start+1, reloaded.Messages)
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

func (r *cancelStrippingRunner) Run(ctx context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	for _, m := range r.add {
		r.session.Add(m)
	}
	return r.err
}
