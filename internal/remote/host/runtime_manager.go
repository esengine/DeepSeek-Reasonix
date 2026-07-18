package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/sessiontelemetry"
)

var (
	ErrRuntimeManagerClosed  = errors.New("remote runtime manager is closed")
	ErrRuntimeClosed         = errors.New("remote session runtime is closed")
	ErrRuntimeBusy           = errors.New("remote session runtime already has active work")
	ErrWorkspaceInUse        = errors.New("remote workspace has active session work")
	ErrTurnNotActive         = errors.New("remote session runtime has no active turn")
	ErrTurnMismatch          = errors.New("remote turn does not match the active turn")
	ErrSubscriptionNotFound  = errors.New("remote subscription does not belong to this attachment")
	ErrInvalidRuntimeTarget  = errors.New("invalid remote runtime target")
	ErrInvalidAttachment     = errors.New("invalid remote attachment")
	ErrInvalidSubmit         = errors.New("remote submit input is empty")
	ErrControllerUnavailable = errors.New("remote controller factory returned no controller")
)

// ControllerFactory constructs the daemon-owned Controller for one Session
// runtime. The supplied context is derived from the daemon root, never from an
// SSH attach request. The sink is permanently bound to the new runtime epoch.
type ControllerFactory interface {
	CreateController(context.Context, protocol.RuntimeTarget, event.Sink) (control.SessionAPI, error)
}

// RecoveryControllerFactory is the optional production extension implemented
// by runtimefactory.Factory. RuntimeManager must consume the resume result at
// construction time because Controller.Resume clears the durable marker that
// proved the previous process died with an accepted Turn.
type RecoveryControllerFactory interface {
	CreateControllerWithRecovery(context.Context, protocol.RuntimeTarget, event.Sink) (control.SessionAPI, control.SessionResumeState, error)
}

// ControllerFactoryFunc adapts a function to ControllerFactory.
type ControllerFactoryFunc func(context.Context, protocol.RuntimeTarget, event.Sink) (control.SessionAPI, error)

func (f ControllerFactoryFunc) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	return f(ctx, target, sink)
}

// AttachmentKey is the daemon-private identity of one attach transport. It is
// deliberately the same lease+generation pair enforced by LeaseManager; no
// runtime lifecycle is owned by this key.
type AttachmentKey struct {
	LeaseID    protocol.LeaseID
	Generation uint64
}

// AttachmentForLease is the explicit integration point between the lease state
// machine and runtime subscriptions.
func AttachmentForLease(binding LeaseBinding) AttachmentKey {
	return AttachmentKey{LeaseID: binding.LeaseID, Generation: binding.Generation}
}

func (a AttachmentKey) validate() error {
	if a.LeaseID == "" || a.Generation == 0 {
		return ErrInvalidAttachment
	}
	return nil
}

// RuntimeManagerOptions makes opaque identities and resource limits injectable
// for deterministic tests. Custom generators may be called concurrently by
// different Session actors and must therefore be concurrency-safe.
type RuntimeManagerOptions struct {
	NewRuntimeEpoch   func() (protocol.RuntimeEpoch, error)
	NewTurnID         func() (protocol.TurnID, error)
	NewOperationID    func() (protocol.OperationID, error)
	NewPromptID       func() (protocol.PromptID, error)
	NewCheckpointID   func() (protocol.CheckpointID, error)
	NewSubscriptionID func() (protocol.SubscriptionID, error)
	NowMillis         func() int64
	OnTelemetryError  func(protocol.RuntimeTarget, error)
	SubscriptionQueue int
	// EventLogLimit is deprecated and ignored. Runtime snapshots retain a
	// semantic live projection rather than truncating a raw event log.
	EventLogLimit int
}

// RuntimeManager is the daemon-owned registry of live SessionRuntime actors.
// Attach disconnect and lease expiration only remove subscriptions; Close or an
// explicit runtime replacement owns Controller teardown.
type RuntimeManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	hostEpoch protocol.HostEpoch
	factory   ControllerFactory
	opts      RuntimeManagerOptions

	mu                    sync.RWMutex
	closed                bool
	runtimes              map[protocol.RuntimeTarget]*SessionRuntime
	retiredSubscriptions  map[protocol.SubscriptionID]*retiredSubscription
	issuedRuntimeEpochs   map[protocol.RuntimeEpoch]struct{}
	operationIDMu         sync.Mutex
	newOperationID        func() (protocol.OperationID, error)
	issuedOperationIDs    map[protocol.OperationID]struct{}
	subscriptionIDMu      sync.Mutex
	newSubscriptionID     func() (protocol.SubscriptionID, error)
	issuedSubscriptionIDs map[protocol.SubscriptionID]struct{}
	closeOnce             sync.Once
}

// SubscriptionInstall is a manager-ordered subscription transaction. For a
// runtime/target migration, the new actor subscription already exists when it
// is returned, while the old terminal channel remains retained until Commit.
// Daemon transports commit only after atomically installing their new active
// subscription and binding its snapshot owners. Abort removes the new actor
// subscription and leaves the old terminal subscription available for retry.
type SubscriptionInstall struct {
	Runtime      *SessionRuntime
	Subscription Subscription

	manager     *RuntimeManager
	attachment  AttachmentKey
	replacement *retiredSubscription
	previousID  protocol.SubscriptionID
	finalize    sync.Once
	finalizeErr error
}

