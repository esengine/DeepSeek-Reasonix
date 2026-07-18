package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/eventwire"
	"reasonix/internal/runtimeapi"
)

type targetManagerTestRuntime struct {
	events         chan runtimeapi.Event
	attachSnapshot runtimeapi.SessionSnapshot
	attachCalls    atomic.Int32
}

func newTargetManagerTestRuntime() *targetManagerTestRuntime {
	return &targetManagerTestRuntime{events: make(chan runtimeapi.Event, 8)}
}

func (r *targetManagerTestRuntime) Connection(context.Context) (runtimeapi.ConnectionView, error) {
	return runtimeapi.ConnectionView{Label: "test"}, nil
}

func (r *targetManagerTestRuntime) BrowseWorkspace(context.Context, runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	return runtimeapi.WorkspacePage{}, runtimeapi.Unavailable(runtimeapi.CapabilityWorkspaceBrowse, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) OpenWorkspace(context.Context, runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	return runtimeapi.OpenWorkspaceResult{}, runtimeapi.Unavailable(runtimeapi.CapabilityWorkspaceBrowse, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) CreateSession(context.Context, runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	return runtimeapi.CreatedSession{}, runtimeapi.Unavailable(runtimeapi.CapabilitySessionCreate, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) AttachAndSubscribe(context.Context, runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	r.attachCalls.Add(1)
	return r.attachSnapshot, nil
}

func (r *targetManagerTestRuntime) ComposerSubmit(context.Context, runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	return runtimeapi.ComposerSubmitResult{}, runtimeapi.Unavailable(runtimeapi.CapabilityComposerSubmit, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) SteerTurn(context.Context, runtimeapi.SteerInput) error {
	return runtimeapi.Unavailable(runtimeapi.CapabilityTurnSteer, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) CancelTurn(context.Context, runtimeapi.CancelTurnInput) error {
	return runtimeapi.Unavailable(runtimeapi.CapabilityTurnCancel, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) ApprovePrompt(context.Context, runtimeapi.ApproveInput) error {
	return runtimeapi.Unavailable(runtimeapi.CapabilityPromptApprove, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) AnswerPrompt(context.Context, runtimeapi.AnswerInput) error {
	return runtimeapi.Unavailable(runtimeapi.CapabilityPromptAnswer, "not used by TargetManager test")
}

func (r *targetManagerTestRuntime) Events() <-chan runtimeapi.Event { return r.events }

type targetManagerTestAdapter struct {
	target       TargetDescriptor
	runtime      *targetManagerTestRuntime
	faults       <-chan error
	canRelease   func(context.Context) (ReleaseStatus, error)
	detach       func(context.Context) error
	abandon      func() error
	detachCalls  atomic.Int32
	abandonCalls atomic.Int32
	releaseCalls atomic.Int32
}

func newTargetManagerTestAdapter(target TargetDescriptor) *targetManagerTestAdapter {
	return &targetManagerTestAdapter{target: target, runtime: newTargetManagerTestRuntime()}
}

func (a *targetManagerTestAdapter) Descriptor() TargetDescriptor      { return a.target }
func (a *targetManagerTestAdapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a.runtime }
func (a *targetManagerTestAdapter) Faults() <-chan error              { return a.faults }

func (a *targetManagerTestAdapter) CanRelease(ctx context.Context) (ReleaseStatus, error) {
	a.releaseCalls.Add(1)
	if a.canRelease != nil {
		return a.canRelease(ctx)
	}
	return ReleaseStatus{}, nil
}

func (a *targetManagerTestAdapter) Detach(ctx context.Context) error {
	a.detachCalls.Add(1)
	if a.detach != nil {
		return a.detach(ctx)
	}
	return nil
}

func (a *targetManagerTestAdapter) AbandonTarget() error {
	a.abandonCalls.Add(1)
	if a.abandon != nil {
		return a.abandon()
	}
	return nil
}

type targetManagerTestConnector struct {
	connectFn   func(context.Context, TargetDescriptor) (TargetAdapter, error)
	reconnectFn func(context.Context, TargetDescriptor, TargetAdapter) (TargetAdapter, error)
}

type targetManagerCommittedDetachError struct{ err error }

func (e *targetManagerCommittedDetachError) Error() string         { return e.err.Error() }
func (e *targetManagerCommittedDetachError) Unwrap() error         { return e.err }
func (e *targetManagerCommittedDetachError) DetachCommitted() bool { return true }

func (c *targetManagerTestConnector) Connect(ctx context.Context, target TargetDescriptor) (TargetAdapter, error) {
	return c.connectFn(ctx, target)
}

func (c *targetManagerTestConnector) Reconnect(ctx context.Context, target TargetDescriptor, previous TargetAdapter) (TargetAdapter, error) {
	return c.reconnectFn(ctx, target, previous)
}

func TestTargetManagerBusyLocalPreflightPreservesGenerationAndEvents(t *testing.T) {
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local", Label: "Local"}
	remoteTarget := TargetDescriptor{Kind: TargetRemote, ID: "host-a", Label: "Host A"}
	local := newTargetManagerTestAdapter(localTarget)
	releaseEntered := make(chan struct{})
	releaseResult := make(chan struct{})
	local.canRelease = func(context.Context) (ReleaseStatus, error) {
		close(releaseEntered)
		<-releaseResult
		return ReleaseStatus{Blockers: []ReleaseBlocker{{Kind: ReleaseRuntimeRunning, Detail: "turn is running"}}}, nil
	}
	var connectCalls atomic.Int32
	connector := TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		connectCalls.Add(1)
		return nil, errors.New("must not connect")
	})
	sink := make(chan TargetRuntimeEvent, 2)
	manager, err := NewTargetManager(connector, local, TargetManagerOptions{EventSink: func(event TargetRuntimeEvent) {
		sink <- event
	}})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	result := make(chan error, 1)
	go func() { result <- manager.Switch(context.Background(), remoteTarget, SwitchTargetOptions{}) }()
	waitSignal(t, releaseEntered, "Local CanRelease")
	local.runtime.events <- runtimeEvent("event during preflight")
	delivered := waitValue(t, sink, "event during Local preflight")
	if delivered.Generation != 1 || delivered.Event.Value.Text != "event during preflight" {
		t.Fatalf("delivered event = %#v", delivered)
	}
	close(releaseResult)
	if err := waitValue(t, result, "busy switch result"); !errors.Is(err, ErrTargetBusy) {
		t.Fatalf("Switch error = %v, want ErrTargetBusy", err)
	}

	snapshot := manager.Snapshot()
	if snapshot.State != TargetLocalConnected || snapshot.Generation != 1 || !sameTarget(snapshot.Target, localTarget) {
		t.Fatalf("snapshot after busy preflight = %#v", snapshot)
	}
	if local.detachCalls.Load() != 0 || connectCalls.Load() != 0 {
		t.Fatalf("detach/connect calls = %d/%d, want 0/0", local.detachCalls.Load(), connectCalls.Load())
	}
	if api, err := manager.RuntimeAPI(); err != nil || api != local.runtime {
		t.Fatalf("RuntimeAPI after busy preflight = %T, %v", api, err)
	}
}

func TestTargetManagerLocalToRemoteDetachesBeforeConnect(t *testing.T) {
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	remoteTarget := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	local := newTargetManagerTestAdapter(localTarget)
	remote := newTargetManagerTestAdapter(remoteTarget)
	var orderMu sync.Mutex
	var order []string
	local.detach = func(context.Context) error {
		orderMu.Lock()
		order = append(order, "detach-local")
		orderMu.Unlock()
		return nil
	}
	connectEntered := make(chan struct{})
	allowConnect := make(chan struct{})
	connector := TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		orderMu.Lock()
		order = append(order, "connect-remote")
		orderMu.Unlock()
		close(connectEntered)
		<-allowConnect
		return remote, nil
	})
	manager, err := NewTargetManager(connector, local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	result := make(chan error, 1)
	go func() { result <- manager.Switch(context.Background(), remoteTarget, SwitchTargetOptions{}) }()
	waitSignal(t, connectEntered, "Remote connector")
	snapshot := manager.Snapshot()
	if snapshot.State != TargetRemoteConnecting || snapshot.Generation != 2 || !sameTarget(snapshot.Target, remoteTarget) {
		t.Fatalf("connecting snapshot = %#v", snapshot)
	}
	orderMu.Lock()
	gotOrder := append([]string(nil), order...)
	orderMu.Unlock()
	if fmt.Sprint(gotOrder) != "[detach-local connect-remote]" {
		t.Fatalf("operation order = %v", gotOrder)
	}
	close(allowConnect)
	if err := waitValue(t, result, "Remote switch result"); err != nil {
		t.Fatal(err)
	}
	snapshot = manager.Snapshot()
	if snapshot.State != TargetRemoteConnected || snapshot.Generation != 2 || !sameTarget(snapshot.Target, remoteTarget) {
		t.Fatalf("connected snapshot = %#v", snapshot)
	}
}

func TestTargetManagerStateSinkPublishesOrderedConnectionAndLossStates(t *testing.T) {
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	remoteTarget := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	local := newTargetManagerTestAdapter(localTarget)
	remote := newTargetManagerTestAdapter(remoteTarget)
	remoteFaults := make(chan error, 1)
	remote.faults = remoteFaults

	var statesMu sync.Mutex
	states := make([]TargetManagerSnapshot, 0, 8)
	manager, err := NewTargetManager(
		TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) { return remote, nil }),
		local,
		TargetManagerOptions{StateSink: func(snapshot TargetManagerSnapshot) {
			statesMu.Lock()
			states = append(states, snapshot)
			statesMu.Unlock()
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)
	if err := manager.Switch(context.Background(), remoteTarget, SwitchTargetOptions{}); err != nil {
		t.Fatal(err)
	}
	remoteFaults <- errors.New("network lost")
	waitSnapshot(t, manager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.State == TargetRemoteReconnecting
	}, "Remote reconnecting state")

	statesMu.Lock()
	got := append([]TargetManagerSnapshot(nil), states...)
	statesMu.Unlock()
	wantOrdered := []TargetState{TargetLocalConnected, TargetSwitching, TargetRemoteConnecting, TargetRemoteConnected, TargetRemoteReconnecting}
	next := 0
	for _, snapshot := range got {
		if next < len(wantOrdered) && snapshot.State == wantOrdered[next] {
			next++
		}
	}
	if next != len(wantOrdered) {
		t.Fatalf("state sink sequence = %+v, missing ordered suffix %v", got, wantOrdered[next:])
	}
	if last := got[len(got)-1]; last.State != TargetRemoteReconnecting || last.Generation != 3 || last.LastError != "network lost" {
		t.Fatalf("last state sink snapshot = %#v", last)
	}
}

func TestTargetManagerConnectFailureDoesNotFallBackToLocal(t *testing.T) {
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	remoteTarget := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	local := newTargetManagerTestAdapter(localTarget)
	connectErr := errors.New("SSH connection refused")
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, connectErr
	}), local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	if err := manager.Switch(context.Background(), remoteTarget, SwitchTargetOptions{}); !errors.Is(err, connectErr) {
		t.Fatalf("Switch error = %v, want %v", err, connectErr)
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetDisconnected || snapshot.Generation != 2 || !sameTarget(snapshot.Target, remoteTarget) || snapshot.LastError != connectErr.Error() {
		t.Fatalf("failed snapshot = %#v", snapshot)
	}
	if _, err := manager.RuntimeAPI(); !errors.Is(err, ErrRuntimeTargetUnavailable) {
		t.Fatalf("RuntimeAPI error = %v", err)
	}
	if local.detachCalls.Load() != 1 {
		t.Fatalf("Local Detach calls = %d, want 1", local.detachCalls.Load())
	}
}

func TestTargetManagerRemoteToLocalRequiresConfirmation(t *testing.T) {
	remoteTarget := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	remote := newTargetManagerTestAdapter(remoteTarget)
	local := newTargetManagerTestAdapter(localTarget)
	connector := TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) { return local, nil })
	manager, err := NewTargetManager(connector, remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	if err := manager.Switch(context.Background(), localTarget, SwitchTargetOptions{}); !errors.Is(err, ErrRemoteDetachConfirmation) {
		t.Fatalf("unconfirmed Switch error = %v", err)
	}
	if snapshot := manager.Snapshot(); snapshot.State != TargetRemoteConnected || snapshot.Generation != 1 {
		t.Fatalf("snapshot after rejected switch = %#v", snapshot)
	}
	if remote.detachCalls.Load() != 0 {
		t.Fatalf("Remote Detach calls = %d, want 0", remote.detachCalls.Load())
	}
	if err := manager.Switch(context.Background(), localTarget, SwitchTargetOptions{ConfirmRemoteDetach: true}); err != nil {
		t.Fatal(err)
	}
	if snapshot := manager.Snapshot(); snapshot.State != TargetLocalConnected || snapshot.Generation != 2 || !sameTarget(snapshot.Target, localTarget) {
		t.Fatalf("local snapshot = %#v", snapshot)
	}
	if remote.detachCalls.Load() != 1 {
		t.Fatalf("Remote Detach calls = %d, want 1", remote.detachCalls.Load())
	}
}

func TestTargetManagerHostSwitchDetachFailureRestoresHostA(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	hostB := TargetDescriptor{Kind: TargetRemote, ID: "host-b"}
	adapterA := newTargetManagerTestAdapter(hostA)
	detachErr := errors.New("remote detach rejected")
	adapterA.detach = func(context.Context) error { return detachErr }
	var connectCalls atomic.Int32
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		connectCalls.Add(1)
		return newTargetManagerTestAdapter(hostB), nil
	}), adapterA, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	if err := manager.Switch(context.Background(), hostB, SwitchTargetOptions{}); !errors.Is(err, detachErr) {
		t.Fatalf("Switch error = %v, want %v", err, detachErr)
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetRemoteConnected || snapshot.Generation != 2 || !sameTarget(snapshot.Target, hostA) || snapshot.LastError != detachErr.Error() {
		t.Fatalf("restored snapshot = %#v", snapshot)
	}
	if connectCalls.Load() != 0 {
		t.Fatalf("Host B Connect calls = %d, want 0", connectCalls.Load())
	}
	if api, err := manager.RuntimeAPI(); err != nil || api != adapterA.runtime {
		t.Fatalf("restored RuntimeAPI = %T, %v", api, err)
	}
}

