package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/liveview"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/snapshotcapture"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/sessiondisplay"
	"reasonix/internal/sessiontelemetry"
)

// RuntimeEvent is the complete realtime notification envelope. RuntimeSnapshot
// reuses the wrapper for adapter compatibility, but semantic snapshot entries
// carry Seq zero and are ordered only by their replay sequence.
type RuntimeEvent struct {
	HostEpoch    protocol.HostEpoch
	Target       protocol.RuntimeTarget
	RuntimeEpoch protocol.RuntimeEpoch
	Seq          uint64
	TurnID       protocol.TurnID
	OperationID  protocol.OperationID
	Event        eventwire.Event
}

// RuntimeSnapshot is the actor-frozen semantic recovery boundary. Events is a
// complete replayable projection of the current unfinished Turn, not a raw seq
// log. Its RuntimeEvent wrappers intentionally have Seq zero; BoundarySeq and
// live notifications are the only holders of realtime ordering identities.
type RuntimeSnapshot struct {
	SnapshotID              protocol.SnapshotID
	HostEpoch               protocol.HostEpoch
	Target                  protocol.RuntimeTarget
	RuntimeEpoch            protocol.RuntimeEpoch
	BoundarySeq             uint64
	Running                 bool
	PendingPrompt           *protocol.PendingPrompt
	ActiveJobs              int
	CurrentTurn             protocol.TurnID
	CurrentOperation        *protocol.OperationState
	CancelRequested         bool
	LastOutcome             protocol.SessionOutcome
	LastError               string
	PreviousTurnInterrupted bool
	InterruptionReason      protocol.InterruptionReason
	// Goal and GoalStatus are read from the Controller at the same actor
	// barrier as runtime state. Catalog sidecars are not authoritative for a
	// live Goal and therefore must not be sampled later by the wire adapter.
	Goal       *string
	GoalStatus protocol.GoalStatus
	Events     []RuntimeEvent
	// Capture is the immutable Controller/history projection frozen by the same
	// actor action that installs a subscription. It is empty only for the
	// internal Snapshot convenience method, which does not create a retained
	// protocol snapshot owner.
	Capture snapshotcapture.Output
	// pendingPromptEvent is the full, Host-only event projection for the active
	// Prompt. It retains MCPTrust and other safety context absent from the frozen
	// protocol.PendingPrompt DTO, but is unexported so generic JSON encoding can
	// never accidentally add an undeclared wire field.
	pendingPromptEvent *eventwire.Event
}

// PendingPromptEvent returns the complete Host-side Prompt event with its ID
// already rewritten to the opaque Host PromptID. Controller-private IDs never
// cross this accessor.
func (s RuntimeSnapshot) PendingPromptEvent() *eventwire.Event {
	return clonePromptEvent(s.pendingPromptEvent)
}

type SubmitResult struct {
	Target       protocol.RuntimeTarget
	RuntimeEpoch protocol.RuntimeEpoch
	TurnID       protocol.TurnID
}

type CancelResult struct {
	Status protocol.CancelStatus
	TurnID protocol.TurnID
}

type mutationReplay struct {
	attempt idempotency.Attempt
}

type TurnMismatchError struct {
	Requested protocol.TurnID
	Current   protocol.TurnID
}

func (e *TurnMismatchError) Error() string {
	return fmt.Sprintf("%v: requested %q, current %q", ErrTurnMismatch, e.Requested, e.Current)
}

func (e *TurnMismatchError) Unwrap() error { return ErrTurnMismatch }

// ResyncRequired is the one terminal message for a subscription. Ordinary
// events stop after queue overflow or runtime/target replacement until the
// same attachment atomically installs a replacement subscription.
type ResyncRequired struct {
	SubscriptionID          protocol.SubscriptionID
	HostEpoch               protocol.HostEpoch
	Target                  protocol.RuntimeTarget
	RuntimeEpoch            protocol.RuntimeEpoch
	LastSeq                 uint64
	Reason                  protocol.ResyncReason
	ReplacementTarget       *protocol.RuntimeTarget
	ReplacementRuntimeEpoch protocol.RuntimeEpoch
}

type SubscriptionMessage struct {
	Event  *RuntimeEvent
	Resync *ResyncRequired
}

type Subscription struct {
	ID       protocol.SubscriptionID
	Snapshot RuntimeSnapshot
	Messages <-chan SubscriptionMessage
}

// SessionRuntime serializes every mutation, event, and subscribe barrier for a
// single Session. Its Controller and context belong to RuntimeManager, not to an
// attach transport.
type SessionRuntime struct {
	ctx       context.Context
	cancel    context.CancelFunc
	hostEpoch protocol.HostEpoch
	target    protocol.RuntimeTarget
	epoch     protocol.RuntimeEpoch
	opts      RuntimeManagerOptions

	controller    control.SessionAPI
	resumeState   control.SessionResumeState
	telemetry     sessiontelemetry.Snapshot
	telemetryPath string
	workspaceRoot string
	mailbox       *actorMailbox
	done          chan struct{}
	startOnce     sync.Once
	stopOnce      sync.Once
	current       atomic.Bool
	accepting     atomic.Bool
}

type runtimeActorState struct {
	seq     uint64
	running bool
	// pendingPrompt is registered only from typed Controller ApprovalRequest or
	// AskRequest events. activeJobs remains an explicit exact-registry hook; it
	// is never guessed from ordinary event text or tool notifications.
	pendingPrompt     *pendingPromptState
	activeJobs        int
	activeJobsManaged bool
	currentTurn       protocol.TurnID
	currentOperation  *currentOperationState
	// liveOperationID retains the identity of the Operation whose reduced
	// progress is still present after completion. A reconnect that races the
	// completion boundary can therefore distinguish those events from a Turn;
	// the next foreground admission resets the live reducer and this identity.
	liveOperationID         protocol.OperationID
	cancelRequested         bool
	lastOutcome             protocol.SessionOutcome
	lastError               string
	previousTurnInterrupted bool
	interruptionReason      protocol.InterruptionReason
	live                    liveview.Reducer
	issuedTurnIDs           map[protocol.TurnID]struct{}
	issuedPromptIDs         map[protocol.PromptID]struct{}
	issuedCheckpointIDs     map[protocol.CheckpointID]struct{}
	checkpointIDs           map[int]protocol.CheckpointID
	issuedSubscriptionIDs   map[protocol.SubscriptionID]struct{}
	acceptedTurn            *snapshotcapture.AcceptedTurn
	readFiles               []sessiontelemetry.ReadFileRecord
	usage                   sessiontelemetry.UsageStats
	memoryResearch          *runtimeservice.MemoryResearchService
	memoryResearchErr       error
	subscriptions           map[protocol.SubscriptionID]*subscriptionState
	stopping                bool
}

type currentOperationState struct {
	id              protocol.OperationID
	kind            protocol.OperationKind
	cancelRequested bool
	handle          *control.OperationHandle
}

type pendingPromptState struct {
	id           protocol.PromptID
	controllerID string
	kind         protocol.PromptKind
	public       protocol.PendingPrompt
	event        eventwire.Event
}

type subscriptionState struct {
	id         protocol.SubscriptionID
	attachment AttachmentKey
	messages   chan SubscriptionMessage
	overflowed bool
	migrating  bool
	lastSeq    uint64
}

// retiredSubscription is detached from its stopped actor but deliberately
// keeps the terminal channel open. RuntimeManager owns it until the same
// attachment installs a replacement subscription, detaches, or shuts down.
type retiredSubscription struct {
	state                   *subscriptionState
	replacementTarget       protocol.RuntimeTarget
	replacementRuntimeEpoch protocol.RuntimeEpoch
	migrating               bool
	closeOnce               sync.Once
}

func (s *retiredSubscription) close() {
	if s == nil || s.state == nil {
		return
	}
	s.closeOnce.Do(func() { closeSubscription(s.state) })
}

type runtimeReplacementResult struct {
	retired []*retiredSubscription
}

type subscriptionAdmission struct {
	subscription Subscription
	previousID   protocol.SubscriptionID
}

type actorAction func(*runtimeActorState)

type runtimeActivity struct {
	summary       protocol.SessionRuntimeSummary
	subscriptions int
}

func (a runtimeActivity) idle() bool {
	return !a.summary.Running && !a.summary.PendingPrompt && a.summary.ActiveJobs == 0 && a.subscriptions == 0
}

// idleReleaseBarrier freezes one Session actor at a sequencer boundary until
// RuntimeManager commits or aborts a release decision. Actions already queued
// run first; subscribe, mutation, and event actions queued later cannot slip
// between the idle observation and the stop transition.
type idleReleaseBarrier struct {
	ready    chan runtimeActivity
	decision chan bool
	done     chan struct{}
}

