package control

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
	"reasonix/internal/tool"
)

type recoveryWriteTool struct {
	name     string
	readOnly bool
	mu       sync.Mutex
	runs     int
	failOnce bool
	failed   bool
}

func (t *recoveryWriteTool) Name() string            { return t.name }
func (t *recoveryWriteTool) Description() string     { return "test tool" }
func (t *recoveryWriteTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *recoveryWriteTool) ReadOnly() bool          { return t.readOnly }
func (t *recoveryWriteTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs++
	if t.failOnce && !t.failed {
		t.failed = true
		return "FAIL", errRecoveryTestFail
	}
	return "ok", nil
}

type recoveryTestFailError struct{}

func (recoveryTestFailError) Error() string { return "exit status 1" }

var errRecoveryTestFail = recoveryTestFailError{}

type controlRecoveryReviewerFunc func(context.Context, *recovery.FailureEvent, []string, recovery.Proposal, string) (recovery.ReviewVerdict, error)

func (f controlRecoveryReviewerFunc) Review(ctx context.Context, failure *recovery.FailureEvent, diagnosis []string, proposal recovery.Proposal, taskSummary string) (recovery.ReviewVerdict, error) {
	return f(ctx, failure, diagnosis, proposal, taskSummary)
}

func TestRecoveryExecutionRiskDoesNotPrompt(t *testing.T) {
	bash := &recoveryWriteTool{name: "bash", failOnce: true}
	reg := tool.NewRegistry()
	reg.Add(bash)

	prov := &recordingProvider{streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "2", Name: "bash", Arguments: `{"command":"git push origin feature"}`}}},
		{{Type: provider.ChunkText, Text: "done"}},
	}}

	sess := agent.NewSession("sys")
	ag := agent.New(prov, reg, sess, agent.Options{MaxSteps: 6}, event.Discard)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.Policy{Mode: permission.Allow},
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.EnableInteractiveApproval()

	if err := c.Run(context.Background(), "test then fix"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if bash.runs != 2 {
		t.Fatalf("bash runs = %d, want failed verification plus automatic push", bash.runs)
	}
	if got := c.RecoveryMetrics().HumanPrompts; got != 0 {
		t.Fatalf("execution risk prompts = %d, want 0", got)
	}
}

func TestRecoveryReviseBlocksPlanTransition(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var c *Controller
	c = New(Options{
		Runner: ag, Executor: ag, Policy: permission.Policy{Mode: permission.Allow},
		RecoveryReviewer: controlRecoveryReviewerFunc(func(context.Context, *recovery.FailureEvent, []string, recovery.Proposal, string) (recovery.ReviewVerdict, error) {
			return recovery.ReviewVerdict{Outcome: recovery.ReviewConfirm, ChangeKind: recovery.ChangeStrategy, Rationale: "choose API direction"}, nil
		}),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest && e.Approval.Kind == recovery.ApprovalKindRecovery {
				_ = c.ResolveRecovery(e.Approval.ID, agent.RecoveryActionRevise, "keep the current API")
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.EnableInteractiveApproval()
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	dec, err := gate.BeforeMutation(context.Background(), recovery.Proposal{
		Tool: "todo_write", ReadOnly: true, PlanTransition: true,
		PlanBefore: "1. Keep API [in_progress]", PlanAfter: "1. Replace API [in_progress]",
	})
	if err != nil || dec.Allow || !dec.Blocked || !strings.Contains(dec.Message, "keep the current API") {
		t.Fatalf("plan revise = %+v, %v", dec, err)
	}
}

func TestRecoveryInactiveUnderYolo(t *testing.T) {
	bash := &recoveryWriteTool{name: "bash", failOnce: true}
	write := &recoveryWriteTool{name: "write_file"}
	reg := tool.NewRegistry()
	reg.Add(bash)
	reg.Add(write)
	prov := &recordingProvider{streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "2", Name: "write_file", Arguments: `{"path":"a.go","content":"x"}`}}},
		{{Type: provider.ChunkText, Text: "done"}},
	}}
	sess := agent.NewSession("sys")
	ag := agent.New(prov, reg, sess, agent.Options{MaxSteps: 6}, event.Discard)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.Policy{Mode: permission.Allow},
	})
	c.SetToolApprovalMode(ToolApprovalYolo)
	c.EnableInteractiveApproval()

	if err := c.Run(context.Background(), "test then fix"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if write.runs != 1 {
		t.Fatalf("yolo should run write without recovery pause, runs=%d", write.runs)
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil {
		if st := gate.Snapshot().Tasks["root"]; st != nil && st.Failure != nil {
			t.Fatalf("yolo must not arm recovery failure: %+v", st)
		}
	}
}

func TestRecoveryHeadlessDoesNotBlockExecutionRisk(t *testing.T) {
	bash := &recoveryWriteTool{name: "bash", failOnce: true}
	reg := tool.NewRegistry()
	reg.Add(bash)
	prov := &recordingProvider{streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "1", Name: "bash", Arguments: `{"command":"go test ./..."}`}}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "2", Name: "bash", Arguments: `{"command":"git push origin feature"}`}}},
		{{Type: provider.ChunkText, Text: "reported blocker"}},
	}}
	sess := agent.NewSession("sys")
	ag := agent.New(prov, reg, sess, agent.Options{MaxSteps: 6}, event.Discard)
	c := New(Options{
		Runner:           ag,
		Executor:         ag,
		Policy:           permission.Policy{Mode: permission.Allow},
		RecoveryHeadless: true,
	})
	c.SetToolApprovalMode(ToolApprovalAuto)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Run(ctx, "test then fix"); err != nil {
		t.Fatalf("headless Run: %v", err)
	}
	if bash.runs != 2 {
		t.Fatalf("headless Auto should leave push to permission policy, bash runs=%d", bash.runs)
	}
	if got := requestMessagesText(prov.requests[len(prov.requests)-1].Messages); strings.Contains(got, "no decision channel") {
		t.Fatalf("execution risk unexpectedly produced headless plan blocker:\n%s", got)
	}
}