func TestTargetManagerCommittedDetachFailureDoesNotRestoreClosedAdapter(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	adapterA := newTargetManagerTestAdapter(hostA)
	persistErr := errors.New("persist cleared lease: disk full")
	committedErr := &targetManagerCommittedDetachError{err: persistErr}
	adapterA.detach = func(context.Context) error { return committedErr }
	var connectCalls atomic.Int32
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		connectCalls.Add(1)
		return newTargetManagerTestAdapter(localTarget), nil
	}), adapterA, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	err = manager.Switch(context.Background(), localTarget, SwitchTargetOptions{ConfirmRemoteDetach: true})
	if !errors.Is(err, persistErr) || !targetDetachCommitted(err) {
		t.Fatalf("Switch error = %v, want committed persistence failure", err)
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetDisconnected || snapshot.Generation != 2 || !sameTarget(snapshot.Target, localTarget) || snapshot.LastError != committedErr.Error() {
		t.Fatalf("snapshot after committed detach failure = %#v", snapshot)
	}
	if _, err := manager.RuntimeAPI(); !errors.Is(err, ErrRuntimeTargetUnavailable) {
		t.Fatalf("RuntimeAPI error = %v, want unavailable", err)
	}
	if connectCalls.Load() != 0 {
		t.Fatalf("Local connector calls = %d, want 0 after persistence failure", connectCalls.Load())
	}
}