type sessionCloseAdmission struct {
	result   protocol.SessionCloseResult
	activity runtimeActivity
	replay   *mutationReplay
	err      error
}

type sessionCloseDecision struct {
	accept bool
	cause  error
}

// sessionCloseBarrier keeps the actor paused after requestId registration,
// epoch admission, and idle observation until RuntimeManager atomically keeps
// or removes the registry entry for this incarnation.
type sessionCloseBarrier struct {
	ready    chan sessionCloseAdmission
	decision chan sessionCloseDecision
	done     chan error
}

type actorMailbox struct {
	mu     sync.Mutex
	ready  *sync.Cond
	queue  []actorAction
	closed bool
	sealed bool
}

func newActorMailbox() *actorMailbox {
	m := &actorMailbox{}
	m.ready = sync.NewCond(&m.mu)
	return m
}

func (m *actorMailbox) enqueue(action actorAction) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.queue = append(m.queue, action)
	m.ready.Signal()
	return true
}

// enqueueAdmission orders ordinary actor admission against replacement or
// shutdown seal. Once either seal wins the mailbox lock, no later RPC action
// can reach requestId registration or Controller side effects on this
// incarnation.
func (m *actorMailbox) enqueueAdmission(action actorAction) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.sealed {
		return false
	}
	m.queue = append(m.queue, action)
	m.ready.Signal()
	return true
}

// sealAdmissions closes only the ordinary RPC admission gate. Controller
// events and the final shutdown barrier may still be appended with enqueue so
// already-admitted work can unwind before the actor exits.
func (m *actorMailbox) sealAdmissions() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.sealed = true
	return true
}

// sealAndEnqueue atomically closes ordinary admission and appends the first
// replacement barrier after every action that already won admission.
func (m *actorMailbox) sealAndEnqueue(action actorAction) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.sealed {
		return false
	}
	m.sealed = true
	m.queue = append(m.queue, action)
	m.ready.Signal()
	return true
}

func (m *actorMailbox) next() (actorAction, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.queue) == 0 && !m.closed {
		m.ready.Wait()
	}
	if len(m.queue) == 0 {
		return nil, false
	}
	action := m.queue[0]
	m.queue[0] = nil
	m.queue = m.queue[1:]
	return action, true
}

func (m *actorMailbox) close() {
	m.mu.Lock()
	m.closed = true
	for i := range m.queue {
		m.queue[i] = nil
	}
	m.queue = nil
	m.ready.Broadcast()
	m.mu.Unlock()
}

func newSessionRuntime(
	ctx context.Context,
	cancel context.CancelFunc,
	hostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
	epoch protocol.RuntimeEpoch,
	opts RuntimeManagerOptions,
) *SessionRuntime {
	runtime := &SessionRuntime{
		ctx: ctx, cancel: cancel, hostEpoch: hostEpoch, target: target, epoch: epoch, opts: opts,
		mailbox: newActorMailbox(), done: make(chan struct{}),
	}
	runtime.current.Store(true)
	runtime.accepting.Store(true)
	return runtime
}

func (r *SessionRuntime) start() {
	r.startOnce.Do(func() { go r.run() })
}

func (r *SessionRuntime) abortInitialization() {
	r.current.Store(false)
	r.accepting.Store(false)
	r.cancel()
	r.mailbox.close()
	close(r.done)
}

func (r *SessionRuntime) discardBuiltRuntime() {
	_ = safeControllerCall(r.controller.Close)
	r.abortInitialization()
}

func (r *SessionRuntime) markCurrent(current bool) { r.current.Store(current) }

func (r *SessionRuntime) Target() protocol.RuntimeTarget { return r.target }

func (r *SessionRuntime) Epoch() protocol.RuntimeEpoch { return r.epoch }

func (r *SessionRuntime) Done() <-chan struct{} { return r.done }

func (r *SessionRuntime) run() {
	memoryResearch, memoryResearchErr := runtimeservice.NewMemoryResearchService(runtimeservice.RuntimeBinding{
		Session: runtimeapi.SessionRef{
			WorkspaceID: runtimeapi.WorkspaceID(r.target.WorkspaceID),
			SessionID:   runtimeapi.SessionID(r.target.SessionID),
		},
		Incarnation: string(r.epoch),
	}, r.workspaceRoot)
	state := runtimeActorState{
		activeJobs:            0,
		issuedTurnIDs:         make(map[protocol.TurnID]struct{}),
		issuedPromptIDs:       make(map[protocol.PromptID]struct{}),
		issuedCheckpointIDs:   make(map[protocol.CheckpointID]struct{}),
		checkpointIDs:         make(map[int]protocol.CheckpointID),
		issuedSubscriptionIDs: make(map[protocol.SubscriptionID]struct{}),
		subscriptions:         make(map[protocol.SubscriptionID]*subscriptionState),
		readFiles:             append([]sessiontelemetry.ReadFileRecord(nil), r.telemetry.ReadFiles...),
		usage:                 r.telemetry.Usage.Clone(),
		memoryResearch:        memoryResearch,
		memoryResearchErr:     memoryResearchErr,
	}
	if r.resumeState.PreviousTurnInterrupted {
		state.lastOutcome = protocol.OutcomeInterrupted
		state.previousTurnInterrupted = true
		state.interruptionReason = protocol.InterruptionHostRestarted
	}
	for {
		action, ok := r.mailbox.next()
		if !ok {
			break
		}
		action(&state)
		if state.stopping {
			break
		}
	}

	r.accepting.Store(false)
	r.cancel()
	resetRuntimeLiveState(&state)
	state.memoryResearch = nil
	for id, subscription := range state.subscriptions {
		delete(state.subscriptions, id)
		closeSubscription(subscription)
	}
	_ = safeControllerCall(r.controller.Close)
	r.mailbox.close()
	close(r.done)
}

func (r *SessionRuntime) stop() {
	r.stopOnce.Do(func() {
		r.accepting.Store(false)
		r.cancel()
		if !r.mailbox.enqueue(func(state *runtimeActorState) {
			resetRuntimeLiveState(state)
			state.stopping = true
		}) {
			return
		}
	})
}

// sealForReplacement creates a two-step sequencer barrier. Ordinary actor
// admission is sealed atomically with the first action. A second commit action
// is then appended at the mailbox tail so synchronous Controller events emitted
// by already-admitted actions can finish before the incarnation is retired.
// Events enqueued after the commit are discarded with the closed mailbox.
func (r *SessionRuntime) sealForReplacement(
	replacementTarget protocol.RuntimeTarget,
	replacementEpoch protocol.RuntimeEpoch,
	reason protocol.ResyncReason,
) (runtimeReplacementResult, error) {
	if replacementEpoch == "" {
		return runtimeReplacementResult{}, errors.New("replacement runtime epoch is empty")
	}
	switch reason {
	case protocol.ResyncRuntimeReplaced:
		if replacementTarget != r.target {
			return runtimeReplacementResult{}, errors.New("runtime replacement changed target")
		}
	case protocol.ResyncTargetReplaced:
		if replacementTarget == r.target {
			return runtimeReplacementResult{}, errors.New("target replacement did not change target")
		}
	default:
		return runtimeReplacementResult{}, errors.New("invalid replacement resync reason")
	}

	ready := make(chan runtimeReplacementResult, 1)
	commit := func(state *runtimeActorState) {
		// current and accepting move together inside the actor. emit also checks
		// them again in its queued action, so a late old event can never consume
		// sequence space or reach a subscription.
		r.accepting.Store(false)
		r.current.Store(false)
		result := runtimeReplacementResult{retired: make([]*retiredSubscription, 0, len(state.subscriptions))}
		for id, subscription := range state.subscriptions {
			delete(state.subscriptions, id)
			if !subscription.overflowed {
				subscription.overflowed = true
				drainSubscription(subscription.messages)
				resync := &ResyncRequired{
					SubscriptionID:          subscription.id,
					HostEpoch:               r.hostEpoch,
					Target:                  r.target,
					RuntimeEpoch:            r.epoch,
					LastSeq:                 subscription.lastSeq,
					Reason:                  reason,
					ReplacementRuntimeEpoch: replacementEpoch,
				}
				if reason == protocol.ResyncTargetReplaced {
					target := replacementTarget
					resync.ReplacementTarget = &target
				}
				subscription.messages <- SubscriptionMessage{Resync: resync}
			}
			result.retired = append(result.retired, &retiredSubscription{
				state: subscription, replacementTarget: replacementTarget,
				replacementRuntimeEpoch: replacementEpoch, migrating: subscription.migrating,
			})
		}
		resetRuntimeLiveState(state)
		state.stopping = true
		ready <- result
	}
	if !r.mailbox.sealAndEnqueue(func(state *runtimeActorState) {
		// enqueue remains available for old event completion during the grace
		// window; ordinary RPC actions use enqueueAdmission and are already shut.
		if !r.mailbox.enqueue(commit) {
			commit(state)
		}
	}) {
		return runtimeReplacementResult{}, ErrRuntimeClosed
	}
	select {
	case result := <-ready:
		return result, nil
	case <-r.done:
		return runtimeReplacementResult{}, ErrRuntimeClosed
	}
}

