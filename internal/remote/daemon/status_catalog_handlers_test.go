package daemon

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

type statusHandlerController struct {
	*daemonFakeController
	base *control.Controller
	root string

	mu       sync.Mutex
	jobViews []jobs.View
}

func (c *statusHandlerController) WorkspaceRoot() string       { return c.root }
func (c *statusHandlerController) ContextSnapshot() (int, int) { return 21, 256 }
func (c *statusHandlerController) LastUsage() *provider.Usage {
	return &provider.Usage{PromptTokens: 13, CompletionTokens: 8, TotalTokens: 21}
}
func (c *statusHandlerController) Jobs() []jobs.View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]jobs.View(nil), c.jobViews...)
}
func (c *statusHandlerController) Balance(context.Context) (*billing.Balance, error) {
	return &billing.Balance{Available: true, Infos: []billing.Info{{Currency: "CNY", TotalBalance: "18.00"}}}, nil
}
func (c *statusHandlerController) CancelBackgroundJob(jobID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, value := range c.jobViews {
		if value.ID == jobID {
			c.jobViews = append(c.jobViews[:index], c.jobViews[index+1:]...)
			return true
		}
	}
	return false
}
func (c *statusHandlerController) Close() {
	c.base.Close()
	c.daemonFakeController.Close()
}

type statusHandlerFactory struct {
	root       string
	controller *statusHandlerController
}

func (f *statusHandlerFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	base := control.New(control.Options{Sink: sink, WorkspaceRoot: f.root, ModelRef: "test/model"})
	fake := newDaemonFakeController(ctx, sink)
	fake.SessionAPI = base
	f.controller = &statusHandlerController{
		daemonFakeController: fake, base: base, root: f.root,
		jobViews: []jobs.View{
			{ID: "job-a", Kind: "bash", Label: "first", Status: "running", StartedAt: 1},
			{ID: "job-b", Kind: "task", Label: "second", Status: "running", StartedAt: 2},
			{ID: "job-c", Kind: "bash", Label: "third", Status: "running", StartedAt: 3},
		},
	}
	return f.controller, nil
}

func newStatusHandlerServer(t *testing.T) (*Server, *statusHandlerFactory, protocol.BuildID) {
	t.Helper()
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	options, _, buildID := daemonTestServerOptions(t, nil)
	factory := &statusHandlerFactory{root: t.TempDir()}
	options.ControllerFactory = factory
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, factory, buildID
}