func TestTargetManagerSupersededConnectDropsLateAdapterABA(t *testing.T) {
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	initialLocal := newTargetManagerTestAdapter(localTarget)
	staleRemote := newTargetManagerTestAdapter(hostA)
	replacementLocal := newTargetManagerTestAdapter(localTarget)
	remoteEntered := make(chan struct{})
	releaseRemote := make(chan struct{})
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if target.Kind == TargetRemote {
			close(remoteEntered)
			<-releaseRemote // deliberately ignore cancellation to model a late result
			return staleRemote, nil
		}
		return replacementLocal, nil
	})
	manager, err := NewTargetManager(connector, initialLocal, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	first := make(chan error, 1)
	go func() { first <- manager.Switch(context.Background(), hostA, SwitchTargetOptions{}) }()
	waitSignal(t, remoteEntered, "Host A connector")
	second := make(chan error, 1)
	go func() { second <- manager.Switch(context.Background(), localTarget, SwitchTargetOptions{}) }()
	waitSnapshot(t, manager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.Generation == 3 && sameTarget(snapshot.Target, localTarget)
	}, "second Local transition")
	close(releaseRemote)
	if err := waitValue(t, first, "superseded Host A switch"); !errors.Is(err, ErrTargetTransitionSuperseded) {
		t.Fatalf("first Switch error = %v", err)
	}
	if err := waitValue(t, second, "replacement Local switch"); err != nil {
		t.Fatalf("second Switch error = %v", err)
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetLocalConnected || snapshot.Generation != 3 || !sameTarget(snapshot.Target, localTarget) {
		t.Fatalf("final snapshot = %#v", snapshot)
	}
	if staleRemote.detachCalls.Load() != 1 {
		t.Fatalf("late Host A adapter Detach calls = %d, want 1", staleRemote.detachCalls.Load())
	}
	if api, err := manager.RuntimeAPI(); err != nil || api != replacementLocal.runtime {
		t.Fatalf("final RuntimeAPI = %T, %v", api, err)
	}
}

