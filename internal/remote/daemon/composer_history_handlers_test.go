package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/sessiondisplay"
)

func newComposerHistoryServer(t *testing.T) (*Server, protocol.BuildID, string) {
	t.Helper()
	root := t.TempDir()
	userHome := filepath.Join(root, "home")
	workspace := filepath.Join(userHome, "workspace")
	sessionDir := filepath.Join(userHome, ".sessions")
	for _, dir := range []string{userHome, workspace, sessionDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: filepath.Join(root, "catalog"), UserHome: userHome,
		SessionDir: func(string) string { return sessionDir }, ProfileResolver: daemonProfileResolver{},
	})
	if err != nil {
		t.Fatal(err)
	}
	options, _, buildID := daemonTestServerOptions(t, nil)
	options.Catalog = catalogValue
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, buildID, workspace
}

func openComposerHistoryPeer(t *testing.T, transport *transport) *daemonPeer {
	t.Helper()
	handlers := composerHistoryHandlerSet(transport)
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
		Name: "composer-history-client", MaxInboundBytes: protocol.FrameBytes,
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

func createComposerHistorySession(t *testing.T, server *Server, workspace string) (protocol.WorkspaceID, protocol.SessionCreateResult, *agent.Session, string) {
	t.Helper()
	browsed, err := server.catalog.Browse(protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: "host-test", TypedPath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := server.catalog.OpenWorkspace(protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "history-open", ExpectedHostEpoch: "host-test"},
		PrimaryDirectoryRef: browsed.Directory.DirectoryRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := server.catalog.CreateSession(context.Background(), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "history-create", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	canonical := "<memory-update>\nwire-only context\n</memory-update>\n\nfirst visible"
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: canonical})
	if err := session.SaveSnapshot(created.SessionPath); err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	if err := session.SaveSnapshot(created.SessionPath); err != nil {
		t.Fatal(err)
	}
	if err := sessiondisplay.Record(created.SessionDir, created.SessionPath, canonical, "first visible"); err != nil {
		t.Fatal(err)
	}
	return opened.Workspace.WorkspaceID, created.Result("runtime-unused"), session, created.SessionPath
}

func TestComposerHistoryHandlerWirePaginationErrorsAndCursorExpiry(t *testing.T) {
	server, buildID, workspace := newComposerHistoryServer(t)
	workspaceID, created, session, sessionPath := createComposerHistorySession(t, server, workspace)
	transportCtx, cancelTransport := context.WithCancel(server.ctx)
	transport := &transport{
		ctx: transportCtx, cancel: cancelTransport, server: server,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	peer := openComposerHistoryPeer(t, transport)
	initializePeer(t, peer, buildID, "composer-history-client", "")

	handlers := composerHistoryHandlerSet(transport)
	if len(handlers) != 1 || handlers[protocol.MethodComposerHistory] == nil {
		t.Fatalf("composer history handler set = %+v", handlers)
	}
	limit := 1
	params := protocol.ComposerHistoryParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: workspaceID, Limit: &limit,
	}
	first := requestResult[protocol.ComposerHistoryResult](t, peer, protocol.MethodComposerHistory, params)
	repeated := requestResult[protocol.ComposerHistoryResult](t, peer, protocol.MethodComposerHistory, params)
	if len(first.Entries) != 1 || first.Entries[0].Text != "second prompt" ||
		first.Entries[0].Target != created.Target || first.Entries[0].Turn != 1 ||
		!first.HasMore || first.NextCursor == "" || repeated.NextCursor != first.NextCursor {
		t.Fatalf("first/repeated wire pages = %+v / %+v", first, repeated)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), sessionPath) || strings.Contains(string(encoded), workspace) {
		t.Fatalf("composer/history leaked a Host path: %s", encoded)
	}
	params.Cursor = first.NextCursor
	second := requestResult[protocol.ComposerHistoryResult](t, peer, protocol.MethodComposerHistory, params)
	if len(second.Entries) != 1 || second.Entries[0].Text != "first visible" || second.HasMore || second.NextCursor != "" {
		t.Fatalf("second wire page = %+v", second)
	}

	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply two"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "third prompt"})
	if err := session.SaveSnapshot(sessionPath); err != nil {
		t.Fatal(err)
	}
	stale := requestError(t, peer, protocol.MethodComposerHistory, params)
	requireRemoteError(t, stale, protocol.ErrStaleCursor)

	params.Cursor = "malformed-cursor"
	malformed := requestError(t, peer, protocol.MethodComposerHistory, params)
	requireRemoteError(t, malformed, protocol.ErrStaleCursor)

	params.Cursor = ""
	params.WorkspaceID = "workspace-missing"
	missing := requestError(t, peer, protocol.MethodComposerHistory, params)
	requireRemoteError(t, missing, protocol.ErrWorkspaceNotFound)

	params.WorkspaceID = workspaceID
	params.ExpectedHostEpoch = "host-stale"
	wrongEpoch := requestError(t, peer, protocol.MethodComposerHistory, params)
	requireRemoteError(t, wrongEpoch, protocol.ErrStaleHostEpoch)
}
