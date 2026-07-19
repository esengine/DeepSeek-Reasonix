package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/runtimeapi"
)

type TargetKind string

const (
	TargetLocal  TargetKind = "local"
	TargetRemote TargetKind = "remote"
)

// TargetDescriptor identifies an execution target without carrying connection
// settings. For Remote targets ID is the Desktop-owned Host entry ID, never an
// SSH hostname, credential or protocol identity.
type TargetDescriptor struct {
	Kind  TargetKind `json:"kind"`
	ID    string     `json:"id"`
	Label string     `json:"label"`
}

func (t TargetDescriptor) Validate() error {
	if t.Kind != TargetLocal && t.Kind != TargetRemote {
		return fmt.Errorf("unsupported target kind %q", t.Kind)
	}
	if strings.TrimSpace(t.ID) == "" {
		return errors.New("target ID is required")
	}
	return nil
}

func sameTarget(left, right TargetDescriptor) bool {
	return left.Kind == right.Kind && left.ID == right.ID
}

type TargetState string

const (
	TargetDisconnected       TargetState = "Disconnected"
	TargetLocalConnected     TargetState = "LocalConnected"
	TargetRemoteConnecting   TargetState = "RemoteConnecting"
	TargetRemoteConnected    TargetState = "RemoteConnected"
	TargetRemoteReconnecting TargetState = "RemoteReconnecting"
	TargetSwitching          TargetState = "Switching"
)

type TargetManagerSnapshot struct {
	State             TargetState      `json:"state"`
	Target            TargetDescriptor `json:"target"`
	Generation        uint64           `json:"generation"`
	RecoveryAvailable bool             `json:"recoveryAvailable"`
	LastError         string           `json:"lastError,omitempty"`
}

type ReleaseBlockerKind string

const (
	ReleaseRuntimeRunning ReleaseBlockerKind = "runtime-running"
	ReleasePromptPending  ReleaseBlockerKind = "prompt-pending"
)

type ReleaseBlocker struct {
	Kind   ReleaseBlockerKind
	Detail string
}

type ReleaseStatus struct {
	Blockers []ReleaseBlocker
}

func (s ReleaseStatus) Busy() bool { return len(s.Blockers) != 0 }

// TargetAdapter owns one connected target. Detach is graceful: Remote
// implementations send remote/detach, while Local implementations release the
// local Controller set. A transport that has already failed reports that loss
// before it ceases to be the current adapter, so no graceful Detach is implied.
type TargetAdapter interface {
	Descriptor() TargetDescriptor
	RuntimeAPI() runtimeapi.RuntimeAPI
	CanRelease(context.Context) (ReleaseStatus, error)
	Detach(context.Context) error
}

// TargetFaultSource is an optional adapter contract for terminal transport
// diagnostics. Remote adapters should implement it so TargetManager can retain
// the actual connection/protocol failure instead of inferring every loss from
// RuntimeAPI.Events closing. Faults must contain non-nil errors; closing the
// fault channel only means that no richer diagnostic remains available.
type TargetFaultSource interface {
	Faults() <-chan error
}

// TargetAbandoner drops adapter-owned local resources without asserting that a
// graceful peer detach succeeded. It is used only during Desktop process
// shutdown or stale-adapter cleanup; resumable Remote lease state stays on
// disk when the transport outcome is unknown.
type TargetAbandoner interface {
	AbandonTarget() error
}

type TargetConnector interface {
	Connect(context.Context, TargetDescriptor) (TargetAdapter, error)
}

// TargetReconnector resumes a failed Remote adapter without throwing away the
// adapter-owned lease, open target and last observed event state. Implementors
// must obtain a fresh atomic AttachAndSubscribe snapshot before returning the
// replacement adapter as connected.
type TargetReconnector interface {
	Reconnect(context.Context, TargetDescriptor, TargetAdapter) (TargetAdapter, error)
}

type TargetConnectorFunc func(context.Context, TargetDescriptor) (TargetAdapter, error)

func (f TargetConnectorFunc) Connect(ctx context.Context, target TargetDescriptor) (TargetAdapter, error) {
	return f(ctx, target)
}

