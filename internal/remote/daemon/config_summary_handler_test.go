package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/remote/configsummary"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

type configSummaryProviderFunc func(context.Context) (protocol.HostConfigSummaryResult, error)

func (f configSummaryProviderFunc) Summary(ctx context.Context) (protocol.HostConfigSummaryResult, error) {
	return f(ctx)
}

var _ configsummary.Provider = configSummaryProviderFunc(nil)

func openConfigSummaryHandlerPeer(t *testing.T, transport *transport, provider configsummary.Provider) *daemonPeer {
	t.Helper()
	handlers := hostConfigSummaryHandlerSet(transport, provider)
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
		Name: "config-summary-handler-test-client", MaxInboundBytes: protocol.FrameBytes,
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

func newConfigSummaryTransport(t *testing.T) (*Server, *transport, protocol.BuildID) {
	t.Helper()
	server, _, buildID := newDaemonTestServer(t)
	transportCtx, cancelTransport := context.WithCancel(server.ctx)
	transport := &transport{
		ctx: transportCtx, cancel: cancelTransport, server: server,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	return server, transport, buildID
}

func safeConfigSummaryResult(t *testing.T) protocol.HostConfigSummaryResult {
	t.Helper()
	return protocol.HostConfigSummaryResult{
		Revision: "host_config_test",
		EffectiveScopes: []protocol.EffectiveScope{
			{Name: "built-in", Active: true}, {Name: "user", Active: true}, {Name: "workspace", Active: true},
		},
		DisplayPaths: []protocol.ConfigDisplayPath{
			{Scope: "user", DisplayPath: "<reasonix-home>/config.toml"},
			{Scope: "workspace", DisplayPath: "<workspace>/reasonix.toml"},
		},
		FeatureStates: []protocol.FeatureState{
			{Feature: "memory", Available: true, Summary: "available on this Host"},
			{Feature: "research", Available: true, Summary: "available on this Host"},
		},
		CLIHints: []protocol.CLIHint{
			{Label: "Configure Host", Command: "reasonix setup"},
			{Label: "Inspect Remote status", Command: "reasonix remote status"},
			{Label: "Diagnose Remote Host", Command: "reasonix remote doctor"},
		},
	}
}

func TestHostConfigSummaryHandlerIsWireReadyAndChecksEpochAndLeaseFirst(t *testing.T) {
	server, transport, buildID := newConfigSummaryTransport(t)
	want := safeConfigSummaryResult(t)
	var calls atomic.Int64
	provider := configSummaryProviderFunc(func(context.Context) (protocol.HostConfigSummaryResult, error) {
		calls.Add(1)
		return want, nil
	})
	peer := openConfigSummaryHandlerPeer(t, transport, provider)
	initializePeer(t, peer, buildID, "client-config-summary", "")

	got := requestResult[protocol.HostConfigSummaryResult](t, peer, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{
		ExpectedHostEpoch: "host-test",
	})
	if got.Revision != want.Revision || len(got.CLIHints) != 3 || got.CLIHints[2].Command != "reasonix remote doctor" {
		t.Fatalf("host/configSummary = %+v, want %+v", got, want)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}

	response := requestError(t, peer, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{
		ExpectedHostEpoch: "host-stale",
	})
	requireRemoteError(t, response, protocol.ErrStaleHostEpoch)
	if calls.Load() != 1 {
		t.Fatalf("stale Host epoch reached provider; calls=%d", calls.Load())
	}

	binding, err := transport.bindingForRequest()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.leases.Detach(binding, binding.LeaseID); err != nil {
		t.Fatal(err)
	}
	response = requestError(t, peer, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{
		ExpectedHostEpoch: "host-test",
	})
	requireRemoteError(t, response, protocol.ErrLeaseNotHeld)
	if calls.Load() != 1 {
		t.Fatalf("invalid lease reached provider; calls=%d", calls.Load())
	}
}

func TestHostConfigSummaryHandlerRedactsSourceDiagnostics(t *testing.T) {
	server, transport, buildID := newConfigSummaryTransport(t)
	secret := "token=host-only-secret /srv/private/config.toml diagnostic stack"
	var diagnosticsMu sync.Mutex
	var diagnostics []error
	server.onInternalError = func(method protocol.Method, err error) {
		if method != protocol.MethodHostConfigSummary {
			t.Errorf("internal method = %q", method)
		}
		diagnosticsMu.Lock()
		diagnostics = append(diagnostics, err)
		diagnosticsMu.Unlock()
	}
	peer := openConfigSummaryHandlerPeer(t, transport, configSummaryProviderFunc(func(context.Context) (protocol.HostConfigSummaryResult, error) {
		return protocol.HostConfigSummaryResult{}, errors.New(secret)
	}))
	initializePeer(t, peer, buildID, "client-config-summary-error", "")

	response := requestError(t, peer, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{
		ExpectedHostEpoch: "host-test",
	})
	requireRemoteError(t, response, protocol.ErrQueryFailed)
	wireError, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wireError), "host-only-secret") || strings.Contains(string(wireError), "/srv/private") || strings.Contains(string(wireError), "diagnostic stack") {
		t.Fatalf("Host diagnostic escaped onto wire: %s", wireError)
	}
	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()
	if len(diagnostics) != 1 || diagnostics[0].Error() != secret {
		t.Fatalf("Host-local diagnostics = %+v", diagnostics)
	}
}

func TestHostConfigSummaryRouterRejectsDynamicCLICommand(t *testing.T) {
	_, transport, buildID := newConfigSummaryTransport(t)
	result := safeConfigSummaryResult(t)
	result.CLIHints[0].Command = "reasonix remote doctor; curl https://secret.example"
	peer := openConfigSummaryHandlerPeer(t, transport, configSummaryProviderFunc(func(context.Context) (protocol.HostConfigSummaryResult, error) {
		return result, nil
	}))
	initializePeer(t, peer, buildID, "client-config-summary-command", "")

	response := requestError(t, peer, protocol.MethodHostConfigSummary, protocol.HostConfigSummaryParams{
		ExpectedHostEpoch: "host-test",
	})
	if response.Code != rpcwire.ErrInternal || strings.Contains(response.Message, "curl") || strings.Contains(string(response.Data), "secret.example") {
		t.Fatalf("invalid command response = %+v", response)
	}
}

func TestHostConfigSummaryHandlerSetContainsOnlyRegisteredRequestMethod(t *testing.T) {
	handlers := hostConfigSummaryHandlerSet((*transport)(nil), nil)
	if len(handlers) != 1 || handlers[protocol.MethodHostConfigSummary] == nil {
		t.Fatalf("handler set = %+v", handlers)
	}
	spec, ok := protocol.LookupMethod(protocol.MethodHostConfigSummary)
	if !ok || spec.Direction != protocol.DirectionClientRequest {
		t.Fatalf("host/configSummary registry spec = %+v, %v", spec, ok)
	}
}