func TestTargetManagerDropsLateEventsAfterReturningToSameTarget(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: "local"}
	oldRemote := newTargetManagerTestAdapter(hostA)
	local := newTargetManagerTestAdapter(localTarget)
	newRemote := newTargetManagerTestAdapter(hostA)
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if target.Kind == TargetLocal {
			return local, nil
		}
		return newRemote, nil
	})
	sink := make(chan TargetRuntimeEvent, 4)
	manager, err := NewTargetManager(connector, oldRemote, TargetManagerOptions{EventSink: func(event TargetRuntimeEvent) { sink <- event }})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	if err := manager.Switch(context.Background(), localTarget, SwitchTargetOptions{ConfirmRemoteDetach: true}); err != nil {
		t.Fatal(err)
	}
	oldRemote.runtime.events <- runtimeEvent("late generation one")
	if err := manager.Switch(context.Background(), hostA, SwitchTargetOptions{}); err != nil {
		t.Fatal(err)
	}
	oldRemote.runtime.events <- runtimeEvent("late ABA event")
	newRemote.runtime.events <- runtimeEvent("current event")
	delivered := waitValue(t, sink, "current Host event")
	if delivered.Generation != 3 || delivered.Event.Value.Text != "current event" {
		t.Fatalf("delivered event = %#v", delivered)
	}
	select {
	case extra := <-sink:
		t.Fatalf("unexpected late event delivered: %#v", extra)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestTargetManagerPrefersConcreteFaultAndReportsLossOnce(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	remote := newTargetManagerTestAdapter(hostA)
	faults := make(chan error, 1)
	remote.faults = faults
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unused")
	}), remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	connectionErr := errors.New("remote connection failure: host key mismatch")
	faults <- connectionErr
	close(remote.runtime.events)
	snapshot := waitSnapshot(t, manager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.State == TargetRemoteReconnecting
	}, "Remote transport loss")
	if snapshot.Generation != 2 || !snapshot.RecoveryAvailable || snapshot.LastError != connectionErr.Error() {
		t.Fatalf("transport-loss snapshot = %#v", snapshot)
	}
	if remote.detachCalls.Load() != 0 {
		t.Fatalf("failed adapter Detach calls = %d, want 0", remote.detachCalls.Load())
	}
	time.Sleep(10 * time.Millisecond)
	if got := manager.Snapshot().Generation; got != 2 {
		t.Fatalf("loss reported more than once, generation = %d", got)
	}
}

