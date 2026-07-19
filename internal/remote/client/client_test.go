package client

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/eventwire"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

const clientTestTimeout = 3 * time.Second

type peerScript func(*rpcwire.Conn, net.Conn)

type scriptedFactory struct {
	mu      sync.Mutex
	scripts []peerScript
}

func (f *scriptedFactory) Open(context.Context) (Transport, error) {
	f.mu.Lock()
	if len(f.scripts) == 0 {
		f.mu.Unlock()
		return nil, errors.New("no scripted peer")
	}
	script := f.scripts[0]
	f.scripts = f.scripts[1:]
	f.mu.Unlock()
	clientSide, serverSide := net.Pipe()
	serverWire := rpcwire.NewConn(serverSide, serverSide, rpcwire.Options{
		Name: "remote-client-test-server", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	script(serverWire, serverSide)
	go func() { _ = serverWire.Serve(context.Background()) }()
	return clientSide, nil
}

func testBuildID() protocol.BuildID {
	return protocol.BuildID{
		ProductVersion: "test", SourceRevision: strings.Repeat("a", 40),
		ProtocolVersion: protocol.ProtocolVersion, SchemaHash: protocol.SchemaHash(),
	}
}

func testInitialize(lease protocol.LeaseID) protocol.InitializeResult {
	return protocol.InitializeResult{
		BuildID: testBuildID(), HostEpoch: "host-epoch", Lease: protocol.LeaseInfo{
			LeaseID: lease, TTLMillis: protocol.LeaseTTLMillis, PingIntervalMs: protocol.LeasePingIntervalMillis,
		},
		Host:         protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "sh", SandboxBackend: "landlock"},
		Capabilities: protocol.FrozenCapabilities(false, false),
	}
}

func basePeer(result protocol.InitializeResult, configure func(*rpcwire.Conn, net.Conn)) peerScript {
	return func(wire *rpcwire.Conn, raw net.Conn) {
		wire.Handle(string(protocol.MethodRemoteInitialize), func(context.Context, json.RawMessage) (any, error) {
			return result, nil
		})
		wire.Handle(string(protocol.MethodRemotePing), func(context.Context, json.RawMessage) (any, error) {
			return protocol.PingResult{HostEpoch: result.HostEpoch, LeaseTTL: protocol.LeaseTTLMillis}, nil
		})
		if configure != nil {
			configure(wire, raw)
		}
	}
}

func newTestClient(t *testing.T, factory TransportFactory) *Client {
	t.Helper()
	client, err := New(Options{Factory: factory, BuildID: testBuildID(), ClientInstanceID: "client-instance"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func connectTestClient(t *testing.T, client *Client) Connection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	connection, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return connection
}

func testSnapshot(target protocol.RuntimeTarget, boundary uint64) protocol.SessionSnapshot {
	goal := ""
	snapshotID := protocol.SnapshotID("snapshot-1")
	return protocol.SessionSnapshot{
		SnapshotID: snapshotID, HostEpoch: "host-epoch", Target: target,
		RuntimeEpoch: "runtime-epoch", BoundarySeq: boundary,
		Meta: protocol.SessionMetaSnapshot{
			TopicID: "topic-1", Title: "Session", Goal: &goal,
			ResolvedProfile: protocol.ResolvedProfile{
				Model: "test/model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
				TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
			},
			Capabilities: protocol.FrozenCapabilities(false, false),
		},
		Runtime: protocol.SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		History: protocol.HistoryPage{
			SnapshotID: snapshotID, Messages: []protocol.HistoryMessage{}, Externalized: []protocol.ExternalizedField{},
		},
		Todos: []protocol.TodoItem{}, Context: protocol.ContextView{
			Sources: []protocol.UsageSourceView{}, ReadFiles: []protocol.ReadFileRecord{},
		}, Jobs: []protocol.JobView{}, Checkpoints: []protocol.CheckpointView{}, Externalized: []protocol.ExternalizedField{},
	}
}

func TestConnectPingAndDetachResponseBeforeImmediateEOF(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
				return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(error) { _ = raw.Close() }), nil
			})
		})}}
		client := newTestClient(t, factory)
		connectTestClient(t, client)
		ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
		ping, err := client.Ping(ctx)
		cancel()
		if err != nil || ping.HostEpoch != "host-epoch" {
			t.Fatalf("iteration %d Ping = %+v, %v", iteration, ping, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), clientTestTimeout)
		detached, err := client.Detach(ctx)
		cancel()
		if err != nil || !detached.Detached {
			t.Fatalf("iteration %d Detach = %+v, %v", iteration, detached, err)
		}
		status := client.Status()
		if status.State != StateDisconnected || status.LeaseID != "" {
			t.Fatalf("iteration %d status after detach = %+v", iteration, status)
		}
		select {
		case fault := <-client.Faults():
			t.Fatalf("iteration %d graceful detach emitted fault: %+v", iteration, fault)
		default:
		}
	}
}