// WorkspaceCloseReservation holds the RuntimeManager registry lock and an
// actor barrier for every current runtime in one workspace. While it is live,
// GetOrCreate cannot admit a cold runtime and already-obtained runtime pointers
// cannot pass their actor barrier. Callers must finish it with Commit, Abort,
// or Close exactly once.
type WorkspaceCloseReservation struct {
	manager   *RuntimeManager
	workspace protocol.WorkspaceID
	prepared  []preparedIdleRuntime
	finished  bool
}

// TargetRemovalReservation prevents a runtime from being recreated while a
// cold catalog mutation moves or deletes its durable artifacts. Existing
// incarnations are terminated before the reservation is returned; Abort leaves
// the durable catalog unchanged and permits a later cold rebuild.
type TargetRemovalReservation struct {
	manager  *RuntimeManager
	finished bool
}

// RuntimeAbsenceReservation keeps a validated absent target absent while a
// cold session/close result is admitted in the daemon's target sequencer.
type RuntimeAbsenceReservation struct {
	manager  *RuntimeManager
	finished bool
}

func NewRuntimeManager(root context.Context, hostEpoch protocol.HostEpoch, factory ControllerFactory, opts RuntimeManagerOptions) (*RuntimeManager, error) {
	if root == nil {
		root = context.Background()
	}
	if hostEpoch == "" {
		return nil, fmt.Errorf("%w: host epoch is empty", ErrInvalidRuntimeTarget)
	}
	if nilutil.IsNil(factory) {
		return nil, ErrControllerUnavailable
	}
	if opts.NewRuntimeEpoch == nil {
		opts.NewRuntimeEpoch = func() (protocol.RuntimeEpoch, error) {
			id, err := randomOpaqueID("runtime")
			return protocol.RuntimeEpoch(id), err
		}
	}
	if opts.NewTurnID == nil {
		opts.NewTurnID = func() (protocol.TurnID, error) {
			id, err := randomOpaqueID("turn")
			return protocol.TurnID(id), err
		}
	}
	if opts.NewOperationID == nil {
		opts.NewOperationID = func() (protocol.OperationID, error) {
			id, err := randomOpaqueID("operation")
			return protocol.OperationID(id), err
		}
	}
	if opts.NewPromptID == nil {
		opts.NewPromptID = func() (protocol.PromptID, error) {
			id, err := randomOpaqueID("prompt")
			return protocol.PromptID(id), err
		}
	}
	if opts.NewCheckpointID == nil {
		opts.NewCheckpointID = func() (protocol.CheckpointID, error) {
			id, err := randomOpaqueID("checkpoint")
			return protocol.CheckpointID(id), err
		}
	}
	if opts.NewSubscriptionID == nil {
		opts.NewSubscriptionID = func() (protocol.SubscriptionID, error) {
			id, err := randomOpaqueID("subscription")
			return protocol.SubscriptionID(id), err
		}
	}
	if opts.SubscriptionQueue <= 0 {
		opts.SubscriptionQueue = 256
	}
	if opts.NowMillis == nil {
		opts.NowMillis = sessiontelemetry.NowMillis
	}
	// Runtime teardown must run its marker-preservation barrier before any
	// runtime/controller context observes cancellation. A directly-derived
	// context would let daemon.Server.Close cancel root first and race ordinary
	// turn cleanup, losing the in-flight marker. Preserve values but own the
	// cancellation edge here; the watcher below orders root cancellation through
	// shutdown just like an explicit Close.
	ctx, cancel := context.WithCancel(context.WithoutCancel(root))
	m := &RuntimeManager{
		ctx: ctx, cancel: cancel, hostEpoch: hostEpoch, factory: factory, opts: opts,
		runtimes:              make(map[protocol.RuntimeTarget]*SessionRuntime),
		retiredSubscriptions:  make(map[protocol.SubscriptionID]*retiredSubscription),
		issuedRuntimeEpochs:   make(map[protocol.RuntimeEpoch]struct{}),
		newOperationID:        opts.NewOperationID,
		issuedOperationIDs:    make(map[protocol.OperationID]struct{}),
		newSubscriptionID:     opts.NewSubscriptionID,
		issuedSubscriptionIDs: make(map[protocol.SubscriptionID]struct{}),
	}
	// Operation IDs can outlive the RPC that admitted them and stale IDs must
	// never name later work, including work in another runtime incarnation.
	// Subscription IDs are transport-map keys with the same daemon-global
	// uniqueness requirement. Both issuers use locks separate from manager.mu so
	// actor code never inverts manager-lock ordering.
	m.opts.NewOperationID = m.nextOperationID
	m.opts.NewSubscriptionID = m.nextSubscriptionID
	go func() {
		select {
		case <-root.Done():
			m.shutdown()
		case <-ctx.Done():
		}
	}()
	return m, nil
}

