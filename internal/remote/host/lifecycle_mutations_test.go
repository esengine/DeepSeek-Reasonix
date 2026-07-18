package host

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

func newLifecycleRegistry(t *testing.T) *idempotency.Registry {
	t.Helper()
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func closeParams(requestID protocol.RequestID, epoch protocol.RuntimeEpoch) protocol.SessionCloseParams {
	return protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: requestID, ExpectedHostEpoch: "host-test", Target: testTarget(), ExpectedRuntimeEpoch: epoch,
	}}
}

func TestSessionCloseSnapshotsBeforeReleaseAndCachesAdmission(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry := newLifecycleRegistry(t)
	params := closeParams("close-snapshot", runtime.Epoch())
	result, replayed, err := manager.CloseSessionMutation(context.Background(), registry, params, nil)
	if err != nil || result.Disposition != protocol.SessionReleased {
		t.Fatalf("CloseSessionMutation = %+v, %v", result, err)
	}
	if replayed {
		t.Fatal("first close was reported as a replay")
	}
	controller := factory.controller(0)
	controller.mu.Lock()
	snapshots, closes := controller.snapshotCalls, controller.closeCalls
	controller.mu.Unlock()
	if snapshots != 1 || closes != 1 {
		t.Fatalf("snapshot/close calls = %d/%d, want 1/1", snapshots, closes)
	}
	if _, exists := manager.Runtime(testTarget()); exists {
		t.Fatal("released runtime remained registered")
	}
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionClose), Target: idempotency.SessionTarget(params.Target), Params: params}
	attempt, found, err := registry.Lookup(request)
	if err != nil || !found {
		t.Fatalf("cached close lookup = found %v, %v", found, err)
	}
	outcome, err := attempt.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var replay protocol.SessionCloseResult
	if err := outcome.Decode(&replay); err != nil || replay != result {
		t.Fatalf("cached close = %+v, %v", replay, err)
	}
}

func TestSessionCloseSnapshotFailureAbortsRequestIDAndCanRetry(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	controller.mu.Lock()
	controller.snapshotErr = errors.New("disk full")
	controller.mu.Unlock()
	registry := newLifecycleRegistry(t)
	params := closeParams("close-retry", runtime.Epoch())
	_, _, err = manager.CloseSessionMutation(context.Background(), registry, params, nil)
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != protocol.ErrSessionPersistFailed {
		t.Fatalf("snapshot failure = %v", err)
	}
	if current, exists := manager.Runtime(testTarget()); !exists || current != runtime {
		t.Fatal("snapshot failure released the runtime")
	}
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionClose), Target: idempotency.SessionTarget(params.Target), Params: params}
	if _, found, lookupErr := registry.Lookup(request); lookupErr != nil || found {
		t.Fatalf("snapshot failure was cached: found=%v err=%v", found, lookupErr)
	}
	controller.mu.Lock()
	controller.snapshotErr = nil
	controller.mu.Unlock()
	result, _, err := manager.CloseSessionMutation(context.Background(), registry, params, nil)
	if err != nil || result.Disposition != protocol.SessionReleased {
		t.Fatalf("retry = %+v, %v", result, err)
	}
}

func TestSessionCloseRetainsActiveRuntimeWithoutSnapshot(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(1)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := manager.CloseSessionMutation(context.Background(), newLifecycleRegistry(t), closeParams("close-active", runtime.Epoch()), nil)
	if err != nil || result.Disposition != protocol.SessionRetainedActive {
		t.Fatalf("active close = %+v, %v", result, err)
	}
	controller := factory.controller(0)
	controller.mu.Lock()
	snapshots, closes := controller.snapshotCalls, controller.closeCalls
	controller.mu.Unlock()
	if snapshots != 0 || closes != 0 {
		t.Fatalf("active close snapshot/close = %d/%d", snapshots, closes)
	}
	if err := runtime.Unsubscribe(context.Background(), attachment, subscription.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCloseReservationOrdersSubscribeCommitAndAbort(t *testing.T) {
	t.Run("commit wins", func(t *testing.T) {
		manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		reservation, err := manager.ReserveIdleWorkspace(testTarget().WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, subscribeErr := runtime.Subscribe(context.Background(), testAttachment(2), "")
			done <- subscribeErr
		}()
		select {
		case err := <-done:
			t.Fatalf("Subscribe crossed reservation before commit: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		if err := reservation.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, ErrRuntimeClosed) {
			t.Fatalf("Subscribe after committed close = %v", err)
		}
	})

	t.Run("abort restores admission", func(t *testing.T) {
		manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		reservation, err := manager.ReserveIdleWorkspace(testTarget().WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			sub Subscription
			err error
		}
		done := make(chan result, 1)
		attachment := testAttachment(3)
		go func() {
			sub, subscribeErr := runtime.Subscribe(context.Background(), attachment, "")
			done <- result{sub: sub, err: subscribeErr}
		}()
		reservation.Abort()
		subscribed := <-done
		if subscribed.err != nil {
			t.Fatalf("Subscribe after abort = %v", subscribed.err)
		}
		if err := runtime.Unsubscribe(context.Background(), attachment, subscribed.sub.ID); err != nil {
			t.Fatal(err)
		}
	})
}
