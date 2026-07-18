package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

type operationHandlerFactory struct {
	mu          sync.Mutex
	controllers []*operationHandlerController
}

func (f *operationHandlerFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	base := newDaemonFakeController(ctx, sink)
	controller := &operationHandlerController{
		daemonFakeController: base,
		core:                 control.New(control.Options{Sink: sink}),
		checkpoint: control.CheckpointSnapshot{
			Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	f.mu.Lock()
	f.controllers = append(f.controllers, controller)
	f.mu.Unlock()
	return controller, nil
}

func (f *operationHandlerFactory) controller(t *testing.T, index int) *operationHandlerController {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if index >= len(f.controllers) {
		t.Fatalf("operation Controller %d was not constructed", index)
	}
	return f.controllers[index]
}

type operationHandlerController struct {
	*daemonFakeController
	core *control.Controller

	operationMu sync.Mutex
	specs       []control.OperationSpec
	checkpoint  control.CheckpointSnapshot
}

func (c *operationHandlerController) StartOperation(spec control.OperationSpec) (*control.OperationHandle, error) {
	c.operationMu.Lock()
	c.specs = append(c.specs, spec)
	c.operationMu.Unlock()
	// The Host contract under test is the exact spec passed to SessionAPI and
	// the lifecycle of its real OperationHandle. Map transcript operations to a
	// blocking real Shell worker so the fixture needs no model provider.
	return c.core.StartOperation(control.OperationSpec{Kind: control.OperationShell, Command: "sleep 30"})
}

func (c *operationHandlerController) CheckpointSnapshot() control.CheckpointSnapshot {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	return control.CheckpointSnapshot{
		Metas:                 append([]checkpoint.Meta(nil), c.checkpoint.Metas...),
		TurnsByMessageIndex:   copyOperationIntMap(c.checkpoint.TurnsByMessageIndex),
		ConversationAvailable: copyOperationBoolMap(c.checkpoint.ConversationAvailable),
	}
}

func (c *operationHandlerController) Close() {
	c.core.Close()
	c.daemonFakeController.Close()
}

func (c *operationHandlerController) specSnapshot() []control.OperationSpec {
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	return append([]control.OperationSpec(nil), c.specs...)
}

func copyOperationIntMap(in map[int]int) map[int]int {
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyOperationBoolMap(in map[int]bool) map[int]bool {
	out := make(map[int]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func newOperationHandlerServer(t *testing.T) (*Server, *operationHandlerFactory, protocol.BuildID) {
	t.Helper()
	options, _, buildID := daemonTestServerOptions(t, nil)
	factory := &operationHandlerFactory{}
	options.ControllerFactory = factory
	var operationIDs atomic.Uint64
	options.RuntimeOptions.NewOperationID = func() (protocol.OperationID, error) {
		return protocol.OperationID(fmt.Sprintf("wire-operation-%d", operationIDs.Add(1))), nil
	}
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, factory, buildID
}

func openOperationHandlerPeer(t *testing.T, transport *transport) *daemonPeer {
	t.Helper()
	handlers := operationHandlerSet(transport)
	handlers[protocol.MethodRemoteInitialize] = transport.handleInitialize
	router, err := protocol.NewRouter(handlers, protocol.RouterOptions{OnInternalError: transport.server.reportInternal})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	transport.raw = serverConn
	wireOptions := router.WireOptions()
	routerBefore := wireOptions.BeforeRequest
	wireOptions.BeforeRequest = func(method string, params json.RawMessage) error {
		if err := routerBefore(method, params); err != nil {
			return err
		}
		return transport.beforeRequest(protocol.Method(method))
	}
	serverWire := rpcwire.NewConn(serverConn, serverConn, wireOptions)
	transport.wire = serverWire
	router.Bind(serverWire)
	serverDone := make(chan error, 1)
	go func() { serverDone <- serverWire.Serve(transport.ctx) }()

	clientWire := rpcwire.NewConn(clientConn, clientConn, rpcwire.Options{
		Name: "operation-handler-test-client", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	clientCtx, cancelClient := context.WithCancel(context.Background())
	clientDone := make(chan error, 1)
	go func() { clientDone <- clientWire.Serve(clientCtx) }()
	peer := &daemonPeer{
		raw: clientConn, wire: clientWire, cancel: cancelClient,
		serverDone: serverDone, clientDone: clientDone,
	}
	t.Cleanup(func() {
		transport.cleanup(false)
		peer.close(t)
	})
	return peer
}

func waitDaemonOperationIdle(t *testing.T, runtime *host.SessionRuntime) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		snapshot, err := runtime.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !snapshot.Running && snapshot.CurrentOperation == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon Operation did not complete: %+v", snapshot.CurrentOperation)
		}
		time.Sleep(time.Millisecond)
	}
}

func operationWireMutation(runtime *host.SessionRuntime, request protocol.RequestID) protocol.SessionMutation {
	return protocol.SessionMutation{
		RequestID: request, ExpectedHostEpoch: "host-test", Target: runtime.Target(),
		ExpectedRuntimeEpoch: runtime.Epoch(),
	}
}

func TestOperationHandlersAreWireReadyAndPreserveMutationSemantics(t *testing.T) {
	server, factory, buildID := newOperationHandlerServer(t)
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-operation-wire", SessionID: "session-operation-wire"}
	runtime, err := server.runtimes.GetOrCreate(target)
	if err != nil {
		t.Fatal(err)
	}
	transportCtx, cancelTransport := context.WithCancel(server.ctx)
	transport := &transport{
		ctx: transportCtx, cancel: cancelTransport, server: server,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	peer := openOperationHandlerPeer(t, transport)
	initializePeer(t, peer, buildID, "client-operation-wire", "")

	handlers := operationHandlerSet(transport)
	wantMethods := []protocol.Method{
		protocol.MethodShellRun, protocol.MethodSessionCompact,
		protocol.MethodSessionSummarize, protocol.MethodOperationCancel,
	}
	if len(handlers) != len(wantMethods) {
		t.Fatalf("Operation handler count = %d, want %d", len(handlers), len(wantMethods))
	}
	for _, method := range wantMethods {
		if handlers[method] == nil {
			t.Fatalf("Operation handler %q is missing", method)
		}
	}

	shellParams := protocol.ShellRunParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-shell"), Command: "printf remote",
	}
	shell := requestResult[protocol.OperationStartedResult](t, peer, protocol.MethodShellRun, shellParams)
	if shell.Disposition != "started" || shell.OperationID == "" {
		t.Fatalf("shell result = %+v", shell)
	}
	cancel := requestResult[protocol.OperationCancelResult](t, peer, protocol.MethodOperationCancel, protocol.OperationCancelParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-shell-cancel"), ExpectedOperationID: shell.OperationID,
	})
	if cancel.Status != protocol.CancelRequested || cancel.OperationID != shell.OperationID {
		t.Fatalf("shell cancel = %+v", cancel)
	}
	waitDaemonOperationIdle(t, runtime)
	replayed := requestResult[protocol.OperationStartedResult](t, peer, protocol.MethodShellRun, shellParams)
	if replayed != shell {
		t.Fatalf("shell replay = %+v, want %+v", replayed, shell)
	}

	compact := requestResult[protocol.OperationStartedResult](t, peer, protocol.MethodSessionCompact, protocol.SessionCompactParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-compact"), Instructions: "preserve constraints",
	})
	requestResult[protocol.OperationCancelResult](t, peer, protocol.MethodOperationCancel, protocol.OperationCancelParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-compact-cancel"), ExpectedOperationID: compact.OperationID,
	})
	waitDaemonOperationIdle(t, runtime)

	controller := factory.controller(t, 0)
	controller.operationMu.Lock()
	controller.checkpoint = control.CheckpointSnapshot{
		Metas:               []checkpoint.Meta{{Turn: 7, Time: time.UnixMilli(1_700_000_000_000), Prompt: "summarize", Paths: []string{}}},
		TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{7: true},
	}
	controller.operationMu.Unlock()
	binding, err := transport.bindingForRequest()
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), host.AttachmentForLease(binding), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(subscription.Snapshot.Capture.Checkpoints) != 1 {
		t.Fatalf("wire checkpoint capture = %+v", subscription.Snapshot.Capture.Checkpoints)
	}
	checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
	summarize := requestResult[protocol.OperationStartedResult](t, peer, protocol.MethodSessionSummarize, protocol.SessionSummarizeParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-summarize"),
		CheckpointID:    checkpointID, Direction: protocol.SummaryFrom,
	})
	requestResult[protocol.OperationCancelResult](t, peer, protocol.MethodOperationCancel, protocol.OperationCancelParams{
		SessionMutation: operationWireMutation(runtime, "request-wire-summarize-cancel"), ExpectedOperationID: summarize.OperationID,
	})
	waitDaemonOperationIdle(t, runtime)

	specs := controller.specSnapshot()
	wantSpecs := []control.OperationSpec{
		{Kind: control.OperationShell, Command: "printf remote"},
		{Kind: control.OperationCompact, Instructions: "preserve constraints"},
		{Kind: control.OperationSummarize, Turn: 7, Direction: control.SummarizeFrom},
	}
	if !reflect.DeepEqual(specs, wantSpecs) {
		t.Fatalf("Controller Operation specs = %+v, want %+v", specs, wantSpecs)
	}
}