// GetOrCreate returns the current runtime incarnation for target. Controller
// construction receives only the daemon-derived context held by the manager.
func (m *RuntimeManager) GetOrCreate(target protocol.RuntimeTarget) (*SessionRuntime, error) {
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return nil, ErrRuntimeManagerClosed
	}
	if runtime := m.runtimes[target]; runtime != nil {
		return runtime, nil
	}
	runtime, err := m.buildRuntimeLocked(target)
	if err != nil {
		return nil, err
	}
	m.runtimes[target] = runtime
	runtime.start()
	return runtime, nil
}

// Replace creates a same-target runtime incarnation. Replacement admission,
// terminal subscription notification, and current=false are one old-actor
// sequencer barrier; the manager lock is always acquired before that barrier.
// This is the low-level lifecycle primitive: New/Clear/other business callers
// must reject active Turn, Prompt, Operation, or Job state before invoking it.
func (m *RuntimeManager) Replace(target protocol.RuntimeTarget) (*SessionRuntime, error) {
	return m.replace(target, target, protocol.ResyncRuntimeReplaced, false)
}

// ReplaceTarget moves the current incarnation to a different catalog target
// and emits the frozen target_replaced migration identity. The destination
// must be absent; callers retain responsibility for the corresponding durable
// catalog transaction.
func (m *RuntimeManager) ReplaceTarget(previousTarget, replacementTarget protocol.RuntimeTarget) (*SessionRuntime, error) {
	if previousTarget == replacementTarget {
		return nil, fmt.Errorf("%w: target replacement did not change target", ErrInvalidRuntimeTarget)
	}
	return m.replace(previousTarget, replacementTarget, protocol.ResyncTargetReplaced, true)
}

func (m *RuntimeManager) replace(
	previousTarget protocol.RuntimeTarget,
	replacementTarget protocol.RuntimeTarget,
	reason protocol.ResyncReason,
	requirePrevious bool,
) (*SessionRuntime, error) {
	if err := previousTarget.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}
	if err := replacementTarget.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}

	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeManagerClosed
	}
	previous := m.runtimes[previousTarget]
	if requirePrevious && previous == nil {
		m.mu.Unlock()
		return nil, ErrRuntimeClosed
	}
	if previousTarget != replacementTarget && m.runtimes[replacementTarget] != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeBusy
	}
	replacement, err := m.buildRuntimeLocked(replacementTarget)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if previous != nil {
		result, sealErr := previous.sealForReplacement(replacementTarget, replacement.epoch, reason)
		if sealErr != nil {
			replacement.discardBuiltRuntime()
			m.mu.Unlock()
			return nil, sealErr
		}
		for _, retired := range result.retired {
			// nextSubscriptionID reserves daemon-global identities before actor
			// state exists, so current and already-retired keys cannot collide.
			m.retiredSubscriptions[retired.state.id] = retired
		}
	}
	if previousTarget != replacementTarget {
		delete(m.runtimes, previousTarget)
	}
	m.runtimes[replacementTarget] = replacement
	replacement.start()
	m.mu.Unlock()

	if previous != nil {
		<-previous.Done()
	}
	return replacement, nil
}

func (m *RuntimeManager) buildRuntimeLocked(target protocol.RuntimeTarget) (*SessionRuntime, error) {
	epoch, err := m.nextRuntimeEpochLocked()
	if err != nil {
		return nil, err
	}
	runtimeCtx, cancel := context.WithCancel(m.ctx)
	runtime := newSessionRuntime(runtimeCtx, cancel, m.hostEpoch, target, epoch, m.opts)
	var controller control.SessionAPI
	resumeState := control.SessionResumeState{}
	if recoveryFactory, ok := m.factory.(RecoveryControllerFactory); ok {
		controller, resumeState, err = recoveryFactory.CreateControllerWithRecovery(runtimeCtx, target, event.FuncSink(runtime.emit))
	} else {
		controller, err = m.factory.CreateController(runtimeCtx, target, event.FuncSink(runtime.emit))
	}
	if err != nil {
		runtime.abortInitialization()
		return nil, fmt.Errorf("create controller for Session %s: %w", target.SessionID, err)
	}
	if nilutil.IsNil(controller) {
		runtime.abortInitialization()
		return nil, ErrControllerUnavailable
	}
	if m.ctx.Err() != nil {
		controller.Close()
		runtime.abortInitialization()
		return nil, ErrRuntimeManagerClosed
	}
	runtime.controller = controller
	runtime.resumeState = resumeState
	if err := safeControllerCall(func() { runtime.workspaceRoot = controller.WorkspaceRoot() }); err != nil {
		runtime.discardBuiltRuntime()
		return nil, fmt.Errorf("read Controller workspace root for Session %s: %w", target.SessionID, err)
	}
	var sessionPath string
	if err := safeControllerCall(func() { sessionPath = controller.SessionPath() }); err != nil {
		runtime.discardBuiltRuntime()
		return nil, fmt.Errorf("read Controller path for Session %s: %w", target.SessionID, err)
	}
	if sessionPath != "" {
		runtime.telemetryPath = sessiontelemetry.Path(sessionPath)
		runtime.telemetry = sessiontelemetry.Load(runtime.telemetryPath)
	} else {
		runtime.telemetry = sessiontelemetry.Snapshot{Version: sessiontelemetry.Version, ReadFiles: []sessiontelemetry.ReadFileRecord{}}
	}
	return runtime, nil
}

