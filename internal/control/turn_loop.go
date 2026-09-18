package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

type queuedTurnKind int

const (
	queuedUser queuedTurnKind = iota
	queuedGoal
)

type queuedTurn struct {
	kind      queuedTurnKind
	body      func(ctx context.Context) error
	onStart   func()
	goalRound *goalRoundReservation
	// admissionCtx is only used for the synchronous durability boundary. The
	// model turn has its own controller-owned context and survives RPC return.
	admissionCtx context.Context
}

// turnLoop is the session-scoped execution authority. Controller.mu guards it.
type turnLoop struct {
	phase           session.RuntimePhase
	cancel          context.CancelFunc
	done            chan struct{}
	recoveryFanout  bool // watchdog terminal publication still owns the stores
	turnID          string
	token           uint64
	lastToken       uint64
	pending         []queuedTurn
	wake            bool
	generation      uint64
	runtime         *session.Runtime
	cancelRequested bool
	finishingBound  turnFinishingBoundary
}

type controllerExecution struct {
	c *Controller
}

func (e controllerExecution) Snapshot() session.RuntimeSnapshot {
	if e.c == nil {
		return session.RuntimeSnapshot{Phase: session.RuntimeIdle}
	}
	e.c.mu.Lock()
	defer e.c.mu.Unlock()
	return session.RuntimeSnapshot{Phase: e.c.turns.phase, Activity: e.c.turns.activityNameLocked()}
}

func (e controllerExecution) Cancel() bool {
	if e.c == nil {
		return false
	}
	return e.c.signalTurnCancel()
}

func (t *turnLoop) activityNameLocked() string {
	switch t.phase {
	case session.RuntimeCancelling:
		return "cancelling"
	case session.RuntimeRecoveryRequired:
		return "recovery_required"
	case session.RuntimeRunning, session.RuntimeFinalizing:
		if t.cancelRequested {
			return "cancelling"
		}
		return "turn"
	default:
		return ""
	}
}

func (c *Controller) bodyActiveLocked() bool {
	switch c.turns.phase {
	case session.RuntimeRunning, session.RuntimeCancelling:
		return true
	default:
		return false
	}
}

func (c *Controller) finalizingLocked() bool {
	return c.turns.phase == session.RuntimeFinalizing
}

func (c *Controller) cancelRequestedLocked() bool {
	if c.closed {
		return false
	}
	return c.turns.cancelRequested || c.turns.phase == session.RuntimeCancelling
}

func (c *Controller) recoveryRequiredLocked() bool {
	return c.turns.phase == session.RuntimeRecoveryRequired
}

func (c *Controller) bindExecutionControl() {
	_, runtime, exclusive := c.v3Binding()
	if !exclusive || runtime == nil {
		return
	}
	snap := runtime.StateSnapshot()
	gen := runtime.BindExecution(controllerExecution{c: c})
	c.executionGeneration.Store(gen)
	c.mu.Lock()
	c.turns.generation = gen
	c.turns.runtime = runtime
	if snap.Phase == session.RuntimeRecoveryRequired {
		c.turns.phase = session.RuntimeRecoveryRequired
	}
	c.mu.Unlock()
}

// ExecutionGeneration returns the session-runtime execution generation owned
// by this controller. Zero means the controller is a prepared replacement that
// has not been published as the execution owner.
func (c *Controller) ExecutionGeneration() uint64 {
	if c == nil {
		return 0
	}
	return c.executionGeneration.Load()
}