// stopForRuntimeShutdown is the daemon/Host lifecycle path. It is deliberately
// separate from replacement, Session close, and explicit Turn Cancel. Actor
// admission is sealed before RecoveryLifecycle runs outside the actor. That
// lifecycle call must be able to cancel and wait for Controller work while an
// already-admitted actor action is itself waiting for durable Turn admission.
// Only after Controller work has unwound does a final actor barrier retire the
// semantic state, before runtimeCtx or Controller.Close can trigger ordinary
// cleanup.
func (r *SessionRuntime) stopForRuntimeShutdown() {
	r.stopOnce.Do(func() {
		if !r.mailbox.sealAdmissions() {
			r.accepting.Store(false)
			r.current.Store(false)
			r.cancel()
			return
		}
		r.accepting.Store(false)
		if recovery, ok := r.controller.(control.RecoveryLifecycle); ok {
			_ = safeControllerCall(recovery.PrepareRuntimeShutdown)
		}
		r.cancel()
		if !r.mailbox.enqueue(func(state *runtimeActorState) {
			r.current.Store(false)
			resetRuntimeLiveState(state)
			state.stopping = true
		}) {
			r.current.Store(false)
		}
	})
}

// emit is the immutable event sink bound at Controller construction time. Late
// events from a replaced runtime are rejected before they can consume seq.
func (r *SessionRuntime) emit(value event.Event) {
	if !r.current.Load() || !r.accepting.Load() {
		return
	}
	// Consume the typed event synchronously while its pointer-backed fields are
	// still owned by the emitter. The queued wire value owns its nested data and
	// can safely wait for the actor without extending event.Sink's call lifetime.
	wireValue := eventwire.ToWire(value)
	kind := value.Kind
	r.mailbox.enqueue(func(state *runtimeActorState) {
		if !r.current.Load() || !r.accepting.Load() {
			return
		}
		r.applyEvent(state, kind, wireValue)
	})
}

func (r *SessionRuntime) applyEvent(state *runtimeActorState, kind event.Kind, wireEvent eventwire.Event) {
	if kind == event.ApprovalRequest || kind == event.AskRequest {
		rewritten, err := r.registerPromptEvent(state, kind, wireEvent)
		if err != nil {
			// Never leak a Controller-private ID as a fallback. A production
			// random generator makes this path exceptional; keeping the turn
			// blocked is safer than fabricating an actionable Prompt identity.
			state.lastError = "register Remote Prompt: " + err.Error()
			return
		}
		wireEvent = rewritten
	}
	r.applyTelemetry(state, kind, wireEvent)
	// Snapshot authority advances before the notification receives its seq. A
	// subscribe barrier can therefore never observe a broadcast event missing
	// from the frozen semantic view.
	state.live.Apply(wireEvent)
	state.seq++
	turnID := state.currentTurn
	operationID := protocol.OperationID("")
	if turnID == "" && state.currentOperation != nil {
		operationID = state.currentOperation.id
	}
	envelope := RuntimeEvent{
		HostEpoch: r.hostEpoch, Target: r.target, RuntimeEpoch: r.epoch,
		Seq: state.seq, TurnID: turnID, OperationID: operationID, Event: wireEvent,
	}
	if kind == event.TurnDone && state.currentOperation == nil {
		switch {
		case state.cancelRequested:
			state.lastOutcome = protocol.OutcomeCancelled
		case wireEvent.Err != "":
			state.lastOutcome = protocol.OutcomeFailed
		default:
			state.lastOutcome = protocol.OutcomeCompleted
		}
		state.lastError = wireEvent.Err
		state.running = false
		state.currentTurn = ""
		state.cancelRequested = false
		state.pendingPrompt = nil
		state.acceptedTurn = nil
		state.previousTurnInterrupted = false
		state.interruptionReason = ""
	} else if kind == event.OperationDone && state.currentOperation != nil {
		switch {
		case state.cancelRequested:
			state.lastOutcome = protocol.OutcomeCancelled
		case wireEvent.Err != "":
			state.lastOutcome = protocol.OutcomeFailed
		default:
			state.lastOutcome = protocol.OutcomeCompleted
		}
		state.lastError = wireEvent.Err
		state.running = false
		state.currentOperation = nil
		state.cancelRequested = false
		state.acceptedTurn = nil
	} else if kind == event.TurnStarted {
		// TurnStarted resets the reducer. Keep the typed prompt authority aligned
		// even if a malformed Controller attempted to overlap Turns.
		state.pendingPrompt = nil
	}

	for _, subscription := range state.subscriptions {
		r.deliver(subscription, envelope)
	}
}

func (r *SessionRuntime) applyTelemetry(state *runtimeActorState, kind event.Kind, wireEvent eventwire.Event) {
	if state == nil {
		return
	}
	now := r.telemetryNowMillis()
	changed := false
	switch kind {
	case event.TurnStarted:
		state.usage.TurnStarted(now)
		changed = true
	case event.Usage:
		state.usage.RecordUsage(wireEvent.Usage)
		changed = true
	case event.TurnDone:
		state.usage.TurnDone(now)
		changed = true
	case event.ToolResult:
		turn := 0
		if r.controller != nil {
			if err := safeControllerCall(func() { turn = r.controller.Turn() }); err != nil {
				r.reportTelemetryError(fmt.Errorf("capture read-file Turn: %w", err))
			}
		}
		if record, ok := sessiontelemetry.ReadFileFromEvent(wireEvent, turn, now, r.workspaceRoot); ok && remoteReadFilePath(record.Path) {
			state.readFiles = append(state.readFiles, record)
			changed = true
		}
	}
	if !changed {
		return
	}
	path := r.telemetryPersistencePath()
	if path == "" {
		return
	}
	if err := sessiontelemetry.Save(path, sessiontelemetry.View(state.readFiles, state.usage, now)); err != nil {
		r.reportTelemetryError(err)
	}
}

// telemetryNowMillis and reportTelemetryError isolate injected hooks from the
// Session actor. A broken observability hook must never terminate the actor and
// strand accepted work or a subscribe caller.
func (r *SessionRuntime) telemetryNowMillis() (now int64) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.reportTelemetryError(fmt.Errorf("read telemetry clock: callback panicked: %v", recovered))
			now = sessiontelemetry.NowMillis()
		}
	}()
	return r.opts.NowMillis()
}

func (r *SessionRuntime) reportTelemetryError(err error) {
	if err == nil || r.opts.OnTelemetryError == nil {
		return
	}
	defer func() { _ = recover() }()
	r.opts.OnTelemetryError(r.target, err)
}

// A Controller may assign its persistent Session path only after the runtime
// is created. Resolve it lazily on the first telemetry mutation so persistence
// remains enabled without moving path authority outside the Controller.
func (r *SessionRuntime) telemetryPersistencePath() string {
	if r.telemetryPath != "" {
		return r.telemetryPath
	}
	if r.controller == nil {
		return ""
	}
	var sessionPath string
	if err := safeControllerCall(func() { sessionPath = r.controller.SessionPath() }); err != nil {
		r.reportTelemetryError(fmt.Errorf("resolve telemetry path: %w", err))
		return ""
	}
	if strings.TrimSpace(sessionPath) == "" {
		return ""
	}
	r.telemetryPath = sessiontelemetry.Path(sessionPath)
	return r.telemetryPath
}

func remoteReadFilePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func clearPendingPrompt(state *runtimeActorState) {
	if state == nil || state.pendingPrompt == nil {
		return
	}
	state.live.ClearPrompt(string(state.pendingPrompt.id))
	state.pendingPrompt = nil
}

func resetRuntimeLiveState(state *runtimeActorState) {
	if state == nil {
		return
	}
	state.live.Reset()
	state.pendingPrompt = nil
	state.acceptedTurn = nil
	state.currentOperation = nil
	state.liveOperationID = ""
}