func (m *RuntimeManager) nextRuntimeEpochLocked() (protocol.RuntimeEpoch, error) {
	for attempt := 0; attempt < 8; attempt++ {
		epoch, err := m.opts.NewRuntimeEpoch()
		if err != nil {
			return "", fmt.Errorf("generate runtime epoch: %w", err)
		}
		if epoch == "" {
			return "", errors.New("generated runtime epoch is empty")
		}
		if _, issued := m.issuedRuntimeEpochs[epoch]; issued {
			continue
		}
		// Reserve before Controller construction. Even a failed incarnation
		// cannot donate its opaque identity to later work in this daemon.
		m.issuedRuntimeEpochs[epoch] = struct{}{}
		return epoch, nil
	}
	return "", errors.New("runtime epoch generator repeatedly returned an issued ID")
}

func (m *RuntimeManager) nextOperationID() (protocol.OperationID, error) {
	m.operationIDMu.Lock()
	defer m.operationIDMu.Unlock()
	for attempt := 0; attempt < 8; attempt++ {
		id, err := m.newOperationID()
		if err != nil {
			return "", fmt.Errorf("generate operation ID: %w", err)
		}
		if strings.TrimSpace(string(id)) == "" {
			return "", errors.New("generated operation ID is empty")
		}
		if _, issued := m.issuedOperationIDs[id]; issued {
			continue
		}
		// Reserve before Controller admission. A rejected or failed admission
		// cannot donate its identity to later work in this daemon lifetime.
		m.issuedOperationIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("operation ID generator repeatedly returned an issued ID")
}

func (m *RuntimeManager) nextSubscriptionID() (protocol.SubscriptionID, error) {
	m.subscriptionIDMu.Lock()
	defer m.subscriptionIDMu.Unlock()
	for attempt := 0; attempt < 8; attempt++ {
		id, err := m.newSubscriptionID()
		if err != nil {
			return "", fmt.Errorf("generate subscription ID: %w", err)
		}
		if id == "" {
			return "", errors.New("generated subscription ID is empty")
		}
		if _, issued := m.issuedSubscriptionIDs[id]; issued {
			continue
		}
		// Reserve before the actor builds channel/snapshot state. Abort, failed
		// projection, and retired cleanup can never donate the wire identity to
		// another current or retired runtime in this daemon lifetime.
		m.issuedSubscriptionIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("subscription ID generator repeatedly returned an issued ID")
}

// Runtime returns the current incarnation without creating a cold Session.
func (m *RuntimeManager) Runtime(target protocol.RuntimeTarget) (*SessionRuntime, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	runtime, ok := m.runtimes[target]
	return runtime, ok
}

// SessionSummary implements the live RuntimeInspector projection without
// exposing Controller or subscription ownership. The actor action is a status
// barrier, so Running, PendingPrompt, and ActiveJobs belong to one runtime
// sequence point rather than independent atomic reads.
func (m *RuntimeManager) SessionSummary(target protocol.RuntimeTarget) (*protocol.SessionRuntimeSummary, bool) {
	if m == nil || target.Validate() != nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed || m.ctx.Err() != nil {
		return nil, false
	}
	runtime := m.runtimes[target]
	if runtime == nil {
		return nil, false
	}
	activity, err := runtime.activity(context.Background())
	if err != nil {
		return nil, false
	}
	summary := activity.summary
	return &summary, true
}

// WorkspaceInUse reports whether any current runtime in workspace has a
// subscription, running Turn, pending Prompt, or active background job. It
// briefly establishes an abort-only barrier on every matching actor, yielding
// one all-Session observation instead of a collection of racy snapshots.
func (m *RuntimeManager) WorkspaceInUse(workspaceID protocol.WorkspaceID) bool {
	if m == nil || workspaceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return false
	}
	prepared, err := prepareIdleRelease(m.workspaceRuntimesLocked(workspaceID))
	if err != nil {
		return true
	}
	busy := false
	for _, item := range prepared {
		if !item.activity.idle() {
			busy = true
			break
		}
	}
	finishIdleRelease(prepared, false)
	return busy
}

// ReleaseIdleSession is the atomic Session close release hint. The three
// frozen dispositions are decided while the Session actor is held at the same
// barrier that checks subscriptions and work, so adapters never need a racy
// SessionSummary probe before release.
func (m *RuntimeManager) ReleaseIdleSession(target protocol.RuntimeTarget) (protocol.SessionCloseDisposition, error) {
	if err := target.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}
	if m == nil {
		return "", ErrRuntimeManagerClosed
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return "", ErrRuntimeManagerClosed
	}
	runtime := m.runtimes[target]
	if runtime == nil {
		m.mu.Unlock()
		return protocol.SessionAlreadyClosed, nil
	}
	prepared, err := prepareIdleRelease([]*SessionRuntime{runtime})
	if err != nil {
		m.mu.Unlock()
		return "", err
	}
	if !prepared[0].activity.idle() {
		finishIdleRelease(prepared, false)
		m.mu.Unlock()
		return protocol.SessionRetainedActive, nil
	}
	delete(m.runtimes, target)
	runtime.markCurrent(false)
	finishIdleRelease(prepared, true)
	m.mu.Unlock()
	<-runtime.Done()
	return protocol.SessionReleased, nil
}