func TestOperationHandlersRejectMissingRuntimeAndInvalidLeaseBeforeAdmission(t *testing.T) {
	server, _, buildID := newOperationHandlerServer(t)
	transportCtx, cancelTransport := context.WithCancel(server.ctx)
	transport := &transport{
		ctx: transportCtx, cancel: cancelTransport, server: server,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	peer := openOperationHandlerPeer(t, transport)
	initializePeer(t, peer, buildID, "client-operation-errors", "")
	missing := protocol.RuntimeTarget{WorkspaceID: "workspace-missing", SessionID: "session-missing"}
	response := requestError(t, peer, protocol.MethodShellRun, protocol.ShellRunParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-missing-operation", ExpectedHostEpoch: "host-test",
			Target: missing, ExpectedRuntimeEpoch: "runtime-missing",
		},
		Command: "true",
	})
	requireRemoteError(t, response, protocol.ErrSessionNotFound)

	binding, err := transport.bindingForRequest()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.leases.Detach(binding, binding.LeaseID); err != nil {
		t.Fatal(err)
	}
	response = requestError(t, peer, protocol.MethodSessionCompact, protocol.SessionCompactParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-detached-operation", ExpectedHostEpoch: "host-test",
			Target: missing, ExpectedRuntimeEpoch: "runtime-missing",
		},
	})
	requireRemoteError(t, response, protocol.ErrLeaseNotHeld)
}