func (r *SessionRuntime) registerPromptEvent(state *runtimeActorState, kind event.Kind, wireEvent eventwire.Event) (eventwire.Event, error) {
	controllerID, err := controllerPromptID(kind, wireEvent)
	if err != nil {
		return eventwire.Event{}, err
	}
	if state.pendingPrompt != nil && state.pendingPrompt.controllerID == controllerID && state.pendingPrompt.kind == promptKind(kind) {
		// ReplayPendingPrompts re-emits the same Controller identity after a
		// frontend reconnect. Preserve both the already-issued Host ID and the
		// original full safety payload: Controller replay currently omits MCPTrust
		// even though the Host must keep it available until resolution.
		return *clonePromptEvent(&state.pendingPrompt.event), nil
	}
	promptID, err := r.nextPromptID(state)
	if err != nil {
		return eventwire.Event{}, err
	}

	pending, rewritten, err := buildPendingPrompt(kind, promptID, controllerID, wireEvent)
	if err != nil {
		return eventwire.Event{}, err
	}
	state.pendingPrompt = pending
	return rewritten, nil
}

func controllerPromptID(kind event.Kind, wireEvent eventwire.Event) (string, error) {
	switch kind {
	case event.ApprovalRequest:
		if wireEvent.Approval == nil || strings.TrimSpace(wireEvent.Approval.ID) == "" {
			return "", errors.New("Controller approval ID is empty")
		}
		return wireEvent.Approval.ID, nil
	case event.AskRequest:
		if wireEvent.Ask == nil || strings.TrimSpace(wireEvent.Ask.ID) == "" {
			return "", errors.New("Controller Ask ID is empty")
		}
		return wireEvent.Ask.ID, nil
	default:
		return "", errors.New("event is not a Prompt request")
	}
}

func promptKind(kind event.Kind) protocol.PromptKind {
	if kind == event.AskRequest {
		return protocol.PromptAsk
	}
	return protocol.PromptApproval
}

func (r *SessionRuntime) nextPromptID(state *runtimeActorState) (protocol.PromptID, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := r.opts.NewPromptID()
		if err != nil {
			return "", fmt.Errorf("generate Prompt ID: %w", err)
		}
		if id == "" {
			return "", errors.New("generated Prompt ID is empty")
		}
		if _, issued := state.issuedPromptIDs[id]; issued {
			continue
		}
		state.issuedPromptIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("Prompt ID generator repeatedly returned an issued ID")
}

func buildPendingPrompt(
	kind event.Kind,
	promptID protocol.PromptID,
	controllerID string,
	wireEvent eventwire.Event,
) (*pendingPromptState, eventwire.Event, error) {
	switch kind {
	case event.ApprovalRequest:
		approval := wireEvent.Approval
		if approval == nil {
			return nil, eventwire.Event{}, errors.New("approval event has no approval payload")
		}
		tool := strings.TrimSpace(approval.Tool)
		if tool == "" {
			return nil, eventwire.Event{}, errors.New("approval event has no tool")
		}
		fresh := approval.Fresh || control.RequiresFreshHumanApprovalTool(tool)
		approval.Fresh = fresh
		approval.ID = string(promptID)
		subject := approval.Subject
		if strings.TrimSpace(subject) == "" {
			// The frozen ApprovalPrompt DTO requires a non-empty subject, while
			// existing plan/memory Controller prompts historically used an empty
			// subject. Tool is the stable, non-secret fallback already displayed
			// by current frontends; the full event sidecar retains the original.
			subject = tool
		}
		var reason *string
		if approval.Reason != "" {
			value := approval.Reason
			reason = &value
		}
		public := protocol.PendingPrompt{
			Kind: protocol.PromptApproval,
			Approval: &protocol.ApprovalPrompt{
				PromptID: promptID, Tool: tool, Subject: subject, Reason: reason, Fresh: fresh,
				AllowedDecisions: allowedPromptDecisions(tool, fresh),
			},
		}
		return &pendingPromptState{
			id: promptID, controllerID: controllerID, kind: protocol.PromptApproval,
			public: public, event: *clonePromptEvent(&wireEvent),
		}, wireEvent, nil
	case event.AskRequest:
		ask := wireEvent.Ask
		if ask == nil || len(ask.Questions) == 0 {
			return nil, eventwire.Event{}, errors.New("Ask event has no questions")
		}
		questions := make([]protocol.AskQuestion, 0, len(ask.Questions))
		seenQuestions := make(map[string]struct{}, len(ask.Questions))
		for _, question := range ask.Questions {
			if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Prompt) == "" {
				return nil, eventwire.Event{}, errors.New("Ask event has an empty question ID or prompt")
			}
			if _, duplicate := seenQuestions[question.ID]; duplicate {
				return nil, eventwire.Event{}, fmt.Errorf("Ask event repeats question ID %q", question.ID)
			}
			seenQuestions[question.ID] = struct{}{}
			options := make([]protocol.AskOption, 0, len(question.Options))
			seenOptions := make(map[string]struct{}, len(question.Options))
			for _, option := range question.Options {
				if strings.TrimSpace(option.Label) == "" {
					return nil, eventwire.Event{}, errors.New("Ask event has an empty option label")
				}
				if _, duplicate := seenOptions[option.Label]; duplicate {
					return nil, eventwire.Event{}, fmt.Errorf("Ask event repeats option label %q", option.Label)
				}
				seenOptions[option.Label] = struct{}{}
				var description *string
				if option.Description != "" {
					value := option.Description
					description = &value
				}
				options = append(options, protocol.AskOption{Label: option.Label, Description: description})
			}
			prompt := question.Prompt
			questions = append(questions, protocol.AskQuestion{
				QuestionID: protocol.QuestionID(question.ID), Header: question.Header, Prompt: &prompt,
				Options: options, Multi: question.Multi,
			})
		}
		ask.ID = string(promptID)
		public := protocol.PendingPrompt{
			Kind: protocol.PromptAsk,
			Ask:  &protocol.AskPrompt{PromptID: promptID, Questions: questions},
		}
		return &pendingPromptState{
			id: promptID, controllerID: controllerID, kind: protocol.PromptAsk,
			public: public, event: *clonePromptEvent(&wireEvent),
		}, wireEvent, nil
	default:
		return nil, eventwire.Event{}, errors.New("event is not a Prompt request")
	}
}

func allowedPromptDecisions(tool string, fresh bool) []protocol.PromptDecision {
	if !fresh {
		return []protocol.PromptDecision{
			protocol.DecisionAllowOnce, protocol.DecisionAllowSession,
			protocol.DecisionAllowPersistent, protocol.DecisionDeny,
		}
	}
	if tool == control.SandboxEscapeApprovalTool || tool == control.ManagedConfigWriteApprovalTool {
		return []protocol.PromptDecision{
			protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionDeny,
		}
	}
	return []protocol.PromptDecision{protocol.DecisionAllowOnce, protocol.DecisionDeny}
}

func (r *SessionRuntime) deliver(subscription *subscriptionState, envelope RuntimeEvent) {
	if subscription.overflowed {
		return
	}
	eventCopy := envelope
	if cloned := clonePromptEvent(&envelope.Event); cloned != nil {
		eventCopy.Event = *cloned
	}
	select {
	case subscription.messages <- SubscriptionMessage{Event: &eventCopy}:
		subscription.lastSeq = envelope.Seq
	default:
		subscription.overflowed = true
		drainSubscription(subscription.messages)
		resync := &ResyncRequired{
			SubscriptionID: subscription.id, HostEpoch: r.hostEpoch, Target: r.target,
			RuntimeEpoch: r.epoch, LastSeq: subscription.lastSeq, Reason: protocol.ResyncQueueOverflow,
		}
		subscription.messages <- SubscriptionMessage{Resync: resync}
	}
}

// Submit atomically reserves an opaque Turn ID before invoking Controller. The
// caller's context controls only response waiting; accepted work is rooted in
// the daemon-owned runtime context and survives attach cancellation.
func (r *SessionRuntime) Submit(ctx context.Context, input string) (SubmitResult, error) {
	if strings.TrimSpace(input) == "" {
		return SubmitResult{}, ErrInvalidSubmit
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if state.running {
			return nil, ErrRuntimeBusy
		}
		turnID, err := r.nextTurnID(state)
		if err != nil {
			return nil, err
		}
		accepted, err := r.acceptedTurn(turnID, input, input)
		if err != nil {
			return nil, err
		}
		state.running = true
		state.currentTurn = turnID
		state.acceptedTurn = accepted
		state.cancelRequested = false
		state.lastError = ""
		state.live.Reset()
		state.liveOperationID = ""
		// SubmitUserTurn is the strict normal-prompt primitive: Stage 2 must
		// not interpret /new, /compact, !shell, or another composer union as
		// a Turn that may never produce TurnDone.
		if err := safeControllerCall(func() { r.controller.SubmitUserTurn(input, input) }); err != nil {
			state.running = false
			state.currentTurn = ""
			state.acceptedTurn = nil
			return nil, err
		}
		return SubmitResult{Target: r.target, RuntimeEpoch: r.epoch, TurnID: turnID}, nil
	})
	if err != nil {
		return SubmitResult{}, err
	}
	return value.(SubmitResult), nil
}