var (
	ErrTargetBusy                 = errors.New("target has active work")
	ErrRemoteDetachConfirmation   = errors.New("switching from Remote requires confirmation")
	ErrTargetTransitionSuperseded = errors.New("target transition superseded")
	ErrRuntimeTargetUnavailable   = errors.New("runtime target is not connected")
	ErrTargetEventStreamClosed    = errors.New("target event stream closed")
	ErrTargetReconnectUnsupported = errors.New("target connector cannot preserve Remote recovery state")
	ErrTargetManagerClosed        = errors.New("target manager is closed")
)

type TargetBusyError struct {
	Status ReleaseStatus
}

func (e *TargetBusyError) Error() string        { return ErrTargetBusy.Error() }
func (e *TargetBusyError) Is(target error) bool { return target == ErrTargetBusy }

type SwitchTargetOptions struct {
	// ConfirmRemoteDetach acknowledges that Host work continues after the
	// Desktop releases its Remote lease and changes to Local.
	ConfirmRemoteDetach bool
}

type TargetRuntimeEvent struct {
	Generation uint64
	Target     TargetDescriptor
	Event      runtimeapi.Event
}

// TargetEventSink must return quickly and must not call back into TargetManager.
// App integration should enqueue onto the existing async Wails event emitter.
type TargetEventSink func(TargetRuntimeEvent)

type TargetManagerOptions struct {
	EventSink TargetEventSink
	// StateSink receives every committed target-state transition in order. It
	// must return quickly and must not call back into TargetManager.
	StateSink func(TargetManagerSnapshot)
}

// TargetManager owns the Desktop workbench's only execution target. mu protects
// logical/physical state; lifecycleMu serializes physical detach/connect work
// while newer Switch calls can still invalidate and cancel an older operation.
// eventDispatchMu linearizes event delivery with generation invalidation.
type TargetManager struct {
	connector TargetConnector

	lifecycleMu     sync.Mutex
	eventDispatchMu sync.Mutex
	stateDispatchMu sync.Mutex
	mu              sync.RWMutex

	state      TargetState
	target     TargetDescriptor
	generation uint64
	lastError  string

	current           TargetAdapter
	currentGeneration uint64
	recovery          TargetAdapter
	eventStop         chan struct{}
	eventSink         TargetEventSink
	stateSink         func(TargetManagerSnapshot)

	transitionCancel     context.CancelFunc
	transitionGeneration uint64
	closed               bool
}

func NewTargetManager(connector TargetConnector, initial TargetAdapter, options TargetManagerOptions) (*TargetManager, error) {
	if connector == nil {
		return nil, errors.New("target connector is required")
	}
	m := &TargetManager{
		connector: connector,
		state:     TargetDisconnected,
		eventSink: options.EventSink,
		stateSink: options.StateSink,
	}
	if initial == nil {
		m.emitInitialState()
		return m, nil
	}
	target := initial.Descriptor()
	if err := target.Validate(); err != nil {
		return nil, fmt.Errorf("initial target: %w", err)
	}
	if initial.RuntimeAPI() == nil {
		return nil, errors.New("initial target RuntimeAPI is required")
	}
	m.target = target
	m.current = initial
	m.generation = 1
	m.currentGeneration = 1
	m.state = connectedTargetState(target.Kind)
	m.startEventPumpLocked(initial, m.generation)
	m.emitInitialState()
	return m, nil
}

func connectedTargetState(kind TargetKind) TargetState {
	if kind == TargetRemote {
		return TargetRemoteConnected
	}
	return TargetLocalConnected
}

func (m *TargetManager) Snapshot() TargetManagerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return TargetManagerSnapshot{
		State: m.state, Target: m.target, Generation: m.generation,
		RecoveryAvailable: m.recovery != nil, LastError: m.lastError,
	}
}

func (m *TargetManager) snapshotLocked() TargetManagerSnapshot {
	return TargetManagerSnapshot{
		State: m.state, Target: m.target, Generation: m.generation,
		RecoveryAvailable: m.recovery != nil, LastError: m.lastError,
	}
}

func (m *TargetManager) emitInitialState() {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.mu.RLock()
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.RUnlock()
	if sink != nil {
		sink(snapshot)
	}
}

// SetStateSink replaces the observer and immediately publishes the current
// snapshot. The observer follows the same no-callback rule as StateSink in the
// constructor options.
func (m *TargetManager) SetStateSink(sink func(TargetManagerSnapshot)) {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.mu.Lock()
	m.stateSink = sink
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
}

