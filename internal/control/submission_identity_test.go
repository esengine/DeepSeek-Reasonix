package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
)

func TestShellSubmissionIsDurableBeforeCommandDispatchAndDeduplicated(t *testing.T) {
	var ctrl *Controller
	var dispatches atomic.Int32
	var admitted atomic.Bool
	done := make(chan struct{}, 1)
	verified := make(chan bool, 1)
	req := SubmissionRequest{ID: "shell-once", Action: "shell", Input: "echo fixture", Display: "echo fixture"}
	sink := event.FuncSink(func(ev event.Event) {
		if ev.Kind == event.TurnStarted {
			receipt, found := ctrl.sessionEventStore().Submission(req.ID)
			snapshot := ctrl.sessionEventStore().Snapshot()
			admitted.Store(found && MatchesSubmissionReceipt(req, receipt) && snapshot.DurableSequence >= snapshot.EventSequence)
		}
		if ev.Kind == event.ToolDispatch {
			dispatches.Add(1)
			verified <- admitted.Load()
		}
		if ev.Kind == event.TurnDone {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	ctrl = newOwnedTestController(t, Options{SessionPath: filepath.Join(t.TempDir(), "shell.jsonl"), Sink: sink})
	defer ctrl.Close()
	first, err := ctrl.SubmitIdentified(req)
	if err != nil {
		t.Fatal(err)
	}
	if !<-verified {
		t.Fatal("shell command dispatched before durable receipt")
	}
	<-done
	second, err := ctrl.SubmitIdentified(req)
	if err != nil || second != first || dispatches.Load() != 1 {
		t.Fatalf("duplicate shell: %v %+v dispatches=%d", err, second, dispatches.Load())
	}
}

func TestSubmissionIdentityDurableAndConflicting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	c := newOwnedTestController(t, Options{SessionPath: path, Sink: event.Discard})
	request := SubmissionRequest{ID: "request-1", Input: "hello", Display: "hello"}
	runs := 0
	receipt, err := c.submitIdentified(request, func() {
		runs++
		if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.TurnID == "" || receipt.MessageID == "" {
		t.Fatalf("incomplete receipt: %+v", receipt)
	}
	retry, err := c.submitIdentified(request, func() { runs++ })
	if err != nil || retry != receipt || runs != 1 {
		t.Fatalf("retry=%+v runs=%d err=%v", retry, runs, err)
	}
	request.Input = "different"
	if _, err := c.submitIdentified(request, func() { runs++ }); err == nil {
		t.Fatal("conflicting request accepted")
	}
	if err := c.emitTurnEventChecked(event.Event{Kind: event.TurnDone, Status: event.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	c.Close()
	reopened := newOwnedTestController(t, Options{SessionPath: path, Sink: event.Discard})
	defer reopened.Close()
	request.Input = "hello"
	recovered, found, err := reopened.LookupSubmission(request)
	if err != nil || !found || recovered != receipt {
		t.Fatalf("recovery: %+v %v %v", recovered, found, err)
	}
}

func TestSubmissionIdentityConcurrentPublicAdmission(t *testing.T) {
	c := newOwnedTestController(t, Options{SessionPath: filepath.Join(t.TempDir(), "session.jsonl"), Sink: event.Discard})
	defer c.Close()
	req := SubmissionRequest{ID: "concurrent", Input: "/mcp__definitely_missing", Display: "request"}
	const callers = 12
	start := make(chan struct{})
	receipts := make(chan session.SubmissionReceipt, callers)
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			<-start
			receipt, err := c.SubmitIdentified(req)
			receipts <- receipt
			errs <- err
		})
	}
	close(start)
	group.Wait()
	close(receipts)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first session.SubmissionReceipt
	for receipt := range receipts {
		if first.SubmissionID == "" {
			first = receipt
		}
		if receipt != first || receipt.TurnID == "" {
			t.Fatalf("different admission: %+v / %+v", first, receipt)
		}
	}
	if receipt, found, err := c.LookupSubmission(req); err != nil || !found || receipt != first {
		t.Fatalf("lookup: %+v %v %v", receipt, found, err)
	}
}

func TestSubmissionIdentityInterruptedAdmissionDoesNotReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	c := newOwnedTestController(t, Options{SessionPath: path, Sink: event.Discard})
	req := SubmissionRequest{ID: "accepted-before-body", Input: "a side effect"}
	first, err := c.submitIdentified(req, func() {
		// Persist admission without ever invoking the returned execution body.
		_ = c.prepareTurnAdmission(func(context.Context) error { t.Fatal("body ran"); return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	reopened := newOwnedTestController(t, Options{SessionPath: path, Sink: event.Discard})
	defer reopened.Close()
	got, err := reopened.submitIdentified(req, func() { t.Fatal("uncertain accepted input was replayed") })
	if err != nil || got != first {
		t.Fatalf("retry: %+v %v", got, err)
	}
	for _, changed := range []SubmissionRequest{
		{ID: req.ID, Input: req.Input, Original: "different edit"},
		{ID: req.ID, Input: req.Input, Invocations: []InvocationRequest{{Name: "different"}}},
		{ID: req.ID, Input: req.Input, ToolApprovalMode: "yolo"},
	} {
		if _, _, err := reopened.LookupSubmission(changed); err == nil {
			t.Fatal("execution options omitted from fingerprint")
		}
	}
}

func TestSubmissionAdmissionCancellationReleasesGateAndRetryReusesReceipt(t *testing.T) {
	flushStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	var once, releaseOnce sync.Once
	releasePhysicalWrite := func() { releaseOnce.Do(func() { close(releaseWrite) }) }
	store, err := session.CreateWithOptions(filepath.Join(t.TempDir(), "session"), "cancelled-admission", session.OpenOptions{
		Sync: func(*os.File) error {
			once.Do(func() { close(flushStarted) })
			<-releaseWrite
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.NewService("desktop", failingFlushPersistence{session: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		releasePhysicalWrite()
		_ = service.CloseAll(context.Background())
	})
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "cancelled-admission"})
	if err != nil {
		t.Fatal(err)
	}

	requests := make(chan provider.Request, 2)
	p := &reviewImageProvider{requests: requests}
	turnDone := make(chan struct{}, 1)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			select {
			case turnDone <- struct{}{}:
			default:
			}
		}
	})
	ag := agent.New(p, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, sink)
	c := newOwnedTestController(t, Options{
		Runner: ag, Executor: ag, Sink: sink,
		SessionService: service, SessionRuntime: runtime, ExclusiveSession: true,
	})
	defer c.Close()

	req := SubmissionRequest{ID: "cancel-and-retry", Input: "inspect once", Display: "inspect once"}
	ctx, cancel := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.SubmitIdentifiedContext(ctx, req)
		firstDone <- err
	}()
	select {
	case <-flushStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("submission did not reach the durable flush")
	}
	cancel()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrSubmissionNotAccepted) {
			t.Fatalf("cancelled admission = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled caller remained blocked on the physical write")
	}

	release := c.trySubmissionAdmissionLock()
	if release == nil {
		t.Fatal("cancelled admission retained the submission gate")
	}
	release()

	retryDone := make(chan struct {
		receipt session.SubmissionReceipt
		err     error
	}, 1)
	go func() {
		receipt, err := c.SubmitIdentifiedContext(t.Context(), req)
		retryDone <- struct {
			receipt session.SubmissionReceipt
			err     error
		}{receipt: receipt, err: err}
	}()
	releasePhysicalWrite()
	var receipt session.SubmissionReceipt
	select {
	case result := <-retryDone:
		if result.err != nil {
			t.Fatal(result.err)
		}
		receipt = result.receipt
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not observe the durable receipt")
	}
	if receipt.SubmissionID != req.ID || receipt.TurnID == "" {
		t.Fatalf("retry receipt = %+v", receipt)
	}

	select {
	case <-turnDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled admission did not reach a durable terminal state")
	}
	select {
	case request := <-requests:
		t.Fatalf("cancelled admission reached the provider: %+v", request)
	case <-time.After(100 * time.Millisecond):
	}
	snapshot := store.ExecutionSnapshot()
	if len(snapshot.Projection.Turns) != 1 {
		t.Fatalf("durable turns = %d, want 1", len(snapshot.Projection.Turns))
	}
}