// SubmitMutation performs requestId registration, epoch admission, semantic
// state commit, and Controller admission in one Session actor action. A caller
// may lose its transport response without losing the daemon-owned work; an
// exact retry observes the first cached SubmitResult and never calls the
// Controller again. beforeBegin revalidates transport generation inside the
// actor; failure is pre-admission and creates no requestId record.
func (r *SessionRuntime) SubmitMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionSubmitParams,
	businessRejection error,
	beforeBegin func() error,
) (protocol.SessionSubmitResult, error) {
	if registry == nil {
		return protocol.SessionSubmitResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodSessionSubmit),
		Target:    idempotency.SessionTarget(params.Target),
		Params:    params,
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(params.SessionMutation); err != nil {
			return nil, err
		}
		registryAttempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := registryAttempt.Claim()
		if !owns {
			return mutationReplay{attempt: registryAttempt}, nil
		}
		if businessRejection != nil {
			return nil, rejectMutation(claim, businessRejection)
		}
		if state.currentOperation != nil {
			target := r.target
			return nil, rejectMutation(claim, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &target}))
		}
		if state.running {
			target := r.target
			return nil, rejectMutation(claim, protocol.MustRemoteError(protocol.ErrTurnAlreadyRunning, protocol.ErrorOptions{Target: &target}))
		}
		turnID, err := r.nextTurnID(state)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		result := protocol.SessionSubmitResult{
			Kind: protocol.SubmitTurn, TurnID: turnID,
			Target: r.target, RuntimeEpoch: r.epoch,
		}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}

		accepted, err := r.acceptedTurn(turnID, params.Input, params.DisplayText)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		state.running = true
		state.currentTurn = turnID
		state.acceptedTurn = accepted
		state.cancelRequested = false
		state.lastError = ""
		state.live.Reset()
		state.liveOperationID = ""
		if err := safeControllerCall(func() { r.controller.SubmitUserTurn(params.Input, params.DisplayText) }); err != nil {
			state.running = false
			state.currentTurn = ""
			state.acceptedTurn = nil
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			// Controller admission and snapshot semantics have already committed.
			// Returning an internal error is safer than fabricating a second
			// operation; a daemon epoch reset is the only expected close race.
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.SessionSubmitResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.SessionSubmitResult{}, err
		}
		var result protocol.SessionSubmitResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.SessionSubmitResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.SessionSubmitResult)
	if !ok {
		return protocol.SessionSubmitResult{}, errors.New("remote submit actor returned an invalid result")
	}
	return result, nil
}

func (r *SessionRuntime) acceptedTurn(turnID protocol.TurnID, input, display string) (accepted *snapshotcapture.AcceptedTurn, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			accepted = nil
			err = fmt.Errorf("capture accepted Turn history: controller call panicked: %v", recovered)
		}
	}()
	historyPrefix := cloneProviderMessages(r.controller.History())
	userMessages := 0
	for _, message := range historyPrefix {
		if message.Role == provider.RoleUser {
			userMessages++
		}
	}
	return &snapshotcapture.AcceptedTurn{
		TurnID: turnID, Input: input, DisplayText: display,
		HistoryMessageCount: len(historyPrefix), UserMessagesBeforeAdmission: userMessages,
		HistoryPrefix: historyPrefix,
	}, nil
}

func cloneProviderMessages(messages []provider.Message) []provider.Message {
	cloned := make([]provider.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].Images = append([]string(nil), message.Images...)
		cloned[index].ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		cloned[index].MemoryCitations = append([]provider.MemoryCitation(nil), message.MemoryCitations...)
	}
	return cloned
}

func (r *SessionRuntime) nextTurnID(state *runtimeActorState) (protocol.TurnID, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := r.opts.NewTurnID()
		if err != nil {
			return "", fmt.Errorf("generate turn ID: %w", err)
		}
		if id == "" {
			return "", errors.New("generated turn ID is empty")
		}
		if _, issued := state.issuedTurnIDs[id]; issued {
			continue
		}
		// IDs remain reserved for the lifetime of this runtime even if the
		// Controller call later fails. No later request can target old work.
		state.issuedTurnIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("turn ID generator repeatedly returned an issued ID")
}

// CancelTurn only delegates to Controller.TryCancel when the opaque ID exactly
// matches the currently active Turn in this runtime epoch.
func (r *SessionRuntime) CancelTurn(ctx context.Context, expected protocol.TurnID) (CancelResult, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if state.currentTurn == "" {
			return nil, ErrTurnNotActive
		}
		if expected == "" || expected != state.currentTurn {
			return nil, &TurnMismatchError{Requested: expected, Current: state.currentTurn}
		}
		var attempt control.CancelAttempt
		if err := safeControllerCall(func() { attempt = r.controller.TryCancel() }); err != nil {
			return nil, err
		}
		switch attempt {
		case control.CancelRequestedNow:
			state.cancelRequested = true
			clearPendingPrompt(state)
			return CancelResult{Status: protocol.CancelRequested, TurnID: state.currentTurn}, nil
		case control.CancelAlreadyRequested:
			state.cancelRequested = true
			clearPendingPrompt(state)
			return CancelResult{Status: protocol.CancelAlreadyRequested, TurnID: state.currentTurn}, nil
		default:
			// Controller completion can win the tiny interval before its
			// TurnDone action reaches this actor. The queued event remains the
			// authority that clears the host state; never fall back to Cancel().
			return nil, ErrTurnNotActive
		}
	})
	if err != nil {
		return CancelResult{}, err
	}
	return value.(CancelResult), nil
}

// CancelTurnMutation gives strict Turn cancellation the same actor-atomic
// requestId contract as SubmitMutation. In particular, a response-loss retry
// returns the first cancel status even after TurnDone has cleared live state.
// beforeBegin is the transport-generation barrier immediately before Begin.
func (r *SessionRuntime) CancelTurnMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.TurnCancelParams,
	beforeBegin func() error,
) (protocol.TurnCancelResult, error) {
	if registry == nil {
		return protocol.TurnCancelResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodTurnCancel),
		Target:    idempotency.SessionTarget(params.Target),
		Params:    params,
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(params.SessionMutation); err != nil {
			return nil, err
		}
		registryAttempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := registryAttempt.Claim()
		if !owns {
			return mutationReplay{attempt: registryAttempt}, nil
		}
		if state.currentTurn == "" {
			target := r.target
			return nil, rejectMutation(claim, protocol.MustRemoteError(protocol.ErrTurnNotActive, protocol.ErrorOptions{Target: &target}))
		}
		if params.ExpectedTurnID == "" || params.ExpectedTurnID != state.currentTurn {
			target := r.target
			return nil, rejectMutation(claim, protocol.MustRemoteError(protocol.ErrTurnMismatch, protocol.ErrorOptions{
				Target: &target, Expected: string(state.currentTurn), Actual: string(params.ExpectedTurnID),
			}))
		}

		var attempt control.CancelAttempt
		if err := safeControllerCall(func() { attempt = r.controller.TryCancel() }); err != nil {
			return nil, abortMutation(claim, err)
		}
		var status protocol.CancelStatus
		switch attempt {
		case control.CancelRequestedNow:
			status = protocol.CancelRequested
		case control.CancelAlreadyRequested:
			status = protocol.CancelAlreadyRequested
		default:
			target := r.target
			return nil, rejectMutation(claim, protocol.MustRemoteError(protocol.ErrTurnNotActive, protocol.ErrorOptions{Target: &target}))
		}
		result := protocol.TurnCancelResult{Status: status, TurnID: state.currentTurn}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		state.cancelRequested = true
		clearPendingPrompt(state)
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.TurnCancelResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.TurnCancelResult{}, err
		}
		var result protocol.TurnCancelResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.TurnCancelResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.TurnCancelResult)
	if !ok {
		return protocol.TurnCancelResult{}, errors.New("remote cancel actor returned an invalid result")
	}
	return result, nil
}