func (m *TargetManager) RuntimeAPI() (runtimeapi.RuntimeAPI, error) {
	api, _, err := m.RuntimeAPISnapshot()
	return api, err
}

// RuntimeAPISnapshot returns the current RuntimeAPI together with the exact
// target snapshot that authorized it. Keeping both reads under one lock avoids
// pairing an adapter from one target generation with an ABA-replaced snapshot
// from a later generation.
func (m *TargetManager) RuntimeAPISnapshot() (runtimeapi.RuntimeAPI, TargetManagerSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := m.snapshotLocked()
	if m.current == nil || m.currentGeneration != m.generation || !sameTarget(m.current.Descriptor(), m.target) ||
		(m.state != TargetLocalConnected && m.state != TargetRemoteConnected) {
		return nil, snapshot, ErrRuntimeTargetUnavailable
	}
	api := m.current.RuntimeAPI()
	if api == nil {
		return nil, snapshot, ErrRuntimeTargetUnavailable
	}
	return api, snapshot, nil
}

func (m *TargetManager) SetEventSink(sink TargetEventSink) {
	m.eventDispatchMu.Lock()
	defer m.eventDispatchMu.Unlock()
	m.mu.Lock()
	m.eventSink = sink
	m.mu.Unlock()
}

func (m *TargetManager) Switch(ctx context.Context, target TargetDescriptor, options SwitchTargetOptions) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrTargetManagerClosed
	}
	if m.current != nil && sameTarget(m.current.Descriptor(), target) &&
		(m.state == TargetLocalConnected || m.state == TargetRemoteConnected) {
		m.mu.RUnlock()
		return nil
	}
	requiresConfirmation := target.Kind == TargetLocal &&
		((m.current != nil && m.current.Descriptor().Kind == TargetRemote) ||
			(m.recovery != nil && m.recovery.Descriptor().Kind == TargetRemote) ||
			m.state == TargetRemoteConnected || m.state == TargetRemoteReconnecting)
	reconnecting := m.state == TargetRemoteReconnecting && m.recovery != nil &&
		sameTarget(m.recovery.Descriptor(), target)
	m.mu.RUnlock()
	if requiresConfirmation && !options.ConfirmRemoteDetach {
		return ErrRemoteDetachConfirmation
	}
	if err := m.preflightLocalRelease(ctx, target); err != nil {
		return err
	}

	transitionCtx, cancel := context.WithCancel(ctx)
	generation, err := m.beginTransition(target, cancel)
	if err != nil {
		cancel()
		return err
	}
	defer func() {
		cancel()
		m.finishTransition(generation)
	}()
	return m.executeSwitch(transitionCtx, generation, target, reconnecting)
}

// preflightLocalRelease deliberately runs before beginTransition invalidates
// the current generation or stops its event pump. A busy/error result therefore
// leaves the Local target and its single event consumer completely untouched.
func (m *TargetManager) preflightLocalRelease(ctx context.Context, target TargetDescriptor) error {
	if target.Kind != TargetRemote {
		return nil
	}
	m.mu.RLock()
	current := m.current
	generation := m.generation
	shouldCheck := current != nil && current.Descriptor().Kind == TargetLocal && m.state == TargetLocalConnected
	m.mu.RUnlock()
	if !shouldCheck {
		return nil
	}
	status, err := current.CanRelease(ctx)
	m.mu.RLock()
	stillCurrent := m.generation == generation && m.currentGeneration == generation && m.current != nil &&
		m.state == TargetLocalConnected && sameTarget(m.current.Descriptor(), current.Descriptor())
	m.mu.RUnlock()
	if !stillCurrent {
		return ErrTargetTransitionSuperseded
	}
	if err != nil {
		return err
	}
	if status.Busy() {
		return &TargetBusyError{Status: status}
	}
	return nil
}

func (m *TargetManager) beginTransition(target TargetDescriptor, cancel context.CancelFunc) (uint64, error) {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		return 0, ErrTargetManagerClosed
	}
	if m.transitionCancel != nil {
		m.transitionCancel()
	}
	m.stopEventPumpLocked()
	m.generation++
	generation := m.generation
	m.transitionCancel = cancel
	m.transitionGeneration = generation
	m.target = target
	m.lastError = ""
	if target.Kind == TargetRemote && m.current == nil {
		m.state = TargetRemoteConnecting
	} else {
		m.state = TargetSwitching
	}
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
	return generation, nil
}