func TestUnsubscribeResponseBeforeImmediateEOFClearsRecovery(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		target := protocol.RuntimeTarget{WorkspaceID: "workspace-unsubscribe-eof", SessionID: "session-unsubscribe-eof"}
		factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-unsubscribe-eof"), func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{
					SubscriptionID: "subscription-unsubscribe-eof", Snapshot: testSnapshot(target, 7),
				}, nil
			})
			wire.Handle(string(protocol.MethodSessionUnsubscribe), func(context.Context, json.RawMessage) (any, error) {
				return rpcwire.RespondThen(protocol.SessionUnsubscribeResult{Unsubscribed: true}, func(error) { _ = raw.Close() }), nil
			})
		})}}
		client := newTestClient(t, factory)
		connectTestClient(t, client)
		ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
		subscription, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60,
		})
		cancel()
		if err != nil {
			t.Fatalf("iteration %d Subscribe: %v", iteration, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), clientTestTimeout)
		result, err := client.Unsubscribe(ctx, subscription.ID)
		cancel()
		if err != nil || !result.Unsubscribed {
			t.Fatalf("iteration %d Unsubscribe = %+v, %v", iteration, result, err)
		}
		select {
		case <-client.Faults():
		case <-time.After(clientTestTimeout):
			t.Fatalf("iteration %d immediate EOF fault not observed", iteration)
		}
		if recovery := client.RecoveryState(); len(recovery.Subscriptions) != 0 {
			t.Fatalf("iteration %d unsubscribe recovery = %+v", iteration, recovery)
		}
	}
}

func TestDetachEOFBeforeResponsePreservesRecoveryAndFaults(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-detach-loss"), func(wire *rpcwire.Conn, raw net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-detach-loss", Snapshot: testSnapshot(target, 9)}, nil
		})
		wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
			_ = raw.Close()
			return protocol.DetachResult{Detached: true}, nil
		})
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	_, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), clientTestTimeout)
	_, err = client.Detach(ctx)
	cancel()
	if err == nil {
		t.Fatal("Detach unexpectedly succeeded before its response was written")
	}
	select {
	case fault := <-client.Faults():
		if !errors.Is(fault.Err, ErrTransportLost) {
			t.Fatalf("fault = %+v", fault)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("pre-response detach EOF did not emit a fault")
	}
	recovery := client.RecoveryState()
	if recovery.ResumeLeaseID != "lease-detach-loss" || len(recovery.Subscriptions) != 1 || recovery.Subscriptions[0].Target != target || recovery.Subscriptions[0].LastSeq != 9 {
		t.Fatalf("recovery after failed detach = %+v", recovery)
	}
}

func TestInitializeBuildMismatchReturnsStructuredRemoteError(t *testing.T) {
	factory := &scriptedFactory{scripts: []peerScript{func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodRemoteInitialize), func(context.Context, json.RawMessage) (any, error) {
			return nil, protocol.MustRemoteError(protocol.ErrVersionMismatch, protocol.ErrorOptions{
				Expected: "host-build", Actual: "desktop-build",
			}).RPCError()
		})
	}}}
	client := newTestClient(t, factory)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	_, err := client.Connect(ctx)
	var remoteError *protocol.RemoteError
	if !errors.As(err, &remoteError) || remoteError.Code != protocol.ErrVersionMismatch {
		t.Fatalf("Connect error = %T %v, want VERSION_MISMATCH", err, err)
	}
}