func (r *SessionRuntime) validateMutationEpochs(hostEpoch protocol.HostEpoch, runtimeEpoch protocol.RuntimeEpoch) error {
	if hostEpoch != r.hostEpoch {
		return protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{
			Expected: string(r.hostEpoch), Actual: string(hostEpoch),
		})
	}
	if runtimeEpoch != r.epoch {
		target := r.target
		return protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{
			Target: &target, Expected: string(r.epoch), Actual: string(runtimeEpoch),
		})
	}
	return nil
}

// preadmitSessionMutation is the actor-local ownership barrier used by strict
// Turn and Prompt mutations before an idempotency claim is created. Runtime
// replacement seals mailbox admission under the same queue lock as call, then
// clears current at its actor commit boundary. No post-seal call can reach this
// point, and this check rejects any non-replacement stale pointer without ever
// consulting RuntimeManager or inverting manager-lock/actor order.
func (r *SessionRuntime) preadmitSessionMutation(mutation protocol.SessionMutation) error {
	if mutation.Target != r.target {
		return ErrInvalidRuntimeTarget
	}
	if mutation.ExpectedHostEpoch != r.hostEpoch {
		return protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{
			Expected: string(r.hostEpoch), Actual: string(mutation.ExpectedHostEpoch),
		})
	}
	if !r.current.Load() {
		target := r.target
		return protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{Target: &target})
	}
	if mutation.ExpectedRuntimeEpoch != r.epoch {
		target := r.target
		return protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{
			Target: &target, Expected: string(r.epoch), Actual: string(mutation.ExpectedRuntimeEpoch),
		})
	}
	return nil
}

func abortMutation(claim *idempotency.Claim, cause error) error {
	if err := claim.Abort(cause); err != nil {
		return fmt.Errorf("abort Remote mutation after %v: %w", cause, err)
	}
	return cause
}

func rejectMutation(claim *idempotency.Claim, rejection error) error {
	if err := claim.Reject(rejection); err != nil {
		_ = claim.Abort(err)
		return err
	}
	return rejection
}

// beginSubscription installs its bounded buffer and freezes the snapshot
// boundary in one actor action. A same-runtime replacement marks the old
// subscription migrating but deliberately retains it until adapter Commit;
// Abort can therefore restore the old ID after projection/owner failure.
func (r *SessionRuntime) beginSubscription(
	ctx context.Context,
	attachment AttachmentKey,
	replace protocol.SubscriptionID,
) (subscriptionAdmission, error) {
	value, err := randomOpaqueID("snapshot")
	if err != nil {
		return subscriptionAdmission{}, fmt.Errorf("generate internal snapshot ID: %w", err)
	}
	return r.beginBoundSubscription(ctx, attachment, replace, protocol.SnapshotID(value))
}

func (r *SessionRuntime) beginBoundSubscription(
	ctx context.Context,
	attachment AttachmentKey,
	replace protocol.SubscriptionID,
	snapshotID protocol.SnapshotID,
) (subscriptionAdmission, error) {
	if err := attachment.validate(); err != nil {
		return subscriptionAdmission{}, err
	}
	if strings.TrimSpace(string(snapshotID)) == "" {
		return subscriptionAdmission{}, errors.New("snapshotId is empty")
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		var previous *subscriptionState
		if replace != "" {
			previous = state.subscriptions[replace]
			if previous == nil || previous.attachment != attachment || previous.migrating {
				return nil, ErrSubscriptionNotFound
			}
			previous.migrating = true
		}
		id, err := r.nextSubscriptionID(state)
		if err != nil {
			if previous != nil {
				previous.migrating = false
			}
			return nil, err
		}
		snapshot, err := r.boundSnapshot(state, snapshotID)
		if err != nil {
			if previous != nil {
				previous.migrating = false
			}
			return nil, err
		}
		messages := make(chan SubscriptionMessage, r.opts.SubscriptionQueue)
		current := &subscriptionState{id: id, attachment: attachment, messages: messages, lastSeq: state.seq}
		state.subscriptions[id] = current
		return subscriptionAdmission{
			subscription: Subscription{ID: id, Snapshot: snapshot, Messages: messages},
			previousID:   replace,
		}, nil
	})
	if err != nil {
		return subscriptionAdmission{}, err
	}
	return value.(subscriptionAdmission), nil
}

// Subscribe is the direct Host convenience API. Protocol adapters use
// RuntimeManager.Subscribe so Commit/Abort spans snapshot projection and owner
// installation. Direct callers have no external owner phase and commit now.
func (r *SessionRuntime) Subscribe(
	ctx context.Context,
	attachment AttachmentKey,
	replace protocol.SubscriptionID,
) (Subscription, error) {
	admission, err := r.beginSubscription(ctx, attachment, replace)
	if err != nil {
		return Subscription{}, err
	}
	if replace != "" {
		if err := r.commitSubscriptionReplacement(ctx, attachment, replace, admission.subscription.ID); err != nil {
			_ = r.abortSubscriptionReplacement(context.Background(), attachment, replace, admission.subscription.ID)
			return Subscription{}, err
		}
	}
	return admission.subscription, nil
}

func (r *SessionRuntime) commitSubscriptionReplacement(
	ctx context.Context,
	attachment AttachmentKey,
	previousID protocol.SubscriptionID,
	currentID protocol.SubscriptionID,
) error {
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		previous := state.subscriptions[previousID]
		current := state.subscriptions[currentID]
		if previous == nil || current == nil || previous.attachment != attachment || current.attachment != attachment || !previous.migrating {
			return nil, ErrSubscriptionNotFound
		}
		delete(state.subscriptions, previousID)
		closeSubscription(previous)
		return struct{}{}, nil
	})
	return err
}

func (r *SessionRuntime) abortSubscriptionReplacement(
	ctx context.Context,
	attachment AttachmentKey,
	previousID protocol.SubscriptionID,
	currentID protocol.SubscriptionID,
) error {
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if current := state.subscriptions[currentID]; current != nil && current.attachment == attachment {
			delete(state.subscriptions, currentID)
			closeSubscription(current)
		}
		if previous := state.subscriptions[previousID]; previous != nil && previous.attachment == attachment {
			previous.migrating = false
		}
		return struct{}{}, nil
	})
	return err
}

func (r *SessionRuntime) nextSubscriptionID(state *runtimeActorState) (protocol.SubscriptionID, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := r.opts.NewSubscriptionID()
		if err != nil {
			return "", fmt.Errorf("generate subscription ID: %w", err)
		}
		if id == "" {
			return "", errors.New("generated subscription ID is empty")
		}
		if _, issued := state.issuedSubscriptionIDs[id]; !issued {
			state.issuedSubscriptionIDs[id] = struct{}{}
			return id, nil
		}
	}
	return "", errors.New("subscription ID generator repeatedly returned an issued ID")
}

// Unsubscribe is idempotent for the current attachment. A different transport
// cannot remove an existing subscription with the same opaque ID.
func (r *SessionRuntime) Unsubscribe(ctx context.Context, attachment AttachmentKey, id protocol.SubscriptionID) error {
	if err := attachment.validate(); err != nil {
		return err
	}
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if subscription := state.subscriptions[id]; subscription != nil && subscription.attachment == attachment {
			delete(state.subscriptions, id)
			closeSubscription(subscription)
		}
		return struct{}{}, nil
	})
	return err
}

func (r *SessionRuntime) detachAttachment(ctx context.Context, attachment AttachmentKey) error {
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		for id, subscription := range state.subscriptions {
			if subscription.attachment == attachment {
				delete(state.subscriptions, id)
				closeSubscription(subscription)
			}
		}
		return struct{}{}, nil
	})
	return err
}

func (r *SessionRuntime) detachOtherAttachments(ctx context.Context, current AttachmentKey) error {
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		for id, subscription := range state.subscriptions {
			if subscription.attachment != current {
				delete(state.subscriptions, id)
				closeSubscription(subscription)
			}
		}
		return struct{}{}, nil
	})
	return err
}

// Snapshot is a sequencer barrier used by tests and internal adapters. The
// protocol-facing attach path normally obtains the same boundary via Subscribe.
func (r *SessionRuntime) Snapshot(ctx context.Context) (RuntimeSnapshot, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		return r.snapshot(state), nil
	})
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return value.(RuntimeSnapshot), nil
}

func (r *SessionRuntime) activity(ctx context.Context) (runtimeActivity, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		return r.activityForState(state), nil
	})
	if err != nil {
		return runtimeActivity{}, err
	}
	return value.(runtimeActivity), nil
}