func TestTargetManagerEventCloseIsTransportLossFallback(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	remote := newTargetManagerTestAdapter(hostA)
	remote.faults = make(chan error)
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unused")
	}), remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	close(remote.runtime.events)
	snapshot := waitSnapshot(t, manager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.State == TargetRemoteReconnecting
	}, "closed event stream")
	if snapshot.LastError != ErrTargetEventStreamClosed.Error() || snapshot.Generation != 2 || !snapshot.RecoveryAvailable {
		t.Fatalf("event-close snapshot = %#v", snapshot)
	}
}

func TestTargetManagerReconnectPreservesFailedAdapterAndPublishesFreshRuntime(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	failed := newTargetManagerTestAdapter(hostA)
	replacement := newTargetManagerTestAdapter(hostA)
	replacement.runtime.attachSnapshot = runtimeapi.SessionSnapshot{
		Session: runtimeapi.SessionRef{WorkspaceID: "workspace", SessionID: "session"},
		Title:   "fresh atomic snapshot",
	}
	previous := make(chan TargetAdapter, 1)
	connector := &targetManagerTestConnector{
		connectFn: func(context.Context, TargetDescriptor) (TargetAdapter, error) {
			return nil, errors.New("Reconnect must not call Connect")
		},
		reconnectFn: func(ctx context.Context, _ TargetDescriptor, old TargetAdapter) (TargetAdapter, error) {
			previous <- old
			if _, err := replacement.runtime.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
				Session: runtimeapi.SessionRef{WorkspaceID: "workspace", SessionID: "session"},
			}); err != nil {
				return nil, err
			}
			return replacement, nil
		},
	}
	manager, err := NewTargetManager(connector, failed, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)

	lossErr := errors.New("connection reset")
	if !manager.ReportTransportLost(1, lossErr) {
		t.Fatal("ReportTransportLost returned false")
	}
	if err := manager.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := waitValue(t, previous, "failed adapter passed to Reconnect"); got != failed {
		t.Fatalf("Reconnect previous adapter = %T %p, want %p", got, got, failed)
	}
	if replacement.runtime.attachCalls.Load() != 1 {
		t.Fatalf("fresh AttachAndSubscribe calls = %d, want 1", replacement.runtime.attachCalls.Load())
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetRemoteConnected || snapshot.Generation != 3 || snapshot.RecoveryAvailable || snapshot.LastError != "" {
		t.Fatalf("reconnected snapshot = %#v", snapshot)
	}
	api, err := manager.RuntimeAPI()
	if err != nil || api != replacement.runtime {
		t.Fatalf("reconnected RuntimeAPI = %T, %v", api, err)
	}
}

