//go:build linux

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/remote/attach"
	"reasonix/internal/remote/catalog"
	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/hostapp"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/tool"
)

const remoteE2ETestTimeout = 15 * time.Second

// remoteE2EProvider keeps the first accepted turn alive while the Desktop side
// loses its attach stream. Releasing it proves that the daemon, rather than the
// short-lived attach transport, owns and completes the turn.
type remoteE2EProvider struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	once     sync.Once
}

func newRemoteE2EProvider() *remoteE2EProvider {
	return &remoteE2EProvider{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
}

func (*remoteE2EProvider) Name() string { return "remote-e2e" }

func (p *remoteE2EProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk, 2)
	p.once.Do(func() { close(p.started) })
	go func() {
		defer close(out)
		defer close(p.finished)
		select {
		case <-ctx.Done():
			return
		case <-p.release:
		}
		out <- provider.Chunk{Type: provider.ChunkText, Text: "completed on the Linux Host"}
		out <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return out, nil
}

type remoteE2EAttachTransport struct {
	net.Conn
	cancel context.CancelFunc
	once   sync.Once
}

func (t *remoteE2EAttachTransport) Close() error {
	if t == nil {
		return nil
	}
	var err error
	t.once.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		if t.Conn != nil {
			err = t.Conn.Close()
		}
	})
	return err
}

// remoteE2EAttachFactory models the process boundary of
// `reasonix remote attach --stdio`: every Open receives a fresh stdio stream,
// and attach itself dials the real user-owned Unix endpoint. It deliberately
// never starts or repairs the daemon.
type remoteE2EAttachFactory struct {
	endpoint *service.Endpoint
	buildID  protocol.BuildID

	mu      sync.Mutex
	current *remoteE2EAttachTransport
}

func (f *remoteE2EAttachFactory) Open(parent context.Context) (remoteclient.Transport, error) {
	if parent == nil {
		parent = context.Background()
	}
	clientSide, attachSide := net.Pipe()
	attachCtx, cancel := context.WithCancel(parent)
	transport := &remoteE2EAttachTransport{Conn: clientSide, cancel: cancel}
	f.mu.Lock()
	f.current = transport
	f.mu.Unlock()
	go func() {
		_ = attach.Run(attachCtx, attachSide, attachSide, attach.Options{
			BuildID: f.buildID,
			Service: f.endpoint,
		})
		_ = attachSide.Close()
	}()
	return transport, nil
}

func (f *remoteE2EAttachFactory) dropCurrent() {
	f.mu.Lock()
	current := f.current
	f.mu.Unlock()
	if current != nil {
		_ = current.Close()
	}
}

type remoteE2ELinuxHost struct {
	root      string
	workspace string
	endpoint  *service.Endpoint
	buildID   protocol.BuildID
	provider  *remoteE2EProvider
}

func startRemoteE2ELinuxHost(t *testing.T) *remoteE2ELinuxHost {
	t.Helper()
	// Unix-domain socket paths are capped at roughly 108 bytes on Linux. Keep
	// this process-restart fixture independent of the (long) Go test name.
	root, err := os.MkdirTemp("/tmp", "reasonix-remote-restart-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove Remote restart fixture: %v", err)
		}
	})
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "config", "systemd", "user", service.UnitName)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/managed/reasonix remote serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := service.NewEndpoint(service.Paths{RuntimeDir: runtimeDir, UnitPath: unitPath})
	buildID, err := protocol.NewBuildID("v-remote-restart-e2e", strings.Repeat("f", 40))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	provider := newRemoteE2EProvider()
	profile := protocol.ResolvedProfile{
		Model: "test/model", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	host, err := hostapp.New(context.Background(), hostapp.Options{
		BuildID: buildID,
		CatalogOptions: catalog.Options{
			StateDir: filepath.Join(root, "state"), UserHome: root,
			SessionDir: func(workspaceRoot string) string {
				return filepath.Join(workspaceRoot, ".reasonix-sessions")
			},
		},
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			return profile, nil
		}),
		ControllerBuilder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			executor := agent.New(provider, tool.NewRegistry(), agent.NewSession("remote restart e2e system"), agent.Options{}, options.Sink)
			return control.New(control.Options{
				Runner: executor, Executor: executor, Sink: options.Sink,
				WorkspaceRoot: options.WorkspaceRoot, SessionDir: options.SessionDir,
			}), nil
		},
		Capabilities: func() *protocol.Capabilities {
			value := protocol.FrozenCapabilities(true, true)
			return &value
		}(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hostCtx, stopHost := context.WithCancel(context.Background())
	hostDone := make(chan error, 1)
	go func() { hostDone <- host.Serve(hostCtx, endpoint) }()
	t.Cleanup(func() {
		stopHost()
		host.Close()
		select {
		case serveErr := <-hostDone:
			if serveErr != nil {
				t.Errorf("Host shutdown: %v", serveErr)
			}
		case <-time.After(remoteE2ETestTimeout):
			t.Error("Host did not stop")
		}
	})
	waitRemoteE2E(t, func() bool {
		path, pathErr := endpoint.SocketPath()
		if pathErr != nil {
			return false
		}
		info, statErr := os.Lstat(path)
		return statErr == nil && info.Mode().Type() == os.ModeSocket && info.Mode().Perm() == 0o600
	}, "Linux Host socket was not ready")
	return &remoteE2ELinuxHost{
		root: root, workspace: workspace, endpoint: endpoint, buildID: buildID, provider: provider,
	}
}

