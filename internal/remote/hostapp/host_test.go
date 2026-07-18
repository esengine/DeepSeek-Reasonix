package hostapp

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func hostappTestBuildID(t *testing.T) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v-hostapp-test", strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func hostappTestProfile() protocol.ResolvedProfile {
	return protocol.ResolvedProfile{
		Model: "provider/model", Effort: "high", CollaborationMode: protocol.CollaborationPlan,
		TokenMode: protocol.TokenEconomy, ToolApprovalMode: protocol.ToolApprovalAuto,
	}
}

func TestConfiguredCapabilitiesDisableMemoryInSafeMode(t *testing.T) {
	t.Setenv("REASONIX_SAFE_MODE", "1")
	capabilities, err := configuredCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Features.Memory || !capabilities.Features.Research {
		t.Fatalf("safe-mode capabilities = %+v", capabilities.Features)
	}
	t.Setenv("REASONIX_SAFE_MODE", "0")
	capabilities, err = configuredCapabilities(context.Background())
	if err != nil || !capabilities.Features.Memory || !capabilities.Features.Research {
		t.Fatalf("normal capabilities = %+v, %v", capabilities.Features, err)
	}
}

func TestNewAssemblesPersistentCatalogSharedRuntimeFactoryAndDaemon(t *testing.T) {
	workspace := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	profile := hostappTestProfile()
	var buildMu sync.Mutex
	var builds []boot.Options
	app, err := New(context.Background(), Options{
		BuildID: hostappTestBuildID(t), HostEpoch: "host-production-test",
		CatalogOptions: catalog.Options{
			StateDir: stateDir, UserHome: workspace,
			SessionDir: func(string) string { return filepath.Join(workspace, ".sessions") },
		},
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			return profile, nil
		}),
		ControllerBuilder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			buildMu.Lock()
			builds = append(builds, options)
			buildMu.Unlock()
			executor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, options.Sink)
			return control.New(control.Options{Runner: executor, Executor: executor, Sink: options.Sink}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)

	serverSide, clientSide := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- app.Server().ServeConn(serverSide) }()
	client := rpcwire.NewConn(clientSide, clientSide, rpcwire.Options{
		Name: "hostapp-test-client", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	clientCtx, cancelClient := context.WithCancel(context.Background())
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Serve(clientCtx) }()

	request := func(method protocol.Method, params any, destination any) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		raw, err := client.Request(ctx, string(method), params)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if err := json.Unmarshal(raw, destination); err != nil {
			t.Fatalf("decode %s: %v", method, err)
		}
	}
	var initialized protocol.InitializeResult
	request(protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: hostappTestBuildID(t), ClientInstanceID: "desktop-hostapp-test",
	}, &initialized)
	if initialized.HostEpoch != app.HostEpoch() || initialized.Host.OS != runtime.GOOS || initialized.Host.Arch != runtime.GOARCH {
		t.Fatalf("initialize = %+v", initialized)
	}
	var browse protocol.WorkspaceBrowseResult
	request(protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: app.HostEpoch(), TypedPath: workspace,
	}, &browse)
	var opened protocol.WorkspaceOpenResult
	request(protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "request-open", ExpectedHostEpoch: app.HostEpoch()},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	}, &opened)
	var workspaces protocol.WorkspaceListResult
	request(protocol.MethodWorkspaceList, protocol.WorkspaceListParams{ExpectedHostEpoch: app.HostEpoch()}, &workspaces)
	if len(workspaces.Items) != 1 || workspaces.Items[0] != opened.Workspace {
		t.Fatalf("workspace/list = %+v", workspaces)
	}
	var created protocol.SessionCreateResult
	request(protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create", ExpectedHostEpoch: app.HostEpoch()},
		WorkspaceID:  opened.Workspace.WorkspaceID, Topic: protocol.TopicSelection{Kind: protocol.TopicNew},
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Profile: protocol.ProfileSelection{},
	}, &created)
	if created.RuntimeEpoch == "" || created.ResolvedProfile != profile {
		t.Fatalf("session/create = %+v", created)
	}
	var sessions protocol.SessionListResult
	request(protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: app.HostEpoch(), WorkspaceID: opened.Workspace.WorkspaceID,
	}, &sessions)
	if len(sessions.Items) != 1 || sessions.Items[0].Target != created.Target {
		t.Fatalf("session/list = %+v", sessions)
	}
	var subscribed protocol.SessionSubscribeResult
	request(protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: app.HostEpoch(), Target: created.Target, PageTurns: 60,
	}, &subscribed)
	if subscribed.Snapshot.Target != created.Target || subscribed.Snapshot.Meta.TopicID != created.TopicID ||
		subscribed.Snapshot.Meta.ResolvedProfile != profile || subscribed.Snapshot.RuntimeEpoch == "" {
		t.Fatalf("subscribe snapshot = %+v", subscribed.Snapshot)
	}
	buildMu.Lock()
	if len(builds) != 1 || builds[0].WorkspaceRoot != workspace || builds[0].SessionDir != filepath.Join(workspace, ".sessions") {
		t.Fatalf("boot builds = %+v", builds)
	}
	buildMu.Unlock()

	cancelClient()
	_ = clientSide.Close()
	select {
	case <-clientDone:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not stop")
	}
	select {
	case <-serverDone:
	case <-time.After(3 * time.Second):
		t.Fatal("server connection did not stop")
	}
}

func TestNewRejectsMissingResolverAndEmptyGeneratedEpoch(t *testing.T) {
	if _, err := New(context.Background(), Options{BuildID: hostappTestBuildID(t)}); err == nil || !strings.Contains(err.Error(), "profile resolver") {
		t.Fatalf("missing resolver error = %v", err)
	}
	_, err := New(context.Background(), Options{
		BuildID:      hostappTestBuildID(t),
		NewHostEpoch: func() (protocol.HostEpoch, error) { return "", nil },
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			return hostappTestProfile(), nil
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "epoch is empty") {
		t.Fatalf("empty generated epoch error = %v", err)
	}
}