func (m *TargetManager) finishTransition(generation uint64) {
	m.mu.Lock()
	if m.transitionGeneration == generation {
		m.transitionCancel = nil
		m.transitionGeneration = 0
	}
	m.mu.Unlock()
}

func (m *TargetManager) executeSwitch(ctx context.Context, generation uint64, target TargetDescriptor, reconnecting bool) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if !m.transitionCurrent(generation) {
		return ErrTargetTransitionSuperseded
	}

	m.mu.RLock()
	current := m.current
	m.mu.RUnlock()
	if current != nil && sameTarget(current.Descriptor(), target) {
		if !m.publishCurrent(generation, current) {
			return ErrTargetTransitionSuperseded
		}
		return nil
	}

	if current != nil {
		if err := current.Detach(ctx); err != nil {
			if targetDetachCommitted(err) {
				// The peer-side detach already committed, so restoring this
				// adapter would publish a physically closed target as connected.
				// Record that truth even if a newer transition superseded this
				// one while Detach was in flight; lifecycleMu keeps the newer
				// connector from publishing until this cleanup is visible.
				m.mu.Lock()
				m.current = nil
				m.currentGeneration = 0
				m.mu.Unlock()
				if !m.transitionCurrent(generation) {
					return ErrTargetTransitionSuperseded
				}
				m.failTransition(generation, target, err, false)
				return err
			}
			if !m.transitionCurrent(generation) {
				return ErrTargetTransitionSuperseded
			}
			m.restoreCurrent(generation, current, err)
			return err
		}
		// Physical truth is updated even when a newer transition invalidated this
		// one while Detach was in flight. lifecycleMu prevents a new adapter from
		// being published until this cleanup is visible.
		m.mu.Lock()
		m.current = nil
		m.currentGeneration = 0
		m.mu.Unlock()
		if !m.transitionCurrent(generation) {
			return ErrTargetTransitionSuperseded
		}
	}

	m.markConnecting(generation, target)
	var adapter TargetAdapter
	var err error
	if reconnecting {
		m.mu.RLock()
		recovery := m.recovery
		m.mu.RUnlock()
		reconnector, ok := m.connector.(TargetReconnector)
		if !ok || recovery == nil {
			err = ErrTargetReconnectUnsupported
		} else {
			adapter, err = reconnector.Reconnect(ctx, target, recovery)
		}
	} else {
		adapter, err = m.connector.Connect(ctx, target)
	}
	if err != nil {
		if !m.transitionCurrent(generation) {
			return ErrTargetTransitionSuperseded
		}
		m.failTransition(generation, target, err, reconnecting)
		return err
	}
	if adapter == nil {
		err = errors.New("target connector returned a nil adapter")
		if m.transitionCurrent(generation) {
			m.failTransition(generation, target, err, reconnecting)
			return err
		}
		return ErrTargetTransitionSuperseded
	}
	if !m.transitionCurrent(generation) {
		m.cleanupStaleAdapter(adapter)
		return ErrTargetTransitionSuperseded
	}
	actual := adapter.Descriptor()
	if validateErr := actual.Validate(); validateErr != nil || !sameTarget(actual, target) || adapter.RuntimeAPI() == nil {
		switch {
		case validateErr != nil:
			err = fmt.Errorf("connected target: %w", validateErr)
		case !sameTarget(actual, target):
			err = fmt.Errorf("connector returned target %s/%s, want %s/%s", actual.Kind, actual.ID, target.Kind, target.ID)
		default:
			err = errors.New("connected target RuntimeAPI is required")
		}
		m.cleanupStaleAdapter(adapter)
		if m.transitionCurrent(generation) {
			m.failTransition(generation, target, err, reconnecting)
			return err
		}
		return ErrTargetTransitionSuperseded
	}
	if !m.publishCurrent(generation, adapter) {
		m.cleanupStaleAdapter(adapter)
		return ErrTargetTransitionSuperseded
	}
	return nil
}

// targetDetachCommitted is intentionally structural so transport adapters can
// report a peer-side detach commit without TargetManager importing an adapter
// implementation package. Ordinary detach failures do not satisfy it and keep
// the current adapter recoverable.
func targetDetachCommitted(err error) bool {
	var committed interface{ DetachCommitted() bool }
	return errors.As(err, &committed) && committed.DetachCommitted()
}