type classifiedTransportFailure struct{ Stage string }

func (e *classifiedTransportFailure) Error() string {
	return "classified transport failure at " + e.Stage
}

type failingTransport struct{ err error }

func (t *failingTransport) Read([]byte) (int, error)    { return 0, t.err }
func (t *failingTransport) Write(p []byte) (int, error) { return len(p), nil }
func (t *failingTransport) Close() error                { return nil }

func TestConnectPreservesPendingInitializeTransportFailure(t *testing.T) {
	want := &classifiedTransportFailure{Stage: "authentication"}
	client := newTestClient(t, TransportFactoryFunc(func(context.Context) (Transport, error) {
		return &failingTransport{err: want}, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	_, err := client.Connect(ctx)
	var classified *classifiedTransportFailure
	if !errors.Is(err, ErrTransportLost) || !errors.As(err, &classified) || classified != want {
		t.Fatalf("Connect error = %T %v, want wrapped classified transport failure", err, err)
	}
}

func TestConnectPendingInitializeEOFIsTransportLost(t *testing.T) {
	client := newTestClient(t, TransportFactoryFunc(func(context.Context) (Transport, error) {
		return &failingTransport{err: io.EOF}, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	_, err := client.Connect(ctx)
	if !errors.Is(err, ErrTransportLost) {
		t.Fatalf("Connect error = %T %v, want ErrTransportLost", err, err)
	}
}

func TestHeartbeatUsesFrozenTenSecondInterval(t *testing.T) {
	pingSeen := make(chan struct{}, 1)
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodRemotePing), func(context.Context, json.RawMessage) (any, error) {
			pingSeen <- struct{}{}
			return protocol.PingResult{HostEpoch: "host-epoch", LeaseTTL: protocol.LeaseTTLMillis}, nil
		})
	})}}
	client := newTestClient(t, factory)
	manual := &manualTicker{ticks: make(chan time.Time, 1)}
	var interval time.Duration
	client.newTicker = func(value time.Duration) ticker { interval = value; return manual }
	connectTestClient(t, client)
	if interval != 10*time.Second {
		t.Fatalf("heartbeat interval = %s", interval)
	}
	manual.ticks <- time.Now()
	select {
	case <-pingSeen:
	case <-time.After(clientTestTimeout):
		t.Fatal("heartbeat ping was not sent")
	}
}

type manualTicker struct {
	ticks   chan time.Time
	stopped atomic.Bool
}

func (t *manualTicker) C() <-chan time.Time { return t.ticks }
func (t *manualTicker) Stop()               { t.stopped.Store(true) }

func TestSubscribeBuffersNotificationUntilAtomicSnapshot(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			event := protocol.SessionEvent{
				SubscriptionID: "subscription-1", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", Seq: 8, Event: eventwire.Event{Kind: "text", Text: "after snapshot"},
				Externalized: []protocol.ExternalizedField{},
			}
			if err := wire.Notify(string(protocol.MethodSessionEvent), event); err != nil {
				return nil, err
			}
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-1", Snapshot: testSnapshot(target, 7)}, nil
		})
		wire.Handle(string(protocol.MethodSessionUnsubscribe), func(context.Context, json.RawMessage) (any, error) {
			return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
		})
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	subscription, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if subscription.Snapshot.BoundarySeq != 7 {
		t.Fatalf("boundarySeq = %d", subscription.Snapshot.BoundarySeq)
	}
	select {
	case update := <-subscription.Updates:
		if update.Event == nil || update.Event.Seq != 8 || update.Event.Event.Text != "after snapshot" || update.Err != nil {
			t.Fatalf("update = %+v", update)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("buffered event was not released")
	}
}

func TestSubscribeSequenceGapRequiresSnapshot(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			_ = wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
				SubscriptionID: "subscription-gap", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", Seq: 10, Event: eventwire.Event{Kind: "text", Text: "gap"},
				Externalized: []protocol.ExternalizedField{},
			})
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-gap", Snapshot: testSnapshot(target, 7)}, nil
		})
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	subscription, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	if err != nil {
		t.Fatal(err)
	}
	update := <-subscription.Updates
	var gap *SequenceGapError
	if !update.SnapshotRequired || !errors.As(update.Err, &gap) || gap.Expected != 8 || gap.Got != 10 {
		t.Fatalf("gap update = %+v", update)
	}
}