// CloseSessionMutation performs the live session/close admission in the owning
// Session actor while RuntimeManager holds the incarnation registry stable.
// requestId Begin, epoch validation, idle observation, cached disposition, and
// an optional stop transition therefore share one actor boundary.
func (m *RuntimeManager) CloseSessionMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionCloseParams,
	beforeBegin func() error,
) (protocol.SessionCloseResult, bool, error) {
	if err := params.Target.Validate(); err != nil {
		return protocol.SessionCloseResult{}, false, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}
	if m == nil {
		return protocol.SessionCloseResult{}, false, ErrRuntimeManagerClosed
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return protocol.SessionCloseResult{}, false, ErrRuntimeManagerClosed
	}
	runtime := m.runtimes[params.Target]
	if runtime == nil {
		m.mu.Unlock()
		return protocol.SessionCloseResult{}, false, ErrRuntimeClosed
	}
	barrier, admission, err := runtime.beginSessionCloseMutation(registry, params, beforeBegin)
	if err != nil {
		m.mu.Unlock()
		return protocol.SessionCloseResult{}, false, err
	}
	if admission.replay != nil {
		m.mu.Unlock()
		outcome, waitErr := admission.replay.attempt.Wait(ctx)
		if waitErr != nil {
			return protocol.SessionCloseResult{}, true, waitErr
		}
		var replay protocol.SessionCloseResult
		if decodeErr := outcome.Decode(&replay); decodeErr != nil {
			return protocol.SessionCloseResult{}, true, decodeErr
		}
		return replay, true, nil
	}
	if admission.result.Disposition == protocol.SessionReleased {
		if m.runtimes[params.Target] != runtime {
			cause := errors.New("remote Session close lost runtime ownership")
			_ = barrier.finish(false, cause)
			m.mu.Unlock()
			return protocol.SessionCloseResult{}, false, cause
		}
		if err := barrier.finish(true, nil); err != nil {
			m.mu.Unlock()
			return protocol.SessionCloseResult{}, false, err
		}
		delete(m.runtimes, params.Target)
		runtime.markCurrent(false)
		m.mu.Unlock()
		<-runtime.Done()
		return admission.result, false, nil
	}
	if err := barrier.finish(true, nil); err != nil {
		m.mu.Unlock()
		return protocol.SessionCloseResult{}, false, err
	}
	m.mu.Unlock()
	return admission.result, false, nil
}

