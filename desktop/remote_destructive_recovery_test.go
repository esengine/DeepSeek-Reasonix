package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/runtimeapi"
)

func TestRemoteDestructiveMutationsKeepUnknownOutcomesOutOfRecovery(t *testing.T) {
	for _, operation := range remoteDestructiveRecoveryOperations() {
		t.Run(operation.name, func(t *testing.T) {
			adapter, target, started, release, subscribeCalls := newRemoteDestructiveRecoveryAdapter(t, operation, true)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- operation.invoke(ctx, adapter, mapRemoteSessionRef(target)) }()
			<-started
			cancel()
			err := <-done
			close(release)
			var unknown *RemoteMutationOutcomeUnknownError
			if !errors.As(err, &unknown) {
				t.Fatalf("destructive mutation error = %v, want unknown outcome", err)
			}
			adapter.mu.RLock()
			binding := adapter.sessions[target]
			adapter.mu.RUnlock()
			if binding != nil {
				t.Fatalf("unknown destructive outcome restored binding: %+v", binding)
			}
			if recovery := adapter.client.RecoveryState(); len(recovery.Subscriptions) != 0 {
				t.Fatalf("unknown destructive outcome recovery = %+v, want empty", recovery.Subscriptions)
			}
			if got := subscribeCalls.Load(); got != 1 {
				t.Fatalf("unknown destructive outcome subscribe calls = %d, want 1", got)
			}
		})
	}
}

func TestRemoteDestructiveMutationsRestoreBindingAfterKnownRejection(t *testing.T) {
	for _, operation := range remoteDestructiveRecoveryOperations() {
		t.Run(operation.name, func(t *testing.T) {
			adapter, target, _, _, subscribeCalls := newRemoteDestructiveRecoveryAdapter(t, operation, false)
			ctx, cancel := remoteAdapterContext(t)
			err := operation.invoke(ctx, adapter, mapRemoteSessionRef(target))
			cancel()
			if err == nil {
				t.Fatal("destructive mutation unexpectedly succeeded")
			}
			var unknown *RemoteMutationOutcomeUnknownError
			if errors.As(err, &unknown) {
				t.Fatalf("known rejection classified as unknown: %v", err)
			}
			adapter.mu.RLock()
			binding := adapter.sessions[target]
			adapter.mu.RUnlock()
			if binding == nil || binding.subscription != "subscription-2" || !binding.hasSnapshot {
				t.Fatalf("known rejection binding = %+v, want fresh subscription-2", binding)
			}
			if recovery := adapter.client.RecoveryState(); len(recovery.Subscriptions) != 1 || recovery.Subscriptions[0].Target != target {
				t.Fatalf("known rejection recovery = %+v, want restored target", recovery.Subscriptions)
			}
			if got := subscribeCalls.Load(); got != 2 {
				t.Fatalf("known rejection subscribe calls = %d, want 2", got)
			}
		})
	}
}

type remoteDestructiveRecoveryOperation struct {
	name    string
	method  protocol.Method
	success any
	invoke  func(context.Context, *RemoteRuntimeAdapter, runtimeapi.SessionRef) error
}

func remoteDestructiveRecoveryOperations() []remoteDestructiveRecoveryOperation {
	return []remoteDestructiveRecoveryOperation{
		{
			name: "trash", method: protocol.MethodSessionTrash,
			success: protocol.SessionTrashResult{Disposition: protocol.DispositionTrashed},
			invoke: func(ctx context.Context, adapter *RemoteRuntimeAdapter, session runtimeapi.SessionRef) error {
				_, err := adapter.TrashSession(ctx, runtimeapi.TrashSessionInput{Session: session, Guard: runtimeapi.TrashNormal})
				return err
			},
		},
		{
			name: "purge", method: protocol.MethodSessionPurge,
			success: protocol.SessionPurgeResult{Purged: true},
			invoke: func(ctx context.Context, adapter *RemoteRuntimeAdapter, session runtimeapi.SessionRef) error {
				_, err := adapter.PurgeSession(ctx, runtimeapi.PurgeSessionInput{Session: session, Guard: runtimeapi.TrashNormal})
				return err
			},
		},
		{
			name: "close_workspace", method: protocol.MethodWorkspaceClose,
			success: protocol.WorkspaceCloseResult{Disposition: protocol.WorkspaceClosed},
			invoke: func(ctx context.Context, adapter *RemoteRuntimeAdapter, session runtimeapi.SessionRef) error {
				_, err := adapter.CloseWorkspace(ctx, runtimeapi.CloseWorkspaceInput{WorkspaceID: session.WorkspaceID})
				return err
			},
		},
	}
}

func newRemoteDestructiveRecoveryAdapter(
	t *testing.T,
	operation remoteDestructiveRecoveryOperation,
	blockResponse bool,
) (*RemoteRuntimeAdapter, protocol.RuntimeTarget, chan struct{}, chan struct{}, *atomic.Int32) {
	t.Helper()
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{
		WorkspaceID: protocol.WorkspaceID("workspace-destructive-" + operation.name), SessionID: "session-destructive",
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var subscribeCalls atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, protocol.LeaseID("lease-destructive-"+operation.name), "", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				call := subscribeCalls.Add(1)
				if params.Target != target || params.ReplaceSubscriptionID != "" {
					return nil, fmt.Errorf("subscribe %d = %+v", call, params)
				}
				return protocol.SessionSubscribeResult{
					SubscriptionID: protocol.SubscriptionID(fmt.Sprintf("subscription-%d", call)),
					Snapshot: remoteAdapterSnapshot(
						target, protocol.RuntimeEpoch(fmt.Sprintf("runtime-%d", call)),
						protocol.SnapshotID(fmt.Sprintf("snapshot-%d", call)), uint64(call), nil,
					),
				}, nil
			})
			wire.Handle(string(protocol.MethodSessionUnsubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
			})
			wire.Handle(string(operation.method), func(context.Context, json.RawMessage) (any, error) {
				close(started)
				if blockResponse {
					<-release
					return operation.success, nil
				}
				return nil, protocol.MustRemoteError(protocol.ErrWorkspaceInUse, protocol.ErrorOptions{})
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() {
		if !blockResponse {
			close(release)
		}
		adapter.shutdown(false)
	})
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: mapRemoteSessionRef(target), HistoryTurns: 20,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	return adapter, target, started, release, &subscribeCalls
}