func TestOperationProjectionIncludesOpaqueIdentityAndRejectsAmbiguity(t *testing.T) {
	operation := &protocol.OperationState{
		OperationID: "operation-project", Kind: protocol.OperationCompact, CancelRequested: true,
	}
	snapshot := host.RuntimeSnapshot{
		HostEpoch: "host-project", Target: protocol.RuntimeTarget{WorkspaceID: "workspace-project", SessionID: "session-project"},
		RuntimeEpoch: "runtime-project", Running: true, CancelRequested: true,
		CurrentOperation: operation, LastOutcome: protocol.OutcomeCompleted,
		Events: []host.RuntimeEvent{{
			HostEpoch: "host-project", Target: protocol.RuntimeTarget{WorkspaceID: "workspace-project", SessionID: "session-project"},
			RuntimeEpoch: "runtime-project", OperationID: "operation-project",
			Event: eventwire.Event{Kind: "compaction_started", Compaction: &eventwire.Compaction{Trigger: "manual"}},
		}},
	}
	state, err := projectHostRuntimeState(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	operation.OperationID = "mutated-source"
	if state.CurrentOperation == nil || state.CurrentOperation.OperationID != "operation-project" || !state.CurrentOperation.CancelRequested {
		t.Fatalf("projected CurrentOperation = %+v", state.CurrentOperation)
	}
	if len(state.LiveEvents) != 1 || state.LiveEvents[0].Kind != "compaction_started" {
		t.Fatalf("projected live events = %+v", state.LiveEvents)
	}

	projected, err := projectHostSessionEvent("subscription-project", host.RuntimeEvent{
		HostEpoch: "host-project", Target: snapshot.Target, RuntimeEpoch: "runtime-project", Seq: 9,
		OperationID: "operation-project", Event: eventwire.Event{Kind: "notice", Text: "done", Level: "info"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected.OperationID != "operation-project" || projected.TurnID != "" {
		t.Fatalf("projected event identity = turn %q operation %q", projected.TurnID, projected.OperationID)
	}
	if err := protocol.ValidateNotification(protocol.MethodSessionEvent, projected); err != nil {
		t.Fatalf("projected SessionEvent is not wire-valid: %v", err)
	}

	ambiguous := snapshot
	ambiguous.CurrentTurn = "turn-also-present"
	if _, err := projectHostRuntimeState(ambiguous); err == nil {
		t.Fatal("snapshot with Turn and Operation was projected")
	}
	if _, err := projectHostSessionEvent("subscription-project", host.RuntimeEvent{
		TurnID: "turn-project", OperationID: "operation-project",
	}); err == nil {
		t.Fatal("event with Turn and Operation was projected")
	}
}

func TestOperationHandlerSetContainsOnlyRegisteredRequestMethods(t *testing.T) {
	handlers := operationHandlerSet((*transport)(nil))
	for method, handler := range handlers {
		if handler == nil {
			t.Fatalf("handler %q is nil", method)
		}
		spec, ok := protocol.LookupMethod(method)
		if !ok || spec.Direction != protocol.DirectionClientRequest {
			t.Fatalf("handler method %q is not a registered request", method)
		}
	}
	if _, err := protocol.NewRouter(protocol.HandlerSet{
		protocol.MethodRemoteInitialize: func(context.Context, any) (any, error) {
			return protocol.InitializeResult{}, errors.New("not invoked")
		},
		protocol.MethodShellRun:         handlers[protocol.MethodShellRun],
		protocol.MethodSessionCompact:   handlers[protocol.MethodSessionCompact],
		protocol.MethodSessionSummarize: handlers[protocol.MethodSessionSummarize],
		protocol.MethodOperationCancel:  handlers[protocol.MethodOperationCancel],
	}, protocol.RouterOptions{}); err != nil {
		t.Fatalf("Operation handlers are not router-compatible: %v", err)
	}
}
