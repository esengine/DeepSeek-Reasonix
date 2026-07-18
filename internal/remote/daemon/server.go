// Package daemon implements the transport-facing Reasonix Remote Host. It
// owns attach connections and leases while host.RuntimeManager owns all Agent
// and Session work beyond an SSH transport's lifetime.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/configsummary"
	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/snapshotowner"
	"reasonix/internal/rpcwire"
)

var ErrServerClosed = errors.New("remote daemon server is closed")

// SessionMetadataProvider supplies the persisted/catalog metadata which is not
// owned by the Stage 2 SessionRuntime actor. The daemon overlays its frozen
// capabilities before returning the snapshot.
type SessionMetadataProvider func(context.Context, protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error)

type catalogGuardedControllerFactory struct {
	catalog   *catalog.Catalog
	hostEpoch protocol.HostEpoch
	delegate  host.ControllerFactory
}

func (f catalogGuardedControllerFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	if err := f.catalog.ValidateLiveTarget(ctx, f.hostEpoch, target); err != nil {
		return nil, err
	}
	return f.delegate.CreateController(ctx, target, sink)
}

func (f catalogGuardedControllerFactory) CreateControllerWithRecovery(
	ctx context.Context,
	target protocol.RuntimeTarget,
	sink event.Sink,
) (control.SessionAPI, control.SessionResumeState, error) {
	if err := f.catalog.ValidateLiveTarget(ctx, f.hostEpoch, target); err != nil {
		return nil, control.SessionResumeState{}, err
	}
	if recoveryFactory, ok := f.delegate.(host.RecoveryControllerFactory); ok {
		return recoveryFactory.CreateControllerWithRecovery(ctx, target, sink)
	}
	controller, err := f.delegate.CreateController(ctx, target, sink)
	return controller, control.SessionResumeState{}, err
}

type Options struct {
	BuildID           protocol.BuildID
	HostEpoch         protocol.HostEpoch
	HostInfo          protocol.HostInfo
	Capabilities      protocol.Capabilities
	Catalog           *catalog.Catalog
	ControllerFactory host.ControllerFactory
	Metadata          SessionMetadataProvider

	LeaseOptions       host.LeaseManagerOptions
	RuntimeOptions     host.RuntimeManagerOptions
	IdempotencyOptions idempotency.Options
	ContentRefOptions  contentref.Config
	HistoryOptions     history.Options
	NewSnapshotID      func() (protocol.SnapshotID, error)
	OnInternalError    func(protocol.Method, error)

	// allowUncataloguedTestRuntimes preserves narrow Stage 2 actor fixtures
	// which intentionally use synthetic targets. Production composition cannot
	// set this unexported test-only escape hatch.
	allowUncataloguedTestRuntimes bool
	// commitSubscription is a daemon-package test seam for the adapter phase
	// between provisional transport installation and Host transaction commit.
	// Production composition always uses SubscriptionInstall.Commit directly.
	commitSubscription func(*host.SubscriptionInstall) error
}

// Server owns one daemon epoch, its single-client lease, all live runtimes, and
// the set of attached transports. It contains no Unix/systemd policy and is
// therefore usable with any net.Listener.
type Server struct {
	ctx    context.Context
	cancel context.CancelFunc

	buildID                       protocol.BuildID
	hostEpoch                     protocol.HostEpoch
	hostInfo                      protocol.HostInfo
	capabilities                  protocol.Capabilities
	configSummary                 configsummary.Provider
	catalog                       *catalog.Catalog
	metadata                      SessionMetadataProvider
	newSnapshotID                 func() (protocol.SnapshotID, error)
	onInternalError               func(protocol.Method, error)
	allowUncataloguedTestRuntimes bool
	commitSubscription            func(*host.SubscriptionInstall) error

	leases    *host.LeaseManager
	runtimes  *host.RuntimeManager
	requests  *idempotency.Registry
	contents  *contentref.Store
	histories *history.Store
	snapshots *snapshotowner.Builder

	// File/Git query cursors are scoped to one live Runtime incarnation. The
	// cache keeps a service's opaque cursor key stable across pages while a
	// Runtime replacement necessarily installs a fresh service/key.
	fileGitMu       sync.Mutex
	fileGitServices map[protocol.RuntimeTarget]fileGitServiceEntry

	// Host catalog mutations use one daemon sequencer around idempotency Begin,
	// durable catalog commit, runtime admission, and outcome publication. The
	// Catalog has its own persistence lock, but that lock deliberately knows
	// nothing about requestId replay semantics.
	catalogMutationMu sync.Mutex

	// Serializes deterministic admission for a Session target that currently
	// has no live actor. Live Session mutations register and commit inside their
	// SessionRuntime sequencer instead.
	missingMutationMu sync.Mutex

	snapshotMu        sync.Mutex
	issuedSnapshotIDs map[protocol.SnapshotID]struct{}

	lifecycleMu  sync.Mutex
	closing      bool
	connections  map[net.Conn]struct{}
	listeners    map[net.Listener]struct{}
	connectionWG sync.WaitGroup
	closeOnce    sync.Once
}

func New(root context.Context, opts Options) (*Server, error) {
	if root == nil {
		root = context.Background()
	}
	if err := opts.BuildID.Validate(); err != nil {
		return nil, fmt.Errorf("daemon Build ID: %w", err)
	}
	if strings.TrimSpace(string(opts.HostEpoch)) == "" {
		return nil, errors.New("daemon hostEpoch is required")
	}
	if strings.TrimSpace(opts.HostInfo.OS) == "" || strings.TrimSpace(opts.HostInfo.Arch) == "" ||
		strings.TrimSpace(opts.HostInfo.ShellKind) == "" || strings.TrimSpace(opts.HostInfo.SandboxBackend) == "" {
		return nil, errors.New("daemon HostInfo fields are required")
	}
	if err := opts.Capabilities.Validate(); err != nil {
		return nil, fmt.Errorf("daemon capabilities: %w", err)
	}
	configSummary, err := configsummary.New(opts.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("daemon config summary: %w", err)
	}
	if opts.Catalog == nil {
		return nil, errors.New("daemon Catalog is required")
	}
	frozenTTL := time.Duration(protocol.LeaseTTLMillis) * time.Millisecond
	frozenPing := time.Duration(protocol.LeasePingIntervalMillis) * time.Millisecond
	if opts.LeaseOptions.TTL != 0 && opts.LeaseOptions.TTL != frozenTTL {
		return nil, fmt.Errorf("daemon lease TTL must be %s", frozenTTL)
	}
	if opts.LeaseOptions.PingInterval != 0 && opts.LeaseOptions.PingInterval != frozenPing {
		return nil, fmt.Errorf("daemon lease ping interval must be %s", frozenPing)
	}
	if opts.ControllerFactory == nil {
		return nil, errors.New("daemon ControllerFactory is required")
	}
	if opts.Metadata == nil {
		return nil, errors.New("daemon SessionMetadataProvider is required")
	}
	if opts.NewSnapshotID == nil {
		opts.NewSnapshotID = func() (protocol.SnapshotID, error) {
			id, err := randomOpaqueID("snapshot")
			return protocol.SnapshotID(id), err
		}
	}
	if opts.commitSubscription == nil {
		opts.commitSubscription = func(install *host.SubscriptionInstall) error { return install.Commit() }
	}

	ctx, cancel := context.WithCancel(root)
	contents, err := contentref.New(opts.HostEpoch, opts.ContentRefOptions)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("daemon contentRef store: %w", err)
	}
	histories, err := history.New(opts.HistoryOptions)
	if err != nil {
		contents.Close()
		cancel()
		return nil, fmt.Errorf("daemon history store: %w", err)
	}
	snapshots, err := snapshotowner.New(histories, contents)
	if err != nil {
		histories.Close()
		contents.Close()
		cancel()
		return nil, fmt.Errorf("daemon snapshot owner: %w", err)
	}
	controllerFactory := opts.ControllerFactory
	if !opts.allowUncataloguedTestRuntimes {
		controllerFactory = catalogGuardedControllerFactory{catalog: opts.Catalog, hostEpoch: opts.HostEpoch, delegate: opts.ControllerFactory}
	}
	runtimes, err := host.NewRuntimeManager(ctx, opts.HostEpoch, controllerFactory, opts.RuntimeOptions)
	if err != nil {
		histories.Close()
		contents.Close()
		cancel()
		return nil, err
	}
	requests, err := idempotency.New(opts.HostEpoch, opts.IdempotencyOptions)
	if err != nil {
		runtimes.Close()
		histories.Close()
		contents.Close()
		cancel()
		return nil, err
	}
	server := &Server{
		ctx: ctx, cancel: cancel,
		buildID: opts.BuildID, hostEpoch: opts.HostEpoch, hostInfo: opts.HostInfo,
		capabilities: opts.Capabilities, configSummary: configSummary, catalog: opts.Catalog, metadata: opts.Metadata,
		newSnapshotID: opts.NewSnapshotID, onInternalError: opts.OnInternalError,
		allowUncataloguedTestRuntimes: opts.allowUncataloguedTestRuntimes,
		commitSubscription:            opts.commitSubscription,
		leases:                        host.NewLeaseManager(opts.LeaseOptions), runtimes: runtimes, requests: requests,
		contents: contents, histories: histories, snapshots: snapshots,
		issuedSnapshotIDs: make(map[protocol.SnapshotID]struct{}),
		connections:       make(map[net.Conn]struct{}), listeners: make(map[net.Listener]struct{}),
	}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	return server, nil
}

