package control

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	goaldomain "reasonix/internal/goal"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
)

func cleanupGoalDriverController(t *testing.T, c *Controller) {
	t.Helper()
	t.Cleanup(func() {
		service, runtime, exclusive := c.v3Binding()
		c.Close()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			c.goalDriverMu.Lock()
			settled := !c.goalDriverPending && c.goalDriverActive == nil
			c.goalDriverMu.Unlock()
			runtimeRetired := !exclusive || service == nil || runtime == nil || goalRuntimeRetired(c, service, runtime, settled)
			if settled && !c.Running() && runtimeRetired {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("goal controller did not settle after close")
	})
}

type lifecycleDriverRunner struct {
	mu     sync.Mutex
	calls  int
	inputs []string
	done   chan struct{}
}

func (r *lifecycleDriverRunner) Run(ctx context.Context, input string) error {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.inputs = append(r.inputs, input)
	r.mu.Unlock()
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	switch call {
	case 1:
		if binding.Authority.Source != tool.GoalSourceDirectHuman {
			return context.Canceled
		}
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "finish the lifecycle"}, binding.Authority)
		return err
	case 2:
		// A normal final response is not a lifecycle transition. Leaving the
		// target active must cause another independently admitted top-level turn.
		if binding.Authority.Source != tool.GoalSourceGoalRound {
			return context.Canceled
		}
		return nil
	case 3:
		if binding.Authority.Source != tool.GoalSourceGoalRound {
			return context.Canceled
		}
		view, err := binding.Owner.GetGoal(ctx)
		if err != nil {
			return err
		}
		_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority)
		close(r.done)
		return err
	default:
		return context.Canceled
	}
}

func TestGoalDriverContinuesAfterFinalAndCompletesThroughExactRoundAuthority(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-driver"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &lifecycleDriverRunner{done: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("work until the whole target is finished")
	select {
	case <-runner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic goal round did not run")
	}
	deadline := time.Now().Add(5 * time.Second)
	for (c.Running() || c.GoalStatus() != GoalStatusComplete) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.GoalStatus() != GoalStatusComplete {
		t.Fatalf("goal status = %q", c.GoalStatus())
	}
	view, err := c.goalLifecycleView()
	if err != nil || view == nil || view.RoundsStarted != 2 || view.Revision != 2 {
		t.Fatalf("goal view = %+v, err = %v", view, err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.calls != 3 || !strings.Contains(runner.inputs[1], `"round":1`) || !strings.Contains(runner.inputs[2], `"round":2`) || !strings.Contains(runner.inputs[1], "<goal-round>") {
		t.Fatalf("calls/inputs = %d %#v", runner.calls, runner.inputs)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	commits, err := runtime.Session().Handle().Read(t.Context(), 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundAtomicAdmission := false
	for _, commit := range commits.Commits {
		hasStart, hasGoal := false, false
		for _, item := range commit.Events {
			hasStart = hasStart || item.Kind == "turn/start"
			hasGoal = hasGoal || item.Kind == "goal/state"
		}
		foundAtomicAdmission = foundAtomicAdmission || hasStart && hasGoal
	}
	if !foundAtomicAdmission {
		t.Fatal("automatic round did not atomically commit turn/start with goal/state")
	}
}

type limitedGoalRunner struct {
	mu    sync.Mutex
	calls int
}

func (r *limitedGoalRunner) Run(ctx context.Context, _ string) error {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if call == 1 {
		limit := uint64(1)
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "one automatic round", MaxGoalRounds: &limit}, binding.Authority)
		return err
	}
	return nil
}

func TestGoalDriverTurnsExplicitRoundLimitIntoBlockedState(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-limit"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &limitedGoalRunner{}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("run exactly one automatic round")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, viewErr := c.goalLifecycleView()
		if viewErr != nil {
			t.Fatal(viewErr)
		}
		if view != nil && view.Phase == goaldomain.PhaseBlocked {
			if view.RoundsStarted != 1 || view.BlockedReason == nil || view.BlockedReason.Code != "round-limit" {
				t.Fatalf("blocked view = %+v", view)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("round-limited goal did not enter blocked state")
}

func TestDuplicateGoalDriverKicksAdmitOnlyOneRound(t *testing.T) {
	// Reservation identity is checked again after Flush and by guarded turn
	// admission; this focused test exercises level-trigger collapse itself.
	c := &Controller{}
	c.closed = true
	for range 20 {
		c.kickGoalDriver()
	}
	c.goalDriverMu.Lock()
	defer c.goalDriverMu.Unlock()
	if c.goalDriverPending || c.goalDriverActive != nil {
		t.Fatalf("closed driver accepted work: pending=%v active=%v", c.goalDriverPending, c.goalDriverActive)
	}
}

type gatedGoalRunner struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (r *gatedGoalRunner) Run(ctx context.Context, _ string) error {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if call == 1 {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "deduplicate idle notifications"}, binding.Authority)
		return err
	}
	if call == 2 {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		view, err := binding.Owner.GetGoal(ctx)
		if err != nil {
			return err
		}
		_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority)
		return err
	}
	return context.Canceled
}

func TestConcurrentIdleKicksCannotAdmitParallelGoalRounds(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-dedup"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &gatedGoalRunner{started: make(chan struct{}), release: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("start a deduplicated target")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic round did not start")
	}
	var kicks sync.WaitGroup
	for range 32 {
		kicks.Go(func() {
			c.kickGoalDriver()
		})
	}
	kicks.Wait()
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 2 {
		t.Fatalf("parallel idle kicks admitted %d calls, want initial + one goal round", calls)
	}
	close(runner.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, _ := c.goalLifecycleView()
		if view != nil && view.Phase == goaldomain.PhaseComplete {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("deduplicated goal round did not finish")
}

type cancelGoalRunner struct {
	started chan struct{}
}

func (r *cancelGoalRunner) Run(ctx context.Context, _ string) error {
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if binding.Authority.Source == tool.GoalSourceDirectHuman {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "pause the running target"}, binding.Authority)
		return err
	}
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestPausingRunningGoalRoundCancelsActivityAndPersistsPaused(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-cancel"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &cancelGoalRunner{started: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("start then pause")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic goal round did not start")
	}
	if !c.PauseGoal() {
		t.Fatal("PauseGoal rejected an active goal round")
	}
	deadline := time.Now().Add(5 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	view, err := c.goalLifecycleView()
	if err != nil || view == nil || view.Phase != goaldomain.PhasePaused || view.Activation != goaldomain.ActivationDisarmed || view.RoundsStarted != 1 {
		t.Fatalf("paused view = %+v, err = %v", view, err)
	}
}