// ReleaseIdleWorkspace implements catalog.IdleWorkspaceReleaser by name and
// signature, without importing catalog. Release is workspace-atomic: all
// matching actors are frozen and checked first; one busy runtime aborts every
// barrier, so no idle sibling is partially removed.
func (m *RuntimeManager) ReleaseIdleWorkspace(workspaceID protocol.WorkspaceID) {
	if m == nil || workspaceID == "" {
		return
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	prepared, err := prepareIdleRelease(m.workspaceRuntimesLocked(workspaceID))
	if err != nil {
		m.mu.Unlock()
		return
	}
	for _, item := range prepared {
		if !item.activity.idle() {
			finishIdleRelease(prepared, false)
			m.mu.Unlock()
			return
		}
	}
	runtimes := make([]*SessionRuntime, 0, len(prepared))
	for _, item := range prepared {
		target := item.runtime.Target()
		if m.runtimes[target] == item.runtime {
			delete(m.runtimes, target)
		}
		item.runtime.markCurrent(false)
		runtimes = append(runtimes, item.runtime)
	}
	finishIdleRelease(prepared, true)
	m.mu.Unlock()
	for _, runtime := range runtimes {
		<-runtime.Done()
	}
}

// ReserveIdleWorkspace establishes the all-or-nothing close boundary required
// by workspace/close. It keeps both the manager registry and all matching
// Session actors frozen until the caller durably closes the catalog and calls
// Commit, or encounters an error and calls Abort/Close.
func (m *RuntimeManager) ReserveIdleWorkspace(workspaceID protocol.WorkspaceID) (*WorkspaceCloseReservation, error) {
	if m == nil {
		return nil, ErrRuntimeManagerClosed
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace ID is empty", ErrInvalidRuntimeTarget)
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeManagerClosed
	}
	prepared, err := prepareIdleRelease(m.workspaceRuntimesLocked(workspaceID))
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	for _, item := range prepared {
		if !item.activity.idle() {
			finishIdleRelease(prepared, false)
			m.mu.Unlock()
			return nil, ErrWorkspaceInUse
		}
	}
	return &WorkspaceCloseReservation{manager: m, workspace: workspaceID, prepared: prepared}, nil
}

// Commit removes every reserved runtime and commits each actor stop transition
// before reopening manager admission. It waits for Controller teardown so a
// subsequent cold operation cannot overlap an old writer lease.
func (r *WorkspaceCloseReservation) Commit() error {
	if r == nil || r.manager == nil {
		return ErrRuntimeManagerClosed
	}
	if r.finished {
		return errors.New("remote workspace close reservation is already finished")
	}
	r.finished = true
	m := r.manager
	runtimes := make([]*SessionRuntime, 0, len(r.prepared))
	for _, item := range r.prepared {
		target := item.runtime.Target()
		if target.WorkspaceID != r.workspace || m.runtimes[target] != item.runtime {
			finishIdleRelease(r.prepared, false)
			m.mu.Unlock()
			return errors.New("remote workspace close reservation lost runtime ownership")
		}
		delete(m.runtimes, target)
		item.runtime.markCurrent(false)
		runtimes = append(runtimes, item.runtime)
	}
	finishIdleRelease(r.prepared, true)
	m.mu.Unlock()
	for _, runtime := range runtimes {
		<-runtime.Done()
	}
	return nil
}

// Abort restores actor admission without releasing any runtime.
func (r *WorkspaceCloseReservation) Abort() {
	if r == nil || r.manager == nil || r.finished {
		return
	}
	r.finished = true
	finishIdleRelease(r.prepared, false)
	r.manager.mu.Unlock()
}

// Close is the fail-safe reservation lifecycle hook and is equivalent to
// Abort until Commit has succeeded.
func (r *WorkspaceCloseReservation) Close() { r.Abort() }

// ReserveTargetsForRemoval seals and tears down any current incarnations of
// targets, then keeps GetOrCreate blocked until the caller completes or aborts
// its durable catalog mutation. This is intentionally destructive to the live
// incarnation: a failed catalog write can rebuild a fresh epoch after Abort,
// but can never resume the stopped writer concurrently with artifact moves.
func (m *RuntimeManager) ReserveTargetsForRemoval(targets []protocol.RuntimeTarget) (*TargetRemovalReservation, error) {
	if m == nil {
		return nil, ErrRuntimeManagerClosed
	}
	unique := make(map[protocol.RuntimeTarget]struct{}, len(targets))
	ordered := make([]protocol.RuntimeTarget, 0, len(targets))
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
		}
		if _, exists := unique[target]; exists {
			continue
		}
		unique[target] = struct{}{}
		ordered = append(ordered, target)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].WorkspaceID != ordered[j].WorkspaceID {
			return ordered[i].WorkspaceID < ordered[j].WorkspaceID
		}
		return ordered[i].SessionID < ordered[j].SessionID
	})

	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeManagerClosed
	}
	runtimes := make([]*SessionRuntime, 0, len(ordered))
	for _, target := range ordered {
		if runtime := m.runtimes[target]; runtime != nil {
			delete(m.runtimes, target)
			runtime.markCurrent(false)
			runtime.stop()
			runtimes = append(runtimes, runtime)
		}
	}
	for _, runtime := range runtimes {
		<-runtime.Done()
	}
	return &TargetRemovalReservation{manager: m}, nil
}

// Commit and Abort both reopen runtime admission. The old incarnation was
// already terminated so Abort cannot resurrect it; it only leaves the catalog
// unchanged and permits a fresh cold runtime on the next request.
func (r *TargetRemovalReservation) Commit() { r.finish() }
func (r *TargetRemovalReservation) Abort()  { r.finish() }
func (r *TargetRemovalReservation) Close()  { r.finish() }

func (r *TargetRemovalReservation) finish() {
	if r == nil || r.manager == nil || r.finished {
		return
	}
	r.finished = true
	r.manager.mu.Unlock()
}

// ReserveRuntimeAbsence holds the manager registry stable only when target has
// no current incarnation. ErrRuntimeBusy asks the caller to retry the live
// Session actor path instead of deciding already_closed from a stale probe.
func (m *RuntimeManager) ReserveRuntimeAbsence(target protocol.RuntimeTarget) (*RuntimeAbsenceReservation, error) {
	if m == nil {
		return nil, ErrRuntimeManagerClosed
	}
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeManagerClosed
	}
	if m.runtimes[target] != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeBusy
	}
	return &RuntimeAbsenceReservation{manager: m}, nil
}

func (r *RuntimeAbsenceReservation) Close() {
	if r == nil || r.manager == nil || r.finished {
		return
	}
	r.finished = true
	r.manager.mu.Unlock()
}

type preparedIdleRuntime struct {
	runtime  *SessionRuntime
	barrier  *idleReleaseBarrier
	activity runtimeActivity
}

func prepareIdleRelease(runtimes []*SessionRuntime) ([]preparedIdleRuntime, error) {
	prepared := make([]preparedIdleRuntime, 0, len(runtimes))
	for _, runtime := range runtimes {
		barrier, activity, err := runtime.beginIdleRelease()
		if err != nil {
			finishIdleRelease(prepared, false)
			return nil, err
		}
		prepared = append(prepared, preparedIdleRuntime{runtime: runtime, barrier: barrier, activity: activity})
	}
	return prepared, nil
}