// ServeListener accepts independent Remote transports until the listener or
// daemon closes. A Unix-domain listener is production's Linux entry point; TCP
// and in-memory listeners remain useful for tests without changing Host core.
func (s *Server) ServeListener(listener net.Listener) error {
	if listener == nil {
		return errors.New("remote daemon listener is nil")
	}
	if !s.registerListener(listener) {
		_ = listener.Close()
		return ErrServerClosed
	}
	defer s.unregisterListener(listener)

	for {
		connection, err := listener.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			var temporary interface{ Temporary() bool }
			if errors.As(err, &temporary) && temporary.Temporary() {
				continue
			}
			return err
		}
		go func() {
			_ = s.ServeConn(connection)
		}()
	}
}

// ServeConn serves exactly one attach transport. Closing or EOF only removes
// subscriptions; accepted Session work remains owned by RuntimeManager.
func (s *Server) ServeConn(connection net.Conn) error {
	if connection == nil {
		return errors.New("remote daemon connection is nil")
	}
	if !s.registerConnection(connection) {
		_ = connection.Close()
		return ErrServerClosed
	}
	defer s.unregisterConnection(connection)
	defer connection.Close()

	transportCtx, cancel := context.WithCancel(s.ctx)
	transport, err := newTransport(transportCtx, cancel, s, connection)
	if err != nil {
		cancel()
		return err
	}
	err = transport.wire.Serve(transportCtx)
	cancel()
	transport.cleanup(false)
	return err
}

func (s *Server) registerListener(listener net.Listener) bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closing {
		return false
	}
	s.listeners[listener] = struct{}{}
	return true
}

func (s *Server) unregisterListener(listener net.Listener) {
	s.lifecycleMu.Lock()
	delete(s.listeners, listener)
	s.lifecycleMu.Unlock()
}

func (s *Server) registerConnection(connection net.Conn) bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closing {
		return false
	}
	s.connections[connection] = struct{}{}
	s.connectionWG.Add(1)
	return true
}

func (s *Server) unregisterConnection(connection net.Conn) {
	s.lifecycleMu.Lock()
	delete(s.connections, connection)
	s.lifecycleMu.Unlock()
	s.connectionWG.Done()
}

// Close terminates transport and runtime ownership for the daemon epoch.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.closing = true
		listeners := make([]net.Listener, 0, len(s.listeners))
		for listener := range s.listeners {
			listeners = append(listeners, listener)
		}
		connections := make([]net.Conn, 0, len(s.connections))
		for connection := range s.connections {
			connections = append(connections, connection)
		}
		s.lifecycleMu.Unlock()

		s.cancel()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		for _, connection := range connections {
			_ = connection.Close()
		}
		s.runtimes.Close()
		s.connectionWG.Wait()
		s.histories.Close()
		s.contents.Close()
	})
}

func (s *Server) nextSnapshotID() (protocol.SnapshotID, error) {
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	for attempt := 0; attempt < 8; attempt++ {
		id, err := s.newSnapshotID()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(string(id)) == "" {
			return "", errors.New("generated snapshotId is empty")
		}
		if _, issued := s.issuedSnapshotIDs[id]; issued {
			continue
		}
		s.issuedSnapshotIDs[id] = struct{}{}
		return id, nil
	}
	return "", errors.New("snapshotId generator repeatedly returned an issued ID")
}

func (s *Server) reportInternal(method protocol.Method, err error) {
	if s.onInternalError != nil {
		s.onInternalError(method, err)
	}
}

func randomOpaqueID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}

type transportPhase uint8

const (
	transportNew transportPhase = iota
	transportReady
	transportDetaching
	transportDetached
)

type activeSubscription struct {
	id      protocol.SubscriptionID
	runtime *host.SessionRuntime
	binding host.LeaseBinding
	pumpCtx context.Context
	cancel  context.CancelFunc

	ownerMu        sync.Mutex
	historyBinding history.Binding
	ownersReleased bool
}

type transport struct {
	ctx    context.Context
	cancel context.CancelFunc
	server *Server
	raw    net.Conn
	wire   *rpcwire.Conn

	mu            sync.Mutex
	phase         transportPhase
	binding       host.LeaseBinding
	subscriptions map[protocol.SubscriptionID]*activeSubscription
	cleanupOnce   sync.Once
}

func newTransport(ctx context.Context, cancel context.CancelFunc, server *Server, raw net.Conn) (*transport, error) {
	t := &transport{
		ctx: ctx, cancel: cancel, server: server, raw: raw,
		phase: transportNew, subscriptions: make(map[protocol.SubscriptionID]*activeSubscription),
	}
	handlers := protocol.HandlerSet{
		protocol.MethodRemoteInitialize:   t.handleInitialize,
		protocol.MethodRemotePing:         t.handlePing,
		protocol.MethodRemoteDetach:       t.handleDetach,
		protocol.MethodHostCapabilities:   t.handleCapabilities,
		protocol.MethodWorkspaceBrowse:    t.handleWorkspaceBrowse,
		protocol.MethodWorkspaceOpen:      t.handleWorkspaceOpen,
		protocol.MethodWorkspaceList:      t.handleWorkspaceList,
		protocol.MethodWorkspaceClose:     t.handleWorkspaceClose,
		protocol.MethodCatalogWorkspace:   t.handleWorkspaceCatalog,
		protocol.MethodTopicList:          t.handleTopicList,
		protocol.MethodTopicCreate:        t.handleTopicCreate,
		protocol.MethodTopicRename:        t.handleTopicRename,
		protocol.MethodTopicDelete:        t.handleTopicDelete,
		protocol.MethodTopicTrash:         t.handleTopicTrash,
		protocol.MethodSessionList:        t.handleSessionList,
		protocol.MethodSessionCreate:      t.handleSessionCreate,
		protocol.MethodSessionRename:      t.handleSessionRename,
		protocol.MethodSessionClose:       t.handleSessionClose,
		protocol.MethodSessionTrashList:   t.handleSessionTrashList,
		protocol.MethodSessionTrash:       t.handleSessionTrash,
		protocol.MethodSessionRestore:     t.handleSessionRestore,
		protocol.MethodSessionPurge:       t.handleSessionPurge,
		protocol.MethodSessionSubscribe:   t.handleSubscribe,
		protocol.MethodSessionUnsubscribe: t.handleUnsubscribe,
		protocol.MethodSessionHistory:     t.handleHistory,
		protocol.MethodSessionContent:     t.handleContent,
		protocol.MethodSessionSubmit:      t.handleSubmit,
		protocol.MethodTurnSteer:          t.handleSteer,
		protocol.MethodTurnCancel:         t.handleCancel,
		protocol.MethodPromptApprove:      t.handlePromptApprove,
		protocol.MethodPromptAnswer:       t.handlePromptAnswer,
		protocol.MethodWorkspaceChanges:   t.handleWorkspaceChanges,
		protocol.MethodFileList:           t.handleFileList,
		protocol.MethodFileSearch:         t.handleFileSearch,
		protocol.MethodFilePreview:        t.handleFilePreview,
		protocol.MethodGitHistory:         t.handleGitHistory,
		protocol.MethodGitCommitDetail:    t.handleGitCommitDetail,
	}
	for _, additional := range []protocol.HandlerSet{
		operationHandlerSet(t),
		hostConfigSummaryHandlerSet(t, server.configSummary),
		statusCatalogHandlerSet(t),
		memoryResearchHandlerSet(t),
		composerHistoryHandlerSet(t),
		sessionLifecycleHandlerSet(t),
	} {
		for method, handler := range additional {
			if _, exists := handlers[method]; exists {
				return nil, fmt.Errorf("duplicate daemon handler %q", method)
			}
			handlers[method] = handler
		}
	}
	// The daemon claims the frozen Remote V1 Build ID, so starting with a
	// partial request surface would be a protocol violation rather than a
	// recoverable capability downgrade. Keep this as a complete-router check:
	// adding a frozen request method without its real Host handler must fail
	// daemon construction immediately.
	router, err := protocol.NewCompleteRouter(handlers, protocol.RouterOptions{OnInternalError: server.reportInternal})
	if err != nil {
		return nil, err
	}
	wireOptions := router.WireOptions()
	routerBefore := wireOptions.BeforeRequest
	wireOptions.BeforeRequest = func(method string, params json.RawMessage) error {
		if err := routerBefore(method, params); err != nil {
			return err
		}
		return t.beforeRequest(protocol.Method(method))
	}
	t.wire = rpcwire.NewConn(raw, raw, wireOptions)
	router.Bind(t.wire)
	return t, nil
}

