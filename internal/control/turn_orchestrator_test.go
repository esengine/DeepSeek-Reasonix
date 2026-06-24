package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
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

type recordingSessionRunner struct {
	session *agent.Session
	inputs  []string
}

func (r *recordingSessionRunner) Run(_ context.Context, input string) error {
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