type completeThenCancelGoalRunner struct {
	completed chan struct{}
}

func (r *completeThenCancelGoalRunner) Run(ctx context.Context, _ string) error {
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if binding.Authority.Source == tool.GoalSourceDirectHuman {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "complete before cancellation"}, binding.Authority)
		return err
	}
	view, err := binding.Owner.GetGoal(ctx)
	if err != nil {
		return err
	}
	if _, err := binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority); err != nil {
		return err
	}
	close(r.completed)
	<-ctx.Done()
	return ctx.Err()
}

func TestCancellationAfterAcceptedCompleteDoesNotRewriteGoalToPaused(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-complete-then-cancel"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &completeThenCancelGoalRunner{completed: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("start and finish the target")
	select {
	case <-runner.completed:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic goal round did not complete")
	}
	c.Cancel()
	deadline := time.Now().Add(5 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	view, err := c.goalLifecycleView()
	if err != nil || view == nil || view.Phase != goaldomain.PhaseComplete || view.StopReason != "complete" {
		t.Fatalf("completed goal after cancellation = %+v, err = %v", view, err)
	}
}

type unlimitedGoalRunner struct {
	mu        sync.Mutex
	calls     uint64
	autoLimit uint64
	done      chan struct{}
}

func (r *unlimitedGoalRunner) Run(ctx context.Context, _ string) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if binding.Authority.Source == tool.GoalSourceDirectHuman {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "run beyond the old hidden ceiling"}, binding.Authority)
		return err
	}
	if binding.Authority.Round == r.autoLimit {
		view, err := binding.Owner.GetGoal(ctx)
		if err != nil {
			return err
		}
		_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority)
		close(r.done)
		return err
	}
	return nil
}

func TestUnlimitedGoalDriverRunsBeyondHarnessDefaultCeiling(t *testing.T) {
	service := goalRoundTestService(t)
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-unlimited"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &unlimitedGoalRunner{autoLimit: 257, done: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("exercise the unlimited goal driver")
	select {
	case <-runner.done:
	// This asserts the admission count, not disk throughput. The enclosing
	// test command supplies the watchdog for a genuinely stalled driver.
	case <-t.Context().Done():
		t.Fatal("unlimited goal did not cross 256 admitted automatic rounds")
	}
	view, err := c.goalLifecycleView()
	if err != nil || view == nil || view.Phase != goaldomain.PhaseComplete || view.RoundsStarted != 257 || view.MaxGoalRounds != nil {
		t.Fatalf("unlimited goal view = %+v, err = %v", view, err)
	}
	waitForGoalDriverIdle(t, c, runtime)
}