func (t *transport) beforeRequest(method protocol.Method) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.phase {
	case transportNew:
		if method == protocol.MethodRemoteInitialize {
			return nil
		}
	case transportReady:
		if method == protocol.MethodRemoteDetach {
			t.phase = transportDetaching
		}
		return nil
	case transportDetaching:
		return &rpcwire.RPCError{Code: rpcwire.ErrInvalidRequest, Message: "remote transport is detaching"}
	case transportDetached:
		return &rpcwire.RPCError{Code: rpcwire.ErrInvalidRequest, Message: "remote transport is detached"}
	}
	return &rpcwire.RPCError{Code: rpcwire.ErrInvalidRequest, Message: "remote transport is not initialized"}
}

func (t *transport) handleInitialize(_ context.Context, value any) (any, error) {
	params := value.(protocol.InitializeParams)
	if err := protocol.CompareBuildID(t.server.buildID, params.BuildID); err != nil {
		t.failInitialize()
		var mismatch *protocol.BuildIDMismatch
		if errors.As(err, &mismatch) {
			return nil, protocol.MustRemoteError(protocol.ErrDaemonRestartRequired, protocol.ErrorOptions{
				Expected: mismatch.Expected, Actual: mismatch.Actual,
			})
		}
		return nil, err
	}
	grant, err := t.server.leases.Acquire(params.ClientInstanceID, params.ResumeLeaseID)
	if err != nil {
		t.failInitialize()
		return nil, err
	}
	if err := t.server.runtimes.ActivateAttachment(grant.Binding); err != nil {
		_ = t.server.leases.Detach(grant.Binding, grant.Binding.LeaseID)
		t.failInitialize()
		return nil, err
	}

	t.mu.Lock()
	t.binding = grant.Binding
	t.phase = transportReady
	t.mu.Unlock()
	return protocol.InitializeResult{
		BuildID: t.server.buildID, HostEpoch: t.server.hostEpoch,
		Lease: protocol.LeaseInfo{
			LeaseID: grant.Binding.LeaseID, TTLMillis: int(grant.TTL / time.Millisecond),
			PingIntervalMs: int(grant.PingInterval / time.Millisecond),
		},
		Host: t.server.hostInfo, Capabilities: t.server.capabilities,
	}, nil
}

func (t *transport) failInitialize() {
	t.mu.Lock()
	t.phase = transportDetached
	t.mu.Unlock()
}

func (t *transport) handlePing(_ context.Context, value any) (any, error) {
	params := value.(protocol.PingParams)
	binding, err := t.bindingForRequest()
	if err != nil {
		return nil, err
	}
	ttl, err := t.server.leases.Ping(binding, params.LeaseID)
	if err != nil {
		return nil, err
	}
	return protocol.PingResult{HostEpoch: t.server.hostEpoch, LeaseTTL: int(ttl / time.Millisecond)}, nil
}

func (t *transport) handleDetach(_ context.Context, value any) (any, error) {
	params := value.(protocol.DetachParams)
	binding, err := t.bindingForRequest()
	if err != nil {
		t.resetDetach()
		return nil, err
	}
	if params.LeaseID != binding.LeaseID {
		t.resetDetach()
		return nil, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	if err := t.server.leases.Validate(binding, false); err != nil {
		t.resetDetach()
		return nil, err
	}
	return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(writeErr error) {
		if writeErr != nil {
			return
		}
		t.cleanup(true)
		_ = t.raw.Close()
	}), nil
}

func (t *transport) resetDetach() {
	t.mu.Lock()
	if t.phase == transportDetaching {
		t.phase = transportReady
	}
	t.mu.Unlock()
}

func (t *transport) handleCapabilities(_ context.Context, value any) (any, error) {
	params := value.(protocol.HostCapabilitiesParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	return protocol.HostCapabilitiesResult{HostEpoch: t.server.hostEpoch, Capabilities: t.server.capabilities}, nil
}

func (t *transport) handleWorkspaceBrowse(_ context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceBrowseParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.Browse(params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodWorkspaceBrowse, nil, err)
	}
	return result, nil
}

func (t *transport) handleWorkspaceOpen(ctx context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceOpenParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodWorkspaceOpen),
		Target: idempotency.HostTarget(), Params: params,
	}
	var replay protocol.WorkspaceOpenResult
	// Registry lookup intentionally precedes epoch and catalog access. A retry
	// after a lost response must replay the original admission even when the
	// selected directory has since disappeared.
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}

	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	if err := t.validateLease(false); err != nil {
		return nil, err
	}
	claim, replayed, err := t.server.beginCatalogMutation(ctx, request, &replay)
	if err != nil || replayed {
		return replay, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, abortCatalogMutation(claim, err)
	}
	result, err := t.server.catalog.OpenWorkspace(params)
	if err != nil {
		mapped := t.server.mapCatalogError(protocol.MethodWorkspaceOpen, nil, err)
		return nil, finishCatalogRejection(claim, mapped)
	}
	if err := completeCatalogMutation(claim, result); err != nil {
		return nil, err
	}
	if result.Disposition == protocol.WorkspaceOpened {
		return t.catalogMutationResponse(result, protocol.CatalogHost, nil, protocol.CatalogWorkspaceCatalog), nil
	}
	return result, nil
}

func (t *transport) handleWorkspaceList(_ context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceListParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.ListWorkspaces(params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodWorkspaceList, nil, err)
	}
	return result, nil
}

func (t *transport) handleWorkspaceClose(ctx context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceCloseParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodWorkspaceClose), Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params}
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodWorkspaceClose, request, params.ExpectedHostEpoch, nil, func() (protocol.WorkspaceCloseResult, error) {
		reservation, reserveErr := t.server.runtimes.ReserveIdleWorkspace(params.WorkspaceID)
		if errors.Is(reserveErr, host.ErrWorkspaceInUse) {
			return protocol.WorkspaceCloseResult{}, protocol.MustRemoteError(protocol.ErrWorkspaceInUse, protocol.ErrorOptions{})
		}
		if reserveErr != nil {
			return protocol.WorkspaceCloseResult{}, reserveErr
		}
		defer reservation.Close()
		capability := t.server.catalog.IssueWorkspaceCloseCapability(params.WorkspaceID)
		closed, closeErr := t.server.catalog.CloseWorkspaceReserved(params, capability)
		if closeErr != nil {
			reservation.Abort()
			return protocol.WorkspaceCloseResult{}, closeErr
		}
		if commitErr := reservation.Commit(); commitErr != nil {
			t.server.reportInternal(protocol.MethodWorkspaceClose, commitErr)
		}
		return closed, nil
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogHost, nil, protocol.CatalogWorkspaceCatalog)
	}
	if replayed {
		return result, nil
	}
	if result.Disposition == protocol.WorkspaceClosed {
		return t.catalogMutationResponse(result, protocol.CatalogHost, nil, protocol.CatalogWorkspaceCatalog), nil
	}
	return result, nil
}

func (t *transport) handleWorkspaceCatalog(ctx context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceCatalogParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.WorkspaceCatalog(ctx, params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodCatalogWorkspace, nil, err)
	}
	return result, nil
}

func (t *transport) handleTopicList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TopicListParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.ListTopics(ctx, params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodTopicList, nil, err)
	}
	return result, nil
}