func TestSubscribeRehydratesEventBeforeDelivery(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	content := []byte(strings.Repeat("streamed ", 40000))
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	descriptor := protocol.ExternalizedField{
		JSONPointer: "/event/text", ContentRef: "event-content", TotalBytes: int64(len(content)), SHA256: sha,
	}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			if err := wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
				SubscriptionID: "subscription-content", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", Seq: 2, Event: eventwire.Event{Kind: "text", Text: string(content)},
				Externalized: []protocol.ExternalizedField{descriptor},
			}); err != nil {
				return nil, err
			}
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-content", Snapshot: testSnapshot(target, 1)}, nil
		})
		registerContentHandler(wire, content, sha)
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	subscription, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-subscription.Updates:
		if update.Err != nil || update.Event == nil || update.Event.Event.Text != string(content) {
			t.Fatalf("rehydrated update = %+v", update)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("rehydrated event was not delivered")
	}
}

func TestHostResyncStopsOldSubscription(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			if err := wire.Notify(string(protocol.MethodSessionResyncRequired), protocol.SessionResyncRequired{
				SubscriptionID: "subscription-resync", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", LastSeq: 4, Reason: protocol.ResyncQueueOverflow,
			}); err != nil {
				return nil, err
			}
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-resync", Snapshot: testSnapshot(target, 4)}, nil
		})
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	subscription, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	if err != nil {
		t.Fatal(err)
	}
	update := <-subscription.Updates
	if !update.SnapshotRequired || update.Resync == nil || update.Resync.Reason != protocol.ResyncQueueOverflow || update.Err != nil {
		t.Fatalf("resync update = %+v", update)
	}
	if _, open := <-subscription.Updates; open {
		t.Fatal("old subscription stream remained open after resync")
	}
}

func TestSubscribeAtomicReplacementMigratesStream(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	sendOldAfterReplacement := make(chan struct{})
	var calls atomic.Int32
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			if calls.Add(1) == 1 {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-old", Snapshot: testSnapshot(target, 1)}, nil
			}
			if err := wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
				SubscriptionID: "subscription-new", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", Seq: 6, Event: eventwire.Event{Kind: "text", Text: "new stream"},
				Externalized: []protocol.ExternalizedField{},
			}); err != nil {
				return nil, err
			}
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-new", Snapshot: testSnapshot(target, 5)}, nil
		})
		go func() {
			<-sendOldAfterReplacement
			_ = wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
				SubscriptionID: "subscription-old", HostEpoch: "host-epoch", Target: target,
				RuntimeEpoch: "runtime-epoch", Seq: 2, Event: eventwire.Event{Kind: "text", Text: "stale"},
				Externalized: []protocol.ExternalizedField{},
			})
		}()
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	old, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), clientTestTimeout)
	replacement, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60, ReplaceSubscriptionID: old.ID,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	oldTerminal := <-old.Updates
	if !errors.Is(oldTerminal.Err, ErrSubscriptionReplaced) {
		t.Fatalf("old terminal update = %+v", oldTerminal)
	}
	newUpdate := <-replacement.Updates
	if newUpdate.Event == nil || newUpdate.Event.Seq != 6 || newUpdate.Event.Event.Text != "new stream" || newUpdate.Err != nil {
		t.Fatalf("replacement update = %+v", newUpdate)
	}
	close(sendOldAfterReplacement)
	select {
	case fault := <-client.Faults():
		var protocolError *ProtocolError
		if !errors.As(fault.Err, &protocolError) {
			t.Fatalf("stale old subscription fault = %+v", fault)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("old subscriptionId notification was accepted after replacement")
	}
}