func openStatusHandlerPeer(t *testing.T, transport *transport) *daemonPeer {
	t.Helper()
	handlers := statusCatalogHandlerSet(transport)
	handlers[protocol.MethodRemoteInitialize] = transport.handleInitialize
	router, err := protocol.NewRouter(handlers, protocol.RouterOptions{OnInternalError: transport.server.reportInternal})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	transport.raw = serverConn
	wireOptions := router.WireOptions()
	before := wireOptions.BeforeRequest
	wireOptions.BeforeRequest = func(method string, params json.RawMessage) error {
		if err := before(method, params); err != nil {
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
		Name: "status-handler-client", MaxInboundBytes: protocol.FrameBytes,
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

func statusHandlerQuery(runtime *host.SessionRuntime) protocol.RuntimeQuery {
	return protocol.RuntimeQuery{
		ExpectedHostEpoch: "host-test", Target: runtime.Target(), ExpectedRuntimeEpoch: runtime.Epoch(),
	}
}

func TestStatusCatalogHandlersAreWireReady(t *testing.T) {
	server, _, buildID := newStatusHandlerServer(t)
	target := daemonTestTarget()
	runtime, err := server.runtimes.GetOrCreate(target)
	if err != nil {
		t.Fatal(err)
	}
	transportCtx, cancelTransport := context.WithCancel(server.ctx)
	transport := &transport{
		ctx: transportCtx, cancel: cancelTransport, server: server,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	peer := openStatusHandlerPeer(t, transport)
	initializePeer(t, peer, buildID, "status-handler-client", "")

	handlers := statusCatalogHandlerSet(transport)
	want := []protocol.Method{
		protocol.MethodCatalogSession, protocol.MethodSessionContext, protocol.MethodSessionBalance,
		protocol.MethodJobList, protocol.MethodJobCancel, protocol.MethodComposerSlashArgs,
	}
	if len(handlers) != len(want) {
		t.Fatalf("handler count = %d, want %d", len(handlers), len(want))
	}
	for _, method := range want {
		if handlers[method] == nil {
			t.Fatalf("handler %q missing", method)
		}
	}

	query := statusHandlerQuery(runtime)
	catalog := requestResult[protocol.SessionCatalogResult](t, peer, protocol.MethodCatalogSession, protocol.SessionCatalogParams{RuntimeQuery: query})
	if catalog.Revision == "" || len(catalog.Commands) == 0 || catalog.MCPServers == nil || catalog.Skills == nil || catalog.Plugins == nil {
		t.Fatalf("session catalog = %+v", catalog)
	}
	contextResult := requestResult[protocol.SessionContextResult](t, peer, protocol.MethodSessionContext, protocol.SessionContextParams{RuntimeQuery: query})
	if contextResult.Context.UsedTokens != 21 || contextResult.Context.WindowTokens != 256 || contextResult.Context.PromptTokens != 13 {
		t.Fatalf("session context = %+v", contextResult)
	}
	balance := requestResult[protocol.SessionBalanceResult](t, peer, protocol.MethodSessionBalance, protocol.SessionBalanceParams{RuntimeQuery: query})
	if !balance.Available || balance.Display != "¥18.00" {
		t.Fatalf("session balance = %+v", balance)
	}
	slash := requestResult[protocol.ComposerSlashArgsResult](t, peer, protocol.MethodComposerSlashArgs, protocol.ComposerSlashArgsParams{
		RuntimeQuery: query, Input: "/goal ",
	})
	if len(slash.Items) != 4 || slash.From != len("/goal ") {
		t.Fatalf("slash args = %+v", slash)
	}

	limit := 2
	first := requestResult[protocol.JobListResult](t, peer, protocol.MethodJobList, protocol.JobListParams{RuntimeQuery: query, Limit: &limit})
	if len(first.Jobs) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first jobs = %+v", first)
	}
	second := requestResult[protocol.JobListResult](t, peer, protocol.MethodJobList, protocol.JobListParams{
		RuntimeQuery: query, Cursor: first.NextCursor,
	})
	if len(second.Jobs) != 1 || second.HasMore || second.Jobs[0].ID != "job-c" {
		t.Fatalf("second jobs = %+v", second)
	}

	mutation := protocol.SessionMutation{
		RequestID: "status-cancel-job", ExpectedHostEpoch: "host-test",
		Target: target, ExpectedRuntimeEpoch: runtime.Epoch(),
	}
	cancelled := requestResult[protocol.JobCancelResult](t, peer, protocol.MethodJobCancel, protocol.JobCancelParams{
		SessionMutation: mutation, JobID: "job-a",
	})
	if cancelled.Disposition != protocol.JobCancelled {
		t.Fatalf("job cancel = %+v", cancelled)
	}
	replay := requestResult[protocol.JobCancelResult](t, peer, protocol.MethodJobCancel, protocol.JobCancelParams{
		SessionMutation: mutation, JobID: "job-a",
	})
	if replay != cancelled {
		t.Fatalf("job cancel replay = %+v, want %+v", replay, cancelled)
	}
	mutation.RequestID = "status-cancel-missing"
	notRunning := requestResult[protocol.JobCancelResult](t, peer, protocol.MethodJobCancel, protocol.JobCancelParams{
		SessionMutation: mutation, JobID: "job-missing",
	})
	if notRunning.Disposition != protocol.JobNotRunning {
		t.Fatalf("missing job cancel = %+v", notRunning)
	}

	stale := query
	stale.ExpectedRuntimeEpoch = "runtime-stale"
	errValue := requestError(t, peer, protocol.MethodSessionContext, protocol.SessionContextParams{RuntimeQuery: stale})
	requireRemoteError(t, errValue, protocol.ErrStaleRuntimeEpoch)
}