func (t *transport) handleTopicCreate(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TopicCreateParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodTopicCreate), Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params}
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodTopicCreate, request, params.ExpectedHostEpoch, nil, func() (protocol.TopicCreateResult, error) {
		return t.server.catalog.CreateTopic(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics)
	}
	if replayed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics), nil
}

func (t *transport) handleTopicRename(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TopicRenameParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodTopicRename), Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params}
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodTopicRename, request, params.ExpectedHostEpoch, nil, func() (protocol.TopicRenameResult, error) {
		return t.server.catalog.RenameTopic(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics, protocol.CatalogSessions)
	}
	if replayed {
		return result, nil
	}
	if !changed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics, protocol.CatalogSessions), nil
}

func (t *transport) handleTopicDelete(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TopicDeleteParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodTopicDelete), Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params}
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodTopicDelete, request, params.ExpectedHostEpoch, nil, func() (protocol.TopicDeleteResult, error) {
		return t.server.catalog.DeleteTopic(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics)
	}
	if replayed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics), nil
}

func (t *transport) handleTopicTrash(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TopicTrashParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodTopicTrash), Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params}
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodTopicTrash, request, params.ExpectedHostEpoch, nil, func() (protocol.TopicTrashResult, error) {
		targets, targetErr := t.server.catalog.TopicTargets(ctx, params.ExpectedHostEpoch, params.WorkspaceID, params.TopicID)
		if targetErr != nil {
			return protocol.TopicTrashResult{}, targetErr
		}
		reservation, reserveErr := t.server.runtimes.ReserveTargetsForRemoval(targets)
		if reserveErr != nil {
			return protocol.TopicTrashResult{}, reserveErr
		}
		defer reservation.Close()
		trashed, trashErr := t.server.catalog.TrashTopicReserved(params)
		if trashErr != nil {
			reservation.Abort()
			return protocol.TopicTrashResult{}, trashErr
		}
		reservation.Commit()
		return trashed, nil
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics, protocol.CatalogSessions, protocol.CatalogTrash)
	}
	if replayed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogTopics, protocol.CatalogSessions, protocol.CatalogTrash), nil
}

func (t *transport) handleSessionList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionListParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.ListSessions(ctx, params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodSessionList, nil, err)
	}
	for index := range result.Items {
		if summary, ok := t.server.runtimes.SessionSummary(result.Items[index].Target); ok {
			result.Items[index].Runtime = summary
		}
	}
	return result, nil
}

func (t *transport) handleSessionCreate(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionCreateParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionCreate),
		Target: idempotency.WorkspaceTarget(params.WorkspaceID), Params: params,
	}
	var replay protocol.SessionCreateResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}

	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	if err := t.validateLease(false); err != nil {
		return nil, err
	}
	claim, replayed, err := t.server.beginCatalogMutation(ctx, request, &replay)
	if err != nil || replayed {
		return replay, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, abortCatalogMutation(claim, err)
	}
	created, err := t.server.catalog.CreateSession(ctx, params)
	if err != nil {
		mapped := t.server.mapCatalogError(protocol.MethodSessionCreate, nil, err)
		return nil, finishCatalogRejection(claim, mapped)
	}

	// Catalog persistence is the durable semantic commit. If Controller startup
	// fails afterward, preserve that allocated target and cache a target-bearing
	// RUNTIME_START_FAILED outcome. Retrying the same requestId must never create
	// a second Session; a later subscribe can retry cold runtime construction for
	// the already-listed target.
	runtime, err := t.server.runtimes.GetOrCreate(created.Target)
	if err != nil {
		t.server.reportInternal(protocol.MethodSessionCreate, err)
		target := created.Target
		rejection := protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
		finished := cacheCatalogRejection(claim, rejection)
		t.notifyCatalogChanged(protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTopics)
		return nil, finished
	}
	result := created.Result(runtime.Epoch())
	if err := completeCatalogMutation(claim, result); err != nil {
		t.notifyCatalogChanged(protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTopics)
		return nil, err
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTopics), nil
}

func (t *transport) handleSessionRename(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionRenameParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionRename), Target: idempotency.SessionTarget(params.Target), Params: params}
	target := params.Target
	result, replayed, changed, err := runCatalogMutation(ctx, t, protocol.MethodSessionRename, request, params.ExpectedHostEpoch, &target, func() (protocol.SessionRenameResult, error) {
		return t.server.catalog.RenameSession(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions)
	}
	if replayed {
		return result, nil
	}
	if changed {
		return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions), nil
	}
	return result, nil
}

func (t *transport) handleSessionClose(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionCloseParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionClose), Target: idempotency.SessionTarget(params.Target), Params: params}
	var replay protocol.SessionCloseResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}

	for {
		result, replayed, err := t.server.runtimes.CloseSessionMutation(ctx, t.server.requests, params, func() error {
			return t.validateLease(false)
		})
		if err == nil {
			if !replayed && result.Disposition == protocol.SessionReleased {
				return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions), nil
			}
			return result, nil
		}
		if !errors.Is(err, host.ErrRuntimeClosed) {
			return nil, err
		}

		// The target-scoped cold sequencer keeps the runtime absent through
		// catalog validation and requestId completion. If a subscribe won the
		// race, retry through the live actor instead of returning already_closed.
		t.server.missingMutationMu.Lock()
		absence, reserveErr := t.server.runtimes.ReserveRuntimeAbsence(params.Target)
		if errors.Is(reserveErr, host.ErrRuntimeBusy) {
			t.server.missingMutationMu.Unlock()
			continue
		}
		if reserveErr != nil {
			t.server.missingMutationMu.Unlock()
			return nil, reserveErr
		}
		if err := t.validateLease(false); err != nil {
			absence.Close()
			t.server.missingMutationMu.Unlock()
			return nil, err
		}
		attempt, beginErr := t.server.requests.Begin(request)
		if beginErr != nil {
			absence.Close()
			t.server.missingMutationMu.Unlock()
			return nil, beginErr
		}
		claim, owns := attempt.Claim()
		if !owns {
			absence.Close()
			t.server.missingMutationMu.Unlock()
			outcome, waitErr := attempt.Wait(ctx)
			if waitErr != nil {
				return nil, waitErr
			}
			if decodeErr := outcome.Decode(&replay); decodeErr != nil {
				return nil, decodeErr
			}
			return replay, nil
		}
		if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
			_ = claim.Abort(err)
			absence.Close()
			t.server.missingMutationMu.Unlock()
			return nil, err
		}
		if err := t.server.catalog.ValidateLiveTarget(ctx, params.ExpectedHostEpoch, params.Target); err != nil {
			mapped := t.server.mapCatalogError(protocol.MethodSessionClose, &params.Target, err)
			finished := finishCatalogRejection(claim, mapped)
			absence.Close()
			t.server.missingMutationMu.Unlock()
			return nil, finished
		}
		result = protocol.SessionCloseResult{Disposition: protocol.SessionAlreadyClosed}
		if err := completeCatalogMutation(claim, result); err != nil {
			absence.Close()
			t.server.missingMutationMu.Unlock()
			return nil, err
		}
		absence.Close()
		t.server.missingMutationMu.Unlock()
		return result, nil
	}
}

func (t *transport) handleSessionTrashList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionTrashListParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	result, err := t.server.catalog.ListTrash(ctx, params)
	if err != nil {
		return nil, t.server.mapCatalogError(protocol.MethodSessionTrashList, nil, err)
	}
	return result, nil
}

func (t *transport) handleSessionTrash(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionTrashParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionTrash), Target: idempotency.SessionTarget(params.Target), Params: params}
	result, replayed, changed, err := runColdSessionMutation(ctx, t, protocol.MethodSessionTrash, request, params.ExpectedHostEpoch, params.Target, func() (protocol.SessionTrashResult, error) {
		return t.server.catalog.TrashSessionReserved(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTrash, protocol.CatalogTopics)
	}
	if replayed {
		return result, nil
	}
	if result.Disposition != protocol.DispositionAlreadyTrashed {
		return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTrash, protocol.CatalogTopics), nil
	}
	return result, nil
}

func (t *transport) handleSessionRestore(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionRestoreParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionRestore), Target: idempotency.SessionTarget(params.Target), Params: params}
	result, replayed, changed, err := runColdSessionMutation(ctx, t, protocol.MethodSessionRestore, request, params.ExpectedHostEpoch, params.Target, func() (protocol.SessionRestoreResult, error) {
		return t.server.catalog.RestoreSession(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTrash, protocol.CatalogTopics)
	}
	if replayed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogSessions, protocol.CatalogTrash, protocol.CatalogTopics), nil
}

