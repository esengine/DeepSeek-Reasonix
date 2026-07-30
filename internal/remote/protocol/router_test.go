package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/rpcwire"
)

func validInitializeParams(t *testing.T) InitializeParams {
	return InitializeParams{BuildID: validBuildID(t), ClientInstanceID: "client-1", Workspace: "/workspace"}
}

func validInitializeResult(t *testing.T) InitializeResult {
	return InitializeResult{
		BuildID: validBuildID(t), HostEpoch: "host-1",
		Lease:        LeaseInfo{LeaseID: "lease-1", TTLMillis: LeaseTTLMillis, PingIntervalMs: LeasePingIntervalMillis},
		Host:         HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "landlock"},
		Capabilities: FrozenCapabilities(true, true),
	}
}

func initHandler(t *testing.T) Handler {
	return func(_ context.Context, params any) (any, error) {
		if _, ok := params.(InitializeParams); !ok {
			t.Fatalf("initialize params type = %T", params)
		}
		return rpcwire.RespondThen(validInitializeResult(t), func(error) {}), nil
	}
}

func TestRouterSupportsRealPartialPhaseAndFinalCoverageGate(t *testing.T) {
	handlers := HandlerSet{MethodRemoteInitialize: initHandler(t)}
	if _, err := NewRouter(handlers, RouterOptions{}); err != nil {
		t.Fatalf("partial phase router rejected: %v", err)
	}
	if _, err := NewCompleteRouter(handlers, RouterOptions{}); err == nil {
		t.Fatal("incomplete final router accepted")
	}
	complete := HandlerSet{}
	for _, spec := range Registry() {
		if spec.Direction == DirectionClientRequest {
			complete[spec.Name] = func(context.Context, any) (any, error) { return nil, errors.New("not invoked") }
		}
	}
	if err := ValidateHandlerCoverage(complete); err != nil {
		t.Fatalf("69-handler coverage rejected: %v", err)
	}
}

