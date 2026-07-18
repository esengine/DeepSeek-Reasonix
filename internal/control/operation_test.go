package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

type blockingSummaryProvider struct {
	started chan struct{}
}

func (p *blockingSummaryProvider) Name() string { return "blocking-summary" }

func (p *blockingSummaryProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	return make(chan provider.Chunk), nil
}

type blockingCompactHooks struct {
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (*blockingCompactHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	return false, ""
}
func (*blockingCompactHooks) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (*blockingCompactHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (*blockingCompactHooks) PostLLMCall(_ context.Context, reasoning string, _ int) string {
	return reasoning
}
func (*blockingCompactHooks) HasPostLLMCall() bool                 { return false }
func (*blockingCompactHooks) SubagentStop(context.Context, string) {}
func (h *blockingCompactHooks) PreCompact(context.Context, string) string {
	h.once.Do(func() { close(h.started) })
	<-h.release
	return ""
}

func compactableSession() *agent.Session {
	return &agent.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "step one"},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: "step two"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
}

func newBlockingCompactController(release <-chan struct{}, sink event.Sink) (*Controller, *blockingCompactHooks) {
	hooks := &blockingCompactHooks{started: make(chan struct{}), release: release}
	sess := compactableSession()
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{RecentKeep: 2, Hooks: hooks}, sink)
	return New(Options{Runner: exec, Executor: exec, Sink: sink}), hooks
}

func requireOperationBusyState(t *testing.T, err error, want ForegroundBusyState) {
	t.Helper()
	if !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("error = %v, want errors.Is ErrSessionBusy", err)
	}
	var busy *OperationAdmissionError
	if !errors.As(err, &busy) || busy.State != want {
		t.Fatalf("error = %v, want busy state %q", err, want)
	}
}

func waitOperationResult(t *testing.T, h *OperationHandle, timeout time.Duration) OperationResult {
	t.Helper()
	select {
	case result := <-h.Done():
		return result
	case <-time.After(timeout):
		t.Fatal("timed out waiting for Operation completion")
		return OperationResult{}
	}
}

func TestOperationAdmissionIsStrictAndClassified(t *testing.T) {
	t.Run("turn", func(t *testing.T) {
		c := New(Options{})
		started := make(chan struct{})
		release := make(chan struct{})
		c.runGuarded(func(context.Context) error {
			close(started)
			<-release
			return nil
		})
		<-started
		h, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo rejected"})
		if h != nil {
			t.Fatal("busy admission returned a handle")
		}
		requireOperationBusyState(t, err, ForegroundTurn)
		close(release)
		c.turnWG.Wait()
		c.autosaveWG.Wait()
	})

	t.Run("finishing", func(t *testing.T) {
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		c := New(Options{Sink: holdFinishingWindow(release, entered, nil)})
		c.runGuarded(func(context.Context) error { return nil })
		<-entered
		_, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo rejected"})
		requireOperationBusyState(t, err, ForegroundFinishing)
		close(release)
		c.turnWG.Wait()
		c.autosaveWG.Wait()
	})

	t.Run("rotation", func(t *testing.T) {
		c := New(Options{})
		if err := c.beginRotation(); err != nil {
			t.Fatal(err)
		}
		_, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo rejected"})
		requireOperationBusyState(t, err, ForegroundRotation)
		c.endRotation()
	})

	t.Run("operation", func(t *testing.T) {
		release := make(chan struct{})
		c, hooks := newBlockingCompactController(release, event.Discard)
		h, err := c.StartOperation(OperationSpec{Kind: OperationCompact})
		if err != nil {
			t.Fatal(err)
		}
		<-hooks.started
		_, err = c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo rejected"})
		requireOperationBusyState(t, err, ForegroundOperation)
		if got := h.Cancel(); got != OperationCancelRequestedNow {
			t.Fatalf("Cancel = %q", got)
		}
		close(release)
		if result := waitOperationResult(t, h, 2*time.Second); !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("result = %v, want cancellation", result.Err)
		}
	})

	t.Run("closed", func(t *testing.T) {
		c := New(Options{})
		c.Close()
		h, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo rejected"})
		if h != nil || !errors.Is(err, ErrControllerClosed) {
			t.Fatalf("StartOperation after Close = (%v, %v)", h, err)
		}
	})
}