func (t *transport) handleSessionPurge(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionPurgeParams)
	request := idempotency.Request{RequestID: params.RequestID, Method: string(protocol.MethodSessionPurge), Target: idempotency.SessionTarget(params.Target), Params: params}
	result, replayed, changed, err := runColdSessionMutation(ctx, t, protocol.MethodSessionPurge, request, params.ExpectedHostEpoch, params.Target, func() (protocol.SessionPurgeResult, error) {
		return t.server.catalog.PurgeSession(params)
	})
	if err != nil {
		return result, t.finishCatalogMutationError(err, changed, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogTrash, protocol.CatalogTopics)
	}
	if replayed {
		return result, nil
	}
	return t.catalogMutationResponse(result, protocol.CatalogWorkspace, []protocol.WorkspaceID{params.Target.WorkspaceID}, protocol.CatalogTrash, protocol.CatalogTopics), nil
}

func (t *transport) handleSubscribe(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionSubscribeParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(params.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	if !t.server.allowUncataloguedTestRuntimes {
		if err := t.server.catalog.ValidateLiveTarget(ctx, params.ExpectedHostEpoch, params.Target); err != nil {
			return nil, t.server.mapCatalogError(protocol.MethodSessionSubscribe, &params.Target, err)
		}
	}
	binding, err := t.bindingForRequest()
	if err != nil {
		return nil, err
	}
	if params.ReplaceSubscriptionID != "" && !t.ownsSubscription(params.ReplaceSubscriptionID, binding) {
		// replaceSubscriptionId is transport-local even when RuntimeManager is
		// retaining the terminal Host channel. Never let another SSH transport
		// probe or consume that identity.
		return nil, protocol.MustRemoteError(protocol.ErrSubscriptionNotFound, protocol.ErrorOptions{})
	}
	attachment := host.AttachmentForLease(binding)
	snapshotID, err := t.server.nextSnapshotID()
	if err != nil {
		return nil, err
	}
	install, err := t.server.runtimes.SubscribeSnapshot(ctx, params.Target, attachment, params.ReplaceSubscriptionID, snapshotID)
	if err != nil {
		if errors.Is(err, host.ErrSubscriptionNotFound) {
			return nil, protocol.MustRemoteError(protocol.ErrSubscriptionNotFound, protocol.ErrorOptions{})
		}
		if errors.Is(err, host.ErrRuntimeManagerClosed) {
			target := params.Target
			return nil, protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &target})
		}
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = install.Abort()
		}
	}()
	snapshot, err := t.projectSnapshot(ctx, install.Subscription.Snapshot, params.PageTurns, binding.LeaseID)
	if err != nil {
		return nil, err
	}
	historyBinding := install.Subscription.Snapshot.Capture.History.Binding
	active, previous := t.installSubscription(install.Runtime, install.Subscription, binding, params.ReplaceSubscriptionID, historyBinding)
	if active == nil {
		return nil, errors.New("subscription ended while its snapshot was being encoded")
	}
	if err := t.server.commitSubscription(install); err != nil {
		active.cancel()
		t.rollbackSubscriptionInstall(active, params.ReplaceSubscriptionID, previous)
		active.releaseOwners(t.server.snapshots)
		return nil, err
	}
	committed = true
	if previous != nil {
		previous.cancel()
		previous.releaseReplacementOwners(t.server.snapshots)
	}
	result := protocol.SessionSubscribeResult{SubscriptionID: install.Subscription.ID, Snapshot: snapshot}
	return rpcwire.RespondThen(result, func(writeErr error) {
		if writeErr != nil {
			active.cancel()
			t.removeSubscription(active.id, active)
			return
		}
		go t.pumpSubscription(active.pumpCtx, active, install.Subscription.Messages)
	}), nil
}

func (t *transport) handleUnsubscribe(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionUnsubscribeParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	active := t.takeSubscription(params.SubscriptionID)
	if active == nil {
		return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
	}
	active.cancel()
	active.releaseOwners(t.server.snapshots)
	binding, err := t.bindingForRequest()
	if err != nil {
		return nil, err
	}
	err = t.server.runtimes.Unsubscribe(ctx, host.AttachmentForLease(binding), params.SubscriptionID)
	if err != nil && !errors.Is(err, host.ErrRuntimeClosed) {
		return nil, err
	}
	return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
}

func (t *transport) handleHistory(_ context.Context, value any) (any, error) {
	params := value.(protocol.SessionHistoryParams)
	leaseBinding, err := t.bindingForRequest()
	if err != nil {
		return nil, err
	}
	if err := t.server.leases.Validate(leaseBinding, true); err != nil {
		return nil, err
	}
	requested := history.Binding{
		SnapshotID: params.SnapshotID, HostEpoch: params.ExpectedHostEpoch,
		Target: params.Target, RuntimeEpoch: params.ExpectedRuntimeEpoch,
	}
	if !t.hasHistoryBinding(requested, leaseBinding) || !t.server.histories.Valid(requested) {
		return nil, snapshotExpired(params.Target)
	}
	runtime, current := t.server.runtimes.Runtime(params.Target)
	if !current || runtime.Epoch() != params.ExpectedRuntimeEpoch {
		return nil, snapshotExpired(params.Target)
	}
	page, err := t.server.snapshots.BuildOlderHistory(
		requested, params.Cursor, params.PageTurns, leaseBinding.LeaseID,
	)
	if err != nil {
		return nil, err
	}
	return page, nil
}

func (t *transport) handleContent(_ context.Context, value any) (any, error) {
	params := value.(protocol.SessionContentParams)
	binding, err := t.bindingForRequest()
	if err != nil {
		return nil, err
	}
	if err := t.server.leases.Validate(binding, true); err != nil {
		return nil, err
	}
	result, owner, err := t.server.contents.ReadBoundForLease(binding.LeaseID, params)
	if errors.Is(err, contentref.ErrInvalidOffset) {
		// The frozen error table intentionally exposes no content length oracle.
		// A stale/impossible offset follows the same resubscribe path as an
		// expired opaque reference.
		return nil, protocol.MustRemoteError(protocol.ErrContentRefExpired, protocol.ErrorOptions{})
	}
	if err != nil {
		return nil, err
	}
	if owner.HostEpoch != t.server.hostEpoch || !t.contentOwnerIsCurrent(binding, owner) {
		invalidateContentOwner(t.server.contents, owner)
		return nil, protocol.MustRemoteError(protocol.ErrContentRefExpired, protocol.ErrorOptions{})
	}
	return result, nil
}

func (t *transport) contentOwnerIsCurrent(leaseBinding host.LeaseBinding, owner contentref.ReferenceBinding) bool {
	if owner.LeaseID != leaseBinding.LeaseID {
		return false
	}
	runtime, ok := t.server.runtimes.Runtime(owner.Target)
	if !ok || runtime.Epoch() != owner.RuntimeEpoch {
		return false
	}
	switch owner.Kind {
	case contentref.ReferenceEvent:
		return owner.Seq != 0
	case contentref.ReferenceSnapshot:
		binding, ok := t.historyBindingForSnapshot(owner.SnapshotID, leaseBinding)
		return ok && binding.Target == owner.Target && binding.RuntimeEpoch == owner.RuntimeEpoch &&
			t.server.histories.Valid(binding)
	default:
		return false
	}
}

func (t *transport) historyBindingForSnapshot(snapshotID protocol.SnapshotID, leaseBinding host.LeaseBinding) (history.Binding, bool) {
	if snapshotID == "" {
		return history.Binding{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, active := range t.subscriptions {
		if binding, ok := active.ownedHistoryBinding(snapshotID, leaseBinding); ok {
			return binding, true
		}
	}
	return history.Binding{}, false
}

func (t *transport) hasHistoryBinding(binding history.Binding, leaseBinding host.LeaseBinding) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, active := range t.subscriptions {
		if active.ownsHistoryBinding(binding, leaseBinding) {
			return true
		}
	}
	return false
}

func (t *transport) handleSubmit(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionSubmitParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionSubmit),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.SessionSubmitResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	result, err := runtime.ComposerSubmitMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
	if err == nil {
		return result, nil
	}
	var delegation *host.ComposerDelegationError
	if errors.As(err, &delegation) {
		return t.handleDelegatedComposerMutation(ctx, params, delegation)
	}
	return nil, err
}