func finishIdleRelease(prepared []preparedIdleRuntime, commit bool) {
	for _, item := range prepared {
		item.barrier.finish(commit)
	}
}

func (m *RuntimeManager) workspaceRuntimesLocked(workspaceID protocol.WorkspaceID) []*SessionRuntime {
	runtimes := make([]*SessionRuntime, 0)
	for target, runtime := range m.runtimes {
		if target.WorkspaceID == workspaceID {
			runtimes = append(runtimes, runtime)
		}
	}
	// Stable actor acquisition order keeps stress tests reproducible and leaves
	// room for future cross-Session operations to share this barrier helper.
	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].Target().SessionID < runtimes[j].Target().SessionID
	})
	return runtimes
}

// Subscribe installs a subscription on the current target incarnation while
// the manager registry is stable. A replaceSubscriptionId retained from a
// stopped incarnation is accepted only for the exact attachment and the
// replacement identity announced by its terminal resync.
func (m *RuntimeManager) Subscribe(
	ctx context.Context,
	target protocol.RuntimeTarget,
	attachment AttachmentKey,
	replace protocol.SubscriptionID,
) (*SubscriptionInstall, error) {
	value, err := randomOpaqueID("snapshot")
	if err != nil {
		return nil, fmt.Errorf("generate internal snapshot ID: %w", err)
	}
	return m.SubscribeSnapshot(ctx, target, attachment, replace, protocol.SnapshotID(value))
}

// SubscribeSnapshot is the protocol-facing admission path. The daemon reserves
// snapshotId before entering the actor so the immutable history binding and the
// N/N+1 event boundary are frozen by the same sequencer action.
func (m *RuntimeManager) SubscribeSnapshot(
	ctx context.Context,
	target protocol.RuntimeTarget,
	attachment AttachmentKey,
	replace protocol.SubscriptionID,
	snapshotID protocol.SnapshotID,
) (*SubscriptionInstall, error) {
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeTarget, err)
	}
	if strings.TrimSpace(string(snapshotID)) == "" {
		return nil, errors.New("snapshotId is empty")
	}
	if err := attachment.validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrRuntimeManagerClosed
	}
	retired := m.retiredSubscriptions[replace]
	if replace != "" && retired != nil {
		runtime := m.runtimes[target]
		if retired.migrating || retired.state.attachment != attachment || retired.replacementTarget != target || runtime == nil || retired.replacementRuntimeEpoch != runtime.epoch {
			m.mu.Unlock()
			return nil, ErrSubscriptionNotFound
		}
		if err := ctx.Err(); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		admission, err := runtime.beginBoundSubscription(m.ctx, attachment, "", snapshotID)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		retired.migrating = true
		install := &SubscriptionInstall{
			Runtime: runtime, Subscription: admission.subscription,
			manager: m, attachment: attachment, replacement: retired,
		}
		m.mu.Unlock()
		return install, nil
	}

	runtime := m.runtimes[target]
	if runtime == nil {
		if replace != "" {
			m.mu.Unlock()
			return nil, ErrSubscriptionNotFound
		}
		var err error
		runtime, err = m.buildRuntimeLocked(target)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.runtimes[target] = runtime
		runtime.start()
	}

	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	admission, err := runtime.beginBoundSubscription(m.ctx, attachment, replace, snapshotID)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	install := &SubscriptionInstall{
		Runtime: runtime, Subscription: admission.subscription,
		manager: m, attachment: attachment, previousID: admission.previousID,
	}
	m.mu.Unlock()
	return install, nil
}

// Commit releases the old terminal channel only after the adapter has made
// the new subscription and all of its response owners active.
func (i *SubscriptionInstall) Commit() error {
	if i == nil {
		return nil
	}
	i.finalize.Do(func() {
		if i.manager == nil {
			return
		}
		m := i.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		if i.previousID != "" {
			if err := i.Runtime.commitSubscriptionReplacement(m.ctx, i.attachment, i.previousID, i.Subscription.ID); err != nil {
				if !errors.Is(err, ErrRuntimeClosed) {
					i.finalizeErr = err
					return
				}
				// Runtime replacement may retire both halves while projection is
				// in flight. Commit consumes only the old half; the new terminal
				// subscription remains transport-owned for its own resync.
				m.removeRetiredSubscriptionLocked(i.attachment, i.previousID)
			}
		}
		if i.replacement != nil {
			if m.retiredSubscriptions[i.replacement.state.id] == i.replacement {
				delete(m.retiredSubscriptions, i.replacement.state.id)
			}
			i.replacement.close()
		}
	})
	return i.finalizeErr
}