func (r *SessionRuntime) activityForState(state *runtimeActorState) runtimeActivity {
	r.syncActiveJobsForState(state)
	return runtimeActivity{
		summary: protocol.SessionRuntimeSummary{
			RuntimeEpoch:  r.epoch,
			Running:       state.running,
			PendingPrompt: state.pendingPrompt != nil,
			ActiveJobs:    state.activeJobs,
		},
		subscriptions: len(state.subscriptions),
	}
}

// setActiveJobsState is the actor-ordered integration point for the future
// exact job registry. A negative count is rejected instead of being coerced.
func (r *SessionRuntime) setActiveJobsState(ctx context.Context, active int) error {
	if active < 0 {
		return errors.New("remote active job count cannot be negative")
	}
	_, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		state.activeJobs = active
		state.activeJobsManaged = true
		return struct{}{}, nil
	})
	return err
}

func (r *SessionRuntime) beginIdleRelease() (*idleReleaseBarrier, runtimeActivity, error) {
	if !r.accepting.Load() {
		return nil, runtimeActivity{}, ErrRuntimeClosed
	}
	barrier := &idleReleaseBarrier{
		ready:    make(chan runtimeActivity, 1),
		decision: make(chan bool, 1),
		done:     make(chan struct{}),
	}
	if !r.mailbox.enqueueAdmission(func(state *runtimeActorState) {
		activity := r.activityForState(state)
		barrier.ready <- activity
		commit := <-barrier.decision
		if commit && activity.idle() {
			// Flip admission inside the same actor action that observed idle.
			// A caller that raced past the atomic precheck may already be queued,
			// but it cannot execute before this stop transition and will receive
			// ErrRuntimeClosed when the actor closes its mailbox.
			r.accepting.Store(false)
			resetRuntimeLiveState(state)
			state.stopping = true
		}
		close(barrier.done)
	}) {
		return nil, runtimeActivity{}, ErrRuntimeClosed
	}
	select {
	case activity := <-barrier.ready:
		return barrier, activity, nil
	case <-r.done:
		return nil, runtimeActivity{}, ErrRuntimeClosed
	}
}

// beginSessionCloseMutation admits session/close inside the Session actor. The
// returned barrier is live only for a newly-owned requestId; an exact replay is
// decoded without re-running idle release semantics.
func (r *SessionRuntime) beginSessionCloseMutation(
	registry *idempotency.Registry,
	params protocol.SessionCloseParams,
	beforeBegin func() error,
) (*sessionCloseBarrier, sessionCloseAdmission, error) {
	if registry == nil {
		return nil, sessionCloseAdmission{}, errors.New("remote idempotency registry is required")
	}
	if !r.accepting.Load() {
		return nil, sessionCloseAdmission{}, ErrRuntimeClosed
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodSessionClose),
		Target:    idempotency.SessionTarget(params.Target),
		Params:    params,
	}
	barrier := &sessionCloseBarrier{
		ready:    make(chan sessionCloseAdmission, 1),
		decision: make(chan sessionCloseDecision, 1),
		done:     make(chan error, 1),
	}
	if !r.mailbox.enqueueAdmission(func(state *runtimeActorState) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				barrier.ready <- sessionCloseAdmission{err: err}
				barrier.done <- err
				return
			}
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			barrier.ready <- sessionCloseAdmission{err: err}
			barrier.done <- err
			return
		}
		claim, owns := attempt.Claim()
		if !owns {
			replay := mutationReplay{attempt: attempt}
			barrier.ready <- sessionCloseAdmission{replay: &replay}
			barrier.done <- nil
			return
		}
		if params.Target != r.target {
			err = abortMutation(claim, ErrInvalidRuntimeTarget)
			barrier.ready <- sessionCloseAdmission{err: err}
			barrier.done <- err
			return
		}
		if err = r.validateMutationEpochs(params.ExpectedHostEpoch, params.ExpectedRuntimeEpoch); err != nil {
			err = abortMutation(claim, err)
			barrier.ready <- sessionCloseAdmission{err: err}
			barrier.done <- err
			return
		}

		activity := r.activityForState(state)
		disposition := protocol.SessionRetainedActive
		if activity.idle() {
			if snapshotErr := safeControllerErrorCall(r.controller.Snapshot); snapshotErr != nil {
				target := r.target
				failure := protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &target})
				err = abortMutation(claim, failure)
				barrier.ready <- sessionCloseAdmission{err: err}
				barrier.done <- err
				return
			}
			disposition = protocol.SessionReleased
		}
		result := protocol.SessionCloseResult{Disposition: disposition}
		barrier.ready <- sessionCloseAdmission{result: result, activity: activity}
		decision := <-barrier.decision
		if !decision.accept {
			cause := decision.cause
			if cause == nil {
				cause = errors.New("remote Session close admission aborted")
			}
			err = abortMutation(claim, cause)
			barrier.done <- err
			return
		}
		outcome, prepareErr := idempotency.PrepareSuccess(result)
		if prepareErr != nil {
			err = abortMutation(claim, prepareErr)
			barrier.done <- err
			return
		}
		if resolveErr := claim.Resolve(outcome); resolveErr != nil {
			_ = claim.Abort(resolveErr)
			barrier.done <- resolveErr
			return
		}
		if disposition == protocol.SessionReleased {
			r.accepting.Store(false)
			resetRuntimeLiveState(state)
			state.stopping = true
		}
		barrier.done <- nil
	}) {
		return nil, sessionCloseAdmission{}, ErrRuntimeClosed
	}
	select {
	case admission := <-barrier.ready:
		if admission.err != nil || admission.replay != nil {
			<-barrier.done
			return nil, admission, admission.err
		}
		return barrier, admission, nil
	case <-r.done:
		return nil, sessionCloseAdmission{}, ErrRuntimeClosed
	}
}

func (b *sessionCloseBarrier) finish(accept bool, cause error) error {
	if b == nil {
		return nil
	}
	b.decision <- sessionCloseDecision{accept: accept, cause: cause}
	return <-b.done
}

func (b *idleReleaseBarrier) finish(commit bool) {
	if b == nil {
		return
	}
	b.decision <- commit
	<-b.done
}

func (r *SessionRuntime) snapshot(state *runtimeActorState) RuntimeSnapshot {
	r.syncActiveJobsForState(state)
	var goal *string
	var goalStatus protocol.GoalStatus
	_ = safeControllerCall(func() {
		if value := strings.TrimSpace(r.controller.Goal()); value != "" {
			copyValue := value
			goal = &copyValue
			goalStatus = mapGoalStatus(r.controller.GoalStatus())
		}
	})
	liveEvents := authoritativeLiveEvents(state)
	events := make([]RuntimeEvent, len(liveEvents))
	liveOperationID := state.liveOperationID
	if state.currentOperation != nil {
		liveOperationID = state.currentOperation.id
	}
	for index, liveEvent := range liveEvents {
		events[index] = RuntimeEvent{
			HostEpoch: r.hostEpoch, Target: r.target, RuntimeEpoch: r.epoch,
			TurnID: state.currentTurn, OperationID: liveOperationID, Event: liveEvent,
		}
	}
	snapshot := RuntimeSnapshot{
		HostEpoch: r.hostEpoch, Target: r.target, RuntimeEpoch: r.epoch, BoundarySeq: state.seq,
		Running: state.running, ActiveJobs: state.activeJobs,
		CurrentTurn: state.currentTurn, CancelRequested: state.cancelRequested,
		LastOutcome: state.lastOutcome, LastError: state.lastError,
		PreviousTurnInterrupted: state.previousTurnInterrupted, InterruptionReason: state.interruptionReason,
		Goal: goal, GoalStatus: goalStatus,
		Events: events,
	}
	if state.currentOperation != nil {
		snapshot.CurrentOperation = &protocol.OperationState{
			OperationID:     state.currentOperation.id,
			Kind:            state.currentOperation.kind,
			CancelRequested: state.currentOperation.cancelRequested,
		}
	}
	if state.pendingPrompt != nil {
		snapshot.PendingPrompt = clonePendingPrompt(&state.pendingPrompt.public)
		snapshot.pendingPromptEvent = clonePromptEvent(&state.pendingPrompt.event)
	}
	return snapshot
}