// ActivateSessionExecution publishes this controller as the exact execution
// owner. expectedGeneration is zero for a previously unbound runtime and the
// outgoing controller generation for a fail-atomic replacement. The method is
// deliberately callback- and I/O-free so hosts may invoke it in their final
// pointer-swap critical section.
func (c *Controller) ActivateSessionExecution(expectedGeneration uint64) error {
	if c == nil {
		return session.ErrSessionNotRunning
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	runtime := c.turns.runtime
	if runtime == nil {
		return nil
	}
	if generation := c.turns.generation; generation != 0 && runtime.OwnsExecution(generation) {
		return nil
	}
	var generation uint64
	if expectedGeneration == 0 {
		generation = runtime.BindExecution(controllerExecution{c: c})
	} else if pending := c.turnEvents.pendingExecutionCommit; pending != nil {
		c.turnEvents.pendingExecutionCommit = nil
		var err error
		generation, _, err = runtime.ReplaceExecutionAndCommit(expectedGeneration, controllerExecution{c: c}, *pending)
		if err != nil {
			return err
		}
	} else {
		generation = runtime.ReplaceExecution(expectedGeneration, controllerExecution{c: c})
	}
	if generation == 0 {
		return session.ErrRuntimeBusy
	}
	c.turns.generation = generation
	c.executionGeneration.Store(generation)
	return nil
}

// ActivateControllerReplacement transfers session execution ownership when a
// host commits a controller pointer swap. Controllers without a shared Runtime
// need no additional activation.
func ActivateControllerReplacement(old, next *Controller) error {
	if next == nil {
		return session.ErrSessionNotRunning
	}
	_, nextRuntime, nextExclusive := next.v3Binding()
	if !nextExclusive || nextRuntime == nil {
		return nil
	}
	expected := uint64(0)
	if old != nil {
		_, oldRuntime, oldExclusive := old.v3Binding()
		if oldExclusive && oldRuntime == nextRuntime {
			expected = old.ExecutionGeneration()
		}
	}
	return next.ActivateSessionExecution(expected)
}

// ActivateSessionAPIReplacement is the host-facing form used at a final
// controller pointer swap. Non-Controller implementations have no exclusive
// Runtime ownership to transfer and are left unchanged.
func ActivateSessionAPIReplacement(old, next SessionAPI) error {
	concreteNext, ok := next.(*Controller)
	if !ok || concreteNext == nil {
		return nil
	}
	concreteOld, _ := old.(*Controller)
	if err := ActivateControllerReplacement(concreteOld, concreteNext); err != nil {
		return err
	}
	if concreteOld != nil && concreteOld.attachmentScope() != "" && concreteOld.attachmentScope() == concreteNext.attachmentScope() && concreteOld.workspaceRoot == concreteNext.workspaceRoot {
		concreteOld.attachmentService().Drafts().CopyScopeTo(concreteOld.attachmentScope(), concreteNext.attachmentService().Drafts())
	}
	return nil
}

func (c *Controller) unbindExecutionControl(runtime *session.Runtime) {
	if runtime == nil {
		return
	}
	c.mu.Lock()
	gen := c.turns.generation
	c.mu.Unlock()
	runtime.UnbindExecution(gen)
}

func (c *Controller) noteExecutionLocked(phase session.RuntimePhase, activity string) {
	if c.turns.runtime == nil || c.turns.generation == 0 {
		return
	}
	c.turns.runtime.NoteExecution(c.turns.generation, phase, activity)
}

func (c *Controller) currentTurnToken() (token uint64, turnID string, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.turns.phase {
	case session.RuntimeIdle, session.RuntimeClosed:
		return c.turns.lastToken, "", false
	default:
		return c.turns.token, c.turns.turnID, true
	}
}

func (c *Controller) discardLateTurnEvent(e event.Event) bool {
	if e.TurnID == "" || !lateBusinessEvent(e.Kind) {
		return false
	}
	_, turnID, active := c.currentTurnToken()
	if active {
		return e.TurnID != turnID
	}
	return true
}

func (c *Controller) startTurnLocked(parent context.Context, next queuedTurn) (ctx context.Context, cancel context.CancelFunc, admitted bool) {
	if parent == nil {
		parent = context.Background()
	}
	if c.turns.runtime != nil && !c.turns.runtime.BeginExecution(c.turns.generation, "turn") {
		return nil, nil, false
	}
	ctx, cancel = context.WithCancel(extension.ContextWithRuntimeOwner(c.withAuthentication(parent), c.runtimeOwner))
	c.turns.cancel = cancel
	c.turns.done = make(chan struct{})
	c.turns.finishingBound.beginIdle()
	c.turns.phase = session.RuntimeRunning
	c.turns.cancelRequested = false
	c.turns.token++
	ctx = context.WithValue(ctx, executionTokenKey{}, c.turns.token)
	c.turns.turnID = ""
	return ctx, cancel, true
}

func (c *Controller) popNextPendingLocked() (queuedTurn, bool) {
	if len(c.turns.pending) == 0 {
		c.turns.wake = false
		return queuedTurn{}, false
	}
	next := c.turns.pending[0]
	c.turns.pending = c.turns.pending[1:]
	c.turns.wake = len(c.turns.pending) > 0
	return next, true
}

func (c *Controller) queueTurnLocked(item queuedTurn) {
	// Harness wakeRequested: input that cannot join the current activity is
	// claimed once when the body converges. Close clears this queue so a
	// disposed session never starts a latched turn.
	c.turns.pending = append(c.turns.pending, item)
	c.turns.wake = true
}

func (c *Controller) signalTurnCancel() bool {
	_, _, cancelled := c.signalTurnCancelIdentity()
	return cancelled
}

func (c *Controller) signalTurnCancelIdentity() (uint64, string, bool) {
	c.mu.Lock()
	token, turnID := c.turns.token, c.turns.turnID
	cancel := c.turns.cancel
	first := cancel != nil && c.turns.phase == session.RuntimeRunning
	if cancel != nil && (c.turns.phase == session.RuntimeRunning || c.turns.phase == session.RuntimeCancelling) {
		c.turns.phase = session.RuntimeCancelling
		c.turns.cancelRequested = true
	}
	done := c.turns.done
	c.mu.Unlock()
	if cancel == nil {
		return token, turnID, false
	}
	cancel()
	if first {
		c.startCancellationWatchdog(done)
	}
	return token, turnID, true
}

func (c *Controller) enterRecoveryLocked(reason string) {
	c.turns.phase = session.RuntimeRecoveryRequired
	c.noteExecutionLocked(session.RuntimeRecoveryRequired, reason)
	if c.turns.runtime != nil {
		c.turns.runtime.RequireRecovery(reason)
	}
}

func (c *Controller) spawnGuardedTurn(ctx context.Context, cancel context.CancelFunc, item queuedTurn) {
	ctx, completion := withGuardedTurnCompletion(ctx)
	admissionCtx := item.admissionCtx
	if admissionCtx == nil {
		admissionCtx = context.Background()
	}
	body := c.prepareTurnAdmissionWithGoalRound(admissionCtx, item.body, item.goalRound)
	if ledger := c.turnEventLedger(); ledger != nil {
		c.mu.Lock()
		c.turns.turnID = ledger.ActiveTurnID()
		c.mu.Unlock()
	}
	c.liveness.reset(time.Now())
	c.autosaveWG.Go(func() {
		c.autosaveWhileRunning(ctx)
	})
	go func() {
		defer cancel()
		defer func() {
			c.finishGoalRoundActivity(item.goalRound)
			c.kickGoalDriver()
		}()
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("internal error: %v", r)
				item.goalRound.setResult(err, false)
				c.finishGuardedTurn(err, completion)
			}
		}()
		err := body(ctx)
		if item.goalRound != nil {
			item.goalRound.setResult(err, errors.Is(ctx.Err(), context.Canceled) && c.CancelRequested())
		}
		c.finishGuardedTurn(explainError(err), completion)
	}()
}