func TestLegacyApproveResolvesWaiterOnlyPlanTransition(t *testing.T) {
	// Old clients only call Approve. Normal-execution plan cards have a
	// live waiter but no taskRuntime, so Snapshot cannot discover them.
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var c *Controller
	var approvalID string
	c = New(Options{
		Runner: ag, Executor: ag,
		Policy: permission.Policy{Mode: permission.Allow},
		RecoveryReviewer: controlRecoveryReviewerFunc(func(context.Context, *recovery.FailureEvent, []string, recovery.Proposal, string) (recovery.ReviewVerdict, error) {
			return recovery.ReviewVerdict{Outcome: recovery.ReviewConfirm, ChangeKind: recovery.ChangeStrategy, Rationale: "choose API direction"}, nil
		}),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest && e.Approval.Kind == recovery.ApprovalKindRecovery {
				approvalID = e.Approval.ID
				// Simulate a legacy client that only knows Approve.
				c.Approve(e.Approval.ID, true, true, true) // session/persist must be ignored
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.EnableInteractiveApproval()

	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dec, err := gate.BeforeMutation(ctx, recovery.Proposal{
		Tool: "todo_write", ReadOnly: true, PlanTransition: true,
		PlanBefore: "1. Keep API [in_progress]", PlanAfter: "1. Replace API [in_progress]",
	})
	if err != nil || !dec.Allow {
		t.Fatalf("legacy Approve did not unblock plan card: %+v %v", dec, err)
	}
	if approvalID == "" {
		t.Fatal("expected a recovery approval id to be emitted")
	}
	if gate.HasApproval(approvalID) {
		t.Fatalf("HasApproval(%q) = true after legacy Approve, want false", approvalID)
	}
}

func TestRecoveryPromptCanResolveSynchronouslyFromSink(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var c *Controller
	var resolveErr error
	c = New(Options{
		Runner: ag, Executor: ag,
		Policy: permission.Policy{Mode: permission.Allow},
		RecoveryReviewer: controlRecoveryReviewerFunc(func(context.Context, *recovery.FailureEvent, []string, recovery.Proposal, string) (recovery.ReviewVerdict, error) {
			return recovery.ReviewVerdict{Outcome: recovery.ReviewConfirm, ChangeKind: recovery.ChangeScope, Rationale: "choose product scope"}, nil
		}),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest && e.Approval.Kind == recovery.ApprovalKindRecovery {
				resolveErr = c.ResolveRecovery(e.Approval.ID, agent.RecoveryActionContinue, "")
			}
		}),
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.EnableInteractiveApproval()

	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dec, err := gate.BeforeMutation(ctx, recovery.Proposal{
		Tool: "todo_write", ReadOnly: true, PlanTransition: true,
		PlanBefore: "1. Current scope [in_progress]", PlanAfter: "1. Expanded scope [in_progress]",
	})
	if resolveErr != nil {
		t.Fatalf("synchronous ResolveRecovery: %v", resolveErr)
	}
	if err != nil || !dec.Allow {
		t.Fatalf("BeforeMutation = (%+v, %v), want synchronous continue", dec, err)
	}
	if st := gate.Snapshot().Tasks["root"]; st != nil && st.ApprovalID != "" {
		t.Fatalf("resolved approval was re-created: %+v", st)
	}
}

func TestSetFreshSessionPathClearsRecoveryState(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	ag := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{
		Runner: ag, Executor: ag, SessionDir: dir, SessionPath: oldPath,
	})
	c.SetToolApprovalMode(ToolApprovalAuto)
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	gate.ObserveResult(context.Background(), recovery.Observation{
		Tool: "bash", Verification: true,
		Args: json.RawMessage(`{"command":"go test ./..."}`), ErrSummary: "fail",
	})
	if st := gate.Snapshot().Tasks["root"]; st == nil || st.Failure == nil {
		t.Fatal("test setup did not arm recovery")
	}
	c.SetFreshSessionPath(newPath)
	if got := gate.Snapshot().Tasks; len(got) != 0 {
		t.Fatalf("new session retained old recovery state: %+v", got)
	}
	// The async write scheduled above captured oldPath; it must not create a
	// failing checkpoint beside the newly selected session. Wait through the
	// gate instead of racing an atomic rename: Windows denies the read while
	// antivirus/indexing filters still hold the destination during replacement.
	gate.FlushPersistence(oldPath)
	oldSnap, err := recovery.LoadSnapshot(oldPath)
	if err != nil {
		t.Fatalf("LoadSnapshot(old): %v", err)
	}
	if len(oldSnap.Tasks) == 0 {
		t.Fatal("old-session recovery snapshot was not persisted")
	}
	newSnap, err := recovery.LoadSnapshot(newPath)
	if err != nil {
		t.Fatalf("LoadSnapshot(new): %v", err)
	}
	if len(newSnap.Tasks) != 0 {
		t.Fatalf("old recovery snapshot landed on new session: %+v", newSnap.Tasks)
	}
}

func TestFreshSessionRotationsClearRecoveryState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rotate func(*Controller) error
	}{
		{name: "new", rotate: func(c *Controller) error { return c.NewSession() }},
		{name: "clear", rotate: func(c *Controller) error { return c.ClearSession() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "old.jsonl")
			sess := agent.NewSession("sys")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
			if err := sess.Save(path); err != nil {
				t.Fatalf("Save session: %v", err)
			}
			ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
			c := New(Options{Runner: ag, Executor: ag, SessionDir: dir, SessionPath: path})
			defer c.Close()
			c.SetToolApprovalMode(ToolApprovalAuto)
			c.mu.Lock()
			gate := c.recoveryGate
			c.mu.Unlock()
			gate.ObserveResult(context.Background(), recovery.Observation{
				Tool: "bash", Verification: true,
				Args: json.RawMessage(`{"command":"go test ./..."}`), ErrSummary: "fail",
			})
			if err := tc.rotate(c); err != nil {
				t.Fatalf("rotate: %v", err)
			}
			if got := gate.Snapshot().Tasks; len(got) != 0 {
				t.Fatalf("fresh session retained recovery state: %+v", got)
			}
			if c.SessionPath() == path {
				t.Fatalf("session path did not rotate: %q", path)
			}
		})
	}
}

func TestNewSessionWaitsForPendingRecoveryPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.Save(path); err != nil {
		t.Fatalf("Save session: %v", err)
	}
	ag := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionDir: dir, SessionPath: path})
	defer c.Close()
	c.SetToolApprovalMode(ToolApprovalAuto)

	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	gate := recovery.NewGate(recovery.Options{
		Mode:           c.ToolApprovalMode,
		PersistenceKey: c.SessionPath,
		Persist: func(capturedPath string, snap recovery.Snapshot) {
			startOnce.Do(func() { close(started) })
			<-release
			c.persistRecoverySnapshot(capturedPath, snap)
		},
	})
	c.mu.Lock()
	c.recoveryGate = gate
	c.mu.Unlock()
	ag.SetRecoveryGate(gate)

	gate.ObserveResult(context.Background(), recovery.Observation{
		Tool: "bash", Verification: true,
		Args: json.RawMessage(`{"command":"go test ./..."}`), ErrSummary: "fail",
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("recovery persistence did not start")
	}

	done := make(chan error, 1)
	go func() { done <- c.NewSession() }()
	select {
	case err := <-done:
		t.Fatalf("NewSession returned before old recovery persistence drained: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NewSession did not resume after recovery persistence drained")
	}
}