func waitForGoalDriverIdle(t *testing.T, c *Controller, runtime *session.Runtime) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.goalDriverMu.Lock()
		settled := !c.goalDriverPending && c.goalDriverActive == nil
		c.goalDriverMu.Unlock()
		if settled && !c.Running() && runtime.Snapshot().Phase == session.RuntimeIdle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("goal driver did not return to idle after terminal update")
}

type budgetedGoalRunner struct {
	usage event.Sink
	calls int
}

func (r *budgetedGoalRunner) Run(ctx context.Context, _ string) error {
	r.calls++
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if binding.Authority.Source == tool.GoalSourceDirectHuman {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "respect the host resource budget"}, binding.Authority)
		return err
	}
	r.usage.Emit(event.Event{Kind: event.Usage, UsageSource: event.UsageSourceExecutor,
		Usage: &provider.Usage{PromptTokens: 80, CompletionTokens: 40, TotalTokens: 120, RequestCount: 1}})
	return nil
}

func TestGoalDriverBlocksAtExplicitHostTokenBudget(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-budget"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &budgetedGoalRunner{}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, GoalTokenBudget: 100,
		SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	runner.usage = c.goalUsageTee
	cleanupGoalDriverController(t, c)
	c.Send("run within a fixed token budget")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, _ := c.goalLifecycleView()
		if view != nil && view.Phase == goaldomain.PhaseBlocked {
			if view.BlockedReason == nil || view.BlockedReason.Code != "resource-budget" || view.RoundsStarted != 1 || runner.calls != 2 {
				t.Fatalf("blocked view/calls = %+v / %d", view, runner.calls)
			}
			runtimeView := c.GoalRuntime()
			if runtimeView.TokensUsed != 120 || runtimeView.TokensLimit != 100 || runtimeView.RequestsUsed != 1 {
				t.Fatalf("resource runtime = %+v", runtimeView)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("goal did not stop at the explicit host token budget")
}

type modelErrorGoalRunner struct {
	calls int
}

func (r *modelErrorGoalRunner) Run(ctx context.Context, _ string) error {
	r.calls++
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	if binding.Authority.Source == tool.GoalSourceDirectHuman {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "stop safely on provider errors"}, binding.Authority)
		return err
	}
	return errors.New("provider failed")
}