func newRemoteE2EDesktopProcess(
	t *testing.T,
	storePath string,
	buildID protocol.BuildID,
	factory *remoteE2EAttachFactory,
) (*App, *RemoteHostStore, *TargetManager) {
	t.Helper()
	store, err := NewRemoteHostStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.ctx = context.Background()
	setRemoteWorkbenchTestEmitter(app, func(context.Context, string, ...interface{}) {})
	app.readyHook = func() {}
	app.projectTreeChangedHook = func() {}
	remoteConnector, err := NewRemoteTargetConnector(RemoteTargetConnectorOptions{
		Store: store, AskPassHelperPath: "/remote-e2e-unused-askpass",
		AskPassHandler: func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
			return RemoteAskPassAnswer{}, errors.New("Remote restart E2E never invokes AskPass")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteConnector.buildID = func() (protocol.BuildID, error) { return buildID, nil }
	remoteConnector.newClient = func(entry RemoteHostEntry, actual protocol.BuildID) (*remoteclient.Client, error) {
		return remoteclient.New(remoteclient.Options{
			Factory: factory, BuildID: actual,
			ClientInstanceID: protocol.ClientInstanceID(entry.ClientInstanceID),
			ResumeLeaseID:    protocol.LeaseID(entry.ResumeLeaseID),
		})
	}
	local, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewTargetManager(
		TargetConnectorMux{Local: appLocalTargetConnector{app: app}, Remote: remoteConnector},
		local,
		TargetManagerOptions{},
	)
	if err != nil {
		local.closeAdapter()
		t.Fatal(err)
	}
	app.remote.initOnce.Do(func() {
		app.remote.store = store
		app.remote.manager = manager
		app.remote.pending = make(map[string]remoteAskPassPending)
	})
	manager.SetEventSink(app.handleTargetRuntimeEvent)
	manager.SetStateSink(app.handleTargetState)
	t.Cleanup(func() {
		manager.SetEventSink(nil)
		manager.SetStateSink(nil)
		ctx, cancel := context.WithTimeout(context.Background(), remoteE2ETestTimeout)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Errorf("Desktop target manager shutdown: %v", err)
		}
	})
	return app, store, manager
}

func TestRemoteDesktopToLinuxHostDisconnectReconnectEndToEnd(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "config", "systemd", "user", service.UnitName)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/managed/reasonix remote serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := service.NewEndpoint(service.Paths{RuntimeDir: runtimeDir, UnitPath: unitPath})

	buildID, err := protocol.NewBuildID("v-remote-e2e", strings.Repeat("e", 40))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	provider := newRemoteE2EProvider()
	profile := protocol.ResolvedProfile{
		Model: "test/model", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	host, err := hostapp.New(context.Background(), hostapp.Options{
		BuildID: buildID,
		CatalogOptions: catalog.Options{
			StateDir: filepath.Join(root, "state"), UserHome: root,
			SessionDir: func(workspaceRoot string) string {
				return filepath.Join(workspaceRoot, ".reasonix-sessions")
			},
		},
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			return profile, nil
		}),
		ControllerBuilder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			executor := agent.New(provider, tool.NewRegistry(), agent.NewSession("remote e2e system"), agent.Options{}, options.Sink)
			return control.New(control.Options{
				Runner: executor, Executor: executor, Sink: options.Sink,
				WorkspaceRoot: options.WorkspaceRoot, SessionDir: options.SessionDir,
			}), nil
		},
		Capabilities: func() *protocol.Capabilities {
			value := protocol.FrozenCapabilities(true, true)
			return &value
		}(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hostCtx, stopHost := context.WithCancel(context.Background())
	hostDone := make(chan error, 1)
	go func() { hostDone <- host.Serve(hostCtx, endpoint) }()
	t.Cleanup(func() {
		stopHost()
		host.Close()
		select {
		case serveErr := <-hostDone:
			if serveErr != nil {
				t.Errorf("Host shutdown: %v", serveErr)
			}
		case <-time.After(remoteE2ETestTimeout):
			t.Error("Host did not stop")
		}
	})
	waitRemoteE2E(t, func() bool {
		path, pathErr := endpoint.SocketPath()
		if pathErr != nil {
			return false
		}
		info, statErr := os.Lstat(path)
		return statErr == nil && info.Mode().Type() == os.ModeSocket && info.Mode().Perm() == 0o600
	}, "Linux Host socket was not ready")

	store, err := NewRemoteHostStore(filepath.Join(root, "desktop", "remote-hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := NewRemoteHostEntry("linux-e2e", "Linux E2E")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	transportFactory := &remoteE2EAttachFactory{endpoint: endpoint, buildID: buildID}
	client, err := remoteclient.New(remoteclient.Options{
		Factory: transportFactory, BuildID: buildID,
		ClientInstanceID: protocol.ClientInstanceID(entry.ClientInstanceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := newRemoteRuntimeAdapter(store, entry, client)
	t.Cleanup(func() { adapter.shutdown(true) })

	ctx, cancel := context.WithTimeout(context.Background(), remoteE2ETestTimeout)
	defer cancel()
	if err := adapter.connectAndPersist(ctx); err != nil {
		t.Fatal(err)
	}
	connection, err := adapter.Connection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if connection.OS != "linux" || !connection.Capabilities.Features.CoreSession {
		t.Fatalf("Remote connection = %+v", connection)
	}

	browse, err := adapter.BrowseWorkspace(ctx, runtimeapi.BrowseWorkspaceInput{TypedPath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := adapter.OpenWorkspace(ctx, runtimeapi.OpenWorkspaceInput{PrimaryDirectory: browse.Directory.Ref})
	if err != nil {
		t.Fatal(err)
	}
	created, err := adapter.CreateSession(ctx, runtimeapi.CreateSessionInput{
		WorkspaceID: opened.Workspace.ID, AdditionalDirectories: []runtimeapi.DirectoryRef{},
		Topic: runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: "disconnect recovery"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: created.Session, HistoryTurns: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Session != created.Session || snapshot.Runtime.Running {
		t.Fatalf("initial snapshot = %+v", snapshot)
	}

	submitted, err := adapter.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{
		Session: created.Session, Input: "finish after disconnect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Kind != runtimeapi.SubmitTurn || submitted.TurnID == "" {
		t.Fatalf("submit result = %+v", submitted)
	}
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal("Host turn did not start")
	}

	transportFactory.dropCurrent()
	waitRemoteE2E(t, func() bool { return client.Status().State == remoteclient.StateDisconnected }, "Desktop did not observe attach loss")
	close(provider.release)
	select {
	case <-provider.finished:
	case <-ctx.Done():
		t.Fatal("Host-owned turn did not finish after attach loss")
	}

	if err := adapter.reconnectAndRestore(ctx); err != nil {
		t.Fatal(err)
	}
	waitRemoteE2E(t, func() bool {
		adapter.mu.RLock()
		defer adapter.mu.RUnlock()
		binding := adapter.sessions[mapRuntimeSessionRef(created.Session)]
		return binding != nil && binding.hasSnapshot && !binding.running
	}, "reconnected snapshot did not converge to the completed Host turn")

	recovered, err := adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: created.Session, HistoryTurns: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawPrompt, sawResponse bool
	for _, message := range recovered.History.Messages {
		if message.Content == nil {
			continue
		}
		sawPrompt = sawPrompt || strings.Contains(*message.Content, "finish after disconnect")
		sawResponse = sawResponse || strings.Contains(*message.Content, "completed on the Linux Host")
	}
	if !sawPrompt || !sawResponse {
		t.Fatalf("recovered history prompt=%v response=%v: %+v", sawPrompt, sawResponse, recovered.History.Messages)
	}

	if err := adapter.Detach(ctx); err != nil {
		t.Fatal(err)
	}
	saved, found, err := store.Get(entry.ID)
	if err != nil || !found {
		t.Fatalf("reload Host entry: found=%v err=%v", found, err)
	}
	if saved.ResumeLeaseID != "" {
		t.Fatalf("normal detach retained lease %q", saved.ResumeLeaseID)
	}
}

func TestRemoteDesktopBackendRestartResumesLinuxHostSessionEndToEnd(t *testing.T) {
	host := startRemoteE2ELinuxHost(t)
	storePath := filepath.Join(host.root, "desktop", "remote-hosts.json")
	bootstrapStore, err := NewRemoteHostStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := NewRemoteHostEntry("linux-restart-e2e", "Linux Restart E2E")
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrapStore.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	factory := &remoteE2EAttachFactory{endpoint: host.endpoint, buildID: host.buildID}

	firstApp, firstStore, firstManager := newRemoteE2EDesktopProcess(t, storePath, host.buildID, factory)
	ctx, cancel := context.WithTimeout(context.Background(), remoteE2ETestTimeout)
	defer cancel()
	if err := firstApp.ConnectRemoteHost(entry.ID); err != nil {
		t.Fatal(err)
	}
	firstTarget := firstManager.Snapshot()
	if firstTarget.State != TargetRemoteConnected || firstTarget.Target.Kind != TargetRemote || firstTarget.Target.ID != entry.ID {
		t.Fatalf("first Desktop target = %#v", firstTarget)
	}
	firstRuntime, err := firstManager.RuntimeAPI()
	if err != nil {
		t.Fatal(err)
	}
	firstAdapter, ok := firstRuntime.(*RemoteRuntimeAdapter)
	if !ok {
		t.Fatalf("first Desktop RuntimeAPI = %T, want *RemoteRuntimeAdapter", firstRuntime)
	}
	firstStatus := firstAdapter.client.Status()
	if firstStatus.HostEpoch == "" || firstStatus.LeaseID == "" {
		t.Fatalf("first Desktop connection status = %#v", firstStatus)
	}

	browse, err := firstApp.BrowseRemoteWorkspace(RemoteWorkspaceBrowseInput{TypedPath: host.workspace})
	if err != nil {
		t.Fatal(err)
	}
	createdStatus, err := firstApp.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: browse.Directory.Ref,
		TopicTitle:          "survive Desktop backend restart",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !createdStatus.SessionAttached || createdStatus.TabID == "" {
		t.Fatalf("first Desktop workbench status = %#v", createdStatus)
	}
	firstApp.remote.workbenchMu.RLock()
	createdSession, _, found := firstApp.remote.workbench.activeSession()
	if found {
		createdSession = &remoteWorkbenchSession{Created: createdSession.Created, Snapshot: cloneRemoteSessionSnapshot(createdSession.Snapshot)}
	}
	firstApp.remote.workbenchMu.RUnlock()
	if !found || createdSession == nil || !createdSession.Created.Session.Valid() {
		t.Fatal("first Desktop did not retain its Host-owned Session identity")
	}
	sessionRef := createdSession.Created.Session
	topicID := createdSession.Created.TopicID
	if err := firstApp.RenameProject(string(sessionRef.WorkspaceID), "Persisted Remote Workspace"); err != nil {
		t.Fatal(err)
	}
	if err := firstApp.SetProjectPinned(string(sessionRef.WorkspaceID), true); err != nil {
		t.Fatal(err)
	}
	persistedBeforeLoss, found, err := firstStore.Get(entry.ID)
	if err != nil || !found {
		t.Fatalf("persisted Host before loss: found=%v err=%v", found, err)
	}
	if persistedBeforeLoss.ClientInstanceID != entry.ClientInstanceID || persistedBeforeLoss.ResumeLeaseID != string(firstStatus.LeaseID) || persistedBeforeLoss.LayoutRef == "" {
		t.Fatalf("persisted Desktop Host state before loss = %#v", persistedBeforeLoss)
	}
	if err := firstApp.SubmitToTab(createdStatus.TabID, "finish after the Desktop backend restarts"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-host.provider.started:
	case <-ctx.Done():
		t.Fatal("Host turn did not start before Desktop backend exit")
	}

	// Model an abrupt Desktop backend exit: the attach stream disappears first,
	// TargetManager retains the Remote recovery identity, and process shutdown
	// abandons Desktop-owned resources without issuing remote/detach.
	factory.dropCurrent()
	lost := waitSnapshot(t, firstManager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.State == TargetRemoteReconnecting
	}, "first Desktop did not retain Remote recovery after attach loss")
	if lost.Target.Kind != TargetRemote || lost.Target.ID != entry.ID || !lost.RecoveryAvailable {
		t.Fatalf("first Desktop loss state = %#v", lost)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), remoteE2ETestTimeout)
	if err := firstManager.Shutdown(shutdownCtx); err != nil {
		shutdownCancel()
		t.Fatal(err)
	}
	shutdownCancel()
	persistedAfterExit, found, err := firstStore.Get(entry.ID)
	if err != nil || !found {
		t.Fatalf("persisted Host after backend exit: found=%v err=%v", found, err)
	}
	if persistedAfterExit.ClientInstanceID != persistedBeforeLoss.ClientInstanceID ||
		persistedAfterExit.ResumeLeaseID != persistedBeforeLoss.ResumeLeaseID ||
		persistedAfterExit.LayoutRef != persistedBeforeLoss.LayoutRef {
		t.Fatalf("abrupt Desktop exit changed recoverable state: before=%#v after=%#v", persistedBeforeLoss, persistedAfterExit)
	}

	// A wholly fresh App, Host store, TargetManager, client and adapter read the
	// same Desktop state. Connect is a new-process operation, but initialize
	// resumes the exact client/lease rather than silently selecting Local.
	secondApp, secondStore, secondManager := newRemoteE2EDesktopProcess(t, storePath, host.buildID, factory)
	if err := secondApp.ConnectRemoteHost(entry.ID); err != nil {
		t.Fatal(err)
	}
	secondTarget := secondManager.Snapshot()
	if secondTarget.State != TargetRemoteConnected || secondTarget.Target.Kind != TargetRemote || secondTarget.Target.ID != entry.ID {
		t.Fatalf("second Desktop target = %#v, want the persisted Remote Host", secondTarget)
	}
	secondApp.localTarget.admissionMu.RLock()
	localSuspended := secondApp.localTarget.suspended
	secondApp.localTarget.admissionMu.RUnlock()
	if !localSuspended {
		t.Fatal("second Desktop resumed the Host lease without suspending Local")
	}
	secondRuntime, err := secondManager.RuntimeAPI()
	if err != nil {
		t.Fatal(err)
	}
	secondAdapter, ok := secondRuntime.(*RemoteRuntimeAdapter)
	if !ok {
		t.Fatalf("second Desktop RuntimeAPI = %T, want *RemoteRuntimeAdapter", secondRuntime)
	}
	if secondAdapter == firstAdapter {
		t.Fatal("Desktop restart reused the old Remote adapter")
	}
	secondStatus := secondAdapter.client.Status()
	if secondStatus.HostEpoch != firstStatus.HostEpoch || secondStatus.LeaseID != firstStatus.LeaseID {
		t.Fatalf("resumed Host identity changed: first=%#v second=%#v", firstStatus, secondStatus)
	}
	loadedBySecondProcess, found, err := secondStore.Get(entry.ID)
	if err != nil || !found {
		t.Fatalf("second Desktop Host load: found=%v err=%v", found, err)
	}
	if loadedBySecondProcess.ClientInstanceID != entry.ClientInstanceID ||
		loadedBySecondProcess.ResumeLeaseID != persistedAfterExit.ResumeLeaseID ||
		loadedBySecondProcess.LayoutRef != persistedAfterExit.LayoutRef {
		t.Fatalf("second Desktop did not read the same persisted Host state: %#v", loadedBySecondProcess)
	}

	tree := secondApp.ListProjectTree()
	var restoredWorkspace *ProjectNode
	var restoredTopic bool
	for index := range tree {
		if tree[index].Root != string(sessionRef.WorkspaceID) {
			continue
		}
		restoredWorkspace = &tree[index]
		for _, topic := range tree[index].Children {
			if topic.TopicID == string(topicID) && len(topic.Children) == 0 && topic.Turns == 1 {
				// One Session is represented by its Topic row, matching Local.
				restoredTopic = true
			}
		}
		break
	}
	restoredSession := false
	for _, session := range secondApp.ListSessions() {
		ref, encoded, tokenErr := parseRemoteSessionToken(session.Path)
		if tokenErr == nil && encoded && ref == sessionRef && session.WorkspaceRoot == string(sessionRef.WorkspaceID) {
			restoredSession = true
			break
		}
	}
	if restoredWorkspace == nil || restoredWorkspace.Label != "Persisted Remote Workspace" || !restoredWorkspace.Pinned || !restoredTopic || !restoredSession {
		t.Fatalf("second Desktop Remote catalog/layout = %#v", tree)
	}
	meta, err := secondApp.OpenTopicSession("project", string(sessionRef.WorkspaceID), string(topicID), string(sessionRef.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.TargetKind != string(TargetRemote) || meta.WorkspaceID != string(sessionRef.WorkspaceID) || meta.SessionID != string(sessionRef.SessionID) {
		t.Fatalf("restored Remote tab = %#v", meta)
	}
	secondApp.remote.workbenchMu.RLock()
	restored := secondApp.remote.workbench.Sessions[sessionRef]
	if restored != nil {
		restored = &remoteWorkbenchSession{Created: restored.Created, Snapshot: cloneRemoteSessionSnapshot(restored.Snapshot)}
	}
	secondApp.remote.workbenchMu.RUnlock()
	if restored == nil || !restored.Snapshot.Runtime.Running || restored.Snapshot.Session != sessionRef {
		t.Fatalf("restart snapshot did not recover the running Host Session: %#v", restored)
	}

	close(host.provider.release)
	select {
	case <-host.provider.finished:
	case <-ctx.Done():
		t.Fatal("Host-owned turn did not finish after the second Desktop attached")
	}
	waitRemoteE2E(t, func() bool {
		secondApp.remote.workbenchMu.RLock()
		defer secondApp.remote.workbenchMu.RUnlock()
		current := secondApp.remote.workbench.Sessions[sessionRef]
		return current != nil && !current.Snapshot.Runtime.Running
	}, "second Desktop did not observe the recovered Host turn finish")
	if _, err := secondApp.OpenTopicSession("project", string(sessionRef.WorkspaceID), string(topicID), string(sessionRef.SessionID)); err != nil {
		t.Fatal(err)
	}
	secondApp.remote.workbenchMu.RLock()
	finalSession := secondApp.remote.workbench.Sessions[sessionRef]
	var finalSnapshot runtimeapi.SessionSnapshot
	if finalSession != nil {
		finalSnapshot = cloneRemoteSessionSnapshot(finalSession.Snapshot)
	}
	secondApp.remote.workbenchMu.RUnlock()
	var sawPrompt, sawResponse bool
	for _, message := range finalSnapshot.History.Messages {
		if message.Content == nil {
			continue
		}
		sawPrompt = sawPrompt || strings.Contains(*message.Content, "finish after the Desktop backend restarts")
		sawResponse = sawResponse || strings.Contains(*message.Content, "completed on the Linux Host")
	}
	if finalSnapshot.Session != sessionRef || finalSnapshot.Runtime.Running || !sawPrompt || !sawResponse {
		t.Fatalf("final recovered Session snapshot prompt=%v response=%v: %#v", sawPrompt, sawResponse, finalSnapshot)
	}
}

func waitRemoteE2E(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(remoteE2ETestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if condition() {
		return
	}
	t.Fatal(errors.New(message))
}

var _ event.Sink = event.Discard