func (m *TargetManager) transitionCurrent(generation uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.generation == generation && m.transitionGeneration == generation
}

func (m *TargetManager) markConnecting(generation uint64, target TargetDescriptor) {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		return
	}
	m.target = target
	if target.Kind == TargetRemote {
		m.state = TargetRemoteConnecting
	} else {
		m.state = TargetSwitching
	}
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
}

func (m *TargetManager) publishCurrent(generation uint64, adapter TargetAdapter) bool {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.generation != generation || m.transitionGeneration != generation {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		return false
	}
	target := adapter.Descriptor()
	m.current = adapter
	m.currentGeneration = generation
	m.recovery = nil
	m.target = target
	m.state = connectedTargetState(target.Kind)
	m.lastError = ""
	m.startEventPumpLocked(adapter, generation)
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
	return true
}

func (m *TargetManager) restoreCurrent(generation uint64, adapter TargetAdapter, transitionErr error) bool {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.generation != generation || m.transitionGeneration != generation || m.current == nil ||
		!sameTarget(m.current.Descriptor(), adapter.Descriptor()) {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		return false
	}
	target := adapter.Descriptor()
	m.currentGeneration = generation
	m.target = target
	m.state = connectedTargetState(target.Kind)
	if transitionErr != nil {
		m.lastError = transitionErr.Error()
	} else {
		m.lastError = ""
	}
	m.startEventPumpLocked(adapter, generation)
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
	return true
}

func (m *TargetManager) failTransition(generation uint64, target TargetDescriptor, transitionErr error, reconnecting bool) {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		return
	}
	m.current = nil
	m.currentGeneration = 0
	m.target = target
	if reconnecting && target.Kind == TargetRemote && m.recovery != nil {
		m.state = TargetRemoteReconnecting
	} else {
		m.state = TargetDisconnected
	}
	if transitionErr != nil {
		m.lastError = transitionErr.Error()
	}
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
}

func (m *TargetManager) cleanupStaleAdapter(adapter TargetAdapter) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = adapter.Detach(ctx)
}

func (m *TargetManager) startEventPumpLocked(adapter TargetAdapter, generation uint64) {
	m.stopEventPumpLocked()
	api := adapter.RuntimeAPI()
	if api == nil {
		return
	}
	events := api.Events()
	var faults <-chan error
	if source, ok := adapter.(TargetFaultSource); ok {
		faults = source.Faults()
	}
	if events == nil && faults == nil {
		return
	}
	stop := make(chan struct{})
	m.eventStop = stop
	go m.pumpEvents(adapter.Descriptor(), generation, stop, events, faults)
}

func (m *TargetManager) stopEventPumpLocked() {
	if m.eventStop != nil {
		close(m.eventStop)
		m.eventStop = nil
	}
}

func (m *TargetManager) pumpEvents(
	target TargetDescriptor,
	generation uint64,
	stop <-chan struct{},
	events <-chan runtimeapi.Event,
	faults <-chan error,
) {
	for {
		select {
		case <-stop:
			return
		case transportErr, ok := <-faults:
			if !ok {
				faults = nil
				if events == nil {
					return
				}
				continue
			}
			if transportErr == nil {
				continue
			}
			m.ReportTransportLost(generation, transportErr)
			return
		case eventValue, ok := <-events:
			if !ok {
				// A Remote client normally publishes its concrete terminal error
				// before closing Events. Prefer that diagnostic if both channels
				// become ready together; Events closure remains the neutral fallback.
				transportErr := ErrTargetEventStreamClosed
				if faults != nil {
					select {
					case fault, faultOK := <-faults:
						if faultOK && fault != nil {
							transportErr = fault
						}
					default:
					}
				}
				m.ReportTransportLost(generation, transportErr)
				return
			}
			m.deliverEvent(target, generation, eventValue)
		}
	}
}

func (m *TargetManager) deliverEvent(target TargetDescriptor, generation uint64, eventValue runtimeapi.Event) bool {
	m.eventDispatchMu.Lock()
	defer m.eventDispatchMu.Unlock()
	m.mu.RLock()
	valid := m.generation == generation && m.currentGeneration == generation &&
		(m.state == TargetLocalConnected || m.state == TargetRemoteConnected) && sameTarget(m.target, target)
	sink := m.eventSink
	m.mu.RUnlock()
	if !valid || sink == nil {
		return false
	}
	sink(TargetRuntimeEvent{Generation: generation, Target: target, Event: eventValue})
	return true
}