// Abort removes the newly-created subscription and reopens the retained old
// identity for a later same-transport retry when it still belongs to manager.
func (i *SubscriptionInstall) Abort() error {
	if i == nil {
		return nil
	}
	i.finalize.Do(func() {
		if i.manager == nil {
			return
		}
		m := i.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		if i.previousID != "" {
			if err := i.Runtime.abortSubscriptionReplacement(m.ctx, i.attachment, i.previousID, i.Subscription.ID); err != nil {
				if !errors.Is(err, ErrRuntimeClosed) {
					i.finalizeErr = err
					return
				}
				m.removeRetiredSubscriptionLocked(i.attachment, i.Subscription.ID)
				if previous := m.retiredSubscriptions[i.previousID]; previous != nil && previous.state.attachment == i.attachment {
					previous.migrating = false
				}
			}
		} else {
			closed := m.removeRetiredSubscriptionLocked(i.attachment, i.Subscription.ID)
			if !closed && i.Runtime != nil {
				if err := i.Runtime.Unsubscribe(m.ctx, i.attachment, i.Subscription.ID); err != nil && !errors.Is(err, ErrRuntimeClosed) {
					i.finalizeErr = err
					return
				}
			}
		}
		if i.replacement != nil && m.retiredSubscriptions[i.replacement.state.id] == i.replacement {
			i.replacement.migrating = false
		}
	})
	return i.finalizeErr
}

// Unsubscribe removes either a current actor subscription or a terminal
// subscription retained across replacement. It is idempotent for IDs not
// owned by the supplied attachment.
func (m *RuntimeManager) Unsubscribe(ctx context.Context, attachment AttachmentKey, id protocol.SubscriptionID) error {
	if err := attachment.validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrRuntimeManagerClosed
	}
	if m.removeRetiredSubscriptionLocked(attachment, id) {
		return nil
	}
	for _, runtime := range m.runtimes {
		if err := runtime.Unsubscribe(ctx, attachment, id); err != nil && !errors.Is(err, ErrRuntimeClosed) {
			return err
		}
	}
	return nil
}

func (m *RuntimeManager) removeRetiredSubscriptionLocked(attachment AttachmentKey, id protocol.SubscriptionID) bool {
	retired := m.retiredSubscriptions[id]
	if retired == nil || retired.state.attachment != attachment {
		return false
	}
	delete(m.retiredSubscriptions, id)
	retired.close()
	return true
}

// DetachAttachment removes every subscription owned by the exact transport.
// It intentionally uses the daemon context, not a possibly-cancelled attach
// context, and never stops a runtime or calls Controller.Cancel/Close.
func (m *RuntimeManager) DetachAttachment(attachment AttachmentKey) error {
	if err := attachment.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrRuntimeManagerClosed
	}
	for id, retired := range m.retiredSubscriptions {
		if retired.state.attachment == attachment {
			delete(m.retiredSubscriptions, id)
			retired.close()
		}
	}
	for _, runtime := range m.runtimes {
		if err := runtime.detachAttachment(m.ctx, attachment); err != nil && !errors.Is(err, ErrRuntimeClosed) {
			return err
		}
	}
	return nil
}

// ActivateAttachment makes the newly granted attachment the daemon's sole
// notification owner. It removes subscriptions from older generations of a
// resumed lease and from expired leases replaced by a fresh client. Runtime
// work itself remains daemon-owned and is never cancelled here.
func (m *RuntimeManager) ActivateAttachment(binding LeaseBinding) error {
	attachment := AttachmentForLease(binding)
	if err := attachment.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrRuntimeManagerClosed
	}
	for id, retired := range m.retiredSubscriptions {
		if retired.state.attachment != attachment {
			delete(m.retiredSubscriptions, id)
			retired.close()
		}
	}
	for _, runtime := range m.runtimes {
		if err := runtime.detachOtherAttachments(m.ctx, attachment); err != nil && !errors.Is(err, ErrRuntimeClosed) {
			return err
		}
	}
	return nil
}

func (m *RuntimeManager) runtimeSnapshot() ([]*SessionRuntime, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, ErrRuntimeManagerClosed
	}
	runtimes := make([]*SessionRuntime, 0, len(m.runtimes))
	for _, runtime := range m.runtimes {
		runtimes = append(runtimes, runtime)
	}
	return runtimes, nil
}

// Close stops all daemon-owned Controllers and waits for their actors. It is
// the lifecycle operation attach detach deliberately does not perform.
func (m *RuntimeManager) Close() {
	m.shutdown()
}

func (m *RuntimeManager) shutdown() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		runtimes := make([]*SessionRuntime, 0, len(m.runtimes))
		for _, runtime := range m.runtimes {
			runtimes = append(runtimes, runtime)
		}
		m.runtimes = make(map[protocol.RuntimeTarget]*SessionRuntime)
		retired := make([]*retiredSubscription, 0, len(m.retiredSubscriptions))
		for _, subscription := range m.retiredSubscriptions {
			retired = append(retired, subscription)
		}
		m.retiredSubscriptions = make(map[protocol.SubscriptionID]*retiredSubscription)
		m.mu.Unlock()

		for _, subscription := range retired {
			subscription.close()
		}
		for _, runtime := range runtimes {
			runtime.stopForRuntimeShutdown()
		}
		for _, runtime := range runtimes {
			<-runtime.Done()
		}
		m.cancel()
	})
}

func randomOpaqueID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}