func (c *Controller) cancellationGrace() time.Duration {
	if c != nil && c.testCancelGrace > 0 {
		return c.testCancelGrace
	}
	return 15 * time.Second
}

func (c *Controller) finishGuardedTurn(err error, completion *guardedTurnCompletion) {
	c.authentication.recordFailure(err, c.ModelRef())
	c.memory.clearAutoRemember()
	c.mu.Lock()
	cancelRequested := c.turns.cancelRequested
	if c.turns.phase == session.RuntimeRecoveryRequired {
		c.turns.cancel = nil
		closing := c.closed
		c.mu.Unlock()
		// The cancellation watchdog already committed the recovery terminal.
		// A closing controller must not emit another terminal after its ledger
		// and session binding have been finalized.
		if !closing {
			c.emitTurnDoneEvent(err, cancelRequested, completion)
		}
		c.mu.Lock()
		// Keep the owned turn live through terminal fanout. Close must not
		// release stores while that fanout can still publish durable events.
		if c.turns.done != nil {
			close(c.turns.done)
			c.turns.done = nil
		}
		closing = c.closed && !c.turns.recoveryFanout
		c.turns.finishingBound.endIdle()
		c.mu.Unlock()
		if closing {
			c.finalizeControllerClose()
		}
		c.refreshRuntimeState(event.Event{})
		return
	}
	if c.turns.done != nil {
		close(c.turns.done)
		c.turns.done = nil
	}
	c.turns.phase = session.RuntimeFinalizing
	c.turns.finishingBound.begin(true)
	c.turns.cancel = nil
	c.noteExecutionLocked(session.RuntimeFinalizing, "turn")
	c.mu.Unlock()

	c.refreshRuntimeState(event.Event{})
	defer func() {
		c.mu.Lock()
		c.turns.finishingBound.end()
		c.turns.cancelRequested = false
		if c.turns.phase == session.RuntimeRecoveryRequired {
			closing := c.closed
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			if closing {
				c.finalizeControllerClose()
			}
			c.refreshRuntimeState(event.Event{})
			return
		}
		if ledger := c.turnEventLedger(); ledger != nil && ledger.CurrentStatus() == event.TurnRecoveryRequired {
			c.enterRecoveryLocked("terminal")
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			c.refreshRuntimeState(event.Event{})
			return
		}
		if c.closed {
			c.turns.lastToken = c.turns.token
			c.turns.phase = session.RuntimeClosed
			c.turns.turnID = ""
			c.noteExecutionLocked(session.RuntimeIdle, "")
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			c.finalizeControllerClose()
			c.refreshRuntimeState(event.Event{})
			return
		}
		if authErr := c.authentication.admissionError(); authErr != nil {
			for _, pending := range c.turns.pending {
				if pending.goalRound != nil {
					pending.goalRound.setResult(authErr, false)
				}
			}
			c.turns.pending = nil
			c.turns.wake = false
			c.turns.lastToken = c.turns.token
			c.turns.phase = session.RuntimeIdle
			c.turns.turnID = ""
			c.noteExecutionLocked(session.RuntimeIdle, "")
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: "authentication_not_ready", Text: authErr.Error()})
			c.refreshRuntimeState(event.Event{})
			return
		}
		next, ok := c.popNextPendingLocked()
		if !ok {
			c.turns.lastToken = c.turns.token
			c.turns.phase = session.RuntimeIdle
			c.turns.turnID = ""
			c.noteExecutionLocked(session.RuntimeIdle, "")
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			c.maybeDispatchInbox()
			c.refreshRuntimeState(event.Event{})
			return
		}
		ctx, cancel, admitted := c.startTurnLocked(context.Background(), next)
		if !admitted {
			c.enterRecoveryLocked("execution_owner_lost")
			c.turns.finishingBound.endIdle()
			c.mu.Unlock()
			c.refreshRuntimeState(event.Event{})
			return
		}
		c.mu.Unlock()
		if next.onStart != nil {
			next.onStart()
		}
		c.spawnGuardedTurn(ctx, cancel, next)
		c.refreshRuntimeState(event.Event{})
	}()
	c.emitTurnDoneEvent(err, cancelRequested, completion)
}