// ReportTransportLost atomically invalidates the connected adapter generation.
// A Remote loss retains Remote identity and enters RemoteReconnecting; it never
// installs or exposes a Local adapter. The reporting adapter has already closed
// its failed transport, so no graceful detach is attempted here.
func (m *TargetManager) ReportTransportLost(generation uint64, transportErr error) bool {
	m.stateDispatchMu.Lock()
	defer m.stateDispatchMu.Unlock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.closed || m.generation != generation || m.currentGeneration != generation || m.current == nil {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		return false
	}
	target := m.current.Descriptor()
	m.stopEventPumpLocked()
	if target.Kind == TargetRemote {
		m.recovery = m.current
	} else {
		m.recovery = nil
	}
	m.current = nil
	m.currentGeneration = 0
	m.generation++
	m.target = target
	if target.Kind == TargetRemote {
		m.state = TargetRemoteReconnecting
	} else {
		m.state = TargetDisconnected
	}
	if transportErr != nil {
		m.lastError = transportErr.Error()
	} else {
		m.lastError = ""
	}
	if m.transitionCancel != nil {
		m.transitionCancel()
		m.transitionCancel = nil
		m.transitionGeneration = 0
	}
	snapshot := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(snapshot)
	}
	return true
}

// Shutdown permanently closes the manager, cancels any connection transition,
// and tears down adapter-owned resources without switching to another target.
// A Remote graceful detach is attempted once; an unknown outcome preserves the
// saved lease and then abandons only the local SSH/client resources.
func (m *TargetManager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Invalidate and cancel in-flight work before waiting for lifecycleMu. The
	// older transition observes the new generation and cleans up any adapter it
	// constructed before releasing the lifecycle lock.
	m.stateDispatchMu.Lock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.eventDispatchMu.Unlock()
		m.stateDispatchMu.Unlock()
		return nil
	}
	m.closed = true
	if m.transitionCancel != nil {
		m.transitionCancel()
	}
	m.transitionCancel = nil
	m.transitionGeneration = 0
	m.stopEventPumpLocked()
	m.generation++
	m.currentGeneration = 0
	m.state = TargetSwitching
	switching := m.snapshotLocked()
	sink := m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(switching)
	}
	m.stateDispatchMu.Unlock()

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.Lock()
	current := m.current
	recovery := m.recovery
	m.current = nil
	m.currentGeneration = 0
	m.recovery = nil
	m.mu.Unlock()

	var shutdownErr error
	if current != nil {
		if err := current.Detach(ctx); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
			if !targetDetachCommitted(err) {
				shutdownErr = errors.Join(shutdownErr, abandonTarget(current))
			}
		}
	}
	if recovery != nil && recovery != current {
		shutdownErr = errors.Join(shutdownErr, abandonTarget(recovery))
	}

	m.stateDispatchMu.Lock()
	m.eventDispatchMu.Lock()
	m.mu.Lock()
	m.state = TargetDisconnected
	if shutdownErr != nil {
		m.lastError = shutdownErr.Error()
	} else {
		m.lastError = ""
	}
	disconnected := m.snapshotLocked()
	sink = m.stateSink
	m.mu.Unlock()
	m.eventDispatchMu.Unlock()
	if sink != nil {
		sink(disconnected)
	}
	m.stateDispatchMu.Unlock()
	return shutdownErr
}

func abandonTarget(adapter TargetAdapter) error {
	if adapter == nil {
		return nil
	}
	abandoner, ok := adapter.(TargetAbandoner)
	if !ok {
		return nil
	}
	return abandoner.AbandonTarget()
}

func (m *TargetManager) Reconnect(ctx context.Context) error {
	snapshot := m.Snapshot()
	if snapshot.State != TargetRemoteReconnecting || snapshot.Target.Kind != TargetRemote {
		return fmt.Errorf("%w: target is not awaiting Remote reconnection", ErrRuntimeTargetUnavailable)
	}
	return m.Switch(ctx, snapshot.Target, SwitchTargetOptions{})
}