func TestHistorySnapshotBindingAndExternalizedRehydration(t *testing.T) {
	content := []byte(strings.Repeat("history ", 40000))
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	descriptor := protocol.ExternalizedField{
		JSONPointer: "/messages/0/content", ContentRef: "history-content", TotalBytes: int64(len(content)), SHA256: sha,
	}
	message := string(content)
	page := protocol.HistoryPage{
		SnapshotID: "snapshot-history", Messages: []protocol.HistoryMessage{{Role: "assistant", Content: &message}},
		StartTurn: 0, EndTurn: 1, TotalTurns: 1, ActualTurns: 1, HasOlder: false,
		Externalized: []protocol.ExternalizedField{descriptor},
	}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionHistory), func(context.Context, json.RawMessage) (any, error) { return page, nil })
		registerContentHandler(wire, content, sha)
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	result, err := client.History(ctx, protocol.SessionHistoryParams{
		RuntimeQuery: protocol.RuntimeQuery{
			ExpectedHostEpoch: "host-epoch", Target: protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"},
			ExpectedRuntimeEpoch: "runtime-epoch",
		}, SnapshotID: "snapshot-history", Cursor: "cursor-history", PageTurns: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 || result.Messages[0].Content == nil || *result.Messages[0].Content != string(content) {
		t.Fatalf("rehydrated history = %+v", result)
	}
}

func TestHistoryRejectsDifferentSnapshotID(t *testing.T) {
	page := protocol.HistoryPage{
		SnapshotID: "snapshot-other", Messages: []protocol.HistoryMessage{},
		StartTurn: 0, EndTurn: 0, TotalTurns: 0, ActualTurns: 0, Externalized: []protocol.ExternalizedField{},
	}
	client := newTestClient(t, &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodSessionHistory), func(context.Context, json.RawMessage) (any, error) { return page, nil })
	})}})
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	_, err := client.History(ctx, protocol.SessionHistoryParams{
		RuntimeQuery: protocol.RuntimeQuery{
			ExpectedHostEpoch: "host-epoch", Target: protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"},
			ExpectedRuntimeEpoch: "runtime-epoch",
		}, SnapshotID: "snapshot-wanted", Cursor: "cursor-history", PageTurns: 60,
	})
	var protocolError *ProtocolError
	if !errors.As(err, &protocolError) {
		t.Fatalf("History error = %T %v", err, err)
	}
}

func TestOldGenerationLateResultIsDiscarded(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	first := basePeer(testInitialize("lease-1"), func(wire *rpcwire.Conn, _ net.Conn) {
		wire.Handle(string(protocol.MethodHostCapabilities), func(context.Context, json.RawMessage) (any, error) {
			close(requestStarted)
			<-releaseRequest
			return protocol.HostCapabilitiesResult{HostEpoch: "host-epoch", Capabilities: protocol.FrozenCapabilities(false, false)}, nil
		})
	})
	secondResult := testInitialize("lease-1")
	second := basePeer(secondResult, nil)
	client := newTestClient(t, &scriptedFactory{scripts: []peerScript{first, second}})
	connectTestClient(t, client)
	requestDone := make(chan error, 1)
	go func() {
		_, err := client.Request(context.Background(), protocol.MethodHostCapabilities,
			protocol.HostCapabilitiesParams{ExpectedHostEpoch: "host-epoch"})
		requestDone <- err
	}()
	<-requestStarted
	connectTestClient(t, client)
	close(releaseRequest)
	select {
	case err := <-requestDone:
		if !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("late request error = %v", err)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("late old-generation request did not finish")
	}
}

func TestEOFRetainsLeaseAndSubscriptionRecovery(t *testing.T) {
	closePeer := make(chan struct{})
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-recover"), func(wire *rpcwire.Conn, raw net.Conn) {
		wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
			return protocol.SessionSubscribeResult{SubscriptionID: "subscription-recover", Snapshot: testSnapshot(target, 3)}, nil
		})
		go func() { <-closePeer; _ = raw.Close() }()
	})}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	_, err := client.Subscribe(ctx, protocol.SessionSubscribeParams{ExpectedHostEpoch: "host-epoch", Target: target, PageTurns: 60})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	close(closePeer)
	select {
	case fault := <-client.Faults():
		if !errors.Is(fault.Err, ErrTransportLost) {
			t.Fatalf("fault = %+v", fault)
		}
	case <-time.After(clientTestTimeout):
		t.Fatal("EOF fault not observed")
	}
	recovery := client.RecoveryState()
	if recovery.ResumeLeaseID != "lease-recover" || len(recovery.Subscriptions) != 1 || recovery.Subscriptions[0].Target != target || recovery.Subscriptions[0].LastSeq != 3 {
		t.Fatalf("recovery = %+v", recovery)
	}
}