func (c *Controller) emitTurnDoneEvent(err error, cancelRequested bool, completion *guardedTurnCompletion) {
	c.inbox.mu.Lock()
	activeInboxID := ""
	for id := range c.inbox.activeItemIDs {
		activeInboxID = id
		break
	}
	c.inbox.mu.Unlock()
	done := event.Event{
		Kind:           event.TurnDone,
		Err:            err,
		Cancelled:      cancelRequested,
		Outcome:        turnOutcome(err),
		CheckpointTurn: c.validatedCheckpointTurn(completion),
		Receipt:        c.executor.CompletionReceipt(),
		ItemID:         activeInboxID,
	}
	if done.CheckpointTurn != nil {
		changes := completion.checkpoint.store.FreezeTurnChanges(*done.CheckpointTurn)
		if done.Receipt == nil && (len(changes.Files) > 0 || len(changes.Reasons) > 0) {
			done.Receipt = &event.CompletionReceipt{AssessmentKind: "facts", Verdict: "unknown"}
		}
		if done.Receipt != nil {
			receipt := *done.Receipt
			receipt.Diff = changes.Summary()
			receipt.Interrupted = cancelRequested
			done.Receipt = &receipt
		}
	}
	done.Receipt = bindCompletionLogSources(done.Receipt, c.History())
	done = c.applyTurnDoneProtocol(done, cancelRequested)
	c.applyToolRecoveryTurnStatus(&done, completion)
	var readErr *agent.IncompleteReadError
	if errors.As(err, &readErr) {
		done.ReadPause = readErr.Pause
	}
	done.Diagnostic = provider.DiagnoseFailure(err)
	done.Detail = provider.FailureDiagnosticDetail(done.Diagnostic)
	if !cancelRequested {
		done.ProtocolRecovery = c.executor.PendingProtocolRecovery()
	}
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		done.Readiness = &event.FinalReadiness{Attempts: readinessErr.Attempts, Missing: append([]string(nil), readinessErr.Missing...)}
	}
	c.onInboxTurnDone()
	c.sink.Emit(done)
}

func (c *Controller) startCancellationWatchdog(done chan struct{}) {
	if c == nil || done == nil {
		return
	}
	go func() {
		timer := time.NewTimer(c.cancellationGrace())
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-done:
			return
		}

		c.mu.Lock()
		stillRunning := c.turns.done == done && (c.turns.phase == session.RuntimeRunning || c.turns.phase == session.RuntimeCancelling)
		turnID := c.turns.turnID
		if stillRunning {
			c.turns.recoveryFanout = true
			c.enterRecoveryLocked("cancellation_grace_expired")
		}
		c.mu.Unlock()
		if !stillRunning {
			return
		}
		if turnID == "" {
			if ledger := c.turnEventLedger(); ledger != nil {
				turnID = ledger.ActiveTurnID()
			}
		}
		recovery := &event.RecoveryStatus{
			State:                "recovery_required",
			Phase:                "cancellation_grace_expired",
			Reason:               "cancellation_grace_expired",
			RequiresUserDecision: true,
		}
		_ = c.emitTurnEventChecked(event.Event{
			Kind:      event.TurnDone,
			TurnID:    turnID,
			Status:    event.TurnRecoveryRequired,
			Cancelled: true,
			Outcome:   "unknown",
			Recovery:  recovery,
		})
		c.mu.Lock()
		c.turns.recoveryFanout = false
		closing := c.closed && c.turns.done == nil && !c.finalizingLocked()
		c.mu.Unlock()
		if closing {
			c.finalizeControllerClose()
		}
		c.refreshRuntimeState(event.Event{})
	}()
}