func (t *transport) handleCancel(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TurnCancelParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodTurnCancel),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.TurnCancelResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.CancelTurnMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSteer(ctx context.Context, value any) (any, error) {
	params := value.(protocol.TurnSteerParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodTurnSteer),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.TurnSteerResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.SteerMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handlePromptApprove(ctx context.Context, value any) (any, error) {
	params := value.(protocol.PromptApproveParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodPromptApprove),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.PromptResolvedResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.ApproveMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handlePromptAnswer(ctx context.Context, value any) (any, error) {
	params := value.(protocol.PromptAnswerParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodPromptAnswer),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.PromptResolvedResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.AnswerMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

// sessionMutationGuard revalidates only transport lease ownership inside the
// Session actor immediately before idempotency Begin. Runtime ownership is
// checked by SessionRuntime from its lock-free current marker: consulting the
// RuntimeManager here would invert manager-lock/actor ordering against catalog
// lifecycle reservations.
func (t *transport) sessionMutationGuard() func() error {
	return func() error {
		return t.validateLease(false)
	}
}

func runCatalogMutation[T any](
	ctx context.Context,
	t *transport,
	method protocol.Method,
	request idempotency.Request,
	expectedHostEpoch protocol.HostEpoch,
	target *protocol.RuntimeTarget,
	apply func() (T, error),
) (T, bool, bool, error) {
	var zero T
	if err := t.validateLease(true); err != nil {
		return zero, false, false, err
	}
	var replay T
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, found, false, err
	}
	t.server.catalogMutationMu.Lock()
	defer t.server.catalogMutationMu.Unlock()
	if err := t.validateLease(false); err != nil {
		return zero, false, false, err
	}
	claim, replayed, err := t.server.beginCatalogMutation(ctx, request, &replay)
	if err != nil || replayed {
		return replay, replayed, false, err
	}
	if err := t.validateHostEpoch(expectedHostEpoch); err != nil {
		return zero, false, false, abortCatalogMutation(claim, err)
	}
	beforeRevision := t.server.catalog.Revision()
	result, err := apply()
	changed := t.server.catalog.Revision() != beforeRevision
	if err != nil {
		mapped := t.server.mapCatalogError(method, target, err)
		if changed {
			return zero, false, true, finishCommittedCatalogRejection(claim, mapped)
		}
		return zero, false, false, finishCatalogRejection(claim, mapped)
	}
	if err := completeCatalogMutation(claim, result); err != nil {
		return zero, false, changed, err
	}
	return result, false, changed, nil
}

func runColdSessionMutation[T any](
	ctx context.Context,
	t *transport,
	method protocol.Method,
	request idempotency.Request,
	expectedHostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
	apply func() (T, error),
) (T, bool, bool, error) {
	targetCopy := target
	return runCatalogMutation(ctx, t, method, request, expectedHostEpoch, &targetCopy, func() (T, error) {
		reservation, err := t.server.runtimes.ReserveTargetsForRemoval([]protocol.RuntimeTarget{target})
		if err != nil {
			var zero T
			return zero, err
		}
		defer reservation.Close()
		result, err := apply()
		if err != nil {
			reservation.Abort()
			var zero T
			return zero, err
		}
		reservation.Commit()
		return result, nil
	})
}

func (t *transport) catalogMutationResponse(
	result any,
	scope protocol.CatalogScope,
	affected []protocol.WorkspaceID,
	kinds ...protocol.CatalogKind,
) any {
	t.notifyCatalogChanged(scope, affected, kinds...)
	return result
}

// notifyCatalogChanged is intentionally independent from RPC success. A
// durable catalog commit may be followed by runtime startup or staged-cleanup
// failure; the owning transport still receives exactly one revision signal,
// while requestId replay returns before this hook and cannot emit a duplicate.
func (t *transport) notifyCatalogChanged(
	scope protocol.CatalogScope,
	affected []protocol.WorkspaceID,
	kinds ...protocol.CatalogKind,
) {
	change := protocol.CatalogChanged{
		HostEpoch: t.server.hostEpoch,
		Revision:  t.server.catalog.Revision(),
		Scope:     scope,
		Kinds:     append([]protocol.CatalogKind(nil), kinds...),
	}
	if len(affected) != 0 {
		change.AffectedWorkspaceIDs = append([]protocol.WorkspaceID(nil), affected...)
	}
	go func() {
		if err := protocol.ValidateNotification(protocol.MethodCatalogChanged, change); err != nil {
			t.server.reportInternal(protocol.MethodCatalogChanged, err)
			return
		}
		binding, err := t.bindingForRequest()
		if err != nil || t.server.leases.Validate(binding, false) != nil {
			return
		}
		if err := t.wire.Notify(string(protocol.MethodCatalogChanged), change); err != nil {
			t.server.reportInternal(protocol.MethodCatalogChanged, err)
		}
	}()
}

func (t *transport) finishCatalogMutationError(
	err error,
	changed bool,
	scope protocol.CatalogScope,
	affected []protocol.WorkspaceID,
	kinds ...protocol.CatalogKind,
) error {
	if err != nil && changed {
		t.notifyCatalogChanged(scope, affected, kinds...)
	}
	return err
}

// lookupMutation is the required registry-before-epoch preflight. Lookup does
// not create a record on a miss; the owning Session actor (or the serialized
// missing-target path) calls Begin at its semantic admission boundary.
func (s *Server) lookupMutation(ctx context.Context, request idempotency.Request, destination any) (bool, error) {
	attempt, found, err := s.requests.Lookup(request)
	if err != nil || !found {
		return found, err
	}
	outcome, err := attempt.Wait(ctx)
	if err != nil {
		return true, err
	}
	return true, outcome.Decode(destination)
}

// beginCatalogMutation runs only while catalogMutationMu is held. Begin's
// second lookup closes the race after the mandatory lock-free preflight. Any
// non-owner observed here has already committed in an earlier sequencer turn,
// so waiting cannot block the sequencer owner.
func (s *Server) beginCatalogMutation(
	ctx context.Context,
	request idempotency.Request,
	destination any,
) (*idempotency.Claim, bool, error) {
	attempt, err := s.requests.Begin(request)
	if err != nil {
		return nil, false, err
	}
	claim, owns := attempt.Claim()
	if owns {
		return claim, false, nil
	}
	outcome, err := attempt.Wait(ctx)
	if err != nil {
		return nil, true, err
	}
	return nil, true, outcome.Decode(destination)
}

func (s *Server) mapCatalogError(method protocol.Method, target *protocol.RuntimeTarget, err error) error {
	if err == nil {
		return nil
	}
	code, ok := catalog.ErrorCode(err)
	if !ok {
		return err
	}
	options := protocol.ErrorOptions{}
	if target != nil {
		copyTarget := *target
		options.Target = &copyTarget
	}
	remote, mapErr := protocol.NewRemoteError(code, options)
	if mapErr != nil {
		// The original catalog detail remains available to the Router's internal
		// diagnostic callback, while the peer receives only "internal error".
		return fmt.Errorf("map %s catalog error %s: %w", method, code, err)
	}
	return remote
}

func abortCatalogMutation(claim *idempotency.Claim, cause error) error {
	if err := claim.Abort(cause); err != nil {
		return err
	}
	return cause
}

func finishCatalogRejection(claim *idempotency.Claim, rejection error) error {
	var remote *protocol.RemoteError
	if !errors.As(rejection, &remote) || remote == nil {
		return abortCatalogMutation(claim, rejection)
	}
	if !cacheableCatalogRejection(remote.Code) {
		return abortCatalogMutation(claim, remote)
	}
	return cacheCatalogRejection(claim, remote)
}

// finishCommittedCatalogRejection runs only after the catalog revision proves
// durable semantic change. Every protocol business error at that point is the
// first requestId outcome even if the generic pre-commit classifier would have
// treated its infrastructure category as retryable.
func finishCommittedCatalogRejection(claim *idempotency.Claim, rejection error) error {
	var remote *protocol.RemoteError
	if !errors.As(rejection, &remote) || remote == nil {
		return abortCatalogMutation(claim, rejection)
	}
	return cacheCatalogRejection(claim, remote)
}

// cacheCatalogRejection intentionally enumerates only state- and input-bound
// catalog decisions. Persistence/query infrastructure failures are retryable
// with the same requestId because no durable semantic commit occurred.
func cacheableCatalogRejection(code protocol.ReasonixErrorCode) bool {
	switch code {
	case protocol.ErrStaleDirectoryRef,
		protocol.ErrDirectoryNotFound,
		protocol.ErrNotDirectory,
		protocol.ErrPermissionDenied,
		protocol.ErrWorkspaceNotFound,
		protocol.ErrWorkspaceInUse,
		protocol.ErrSessionNotFound,
		protocol.ErrWorkspaceSessionMismatch,
		protocol.ErrSessionTrashed,
		protocol.ErrSessionBusy,
		protocol.ErrSessionCleanupPending,
		protocol.ErrTopicNotFound,
		protocol.ErrTopicNotEmpty,
		protocol.ErrTrashEntryNotFound,
		protocol.ErrRecoveryGuardFailed,
		protocol.ErrInvalidProfile,
		protocol.ErrModelNotAvailable,
		protocol.ErrEffortNotSupported:
		return true
	default:
		return false
	}
}

func cacheCatalogRejection(claim *idempotency.Claim, remote *protocol.RemoteError) error {
	if err := claim.Reject(remote); err != nil {
		_ = claim.Abort(err)
		return err
	}
	return remote
}

func completeCatalogMutation(claim *idempotency.Claim, result any) error {
	outcome, err := idempotency.PrepareSuccess(result)
	if err != nil {
		_ = claim.Abort(err)
		return err
	}
	if err := claim.Resolve(outcome); err != nil {
		_ = claim.Abort(err)
		return err
	}
	return nil
}

// rejectMissingSessionMutation is the target-absent semantic sequencer. The
// transport-generation guard runs while missingMutationMu is held and before
// Begin, so a replaced attach cannot cache a SESSION_NOT_FOUND decision that
// the current generation never made.
func (s *Server) rejectMissingSessionMutation(
	ctx context.Context,
	request idempotency.Request,
	expectedHostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
	destination any,
	beforeBegin func() error,
) error {
	s.missingMutationMu.Lock()
	if beforeBegin != nil {
		if err := beforeBegin(); err != nil {
			s.missingMutationMu.Unlock()
			return err
		}
	}
	attempt, err := s.requests.Begin(request)
	if err != nil {
		s.missingMutationMu.Unlock()
		return err
	}
	claim, owns := attempt.Claim()
	if !owns {
		s.missingMutationMu.Unlock()
		outcome, err := attempt.Wait(ctx)
		if err != nil {
			return err
		}
		return outcome.Decode(destination)
	}
	if expectedHostEpoch != s.hostEpoch {
		stale := protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{
			Expected: string(s.hostEpoch), Actual: string(expectedHostEpoch),
		})
		err = claim.Abort(stale)
		s.missingMutationMu.Unlock()
		if err != nil {
			return err
		}
		return stale
	}
	targetCopy := target
	missing := protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{Target: &targetCopy})
	err = claim.Reject(missing)
	s.missingMutationMu.Unlock()
	if err != nil {
		_ = claim.Abort(err)
		return err
	}
	return missing
}