func (r *SessionRuntime) boundSnapshot(state *runtimeActorState, snapshotID protocol.SnapshotID) (snapshot RuntimeSnapshot, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			snapshot = RuntimeSnapshot{}
			err = fmt.Errorf("capture Remote Session snapshot: controller getter panicked: %v", recovered)
		}
	}()
	if strings.TrimSpace(string(snapshotID)) == "" {
		return RuntimeSnapshot{}, errors.New("snapshotId is empty")
	}
	checkpointSnapshot := r.controller.CheckpointSnapshot()
	checkpointIDs, err := r.reconcileCheckpointIDs(state, checkpointSnapshot.Metas)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	usedTokens, windowTokens := r.controller.ContextSnapshot()
	getters := snapshotcapture.GetterSnapshot{
		History:                        r.controller.History(),
		Todos:                          r.controller.Todos(),
		UsedTokens:                     usedTokens,
		WindowTokens:                   windowTokens,
		LastUsage:                      r.controller.LastUsage(),
		Jobs:                           r.controller.Jobs(),
		Checkpoints:                    checkpointSnapshot.Metas,
		CheckpointTurnsByMessageIndex:  checkpointSnapshot.TurnsByMessageIndex,
		CheckpointConversationBoundary: checkpointSnapshot.ConversationAvailable,
	}
	if !state.activeJobsManaged {
		state.activeJobs = len(getters.Jobs)
	}
	sessionPath := r.controller.SessionPath()
	sessionDir := r.controller.SessionDir()
	if sessionDir == "" && sessionPath != "" {
		sessionDir = filepath.Dir(sessionPath)
	}
	displays := sessiondisplay.Map{}
	if sessionDir != "" {
		displays = sessiondisplay.Load(sessionDir)
	}
	binding := history.Binding{
		SnapshotID: snapshotID, HostEpoch: r.hostEpoch, Target: r.target, RuntimeEpoch: r.epoch,
	}
	projection, err := snapshotcapture.Project(snapshotcapture.Input{
		Binding: binding, SessionPath: sessionPath, Displays: displays, Getters: getters,
		Telemetry:     sessiontelemetry.View(state.readFiles, state.usage, r.telemetryNowMillis()),
		CheckpointIDs: checkpointIDs, AcceptedTurn: state.acceptedTurn,
	})
	if err != nil {
		return RuntimeSnapshot{}, fmt.Errorf("capture Remote Session snapshot: %w", err)
	}
	// Checkpoint identity reconciliation is committed only with the complete
	// projection. IDs minted by a failed attempt remain globally reserved, but
	// no partial deletion/addition can alter the actor's visible mapping.
	state.checkpointIDs = checkpointIDs
	snapshot = r.snapshot(state)
	snapshot.SnapshotID = snapshotID
	snapshot.Capture = projection
	return snapshot, nil
}

func (r *SessionRuntime) reconcileCheckpointIDs(state *runtimeActorState, metas []checkpoint.Meta) (map[int]protocol.CheckpointID, error) {
	current := make(map[int]struct{}, len(metas))
	for _, meta := range metas {
		if meta.Turn < 0 {
			return nil, fmt.Errorf("checkpoint turn %d is negative", meta.Turn)
		}
		if _, duplicate := current[meta.Turn]; duplicate {
			return nil, fmt.Errorf("checkpoint turn %d is duplicated", meta.Turn)
		}
		current[meta.Turn] = struct{}{}
	}
	next := make(map[int]protocol.CheckpointID, len(current))
	for _, meta := range metas {
		if existing := state.checkpointIDs[meta.Turn]; existing != "" {
			next[meta.Turn] = existing
			continue
		}
		id, err := r.nextCheckpointID(state)
		if err != nil {
			return nil, err
		}
		next[meta.Turn] = id
	}
	result := make(map[int]protocol.CheckpointID, len(next))
	for turn, id := range next {
		result[turn] = id
	}
	return result, nil
}

func (r *SessionRuntime) nextCheckpointID(state *runtimeActorState) (protocol.CheckpointID, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := r.opts.NewCheckpointID()
		if err != nil {
			return "", fmt.Errorf("generate Checkpoint ID: %w", err)
		}
		if strings.TrimSpace(string(id)) == "" {
			return "", errors.New("generated Checkpoint ID is empty")
		}
		if _, issued := state.issuedCheckpointIDs[id]; issued {
			continue
		}
		state.issuedCheckpointIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("Checkpoint ID generator repeatedly returned an issued ID")
}

func authoritativeLiveEvents(state *runtimeActorState) []eventwire.Event {
	reduced := state.live.Snapshot()
	result := make([]eventwire.Event, 0, len(reduced)+1)
	promptAdded := false
	for _, liveEvent := range reduced {
		if liveEvent.Kind != "approval_request" && liveEvent.Kind != "ask_request" {
			result = append(result, liveEvent)
			continue
		}
		if state.pendingPrompt == nil || promptAdded {
			continue
		}
		if prompt := clonePromptEvent(&state.pendingPrompt.event); prompt != nil {
			result = append(result, *prompt)
			promptAdded = true
		}
	}
	if state.pendingPrompt != nil && !promptAdded {
		// This is only an invariant-recovery path: prompt registration and reducer
		// Apply normally happen in the same actor action. Prefer one authoritative
		// prompt over silently omitting the only UI capable of resolving the Turn.
		if prompt := clonePromptEvent(&state.pendingPrompt.event); prompt != nil {
			result = append(result, *prompt)
		}
	}
	return result
}

func clonePendingPrompt(in *protocol.PendingPrompt) *protocol.PendingPrompt {
	if in == nil {
		return nil
	}
	out := *in
	if in.Approval != nil {
		approval := *in.Approval
		approval.Reason = cloneStringPointer(in.Approval.Reason)
		approval.AllowedDecisions = append([]protocol.PromptDecision(nil), in.Approval.AllowedDecisions...)
		out.Approval = &approval
	}
	if in.Ask != nil {
		ask := *in.Ask
		ask.Questions = make([]protocol.AskQuestion, len(in.Ask.Questions))
		for index, question := range in.Ask.Questions {
			copyQuestion := question
			copyQuestion.Prompt = cloneStringPointer(question.Prompt)
			copyQuestion.Options = make([]protocol.AskOption, len(question.Options))
			for optionIndex, option := range question.Options {
				copyOption := option
				copyOption.Description = cloneStringPointer(option.Description)
				copyQuestion.Options[optionIndex] = copyOption
			}
			ask.Questions[index] = copyQuestion
		}
		out.Ask = &ask
	}
	return &out
}

func cloneStringPointer(in *string) *string {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func clonePromptEvent(in *eventwire.Event) *eventwire.Event {
	if in == nil {
		return nil
	}
	out := *in
	if in.Approval != nil {
		approval := *in.Approval
		if in.Approval.MCPTrust != nil {
			trust := *in.Approval.MCPTrust
			trust.ChangedTools = append([]string(nil), in.Approval.MCPTrust.ChangedTools...)
			trust.ToolChanges = append([]eventwire.MCPToolChange(nil), in.Approval.MCPTrust.ToolChanges...)
			trust.Readers = append([]string(nil), in.Approval.MCPTrust.Readers...)
			trust.Writers = append([]string(nil), in.Approval.MCPTrust.Writers...)
			trust.Destructive = append([]string(nil), in.Approval.MCPTrust.Destructive...)
			approval.MCPTrust = &trust
		}
		out.Approval = &approval
	}
	if in.Ask != nil {
		ask := *in.Ask
		ask.Questions = make([]eventwire.AskQuestion, len(in.Ask.Questions))
		for index, question := range in.Ask.Questions {
			copyQuestion := question
			copyQuestion.Options = append([]eventwire.AskOption(nil), question.Options...)
			ask.Questions[index] = copyQuestion
		}
		out.Ask = &ask
	}
	return &out
}

type actorResponse struct {
	value any
	err   error
}

func (r *SessionRuntime) call(ctx context.Context, action func(*runtimeActorState) (any, error)) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.accepting.Load() {
		return nil, ErrRuntimeClosed
	}
	response := make(chan actorResponse, 1)
	if !r.mailbox.enqueueAdmission(func(state *runtimeActorState) {
		value, err := action(state)
		response <- actorResponse{value: value, err: err}
	}) {
		return nil, ErrRuntimeClosed
	}
	select {
	case result := <-response:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, ErrRuntimeClosed
	}
}

func closeSubscription(subscription *subscriptionState) {
	drainSubscription(subscription.messages)
	close(subscription.messages)
}

func drainSubscription(messages chan SubscriptionMessage) {
	for {
		select {
		case <-messages:
		default:
			return
		}
	}
}

func safeControllerCall(call func()) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("controller call panicked: %v", recovered)
		}
	}()
	call()
	return nil
}

func safeControllerErrorCall(call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("controller call panic: %v", recovered)
		}
	}()
	return call()
}