func TestTargetManagerReconnectFailureKeepsRemoteRecovery(t *testing.T) {
	hostA := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	failed := newTargetManagerTestAdapter(hostA)
	reconnectErr := errors.New("resume lease rejected")
	connector := &targetManagerTestConnector{
		connectFn: func(context.Context, TargetDescriptor) (TargetAdapter, error) { return nil, errors.New("unused") },
		reconnectFn: func(context.Context, TargetDescriptor, TargetAdapter) (TargetAdapter, error) {
			return nil, reconnectErr
		},
	}
	manager, err := NewTargetManager(connector, failed, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTargetManager(t, manager)
	if !manager.ReportTransportLost(1, errors.New("connection reset")) {
		t.Fatal("ReportTransportLost returned false")
	}
	if err := manager.Reconnect(context.Background()); !errors.Is(err, reconnectErr) {
		t.Fatalf("Reconnect error = %v, want %v", err, reconnectErr)
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetRemoteReconnecting || snapshot.Generation != 3 || !snapshot.RecoveryAvailable || snapshot.LastError != reconnectErr.Error() {
		t.Fatalf("failed reconnect snapshot = %#v", snapshot)
	}
	if _, err := manager.RuntimeAPI(); !errors.Is(err, ErrRuntimeTargetUnavailable) {
		t.Fatalf("RuntimeAPI error = %v", err)
	}
}

func TestTargetManagerShutdownAbandonsUnknownRemoteDetachAndClosesManager(t *testing.T) {
	host := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	remote := newTargetManagerTestAdapter(host)
	detachErr := errors.New("SSH outcome unknown")
	remote.detach = func(context.Context) error { return detachErr }
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("must not connect")
	}), remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Shutdown(context.Background()); !errors.Is(err, detachErr) {
		t.Fatalf("Shutdown error = %v, want %v", err, detachErr)
	}
	if remote.detachCalls.Load() != 1 || remote.abandonCalls.Load() != 1 {
		t.Fatalf("detach/abandon calls = %d/%d, want 1/1", remote.detachCalls.Load(), remote.abandonCalls.Load())
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetDisconnected || snapshot.RecoveryAvailable || snapshot.LastError == "" {
		t.Fatalf("shutdown snapshot = %#v", snapshot)
	}
	if err := manager.Switch(context.Background(), TargetDescriptor{Kind: TargetLocal, ID: "local"}, SwitchTargetOptions{}); !errors.Is(err, ErrTargetManagerClosed) {
		t.Fatalf("Switch after shutdown error = %v, want ErrTargetManagerClosed", err)
	}
	if manager.ReportTransportLost(snapshot.Generation, errors.New("late loss")) {
		t.Fatal("closed manager accepted a late transport loss")
	}
}

func TestTargetManagerShutdownAbandonsRetainedRecoveryWithoutDetach(t *testing.T) {
	host := TargetDescriptor{Kind: TargetRemote, ID: "host-a"}
	remote := newTargetManagerTestAdapter(host)
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unused")
	}), remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !manager.ReportTransportLost(1, errors.New("network lost")) {
		t.Fatal("ReportTransportLost returned false")
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if remote.detachCalls.Load() != 0 || remote.abandonCalls.Load() != 1 {
		t.Fatalf("recovery detach/abandon calls = %d/%d, want 0/1", remote.detachCalls.Load(), remote.abandonCalls.Load())
	}
}

func runtimeEvent(text string) runtimeapi.Event {
	return runtimeapi.Event{
		Session: runtimeapi.SessionRef{WorkspaceID: "workspace", SessionID: "session"},
		Value:   eventwire.Event{Kind: "notice", Text: text},
	}
}

func cleanupTargetManager(t *testing.T, manager *TargetManager) {
	t.Helper()
	t.Cleanup(func() {
		snapshot := manager.Snapshot()
		manager.ReportTransportLost(snapshot.Generation, errors.New("test cleanup"))
	})
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitValue[T any](t *testing.T, values <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func waitSnapshot(t *testing.T, manager *TargetManager, accept func(TargetManagerSnapshot) bool, description string) TargetManagerSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := manager.Snapshot()
		if accept(snapshot) {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; last snapshot = %#v", description, snapshot)
		}
		time.Sleep(time.Millisecond)
	}
}