func TestOperationAndTurnGatesAreMutuallyExclusive(t *testing.T) {
	release := make(chan struct{})
	c, hooks := newBlockingCompactController(release, event.Discard)
	h, err := c.StartOperation(OperationSpec{Kind: OperationCompact})
	if err != nil {
		t.Fatal(err)
	}
	<-hooks.started

	ran := make(chan struct{}, 1)
	if got := c.runGuarded(func(context.Context) error {
		ran <- struct{}{}
		return nil
	}); got != turnDroppedRunning {
		t.Fatalf("async Turn admission = %v, want running drop", got)
	}
	if err := c.RunTurn(context.Background(), "must not run"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("RunTurn error = %v, want ErrTurnRunning", err)
	}
	if err := c.beginRotation(); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("beginRotation error = %v, want rotation busy", err)
	}
	if got := c.TryCancel(); got != CancelNotActive {
		t.Fatalf("TryCancel during Operation = %q, want Turn not active", got)
	}
	select {
	case <-ran:
		t.Fatal("Turn ran while Operation owned the foreground")
	default:
	}
	if got := h.Cancel(); got != OperationCancelRequestedNow {
		t.Fatalf("Operation Cancel = %q", got)
	}
	if got := h.Cancel(); got != OperationCancelAlreadyRequested {
		t.Fatalf("second Operation Cancel = %q", got)
	}
	close(release)
	waitOperationResult(t, h, 2*time.Second)
	if got := h.Cancel(); got != OperationCancelNotActive {
		t.Fatalf("completed Operation Cancel = %q", got)
	}
}

func TestOperationShellHasOwnCompletionAndKillsChild(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "started")
	sh := sandbox.ResolveShell("", "", nil)
	command := "echo ready > started; sleep 30"
	if sh.Kind == sandbox.ShellPowerShell {
		command = "Set-Content -Path started -Value ready; Start-Sleep -Seconds 30"
	}
	events := make(chan event.Event, 32)
	c := New(Options{WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) { events <- e })})
	h, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: command})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		h.Cancel()
		waitOperationResult(t, h, shellWaitDelay+10*time.Second)
		t.Fatalf("shell child did not create readiness marker: %v", err)
	}
	if got := h.Cancel(); got != OperationCancelRequestedNow {
		t.Fatalf("Cancel = %q", got)
	}
	result := waitOperationResult(t, h, shellWaitDelay+10*time.Second)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("shell result = %v, want context.Canceled", result.Err)
	}

	var dispatch, toolResult, turnDone, operationDone int
	var operationErr error
	for {
		select {
		case e := <-events:
			switch e.Kind {
			case event.ToolDispatch:
				dispatch++
			case event.ToolResult:
				toolResult++
			case event.TurnDone:
				turnDone++
			case event.OperationDone:
				operationDone++
				operationErr = e.Err
			}
		default:
			if dispatch != 1 || toolResult != 1 || turnDone != 0 || operationDone != 1 || !errors.Is(operationErr, context.Canceled) {
				t.Fatalf("events dispatch=%d result=%d TurnDone=%d OperationDone=%d err=%v, want 1/1/0/1/cancelled", dispatch, toolResult, turnDone, operationDone, operationErr)
			}
			return
		}
	}
}