func (t *transport) validateSessionMutation(mutation protocol.SessionMutation) error {
	if err := t.validateHostEpoch(mutation.ExpectedHostEpoch); err != nil {
		return err
	}
	_, err := t.currentRuntime(mutation.Target, mutation.ExpectedRuntimeEpoch)
	return err
}

func (t *transport) currentRuntime(target protocol.RuntimeTarget, expected protocol.RuntimeEpoch) (*host.SessionRuntime, error) {
	runtime, ok := t.server.runtimes.Runtime(target)
	if !ok {
		targetCopy := target
		return nil, protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{Target: &targetCopy})
	}
	if runtime.Epoch() != expected {
		targetCopy := target
		return nil, protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{
			Target: &targetCopy, Expected: string(runtime.Epoch()), Actual: string(expected),
		})
	}
	return runtime, nil
}

func (t *transport) validateHostEpoch(expected protocol.HostEpoch) error {
	if expected == t.server.hostEpoch {
		return nil
	}
	return protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{
		Expected: string(t.server.hostEpoch), Actual: string(expected),
	})
}

func (t *transport) validateLease(renew bool) error {
	binding, err := t.bindingForRequest()
	if err != nil {
		return err
	}
	return t.server.leases.Validate(binding, renew)
}

func (t *transport) bindingForRequest() (host.LeaseBinding, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase != transportReady && t.phase != transportDetaching {
		return host.LeaseBinding{}, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	if t.binding.LeaseID == "" || t.binding.ClientInstanceID == "" || t.binding.Generation == 0 {
		return host.LeaseBinding{}, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	return t.binding, nil
}

func (t *transport) mapRuntimeError(target protocol.RuntimeTarget, expected protocol.RuntimeEpoch, err error) error {
	targetCopy := target
	switch {
	case errors.Is(err, host.ErrRuntimeBusy):
		return protocol.MustRemoteError(protocol.ErrTurnAlreadyRunning, protocol.ErrorOptions{Target: &targetCopy})
	case errors.Is(err, host.ErrTurnNotActive):
		return protocol.MustRemoteError(protocol.ErrTurnNotActive, protocol.ErrorOptions{Target: &targetCopy})
	case errors.Is(err, host.ErrTurnMismatch):
		var mismatch *host.TurnMismatchError
		options := protocol.ErrorOptions{Target: &targetCopy}
		if errors.As(err, &mismatch) {
			options.Expected = string(mismatch.Current)
			options.Actual = string(mismatch.Requested)
		}
		return protocol.MustRemoteError(protocol.ErrTurnMismatch, options)
	case errors.Is(err, host.ErrRuntimeClosed):
		options := protocol.ErrorOptions{Target: &targetCopy}
		if current, ok := t.server.runtimes.Runtime(target); ok && current.Epoch() != expected {
			options.Expected = string(current.Epoch())
			options.Actual = string(expected)
		}
		return protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, options)
	default:
		return err
	}
}

func (t *transport) installSubscription(
	runtime *host.SessionRuntime,
	subscription host.Subscription,
	binding host.LeaseBinding,
	replace protocol.SubscriptionID,
	historyBinding history.Binding,
) (*activeSubscription, *activeSubscription) {
	pumpCtx, cancel := context.WithCancel(t.ctx)
	active := &activeSubscription{id: subscription.ID, runtime: runtime, binding: binding, pumpCtx: pumpCtx, cancel: cancel}
	if !active.bindOwners(t.server.snapshots, historyBinding) {
		cancel()
		return nil, nil
	}
	t.mu.Lock()
	previous := t.subscriptions[replace]
	if t.subscriptions[subscription.ID] != nil || (replace != "" && previous == nil) {
		t.mu.Unlock()
		cancel()
		active.releaseOwners(t.server.snapshots)
		return nil, nil
	}
	t.subscriptions[subscription.ID] = active
	if replace != "" {
		delete(t.subscriptions, replace)
	}
	t.mu.Unlock()
	return active, previous
}

// rollbackSubscriptionInstall restores the old transport entry if the Host
// actor/manager commit fails after the provisional map swap. Host commit closes
// the old message channel on success, so swapping first also prevents the old
// pump from racing daemon commit and deleting the replacement's rollback slot.
func (t *transport) rollbackSubscriptionInstall(active *activeSubscription, replace protocol.SubscriptionID, previous *activeSubscription) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if active != nil && t.subscriptions[active.id] == active {
		delete(t.subscriptions, active.id)
	}
	if replace != "" && previous != nil && t.subscriptions[replace] == nil {
		t.subscriptions[replace] = previous
	}
}

func (t *transport) pumpSubscription(ctx context.Context, active *activeSubscription, messages <-chan host.SubscriptionMessage) {
	defer t.removeSubscription(active.id, active)
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-messages:
			if !ok {
				return
			}
			// Notifications never renew the control lease. A half-open SSH
			// transport must stop receiving events at TTL expiry even when no
			// replacement attach has arrived yet.
			if err := t.server.leases.Validate(active.binding, false); err != nil {
				_ = t.server.runtimes.Unsubscribe(t.server.ctx, host.AttachmentForLease(active.binding), active.id)
				return
			}
			if err := t.notifySubscriptionMessage(active, message); err != nil {
				t.server.reportInternal(protocol.MethodSessionEvent, err)
				_ = t.raw.Close()
				return
			}
		}
	}
}