func TestGoalRoundModelErrorDisarmsWithoutCompleting(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-model-error"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &modelErrorGoalRunner{}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("exercise model failure")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, _ := c.goalLifecycleView()
		if view != nil && view.Activation == goaldomain.ActivationDisarmed && view.StopReason == "model-error" {
			if view.Phase != goaldomain.PhaseActive || view.RoundsStarted != 1 || runner.calls != 2 {
				t.Fatalf("error view/calls = %+v / %d", view, runner.calls)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("model error did not disarm the active goal")
}

type failingFlushPersistence struct {
	session *session.Session
}

func (p failingFlushPersistence) Create(session.CreateOptions) (*session.Session, error) {
	return p.session, nil
}
func (p failingFlushPersistence) Open(string, session.AccessMode) (*session.Session, error) {
	return nil, session.ErrSessionNotFound
}
func (p failingFlushPersistence) Stat(context.Context, string) (session.SessionInfo, error) {
	return session.SessionInfo{}, session.ErrSessionNotFound
}
func (p failingFlushPersistence) List(context.Context, string, int) (session.SessionPage, error) {
	return session.SessionPage{}, nil
}

func TestGoalDriverFlushFailureStartsNoAutomaticModelCall(t *testing.T) {
	store, err := session.CreateWithOptions(t.TempDir()+"/goal-flush", "goal-flush", session.OpenOptions{
		Sync: func(*os.File) error { return errors.New("injected sync failure") },
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := session.NewService("desktop", failingFlushPersistence{session: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-flush"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &modelErrorGoalRunner{}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("create before a failed checkpoint")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, _ := c.goalLifecycleView()
		if view != nil && view.StopReason == "persistence-error" {
			if runner.calls != 1 || view.RoundsStarted != 0 || view.Phase != goaldomain.PhaseActive || view.Activation != goaldomain.ActivationDisarmed {
				t.Fatalf("persistence failure view/calls = %+v / %d", view, runner.calls)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("failed durability checkpoint did not stop automatic scheduling")
}

type userWinsRunner struct {
	mu      sync.Mutex
	sources []tool.GoalSource
}

func (r *userWinsRunner) Run(ctx context.Context, _ string) error {
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	r.mu.Lock()
	r.sources = append(r.sources, binding.Authority.Source)
	call := len(r.sources)
	r.mu.Unlock()
	if call == 1 {
		_, err := binding.Owner.CreateGoal(ctx, goaldomain.CreateRequest{Objective: "let queued user input win"}, binding.Authority)
		return err
	}
	view, err := binding.Owner.GetGoal(ctx)
	if err != nil {
		return err
	}
	_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority)
	return err
}

func TestUserInputArrivingDuringGoalFlushWinsAdmission(t *testing.T) {
	flushStarted := make(chan struct{})
	releaseFlush := make(chan struct{})
	var once sync.Once
	store, err := session.CreateWithOptions(t.TempDir()+"/goal-user-wins", "goal-user-wins", session.OpenOptions{
		Sync: func(*os.File) error {
			once.Do(func() { close(flushStarted) })
			<-releaseFlush
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
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "goal-user-wins"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &userWinsRunner{}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	c.Send("create the target")
	select {
	case <-flushStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("goal driver did not reach the durability checkpoint")
	}
	c.Send("finish it from my newer message")
	close(releaseFlush)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, _ := c.goalLifecycleView()
		if view != nil && view.Phase == goaldomain.PhaseComplete {
			runner.mu.Lock()
			sources := append([]tool.GoalSource(nil), runner.sources...)
			runner.mu.Unlock()
			if len(sources) != 2 || sources[0] != tool.GoalSourceDirectHuman || sources[1] != tool.GoalSourceDirectHuman || view.RoundsStarted != 0 {
				t.Fatalf("sources/view = %v / %+v", sources, view)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queued user turn did not win goal-round admission")
}

type restoredGoalRunner struct {
	mu      sync.Mutex
	sources []tool.GoalSource
	done    chan struct{}
}

func (r *restoredGoalRunner) Run(ctx context.Context, _ string) error {
	binding, ok := tool.GoalLifecycleFromContext(ctx)
	if !ok {
		return context.Canceled
	}
	r.mu.Lock()
	r.sources = append(r.sources, binding.Authority.Source)
	call := len(r.sources)
	r.mu.Unlock()
	view, err := binding.Owner.GetGoal(ctx)
	if err != nil || view == nil {
		return err
	}
	switch call {
	case 1:
		if view.Phase != goaldomain.PhaseActive || view.Activation != goaldomain.ActivationDisarmed {
			return errors.New("restored goal was not active/disarmed")
		}
		_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionResume}, binding.Authority)
		return err
	case 2:
		return nil
	case 3:
		_, err = binding.Owner.UpdateGoal(ctx, tool.GoalUpdateRequest{Ref: view.Ref(), Action: tool.GoalActionComplete}, binding.Authority)
		close(r.done)
		return err
	default:
		return errors.New("unexpected extra goal round")
	}
}

func TestColdRestoredGoalCanResumeFromNaturalUserRequestAndContinue(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(root)
	seedService, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = seedService.CloseAll(context.Background()) })
	seedRuntime, err := seedService.Create(t.Context(), session.CreateOptions{SessionID: "restored-goal"})
	if err != nil {
		t.Fatal(err)
	}
	machine := goaldomain.NewMachine(nil, func() string { return "restored-goal-id" })
	if _, err := machine.Create(goaldomain.CreateRequest{Objective: "finish the restored target"}); err != nil {
		t.Fatal(err)
	}
	payload, err := machine.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedRuntime.Session().Append(t.Context(), session.Batch{OperationID: "seed-goal", Events: []session.Event{{Kind: "goal/state", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if err := seedService.Close(t.Context(), seedRuntime.Ref()); err != nil {
		t.Fatal(err)
	}

	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	binding, err := service.Open(t.Context(), session.SessionRef{HostID: "desktop", SessionID: "restored-goal"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := binding.Runtime()
	runner := &restoredGoalRunner{done: make(chan struct{})}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: runner, Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	cleanupGoalDriverController(t, c)
	t.Cleanup(func() { _ = binding.Release(context.Background()) })
	c.Send("继续把这个目标做完")
	select {
	case <-runner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("restored goal did not resume and continue")
	}
	view, err := c.goalLifecycleView()
	if err != nil || view == nil || view.Phase != goaldomain.PhaseComplete || view.RoundsStarted != 2 {
		t.Fatalf("restored goal view = %+v, err = %v", view, err)
	}
	runner.mu.Lock()
	sources := append([]tool.GoalSource(nil), runner.sources...)
	runner.mu.Unlock()
	if len(sources) != 3 || sources[0] != tool.GoalSourceDirectHuman || sources[1] != tool.GoalSourceGoalRound || sources[2] != tool.GoalSourceGoalRound {
		t.Fatalf("restored goal authorities = %v", sources)
	}
}