func TestFetchContentValidatesChunksAndSHA256(t *testing.T) {
	data := []byte(strings.Repeat("界", 100000))
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	descriptor := protocol.ExternalizedField{
		JSONPointer: "/text", ContentRef: "content-1", TotalBytes: int64(len(data)), SHA256: sha,
	}
	factory := &scriptedFactory{scripts: []peerScript{contentPeer(data, sha)}}
	client := newTestClient(t, factory)
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	result, err := client.FetchContent(ctx, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if result.JSONPointer != "/text" || result.Value != string(data) {
		t.Fatalf("rehydrated content mismatch: pointer=%q bytes=%d", result.JSONPointer, len(result.Value))
	}
}

func TestFetchContentHashMismatchClosesTransport(t *testing.T) {
	declared := []byte("declared")
	digest := sha256.Sum256(declared)
	sha := hex.EncodeToString(digest[:])
	actual := []byte("tampered")
	client := newTestClient(t, &scriptedFactory{scripts: []peerScript{contentPeer(actual, sha)}})
	connectTestClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), clientTestTimeout)
	defer cancel()
	_, err := client.FetchContent(ctx, protocol.ExternalizedField{
		JSONPointer: "/text", ContentRef: "content-1", TotalBytes: int64(len(actual)), SHA256: sha,
	})
	var protocolError *ProtocolError
	if !errors.As(err, &protocolError) {
		t.Fatalf("FetchContent error = %T %v", err, err)
	}
}

func contentPeer(data []byte, declaredSHA string) peerScript {
	return basePeer(testInitialize("lease-content"), func(wire *rpcwire.Conn, _ net.Conn) {
		registerContentHandler(wire, data, declaredSHA)
	})
}

func registerContentHandler(wire *rpcwire.Conn, data []byte, declaredSHA string) {
	wire.Handle(string(protocol.MethodSessionContent), func(_ context.Context, raw json.RawMessage) (any, error) {
		value, err := protocol.DecodeRequestParams(protocol.MethodSessionContent, raw)
		if err != nil {
			return nil, err
		}
		params := value.(protocol.SessionContentParams)
		start := int(params.Offset)
		end := start + protocol.ContentRefChunkBytes
		if end > len(data) {
			end = len(data)
		}
		var next *int64
		if end < len(data) {
			value := int64(end)
			next = &value
		}
		return protocol.SessionContentResult{
			ContentRef: params.ContentRef, Offset: params.Offset,
			DataBase64: base64.StdEncoding.EncodeToString(data[start:end]), NextOffset: next,
			TotalBytes: int64(len(data)), SHA256: declaredSHA, Encoding: protocol.ContentUTF8,
		}, nil
	})
}

func TestMalformedAndOversizedNotificationsTerminateTransport(t *testing.T) {
	tests := []struct {
		name   string
		writer func(*rpcwire.Conn, net.Conn) error
	}{
		{name: "malformed", writer: func(wire *rpcwire.Conn, _ net.Conn) error {
			return wire.Notify(string(protocol.MethodCatalogChanged), map[string]any{"unknown": true})
		}},
		{name: "oversized", writer: func(_ *rpcwire.Conn, raw net.Conn) error {
			_, err := fmt.Fprintf(raw, "{\"jsonrpc\":\"2.0\",\"method\":\"catalog/changed\",\"params\":{\"padding\":\"%s\"}}\n", strings.Repeat("x", protocol.FrameBytes))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trigger := make(chan struct{})
			factory := &scriptedFactory{scripts: []peerScript{basePeer(testInitialize("lease-fault"), func(wire *rpcwire.Conn, raw net.Conn) {
				go func() { <-trigger; _ = test.writer(wire, raw) }()
			})}}
			client := newTestClient(t, factory)
			connectTestClient(t, client)
			close(trigger)
			select {
			case fault := <-client.Faults():
				if fault.Err == nil {
					t.Fatal("terminal fault omitted error")
				}
			case <-time.After(clientTestTimeout):
				t.Fatal("malformed transport was not terminated")
			}
		})
	}
}