func TestRouterInitializeFirstAndReadyState(t *testing.T) {
	router, err := NewRouter(HandlerSet{MethodRemoteInitialize: initHandler(t)}, RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.BeforeRequest(string(MethodRemotePing), nil); err == nil {
		t.Fatal("non-initialize first request accepted")
	}
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err == nil {
		t.Fatal("initialize accepted after failed first request")
	}

	router, _ = NewRouter(HandlerSet{MethodRemoteInitialize: initHandler(t)}, RouterOptions{})
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(validInitializeParams(t))
	spec, _ := LookupMethod(MethodRemoteInitialize)
	if _, err := router.invoke(context.Background(), spec, raw); err != nil {
		t.Fatalf("initialize invoke: %v", err)
	}
	if !router.Ready() {
		t.Fatal("router did not become ready")
	}
	if err := router.BeforeRequest(string(MethodRemotePing), nil); err != nil {
		t.Fatalf("ready router rejected ping: %v", err)
	}
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err == nil {
		t.Fatal("duplicate initialize accepted")
	}
}

func TestRouterClientNotificationPoisonsHandshakeBeforeAndAfterInitialize(t *testing.T) {
	newRouter := func() *Router {
		router, err := NewRouter(HandlerSet{MethodRemoteInitialize: initHandler(t)}, RouterOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return router
	}

	router := newRouter()
	if err := router.WireOptions().BeforeNotification("client/note", nil); err == nil {
		t.Fatal("pre-initialize client notification accepted")
	}
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err == nil {
		t.Fatal("initialize accepted after client notification poisoned handshake")
	}

	router = newRouter()
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(validInitializeParams(t))
	spec, _ := LookupMethod(MethodRemoteInitialize)
	if _, err := router.invoke(context.Background(), spec, raw); err != nil {
		t.Fatal(err)
	}
	if err := router.WireOptions().BeforeNotification("client/note", nil); err == nil {
		t.Fatal("post-initialize client notification accepted")
	}
	if err := router.BeforeRequest(string(MethodRemotePing), nil); err == nil {
		t.Fatal("request accepted after post-initialize notification poisoned transport")
	}
}

func TestRouterAcceptsTypedBrokerNotificationAfterInitialize(t *testing.T) {
	router, err := NewRouter(HandlerSet{MethodRemoteInitialize: initHandler(t)}, RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(validInitializeParams(t))
	spec, _ := LookupMethod(MethodRemoteInitialize)
	if _, err := router.invoke(context.Background(), spec, raw); err != nil {
		t.Fatal(err)
	}
	chunk, _ := json.Marshal(BrokerStreamChunkParams{
		StreamID: "stream-1", Seq: 1,
		Chunk: BrokerProviderChunk{Type: BrokerChunkText, Text: "ok"},
	})
	if err := router.BeforeNotification(string(MethodBrokerStreamChunk), chunk); err != nil {
		t.Fatalf("valid Broker notification rejected: %v", err)
	}
	if !router.Ready() {
		t.Fatal("valid Broker notification poisoned ready router")
	}
}

func TestRouterRejectsUnknownParamsAndRedactsInternalErrors(t *testing.T) {
	reported := make(chan error, 1)
	router, err := NewRouter(HandlerSet{
		MethodRemoteInitialize: initHandler(t),
		MethodRemotePing: func(context.Context, any) (any, error) {
			return nil, fmt.Errorf("raw secret path /home/user/token")
		},
	}, RouterOptions{OnInternalError: func(_ Method, err error) { reported <- err }})
	if err != nil {
		t.Fatal(err)
	}
	_ = router.BeforeRequest(string(MethodRemoteInitialize), nil)
	initRaw, _ := json.Marshal(validInitializeParams(t))
	initSpec, _ := LookupMethod(MethodRemoteInitialize)
	if _, err := router.invoke(context.Background(), initSpec, initRaw); err != nil {
		t.Fatal(err)
	}
	pingSpec, _ := LookupMethod(MethodRemotePing)
	_, err = router.invoke(context.Background(), pingSpec, json.RawMessage(`{"leaseId":"lease-1","extra":true}`))
	var rpcErr *rpcwire.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != rpcwire.ErrInvalidParams {
		t.Fatalf("unknown params error = %#v", err)
	}
	_, err = router.invoke(context.Background(), pingSpec, json.RawMessage(`{"leaseId":"lease-1"}`))
	if !errors.As(err, &rpcErr) || rpcErr.Code != rpcwire.ErrInternal || rpcErr.Message != "internal error" {
		t.Fatalf("internal handler error leaked: %#v", err)
	}
	if reportedErr := <-reported; !stringsContains(reportedErr.Error(), "/home/user/token") {
		t.Fatalf("internal hook lost diagnostic: %v", reportedErr)
	}
}

func stringsContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestDetachRequiresAndPreservesAfterWriteResponse(t *testing.T) {
	callback := func(error) {}
	wrapped := rpcwire.RespondThen(DetachResult{Detached: true}, callback)
	result, err := validateHandlerResult(wrapped, typeOf[DetachResult](), true)
	if err != nil {
		t.Fatal(err)
	}
	response, ok := result.(rpcwire.HandlerResponse)
	if !ok || response.AfterWrite == nil || response.Result != (DetachResult{Detached: true}) {
		t.Fatalf("wrapped response lost: %#v", result)
	}
	if _, err := validateHandlerResult(DetachResult{Detached: true}, typeOf[DetachResult](), true); err == nil {
		t.Fatal("plain detach result accepted without after-write callback")
	}
}

func TestRouterInitializeRequiresPostWriteCommit(t *testing.T) {
	router, err := NewRouter(HandlerSet{
		MethodRemoteInitialize: func(context.Context, any) (any, error) {
			return validInitializeResult(t), nil
		},
	}, RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.BeforeRequest(string(MethodRemoteInitialize), nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(validInitializeParams(t))
	spec, _ := LookupMethod(MethodRemoteInitialize)
	_, err = router.invoke(context.Background(), spec, raw)
	var rpcErr *rpcwire.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != rpcwire.ErrInternal {
		t.Fatalf("plain initialize response error = %#v", err)
	}
	if router.Ready() {
		t.Fatal("router became ready before an initialize response commit callback")
	}
}

func TestRouterMapsControlledDomainError(t *testing.T) {
	remoteErr := MustRemoteError(ErrSessionBusy, ErrorOptions{})
	router := &Router{}
	err := router.mapHandlerError(MethodSessionSubmit, remoteErr)
	var rpcErr *rpcwire.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != DomainErrorCode {
		t.Fatalf("domain error mapping = %#v", err)
	}
	data, ok := rpcErr.Data.(RemoteErrorData)
	if !ok || data.ReasonixCode != ErrSessionBusy {
		t.Fatalf("domain error data = %#v", rpcErr.Data)
	}
}