func (t *transport) notifySubscriptionMessage(active *activeSubscription, message host.SubscriptionMessage) error {
	switch {
	case message.Event != nil && message.Resync == nil:
		payload, err := projectHostSessionEvent(active.id, *message.Event)
		if err != nil {
			return err
		}
		payload, err = contentref.ExternalizeSessionEvent(t.server.contents, payload, contentref.ExternalizeOptions{
			LeaseID: active.binding.LeaseID,
		})
		if err != nil {
			return err
		}
		if err := protocol.ValidateNotification(protocol.MethodSessionEvent, payload); err != nil {
			releaseDescriptors(t.server.contents, payload.Externalized)
			return err
		}
		if err := t.wire.Notify(string(protocol.MethodSessionEvent), payload); err != nil {
			releaseDescriptors(t.server.contents, payload.Externalized)
			return err
		}
		return nil
	case message.Resync != nil && message.Event == nil:
		resync := message.Resync
		payload := protocol.SessionResyncRequired{
			SubscriptionID: active.id, HostEpoch: resync.HostEpoch, Target: resync.Target,
			RuntimeEpoch: resync.RuntimeEpoch, LastSeq: resync.LastSeq, Reason: resync.Reason,
			ReplacementTarget:       resync.ReplacementTarget,
			ReplacementRuntimeEpoch: resync.ReplacementRuntimeEpoch,
		}
		if err := protocol.ValidateNotification(protocol.MethodSessionResyncRequired, payload); err != nil {
			return err
		}
		return t.wire.Notify(string(protocol.MethodSessionResyncRequired), payload)
	default:
		return errors.New("invalid host subscription message union")
	}
}

func (t *transport) projectSnapshot(
	ctx context.Context,
	snapshot host.RuntimeSnapshot,
	pageTurns int,
	leaseID protocol.LeaseID,
) (protocol.SessionSnapshot, error) {
	metadata, err := t.server.metadata(ctx, snapshot.Target)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	metadata = projectHostSessionMetadata(snapshot, metadata)
	metadata.Capabilities = t.server.capabilities
	runtimeState, err := projectHostRuntimeState(snapshot)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	projected := protocol.SessionSnapshot{
		SnapshotID: snapshot.SnapshotID, HostEpoch: snapshot.HostEpoch, Target: snapshot.Target,
		RuntimeEpoch: snapshot.RuntimeEpoch, BoundarySeq: snapshot.BoundarySeq,
		Meta: metadata, Runtime: runtimeState, PendingPrompt: clonePendingPromptForWire(snapshot.PendingPrompt),
		History: protocol.HistoryPage{
			SnapshotID: snapshot.SnapshotID, Messages: []protocol.HistoryMessage{}, Externalized: []protocol.ExternalizedField{},
		},
		Todos: snapshot.Capture.Todos, Context: snapshot.Capture.Context,
		Jobs: snapshot.Capture.Jobs, Checkpoints: snapshot.Capture.Checkpoints,
		Externalized: []protocol.ExternalizedField{},
	}
	return t.server.snapshots.BuildSubscribeSnapshot(projected, snapshot.Capture.History, pageTurns, leaseID)
}

func clonePendingPromptForWire(in *protocol.PendingPrompt) *protocol.PendingPrompt {
	if in == nil {
		return nil
	}
	out := *in
	if in.Approval != nil {
		approval := *in.Approval
		if in.Approval.Reason != nil {
			reason := *in.Approval.Reason
			approval.Reason = &reason
		}
		approval.AllowedDecisions = append([]protocol.PromptDecision(nil), in.Approval.AllowedDecisions...)
		out.Approval = &approval
	}
	if in.Ask != nil {
		ask := *in.Ask
		ask.Questions = make([]protocol.AskQuestion, len(in.Ask.Questions))
		for index, question := range in.Ask.Questions {
			copyQuestion := question
			if question.Prompt != nil {
				prompt := *question.Prompt
				copyQuestion.Prompt = &prompt
			}
			copyQuestion.Options = make([]protocol.AskOption, len(question.Options))
			for optionIndex, option := range question.Options {
				copyOption := option
				if option.Description != nil {
					description := *option.Description
					copyOption.Description = &description
				}
				copyQuestion.Options[optionIndex] = copyOption
			}
			ask.Questions[index] = copyQuestion
		}
		out.Ask = &ask
	}
	return &out
}

func (t *transport) takeSubscription(id protocol.SubscriptionID) *activeSubscription {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := t.subscriptions[id]
	delete(t.subscriptions, id)
	return active
}

func (t *transport) ownsSubscription(id protocol.SubscriptionID, binding host.LeaseBinding) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := t.subscriptions[id]
	return active != nil && active.binding == binding
}

func (t *transport) removeSubscription(id protocol.SubscriptionID, expected *activeSubscription) {
	t.mu.Lock()
	if t.subscriptions[id] == expected {
		delete(t.subscriptions, id)
	}
	t.mu.Unlock()
	expected.releaseOwners(t.server.snapshots)
}

// cleanup removes only transport-owned subscriptions. When detach is true it
// additionally releases the current lease, always after RespondThen's write.
func (t *transport) cleanup(detach bool) {
	t.cleanupOnce.Do(func() {
		t.cancel()
		t.mu.Lock()
		binding := t.binding
		for id, subscription := range t.subscriptions {
			delete(t.subscriptions, id)
			subscription.cancel()
			subscription.releaseOwners(t.server.snapshots)
		}
		t.phase = transportDetached
		t.mu.Unlock()
		if binding.LeaseID == "" {
			return
		}
		_ = t.server.runtimes.DetachAttachment(host.AttachmentForLease(binding))
		if detach {
			_ = t.server.leases.Detach(binding, binding.LeaseID)
		}
	})
}

func (a *activeSubscription) bindOwners(builder *snapshotowner.Builder, binding history.Binding) bool {
	a.ownerMu.Lock()
	if a.ownersReleased {
		a.ownerMu.Unlock()
		builder.Release(binding)
		return false
	}
	if a.historyBinding.SnapshotID != "" {
		a.ownerMu.Unlock()
		builder.Release(binding)
		return false
	}
	a.historyBinding = binding
	a.ownerMu.Unlock()
	return true
}

func (a *activeSubscription) releaseOwners(builder *snapshotowner.Builder) {
	if a == nil {
		return
	}
	a.ownerMu.Lock()
	if a.ownersReleased {
		a.ownerMu.Unlock()
		return
	}
	a.ownersReleased = true
	binding := a.historyBinding
	a.historyBinding = history.Binding{}
	a.ownerMu.Unlock()
	if binding.SnapshotID != "" {
		builder.Release(binding)
	}
}

// releaseReplacementOwners is the single migration cleanup hook. History and
// content share one snapshot owner and must never diverge across replacement
// installation paths.
func (a *activeSubscription) releaseReplacementOwners(builder *snapshotowner.Builder) {
	if a == nil {
		return
	}
	a.releaseOwners(builder)
}

func (a *activeSubscription) ownsHistoryBinding(binding history.Binding, leaseBinding host.LeaseBinding) bool {
	a.ownerMu.Lock()
	defer a.ownerMu.Unlock()
	return !a.ownersReleased && a.historyBinding == binding && a.binding == leaseBinding
}

func (a *activeSubscription) ownedHistoryBinding(snapshotID protocol.SnapshotID, leaseBinding host.LeaseBinding) (history.Binding, bool) {
	a.ownerMu.Lock()
	defer a.ownerMu.Unlock()
	if a.ownersReleased || a.historyBinding.SnapshotID != snapshotID || a.binding != leaseBinding {
		return history.Binding{}, false
	}
	return a.historyBinding, true
}

func releaseSnapshotOwner(store *contentref.Store, snapshotID protocol.SnapshotID) {
	owner, err := contentref.SnapshotOwner(snapshotID)
	if err == nil {
		store.ReleaseOwner(owner)
	}
}

func snapshotExpired(target protocol.RuntimeTarget) error {
	targetCopy := target
	return protocol.MustRemoteError(protocol.ErrSnapshotExpired, protocol.ErrorOptions{Target: &targetCopy})
}

func releaseDescriptors(store *contentref.Store, fields []protocol.ExternalizedField) {
	for _, field := range fields {
		store.Release(field.ContentRef)
	}
}

func invalidateContentOwner(store *contentref.Store, binding contentref.ReferenceBinding) {
	switch binding.Kind {
	case contentref.ReferenceSnapshot:
		releaseSnapshotOwner(store, binding.SnapshotID)
	case contentref.ReferenceEvent:
		owner, err := contentref.EventOwner(binding.RuntimeEpoch, binding.Seq)
		if err == nil {
			store.ReleaseOwner(owner)
		}
	}
}