func TestOperationShutdownCancelsAndWaitsForWorker(t *testing.T) {
	tests := []struct {
		name     string
		shutdown func(*Controller)
	}{
		{name: "runtime", shutdown: func(c *Controller) { c.PrepareRuntimeShutdown() }},
		{name: "close", shutdown: func(c *Controller) { c.Close() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			c, hooks := newBlockingCompactController(release, event.Discard)
			h, err := c.StartOperation(OperationSpec{Kind: OperationCompact})
			if err != nil {
				t.Fatal(err)
			}
			<-hooks.started
			shutdownDone := make(chan struct{})
			go func() {
				tc.shutdown(c)
				close(shutdownDone)
			}()
			select {
			case <-shutdownDone:
				t.Fatal("shutdown returned before operation worker exited")
			case <-time.After(75 * time.Millisecond):
			}
			close(release)
			select {
			case <-shutdownDone:
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown did not wait through cancellation")
			}
			if result := waitOperationResult(t, h, time.Second); !errors.Is(result.Err, context.Canceled) {
				t.Fatalf("shutdown result = %v, want context.Canceled", result.Err)
			}
			if _, err := c.StartOperation(OperationSpec{Kind: OperationShell, Command: "echo no"}); !errors.Is(err, ErrControllerClosed) {
				t.Fatalf("shutdown did not seal admission: %v", err)
			}
		})
	}
}

func TestSummarizeOperationCancellationPreservesBoundaryAndSession(t *testing.T) {
	prov := &blockingSummaryProvider{started: make(chan struct{}, 1)}
	sess := &agent.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "one"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "two"},
		{Role: provider.RoleAssistant, Content: "answer two"},
	}}
	exec := agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec, Sink: event.Discard})
	c.checkpoints.begin("one", 1)
	beforeMessages := sess.Snapshot()
	beforeCheckpoints := c.CheckpointSnapshot()

	h, err := c.StartOperation(OperationSpec{Kind: OperationSummarize, Turn: 0, Direction: SummarizeFrom})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-prov.started:
	case <-time.After(2 * time.Second):
		t.Fatal("summarize provider did not start")
	}
	if got := h.Cancel(); got != OperationCancelRequestedNow {
		t.Fatalf("Cancel = %q", got)
	}
	if result := waitOperationResult(t, h, 2*time.Second); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("result = %v, want context.Canceled", result.Err)
	}
	if got := sess.Snapshot(); !reflect.DeepEqual(got, beforeMessages) {
		t.Fatalf("cancelled summarize rewrote messages:\nbefore=%+v\nafter=%+v", beforeMessages, got)
	}
	if got := c.CheckpointSnapshot(); !reflect.DeepEqual(got, beforeCheckpoints) {
		t.Fatalf("cancelled summarize changed checkpoints:\nbefore=%+v\nafter=%+v", beforeCheckpoints, got)
	}
}

func TestOperationCompletionCancelRaceDoesNotLeakAdmission(t *testing.T) {
	sess := agent.NewSession("sys")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: exec, Executor: exec})
	for i := 0; i < 100; i++ {
		h, err := c.StartOperation(OperationSpec{Kind: OperationCompact})
		if err != nil {
			t.Fatalf("iteration %d StartOperation: %v", i, err)
		}
		cancelled := make(chan OperationCancelAttempt, 1)
		go func() { cancelled <- h.Cancel() }()
		result := waitOperationResult(t, h, 2*time.Second)
		attempt := <-cancelled
		if attempt != OperationCancelRequestedNow && attempt != OperationCancelNotActive {
			t.Fatalf("iteration %d Cancel = %q", i, attempt)
		}
		if result.Err != nil && !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("iteration %d result = %v", i, result.Err)
		}
		if got := h.Cancel(); got != OperationCancelNotActive {
			t.Fatalf("iteration %d completed Cancel = %q", i, got)
		}
	}
}

func TestInvalidOperationNeverReturnsHandle(t *testing.T) {
	c := New(Options{})
	for _, spec := range []OperationSpec{
		{},
		{Kind: OperationShell},
		{Kind: OperationShell, Command: "echo x", Instructions: "extra"},
		{Kind: OperationCompact, Command: "echo x"},
		{Kind: OperationSummarize, Turn: -1, Direction: SummarizeFrom},
		{Kind: OperationSummarize, Direction: "sideways"},
	} {
		h, err := c.StartOperation(spec)
		if h != nil || !errors.Is(err, ErrInvalidOperation) {
			t.Fatalf("StartOperation(%+v) = (%v, %v), want invalid", spec, h, err)
		}
	}
}
