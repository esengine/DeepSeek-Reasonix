package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/eventwire"
	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
)

const (
	defaultRemoteAskPassLifetime = 10 * time.Minute
	remoteAdapterEventBuffer     = 512
	remoteAdapterFaultBuffer     = 16
	remoteAdapterCatalogBuffer   = 128
)

var ErrRemoteDetachCommitted = errors.New("Remote detach committed")

// RemoteDetachCommittedError means the daemon acknowledged remote/detach and
// the transport/lease are no longer usable, but Desktop could not durably clear
// the saved resume lease. TargetManager must clear the physical adapter rather
// than restoring it as connected, while still surfacing the persistence error.
type RemoteDetachCommittedError struct {
	Err error
}

func (e *RemoteDetachCommittedError) Error() string {
	if e == nil || e.Err == nil {
		return ErrRemoteDetachCommitted.Error()
	}
	return fmt.Sprintf("%s: %v", ErrRemoteDetachCommitted, e.Err)
}

func (e *RemoteDetachCommittedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *RemoteDetachCommittedError) Is(target error) bool { return target == ErrRemoteDetachCommitted }

func (e *RemoteDetachCommittedError) DetachCommitted() bool { return true }

// RemoteTargetConnectorOptions contains Desktop-owned connection policy only.
// Host installation, daemon lifecycle, repair and upgrade deliberately have no
// representation here.
type RemoteTargetConnectorOptions struct {
	Store             *RemoteHostStore
	SSHPath           string
	AskPassHelperPath string
	AskPassHandler    RemoteAskPassHandler
	AskPassLifetime   time.Duration
}

type remoteClientConstructor func(RemoteHostEntry, protocol.BuildID) (*remoteclient.Client, error)

// RemoteTargetConnector opens saved Remote Host entries for TargetManager. A
// successful Connect means both initialize and durable lease persistence have
// completed; callers can therefore publish RemoteConnected without a disk race.
type RemoteTargetConnector struct {
	store             *RemoteHostStore
	sshPath           string
	askPassHelperPath string
	askPassHandler    RemoteAskPassHandler
	askPassLifetime   time.Duration

	buildID   func() (protocol.BuildID, error)
	newClient remoteClientConstructor
}

func NewRemoteTargetConnector(options RemoteTargetConnectorOptions) (*RemoteTargetConnector, error) {
	if options.Store == nil {
		return nil, errors.New("Remote Host store is required")
	}
	if options.AskPassHandler == nil {
		return nil, errors.New("Remote AskPass handler is required")
	}
	if !filepath.IsAbs(options.AskPassHelperPath) || strings.IndexByte(options.AskPassHelperPath, 0) >= 0 {
		return nil, errors.New("Remote AskPass helper path must be absolute")
	}
	lifetime := options.AskPassLifetime
	if lifetime == 0 {
		lifetime = defaultRemoteAskPassLifetime
	}
	if lifetime < 0 {
		return nil, errors.New("Remote AskPass lifetime must be positive")
	}
	connector := &RemoteTargetConnector{
		store: options.Store, sshPath: strings.TrimSpace(options.SSHPath),
		askPassHelperPath: options.AskPassHelperPath, askPassHandler: options.AskPassHandler,
		askPassLifetime: lifetime, buildID: currentDesktopRemoteBuildID,
	}
	connector.newClient = connector.newProductionClient
	return connector, nil
}

func (c *RemoteTargetConnector) newProductionClient(entry RemoteHostEntry, buildID protocol.BuildID) (*remoteclient.Client, error) {
	factory := &remoteBrokeredSSHFactory{
		entry: entry, sshPath: c.sshPath, askPassHelperPath: c.askPassHelperPath,
		askPassHandler: c.askPassHandler, askPassLifetime: c.askPassLifetime,
	}
	return remoteclient.New(remoteclient.Options{
		Factory: factory, BuildID: buildID,
		ClientInstanceID: protocol.ClientInstanceID(entry.ClientInstanceID),
		ResumeLeaseID:    protocol.LeaseID(entry.ResumeLeaseID),
	})
}

type remoteBrokeredSSHFactory struct {
	entry             RemoteHostEntry
	sshPath           string
	askPassHelperPath string
	askPassHandler    RemoteAskPassHandler
	askPassLifetime   time.Duration
}

func (f *remoteBrokeredSSHFactory) Open(ctx context.Context) (remoteclient.Transport, error) {
	handler := func(promptCtx context.Context, prompt RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		prompt.HostLabel = f.entry.Label
		return f.askPassHandler(promptCtx, prompt)
	}
	broker, err := StartRemoteAskPassBroker(ctx, f.askPassLifetime, handler)
	if err != nil {
		return nil, err
	}
	sshFactory := &RemoteSSHTransportFactory{
		SSHPath: f.sshPath, AskPass: broker, AskPassHelper: f.askPassHelperPath,
	}
	bound, err := NewRemoteSSHHostTransportFactory(sshFactory, f.entry)
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	transport, err := bound.Open(ctx)
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	return &remoteBrokeredTransport{Transport: transport, broker: broker}, nil
}

type remoteBrokeredTransport struct {
	remoteclient.Transport
	broker *RemoteAskPassBroker
	once   sync.Once
	err    error
}

func (t *remoteBrokeredTransport) Close() error {
	if t == nil {
		return nil
	}
	t.once.Do(func() {
		var transportErr error
		if t.Transport != nil {
			transportErr = t.Transport.Close()
		}
		var brokerErr error
		if t.broker != nil {
			brokerErr = t.broker.Close()
		}
		t.err = errors.Join(transportErr, brokerErr)
	})
	return t.err
}

func (c *RemoteTargetConnector) Connect(ctx context.Context, target TargetDescriptor) (TargetAdapter, error) {
	if c == nil || c.store == nil || c.newClient == nil || c.buildID == nil {
		return nil, errors.New("Remote target connector is not configured")
	}
	if err := validateRemoteTargetDescriptor(target); err != nil {
		return nil, err
	}
	entry, found, err := c.store.Get(target.ID)
	if err != nil {
		return nil, fmt.Errorf("load Remote Host entry: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("Remote Host entry %q is not saved", target.ID)
	}
	buildID, err := c.buildID()
	if err != nil {
		return nil, err
	}
	if err := buildID.Validate(); err != nil {
		return nil, fmt.Errorf("Desktop Remote Build ID: %w", err)
	}
	client, err := c.newClient(entry, buildID)
	if err != nil {
		return nil, fmt.Errorf("create Remote client: %w", err)
	}
	adapter := newRemoteRuntimeAdapter(c.store, entry, client)
	if err := adapter.connectAndPersist(ctx); err != nil {
		adapter.shutdown(false)
		return nil, err
	}
	return adapter, nil
}

func (c *RemoteTargetConnector) Reconnect(ctx context.Context, target TargetDescriptor, previous TargetAdapter) (TargetAdapter, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("Remote target connector is not configured")
	}
	if err := validateRemoteTargetDescriptor(target); err != nil {
		return nil, err
	}
	adapter, ok := previous.(*RemoteRuntimeAdapter)
	if !ok || adapter == nil {
		return nil, errors.New("Remote reconnect requires the existing Remote adapter")
	}
	if !sameTarget(adapter.Descriptor(), target) {
		return nil, errors.New("Remote reconnect target differs from the retained adapter")
	}
	if adapter.store == nil || adapter.store.Path() != c.store.Path() {
		return nil, errors.New("Remote reconnect store differs from the retained adapter")
	}
	entry, found, err := c.store.Get(target.ID)
	if err != nil {
		return nil, fmt.Errorf("reload Remote Host entry: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("Remote Host entry %q is not saved", target.ID)
	}
	adapter.mu.RLock()
	retainedEntry := adapter.entry
	adapter.mu.RUnlock()
	if !sameRemoteConnectionIdentity(entry, retainedEntry) {
		return nil, errors.New("Remote Host connection identity changed while recovery is pending")
	}
	if err := adapter.reconnectAndRestore(ctx); err != nil {
		return nil, err
	}
	adapter.mu.Lock()
	adapter.entry.Label = entry.Label
	adapter.entry.LayoutRef = entry.LayoutRef
	adapter.mu.Unlock()
	return adapter, nil
}

func validateRemoteTargetDescriptor(target TargetDescriptor) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.Kind != TargetRemote {
		return errors.New("Remote connector only accepts Remote targets")
	}
	return ValidateRemoteHostEntryID(target.ID)
}

func sameRemoteConnectionIdentity(left, right RemoteHostEntry) bool {
	return left.ID == right.ID && left.Alias == right.Alias && left.SSHConfigPath == right.SSHConfigPath &&
		left.ClientInstanceID == right.ClientInstanceID
}

type remoteSessionBinding struct {
	runtimeEpoch protocol.RuntimeEpoch
	subscription protocol.SubscriptionID
	generation   uint64
	pumpToken    uint64
	pageTurns    int
	snapshot     protocol.SessionSnapshot
	hasSnapshot  bool
	running      bool
	prompt       bool
}

type remoteAdapterStreams struct {
	generation uint64
	events     chan runtimeapi.Event
	faults     chan error
}

// RemoteRuntimeAdapter is the sole Phase 5 mapping between the transport wire
// contract and target-neutral RuntimeAPI. It intentionally contains no Wails,
// tab, path or local Controller identity.
type RemoteRuntimeAdapter struct {
	store  *RemoteHostStore
	entry  RemoteHostEntry
	client *remoteclient.Client

	ctx    context.Context
	cancel context.CancelFunc

	connectionMu sync.Mutex
	subscribeMu  sync.Mutex
	mu           sync.RWMutex
	sessions     map[protocol.RuntimeTarget]*remoteSessionBinding
	nextPump     uint64
	detached     bool

	streams       map[uint64]*remoteAdapterStreams
	activeStreams *remoteAdapterStreams
	pendingFaults map[uint64][]error
	catalogEvents chan runtimeapi.CatalogInvalidation
	wg            sync.WaitGroup

	newRequestID func() (protocol.RequestID, error)
	mutations    remoteMutationJournal
	shutdownOnce sync.Once
}

func newRemoteRuntimeAdapter(store *RemoteHostStore, entry RemoteHostEntry, client *remoteclient.Client) *RemoteRuntimeAdapter {
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &RemoteRuntimeAdapter{
		store: store, entry: entry, client: client, ctx: ctx, cancel: cancel,
		sessions: make(map[protocol.RuntimeTarget]*remoteSessionBinding),
		streams:  make(map[uint64]*remoteAdapterStreams), pendingFaults: make(map[uint64][]error),
		catalogEvents: make(chan runtimeapi.CatalogInvalidation, remoteAdapterCatalogBuffer),
		newRequestID:  newRemoteMutationRequestID, mutations: newRemoteMutationJournal(),
	}
	adapter.wg.Add(2)
	go adapter.forwardClientFaults()
	go adapter.forwardCatalogNotifications()
	return adapter
}

func newRemoteMutationRequestID() (protocol.RequestID, error) {
	var entropy [32]byte
	if _, err := io.ReadFull(rand.Reader, entropy[:]); err != nil {
		return "", fmt.Errorf("generate Remote requestId: %w", err)
	}
	return protocol.RequestID("request_" + hex.EncodeToString(entropy[:])), nil
}

func (a *RemoteRuntimeAdapter) Descriptor() TargetDescriptor {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return TargetDescriptor{Kind: TargetRemote, ID: a.entry.ID, Label: a.entry.Label}
}

func (a *RemoteRuntimeAdapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a }

// AbandonTarget closes only Desktop-owned transport/client resources. It does
// not send detach or clear the saved lease, so an unknown SSH outcome remains
// recoverable on the next Desktop start.
func (a *RemoteRuntimeAdapter) AbandonTarget() error {
	a.shutdown(false)
	return nil
}

func (a *RemoteRuntimeAdapter) Events() <-chan runtimeapi.Event {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.activeStreams == nil {
		return nil
	}
	return a.activeStreams.events
}

func (a *RemoteRuntimeAdapter) Faults() <-chan error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.activeStreams == nil {
		return nil
	}
	return a.activeStreams.faults
}

func (a *RemoteRuntimeAdapter) connectAndPersist(ctx context.Context) error {
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	if err := a.requireUsable(); err != nil {
		return err
	}
	connection, err := a.connectWithOwnedLifetime(ctx)
	if err != nil {
		return err
	}
	if err := a.persistLease(connection.Initialize.Lease.LeaseID); err != nil {
		a.bestEffortDetach()
		return err
	}
	if status := a.client.Status(); status.State != remoteclient.StateConnected || status.Generation != connection.Generation {
		return errors.New("Remote transport was lost before lease persistence completed")
	}
	a.activateStreams(connection.Generation)
	return nil
}

func (a *RemoteRuntimeAdapter) reconnectAndRestore(ctx context.Context) error {
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	if err := a.requireUsable(); err != nil {
		return err
	}
	recovery := a.client.RecoveryState()
	connection, err := a.connectWithOwnedLifetime(ctx)
	if err != nil {
		return err
	}
	if err := a.persistLease(connection.Initialize.Lease.LeaseID); err != nil {
		return err
	}
	a.activateStreams(connection.Generation)
	for _, item := range recovery.Subscriptions {
		if _, err := a.subscribeFresh(ctx, item.Target, item.PageTurns, "", protocol.RuntimeTarget{}); err != nil {
			return fmt.Errorf("restore Remote Session %s/%s: %w", item.Target.WorkspaceID, item.Target.SessionID, err)
		}
	}
	return nil
}

// connectWithOwnedLifetime mirrors caller cancellation only during the
// handshake. The SSH process remains parented to adapter lifetime afterwards;
// TargetManager cancels its transition context as soon as Connect returns.
func (a *RemoteRuntimeAdapter) connectWithOwnedLifetime(caller context.Context) (remoteclient.Connection, error) {
	if caller == nil {
		caller = context.Background()
	}
	attemptCtx, attemptCancel := context.WithCancel(a.ctx)
	bridgeDone := make(chan struct{})
	go func() {
		select {
		case <-caller.Done():
			attemptCancel()
		case <-bridgeDone:
		case <-a.ctx.Done():
		}
	}()
	connection, err := a.client.Connect(attemptCtx)
	close(bridgeDone)
	if err != nil {
		attemptCancel()
		return remoteclient.Connection{}, err
	}
	return connection, nil
}

func (a *RemoteRuntimeAdapter) persistLease(lease protocol.LeaseID) error {
	if strings.TrimSpace(string(lease)) == "" {
		return errors.New("Remote initialize omitted lease identity")
	}
	if err := a.store.UpdateResumeLease(a.entry.ID, string(lease)); err != nil {
		return fmt.Errorf("persist Remote resume lease: %w", err)
	}
	a.mu.Lock()
	a.entry.ResumeLeaseID = string(lease)
	a.mu.Unlock()
	return nil
}

func (a *RemoteRuntimeAdapter) requireUsable() error {
	a.mu.RLock()
	detached := a.detached
	a.mu.RUnlock()
	if detached {
		return errors.New("Remote target is detached")
	}
	select {
	case <-a.ctx.Done():
		return errors.New("Remote target is closed")
	default:
		return nil
	}
}

func (a *RemoteRuntimeAdapter) forwardClientFaults() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case fault := <-a.client.Faults():
			if fault.Err != nil {
				a.emitFault(fault.Generation, fmt.Errorf("Remote generation %d: %w", fault.Generation, fault.Err))
			}
		}
	}
}

func (a *RemoteRuntimeAdapter) forwardCatalogNotifications() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case notification := <-a.client.CatalogNotifications():
			workspaceIDs := make([]runtimeapi.WorkspaceID, len(notification.Change.AffectedWorkspaceIDs))
			for index, id := range notification.Change.AffectedWorkspaceIDs {
				workspaceIDs[index] = runtimeapi.WorkspaceID(id)
			}
			kinds := make([]runtimeapi.CatalogKind, len(notification.Change.Kinds))
			for index, kind := range notification.Change.Kinds {
				kinds[index] = runtimeapi.CatalogKind(kind)
			}
			value := runtimeapi.CatalogInvalidation{
				Revision: runtimeapi.CatalogRevision(notification.Change.Revision), Scope: runtimeapi.CatalogScope(notification.Change.Scope),
				AffectedWorkspaceIDs: workspaceIDs, Kinds: kinds,
			}
			select {
			case a.catalogEvents <- value:
			case <-a.ctx.Done():
				return
			default:
				a.emitFault(notification.Generation, errors.New("Remote catalog invalidation queue overflow; catalogs must be refreshed"))
			}
		}
	}
}

func (a *RemoteRuntimeAdapter) CatalogEvents() <-chan runtimeapi.CatalogInvalidation {
	return a.catalogEvents
}

func (a *RemoteRuntimeAdapter) activateStreams(generation uint64) {
	a.mu.Lock()
	streams := a.streams[generation]
	if streams == nil {
		streams = &remoteAdapterStreams{
			generation: generation, events: make(chan runtimeapi.Event, remoteAdapterEventBuffer),
			faults: make(chan error, remoteAdapterFaultBuffer),
		}
		a.streams[generation] = streams
	}
	a.activeStreams = streams
	pending := append([]error(nil), a.pendingFaults[generation]...)
	delete(a.pendingFaults, generation)
	a.mu.Unlock()
	for _, err := range pending {
		a.emitFault(generation, err)
	}
}

func (a *RemoteRuntimeAdapter) emitFault(generation uint64, err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	streams := a.streams[generation]
	if streams == nil {
		if len(a.pendingFaults[generation]) < remoteAdapterFaultBuffer {
			a.pendingFaults[generation] = append(a.pendingFaults[generation], err)
		}
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	select {
	case streams.faults <- err:
	case <-a.ctx.Done():
	default:
	}
}

func (a *RemoteRuntimeAdapter) emitEvent(generation uint64, event runtimeapi.Event) bool {
	a.mu.RLock()
	streams := a.streams[generation]
	a.mu.RUnlock()
	if streams == nil {
		return false
	}
	select {
	case streams.events <- event:
		return true
	case <-a.ctx.Done():
		return false
	default:
		a.emitFault(generation, errors.New("Remote workbench event queue overflow; a fresh snapshot is required"))
		return false
	}
}

func (a *RemoteRuntimeAdapter) Connection(ctx context.Context) (runtimeapi.ConnectionView, error) {
	if err := a.requireUsable(); err != nil {
		return runtimeapi.ConnectionView{}, err
	}
	status := a.client.Status()
	if status.State != remoteclient.StateConnected {
		return runtimeapi.ConnectionView{}, remoteclient.ErrNotConnected
	}
	config, err := a.HostConfigSummary(ctx)
	if err != nil {
		return runtimeapi.ConnectionView{}, err
	}
	return runtimeapi.ConnectionView{
		Label: a.Descriptor().Label, OS: status.Host.OS, Arch: status.Host.Arch, ShellKind: status.Host.ShellKind,
		Capabilities: mapRemoteCapabilities(status.Capabilities), Config: config,
	}, nil
}

func mapRemoteCapabilities(value protocol.Capabilities) runtimeapi.Capabilities {
	f := value.Features
	l := value.Limits
	return runtimeapi.Capabilities{
		HostConfig: true, WorkspaceBrowse: true, SessionCreate: true, SessionAttach: true, ComposerSubmit: true,
		TurnSteer: true, TurnCancel: true, PromptApprove: true, PromptAnswer: true,
		Features: runtimeapi.Features{
			CoreSession: f.CoreSession, PrimaryFileQueries: f.PrimaryFileQueries, UserShell: f.UserShell,
			JobCancel: f.JobCancel, Memory: f.Memory, Research: f.Research, MediaPreview: f.MediaPreview,
			Attachments: f.Attachments, ClipboardImages: f.ClipboardImages, SFTP: f.SFTP,
			LocalPathOperations: f.LocalPathOperations, GitWrite: f.GitWrite, PTY: f.PTY,
			DeliveryWorktree: f.DeliveryWorktree,
		},
		Limits: runtimeapi.Limits{
			FrameBytes: l.FrameBytes, SnapshotHistoryBytes: l.SnapshotHistoryBytes,
			ExternalizeFieldBytes: l.ExternalizeFieldBytes, ContentRefChunkBytes: l.ContentRefChunkBytes,
			ContentRefObjectBytes: l.ContentRefObjectBytes, ContentRefIdleMillis: l.ContentRefIdleMs,
			ContentRefMaxAgeMillis: l.ContentRefMaxAgeMs, HistoryMaxTurns: l.HistoryMaxTurns,
			PageDefaultItems: l.PageDefaultItems, PageMaxItems: l.PageMaxItems,
			SearchDefaultItems: l.SearchDefaultItems, SearchMaxItems: l.SearchMaxItems,
			SearchMaxVisitedItems: l.SearchMaxVisitedItems, PreviewBytes: l.PreviewBytes,
			GitHistoryCommits: l.GitHistoryCommits, GitPatchBytes: l.GitPatchBytes,
		},
	}
}

func (a *RemoteRuntimeAdapter) BrowseWorkspace(ctx context.Context, input runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	var limit *int
	if input.Limit != 0 {
		value := input.Limit
		limit = &value
	}
	value, err := a.client.Request(ctx, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: status.HostEpoch, DirectoryRef: protocol.DirectoryRef(input.DirectoryRef),
		TypedPath: input.TypedPath, Cursor: protocol.Cursor(input.Cursor), Limit: limit,
	})
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	result, ok := value.(protocol.WorkspaceBrowseResult)
	if !ok {
		return runtimeapi.WorkspacePage{}, fmt.Errorf("workspace/browse returned %T", value)
	}
	entries := make([]runtimeapi.Directory, len(result.Entries))
	for index, entry := range result.Entries {
		entries[index] = mapRemoteDirectory(entry)
	}
	return runtimeapi.WorkspacePage{
		Directory: mapRemoteDirectory(result.Directory), Entries: entries, HasMore: result.HasMore,
		Next: runtimeapi.Cursor(result.NextCursor),
	}, nil
}

func mapRemoteDirectory(value protocol.DirectoryItem) runtimeapi.Directory {
	return runtimeapi.Directory{
		Ref: runtimeapi.DirectoryRef(value.DirectoryRef), Name: value.Name, DisplayPath: value.DisplayPath,
		ParentRef: runtimeapi.DirectoryRef(value.ParentRef),
	}
}

func (a *RemoteRuntimeAdapter) OpenWorkspace(ctx context.Context, input runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.OpenWorkspaceResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodWorkspaceOpen, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.OpenWorkspaceResult{}, err
	}
	value, err := a.client.Request(ctx, protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch},
		PrimaryDirectoryRef: protocol.DirectoryRef(input.PrimaryDirectory),
	})
	if err = attempt.finish(err); err != nil {
		return runtimeapi.OpenWorkspaceResult{}, err
	}
	result, ok := value.(protocol.WorkspaceOpenResult)
	if !ok {
		return runtimeapi.OpenWorkspaceResult{}, fmt.Errorf("workspace/open returned %T", value)
	}
	return runtimeapi.OpenWorkspaceResult{Workspace: runtimeapi.Workspace{
		ID: runtimeapi.WorkspaceID(result.Workspace.WorkspaceID), Name: result.Workspace.Name,
		DisplayPath: result.Workspace.DisplayPath,
	}, AlreadyOpen: result.Disposition == protocol.WorkspaceAlreadyOpen}, nil
}

func (a *RemoteRuntimeAdapter) CreateSession(ctx context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionCreate, status.HostEpoch, protocol.RuntimeTarget{}, "", input)
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	directories := make([]protocol.DirectoryRef, len(input.AdditionalDirectories))
	for index, directory := range input.AdditionalDirectories {
		directories[index] = protocol.DirectoryRef(directory)
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: attempt.id(), ExpectedHostEpoch: status.HostEpoch},
		WorkspaceID:  protocol.WorkspaceID(input.WorkspaceID), AdditionalDirectoryRefs: directories,
		Topic: protocol.TopicSelection{
			Kind: protocol.TopicSelectionKind(input.Topic.Kind), TopicID: protocol.TopicID(input.Topic.TopicID), Title: input.Topic.Title,
		},
		Profile: mapRemoteProfileSelection(input.Profile),
	})
	if err = attempt.finish(err); err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	result, ok := value.(protocol.SessionCreateResult)
	if !ok {
		return runtimeapi.CreatedSession{}, fmt.Errorf("session/create returned %T", value)
	}
	if result.Target.WorkspaceID != protocol.WorkspaceID(input.WorkspaceID) {
		return runtimeapi.CreatedSession{}, errors.New("session/create returned a Session in a different workspace")
	}
	a.mu.Lock()
	binding := a.sessionBindingLocked(result.Target)
	binding.runtimeEpoch = result.RuntimeEpoch
	binding.generation = status.Generation
	a.mu.Unlock()
	return runtimeapi.CreatedSession{
		Session: mapRemoteSessionRef(result.Target), TopicID: runtimeapi.TopicID(result.TopicID), TopicTitle: result.TopicTitle,
		ResolvedProfile: mapRemoteResolvedProfile(result.ResolvedProfile),
	}, nil
}

func mapRemoteProfileSelection(value runtimeapi.ProfileSelection) protocol.ProfileSelection {
	return protocol.ProfileSelection{
		Model: optionalString(value.Model), Effort: optionalString(value.Effort),
		CollaborationMode: optionalCollaborationMode(value.CollaborationMode),
		TokenMode:         optionalTokenMode(value.TokenMode), ToolApprovalMode: optionalToolApprovalMode(value.ToolApprovalMode),
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func optionalCollaborationMode(value string) *protocol.CollaborationMode {
	if value == "" {
		return nil
	}
	copy := protocol.CollaborationMode(value)
	return &copy
}

func optionalTokenMode(value string) *protocol.TokenMode {
	if value == "" {
		return nil
	}
	copy := protocol.TokenMode(value)
	return &copy
}

func optionalToolApprovalMode(value string) *protocol.ToolApprovalMode {
	if value == "" {
		return nil
	}
	copy := protocol.ToolApprovalMode(value)
	return &copy
}

func (a *RemoteRuntimeAdapter) AttachAndSubscribe(ctx context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	if !input.Session.Valid() {
		return runtimeapi.SessionSnapshot{}, errors.New("Remote Session identity is required")
	}
	if input.HistoryTurns < 1 || input.HistoryTurns > protocol.HistoryMaxTurns {
		return runtimeapi.SessionSnapshot{}, fmt.Errorf("historyTurns must be between 1 and %d", protocol.HistoryMaxTurns)
	}
	target := mapRuntimeSessionRef(input.Session)
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	a.mu.RLock()
	binding := a.sessions[target]
	var replace protocol.SubscriptionID
	if binding != nil && binding.generation == status.Generation {
		replace = binding.subscription
	}
	a.mu.RUnlock()
	return a.subscribeFresh(ctx, target, input.HistoryTurns, replace, protocol.RuntimeTarget{})
}

// UnsubscribeSession removes the current transport subscription for one
// Session without closing or deleting the Host-owned Session runtime. It is a
// Desktop view-lifecycle operation, not a Session mutation.
func (a *RemoteRuntimeAdapter) UnsubscribeSession(ctx context.Context, input runtimeapi.UnsubscribeSessionInput) error {
	session := input.Session
	if !session.Valid() {
		return errors.New("Remote Session identity is required")
	}
	target := mapRuntimeSessionRef(session)
	status, err := a.connectedStatus()
	if err != nil {
		return err
	}
	a.mu.RLock()
	binding := a.sessions[target]
	var subscription protocol.SubscriptionID
	var generation uint64
	var token uint64
	if binding != nil {
		subscription = binding.subscription
		generation = binding.generation
		token = binding.pumpToken
	}
	a.mu.RUnlock()
	if binding == nil || subscription == "" || generation != status.Generation {
		return errors.New("Remote Session has no current subscription")
	}
	if _, err := a.client.Unsubscribe(ctx, subscription); err != nil {
		return err
	}
	a.mu.Lock()
	if current := a.sessions[target]; current != nil && current.subscription == subscription &&
		current.generation == generation && current.pumpToken == token {
		delete(a.sessions, target)
	}
	a.mu.Unlock()
	return nil
}

func (a *RemoteRuntimeAdapter) subscribeFresh(
	ctx context.Context,
	target protocol.RuntimeTarget,
	pageTurns int,
	replace protocol.SubscriptionID,
	previousTarget protocol.RuntimeTarget,
) (runtimeapi.SessionSnapshot, error) {
	return a.subscribeFreshOrdered(ctx, target, pageTurns, replace, previousTarget, nil)
}

// subscribeFreshOrdered commits the adapter binding, publishes the replacement
// snapshot, and only then starts consuming seq>N updates. This preserves the
// RuntimeAPI analogue of the wire's atomic subscribe boundary: a fast Host
// event can never overtake the authority snapshot in the Desktop workbench.
func (a *RemoteRuntimeAdapter) subscribeFreshOrdered(
	ctx context.Context,
	target protocol.RuntimeTarget,
	pageTurns int,
	replace protocol.SubscriptionID,
	previousTarget protocol.RuntimeTarget,
	beforePump func(runtimeapi.SessionSnapshot) error,
) (runtimeapi.SessionSnapshot, error) {
	a.subscribeMu.Lock()
	defer a.subscribeMu.Unlock()
	status, err := a.connectedStatus()
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	subscription, err := a.client.Subscribe(ctx, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: status.HostEpoch, Target: target, PageTurns: pageTurns, ReplaceSubscriptionID: replace,
	})
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	if subscription.Snapshot.Target != target || subscription.Generation != status.Generation {
		return runtimeapi.SessionSnapshot{}, errors.New("Remote subscription identity changed during attach")
	}
	mapped := mapRemoteSessionSnapshot(subscription.Snapshot)
	a.mu.Lock()
	a.nextPump++
	pumpToken := a.nextPump
	var replacedBinding *remoteSessionBinding
	if current := a.sessions[target]; current != nil {
		copy := *current
		replacedBinding = &copy
	}
	binding := a.sessionBindingLocked(target)
	binding.runtimeEpoch = subscription.Snapshot.RuntimeEpoch
	binding.subscription = subscription.ID
	binding.generation = subscription.Generation
	binding.pumpToken = pumpToken
	binding.pageTurns = pageTurns
	binding.snapshot = subscription.Snapshot
	binding.hasSnapshot = true
	binding.running = subscription.Snapshot.Runtime.Running
	binding.prompt = subscription.Snapshot.PendingPrompt != nil
	a.mu.Unlock()
	if beforePump != nil {
		if err := beforePump(mapped); err != nil {
			a.mu.Lock()
			if current := a.sessions[target]; current != nil && current.pumpToken == pumpToken {
				if replacedBinding == nil {
					delete(a.sessions, target)
				} else {
					a.sessions[target] = replacedBinding
				}
			}
			a.mu.Unlock()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = a.client.Unsubscribe(cleanupCtx, subscription.ID)
			cleanupCancel()
			return runtimeapi.SessionSnapshot{}, err
		}
	}
	a.mu.Lock()
	if previousTarget != (protocol.RuntimeTarget{}) && previousTarget != target {
		delete(a.sessions, previousTarget)
	}
	a.mu.Unlock()
	a.wg.Add(1)
	go a.pumpSubscription(target, subscription.ID, subscription.Generation, pumpToken, subscription.Updates)
	return mapped, nil
}

func (a *RemoteRuntimeAdapter) pumpSubscription(
	target protocol.RuntimeTarget,
	subscriptionID protocol.SubscriptionID,
	generation uint64,
	pumpToken uint64,
	updates <-chan remoteclient.SubscriptionUpdate,
) {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if !a.subscriptionCurrent(target, generation, pumpToken) {
				return
			}
			if update.Event != nil {
				event := runtimeapi.Event{
					Session: mapRemoteSessionRef(update.Event.Target), TurnID: runtimeapi.TurnID(update.Event.TurnID),
					OperationID: runtimeapi.OperationID(update.Event.OperationID), Value: update.Event.Event,
				}
				a.applyLiveEvent(target, update.Event)
				if !a.emitEvent(generation, event) {
					return
				}
				continue
			}
			if update.SnapshotRequired && a.client.Status().State == remoteclient.StateConnected {
				replacement := target
				if update.Resync != nil && update.Resync.ReplacementTarget != nil {
					replacement = *update.Resync.ReplacementTarget
				}
				a.mu.RLock()
				binding := a.sessions[target]
				pageTurns := protocol.HistoryMaxTurns
				if binding != nil && binding.pageTurns > 0 {
					pageTurns = binding.pageTurns
				}
				a.mu.RUnlock()
				ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
				previous := mapRemoteSessionRef(target)
				_, err := a.subscribeFreshOrdered(ctx, replacement, pageTurns, subscriptionID, target, func(snapshot runtimeapi.SessionSnapshot) error {
					if !a.emitEvent(generation, runtimeapi.Event{
						Session: snapshot.Session,
						Snapshot: &runtimeapi.SnapshotUpdate{
							Previous: previous,
							Snapshot: snapshot,
						},
					}) {
						return errors.New("Remote replacement snapshot could not be delivered to the workbench")
					}
					return nil
				})
				cancel()
				if err != nil {
					a.emitFault(generation, fmt.Errorf("migrate Remote subscription: %w", err))
				}
				return
			}
			// Transport loss has a richer generation-qualified client fault. Do
			// not race it with a generic subscription terminal error.
			if update.Err != nil && a.client.Status().State == remoteclient.StateConnected {
				a.emitFault(generation, update.Err)
			}
			return
		}
	}
}

func (a *RemoteRuntimeAdapter) subscriptionCurrent(target protocol.RuntimeTarget, generation, token uint64) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	binding := a.sessions[target]
	return binding != nil && binding.generation == generation && binding.pumpToken == token
}

func (a *RemoteRuntimeAdapter) applyLiveEvent(target protocol.RuntimeTarget, value *protocol.SessionEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding := a.sessions[target]
	if binding == nil || value == nil {
		return
	}
	switch value.Event.Kind {
	case "turn_started":
		binding.running = true
	case "turn_done", "operation_done":
		binding.running = false
		binding.prompt = false
	case "approval_request", "ask_request":
		binding.prompt = true
	}
}

func (a *RemoteRuntimeAdapter) ComposerSubmit(ctx context.Context, input runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodSessionSubmit, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	displayText := input.DisplayText
	if displayText == "" {
		displayText = input.Input
	}
	invocations := make([]protocol.Invocation, len(input.Invocations))
	for index, invocation := range input.Invocations {
		invocations[index] = protocol.Invocation{Name: invocation.Name, Kind: protocol.InvocationKind(invocation.Kind)}
	}
	value, err := a.client.Request(ctx, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: attempt.id(), ExpectedHostEpoch: identity.hostEpoch, Target: identity.target,
			ExpectedRuntimeEpoch: identity.runtimeEpoch,
		},
		Input: input.Input, DisplayText: displayText, EditedOriginal: input.EditedOriginal,
		Invocations: invocations, DeliveryRecovery: input.DeliveryRecovery,
	})
	if err = attempt.finish(err); err != nil {
		return runtimeapi.ComposerSubmitResult{}, err
	}
	result, ok := value.(protocol.SessionSubmitResult)
	if !ok {
		return runtimeapi.ComposerSubmitResult{}, fmt.Errorf("session/submit returned %T", value)
	}
	targetReplaced := result.Target != identity.target
	if targetReplaced && (result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectSessionReplaced || !result.SnapshotRequired || result.Target.WorkspaceID != identity.target.WorkspaceID) {
		return runtimeapi.ComposerSubmitResult{}, errors.New("session/submit returned an invalid replacement Session target")
	}
	a.mu.Lock()
	binding := a.sessionBindingLocked(result.Target)
	binding.runtimeEpoch = result.RuntimeEpoch
	binding.generation = identity.generation
	binding.running = result.Kind == protocol.SubmitTurn || result.Kind == protocol.SubmitOperation
	if result.SnapshotRequired {
		// The Host has replaced either the runtime epoch or the persistent
		// Session identity. Never admit another mutation against a stale
		// snapshot while the subscription pump performs its atomic migration.
		binding.hasSnapshot = false
	}
	a.mu.Unlock()
	return runtimeapi.ComposerSubmitResult{
		Kind: runtimeapi.SubmitKind(result.Kind), TurnID: runtimeapi.TurnID(result.TurnID),
		OperationID: runtimeapi.OperationID(result.OperationID), Operation: string(result.Operation), Effect: string(result.Effect),
		Session: mapRemoteSessionRef(result.Target), SnapshotRequired: result.SnapshotRequired,
	}, nil
}

func (a *RemoteRuntimeAdapter) SteerTurn(ctx context.Context, input runtimeapi.SteerInput) error {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTurnSteer, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return err
	}
	value, err := a.client.Request(ctx, protocol.MethodTurnSteer, protocol.TurnSteerParams{
		SessionMutation: identity.mutation(attempt.id()), ExpectedTurnID: protocol.TurnID(input.TurnID), Text: input.Text,
	})
	if err = attempt.finish(err); err != nil {
		return err
	}
	result, ok := value.(protocol.TurnSteerResult)
	if !ok || !result.Accepted || result.TurnID != protocol.TurnID(input.TurnID) {
		return errors.New("turn/steer response does not match the requested Turn")
	}
	return nil
}

func (a *RemoteRuntimeAdapter) CancelTurn(ctx context.Context, input runtimeapi.CancelTurnInput) error {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodTurnCancel, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return err
	}
	value, err := a.client.Request(ctx, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: identity.mutation(attempt.id()), ExpectedTurnID: protocol.TurnID(input.TurnID),
	})
	if err = attempt.finish(err); err != nil {
		return err
	}
	result, ok := value.(protocol.TurnCancelResult)
	if !ok || result.TurnID != protocol.TurnID(input.TurnID) {
		return errors.New("turn/cancel response does not match the requested Turn")
	}
	return nil
}

func (a *RemoteRuntimeAdapter) ApprovePrompt(ctx context.Context, input runtimeapi.ApproveInput) error {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodPromptApprove, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return err
	}
	value, err := a.client.Request(ctx, protocol.MethodPromptApprove, protocol.PromptApproveParams{
		SessionMutation: identity.mutation(attempt.id()), PromptID: protocol.PromptID(input.PromptID),
		Decision: protocol.PromptDecision(input.Decision),
	})
	if err = attempt.finish(err); err != nil {
		return err
	}
	if err := validatePromptResolved(value, protocol.PromptID(input.PromptID)); err != nil {
		return err
	}
	a.clearPendingPrompt(identity.target)
	return nil
}

func (a *RemoteRuntimeAdapter) AnswerPrompt(ctx context.Context, input runtimeapi.AnswerInput) error {
	identity, err := a.sessionMutationIdentity(input.Session)
	if err != nil {
		return err
	}
	attempt, err := a.mutations.begin(a.newRequestID, protocol.MethodPromptAnswer, identity.hostEpoch, identity.target, identity.runtimeEpoch, input)
	if err != nil {
		return err
	}
	answers := make([]protocol.QuestionAnswer, len(input.Answers))
	for index, answer := range input.Answers {
		answers[index] = protocol.QuestionAnswer{QuestionID: protocol.QuestionID(answer.QuestionID), Selected: append([]string(nil), answer.Selected...)}
	}
	value, err := a.client.Request(ctx, protocol.MethodPromptAnswer, protocol.PromptAnswerParams{
		SessionMutation: identity.mutation(attempt.id()), PromptID: protocol.PromptID(input.PromptID), Answers: answers,
	})
	if err = attempt.finish(err); err != nil {
		return err
	}
	if err := validatePromptResolved(value, protocol.PromptID(input.PromptID)); err != nil {
		return err
	}
	a.clearPendingPrompt(identity.target)
	return nil
}

func validatePromptResolved(value any, promptID protocol.PromptID) error {
	result, ok := value.(protocol.PromptResolvedResult)
	if !ok || !result.Resolved || result.PromptID != promptID {
		return errors.New("prompt response does not match the requested Prompt")
	}
	return nil
}

func (a *RemoteRuntimeAdapter) clearPendingPrompt(target protocol.RuntimeTarget) {
	a.mu.Lock()
	if binding := a.sessions[target]; binding != nil {
		binding.prompt = false
	}
	a.mu.Unlock()
}

type remoteMutationIdentity struct {
	hostEpoch    protocol.HostEpoch
	target       protocol.RuntimeTarget
	runtimeEpoch protocol.RuntimeEpoch
	generation   uint64
}

func (i remoteMutationIdentity) mutation(requestID protocol.RequestID) protocol.SessionMutation {
	return protocol.SessionMutation{
		RequestID: requestID, ExpectedHostEpoch: i.hostEpoch, Target: i.target, ExpectedRuntimeEpoch: i.runtimeEpoch,
	}
}

func (a *RemoteRuntimeAdapter) sessionMutationIdentity(session runtimeapi.SessionRef) (remoteMutationIdentity, error) {
	if !session.Valid() {
		return remoteMutationIdentity{}, errors.New("Remote Session identity is required")
	}
	status, err := a.connectedStatus()
	if err != nil {
		return remoteMutationIdentity{}, err
	}
	target := mapRuntimeSessionRef(session)
	a.mu.RLock()
	binding := a.sessions[target]
	var runtimeEpoch protocol.RuntimeEpoch
	var bindingGeneration uint64
	var hasSnapshot bool
	if binding != nil {
		runtimeEpoch = binding.runtimeEpoch
		bindingGeneration = binding.generation
		hasSnapshot = binding.hasSnapshot
	}
	a.mu.RUnlock()
	if binding == nil || runtimeEpoch == "" || !hasSnapshot || bindingGeneration != status.Generation {
		return remoteMutationIdentity{}, errors.New("Remote Session requires an atomic snapshot before mutation")
	}
	return remoteMutationIdentity{
		hostEpoch: status.HostEpoch, target: target, runtimeEpoch: runtimeEpoch, generation: bindingGeneration,
	}, nil
}

func (a *RemoteRuntimeAdapter) connectedStatus() (remoteclient.Status, error) {
	if err := a.requireUsable(); err != nil {
		return remoteclient.Status{}, err
	}
	status := a.client.Status()
	if status.State != remoteclient.StateConnected {
		return remoteclient.Status{}, remoteclient.ErrNotConnected
	}
	return status, nil
}

func (a *RemoteRuntimeAdapter) nextRequestID() (protocol.RequestID, error) {
	if a.newRequestID == nil {
		return "", errors.New("Remote requestId generator is unavailable")
	}
	return a.newRequestID()
}

func (a *RemoteRuntimeAdapter) sessionBindingLocked(target protocol.RuntimeTarget) *remoteSessionBinding {
	binding := a.sessions[target]
	if binding == nil {
		binding = &remoteSessionBinding{}
		a.sessions[target] = binding
	}
	return binding
}

func (a *RemoteRuntimeAdapter) CanRelease(context.Context) (ReleaseStatus, error) {
	if err := a.requireUsable(); err != nil {
		return ReleaseStatus{}, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	status := ReleaseStatus{}
	for target, binding := range a.sessions {
		identity := string(target.WorkspaceID) + "/" + string(target.SessionID)
		if binding.running {
			status.Blockers = append(status.Blockers, ReleaseBlocker{Kind: ReleaseRuntimeRunning, Detail: identity})
		}
		if binding.prompt {
			status.Blockers = append(status.Blockers, ReleaseBlocker{Kind: ReleasePromptPending, Detail: identity})
		}
	}
	return status, nil
}

func (a *RemoteRuntimeAdapter) Detach(ctx context.Context) error {
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	if err := a.requireUsable(); err != nil {
		return err
	}
	if _, err := a.client.Detach(ctx); err != nil {
		return err // preserve the saved lease and adapter recovery state
	}
	clearErr := a.store.UpdateResumeLease(a.entry.ID, "")
	a.mu.Lock()
	a.entry.ResumeLeaseID = ""
	a.detached = true
	a.mu.Unlock()
	a.shutdown(true)
	if clearErr != nil {
		return &RemoteDetachCommittedError{Err: fmt.Errorf("clear detached Remote lease: %w", clearErr)}
	}
	return nil
}

func (a *RemoteRuntimeAdapter) bestEffortDetach() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = a.client.Detach(ctx)
}

func (a *RemoteRuntimeAdapter) shutdown(closeStreams bool) {
	a.shutdownOnce.Do(func() {
		a.cancel()
		_ = a.client.Close()
		a.wg.Wait()
		if closeStreams {
			a.mu.Lock()
			for _, streams := range a.streams {
				close(streams.events)
				close(streams.faults)
			}
			a.mu.Unlock()
			close(a.catalogEvents)
		}
	})
}

func mapRuntimeSessionRef(value runtimeapi.SessionRef) protocol.RuntimeTarget {
	return protocol.RuntimeTarget{WorkspaceID: protocol.WorkspaceID(value.WorkspaceID), SessionID: protocol.SessionID(value.SessionID)}
}

func mapRemoteSessionRef(value protocol.RuntimeTarget) runtimeapi.SessionRef {
	return runtimeapi.SessionRef{WorkspaceID: runtimeapi.WorkspaceID(value.WorkspaceID), SessionID: runtimeapi.SessionID(value.SessionID)}
}

func mapRemoteResolvedProfile(value protocol.ResolvedProfile) runtimeapi.ResolvedProfile {
	return runtimeapi.ResolvedProfile{
		Model: value.Model, Effort: value.Effort, CollaborationMode: string(value.CollaborationMode),
		TokenMode: string(value.TokenMode), ToolApprovalMode: string(value.ToolApprovalMode),
	}
}

func mapRemoteSessionSnapshot(value protocol.SessionSnapshot) runtimeapi.SessionSnapshot {
	return runtimeapi.SessionSnapshot{
		Session: mapRemoteSessionRef(value.Target), TopicID: runtimeapi.TopicID(value.Meta.TopicID), Title: value.Meta.Title,
		Profile: mapRemoteResolvedProfile(value.Meta.ResolvedProfile), Goal: cloneStringPointer(value.Meta.Goal),
		GoalStatus: runtimeapi.GoalStatus(value.Meta.GoalStatus), Capabilities: mapRemoteCapabilities(value.Meta.Capabilities),
		Runtime: mapRemoteRuntimeState(value.Runtime), History: mapRemoteHistory(value.History),
		PendingPrompt: mapRemotePendingPrompt(value.PendingPrompt), Todos: mapRemoteTodos(value.Todos),
		Context: mapRemoteContext(value.Context), Jobs: mapRemoteJobs(value.Jobs), Checkpoints: mapRemoteCheckpoints(value.Checkpoints),
	}
}

func mapRemoteRuntimeState(value protocol.SessionRuntimeState) runtimeapi.RuntimeState {
	result := runtimeapi.RuntimeState{
		Running: value.Running, CancelRequested: value.CancelRequested, LastOutcome: runtimeapi.SessionOutcome(value.LastOutcome),
		LastError: cloneStringPointer(value.LastError), LiveEvents: append([]eventwire.Event(nil), value.LiveEvents...),
	}
	if value.CurrentTurn != nil {
		result.CurrentTurn = &runtimeapi.TurnState{ID: runtimeapi.TurnID(value.CurrentTurn.TurnID), CancelRequested: value.CurrentTurn.CancelRequested}
	}
	if value.CurrentOperation != nil {
		result.CurrentOperation = &runtimeapi.OperationState{
			ID: runtimeapi.OperationID(value.CurrentOperation.OperationID), Kind: string(value.CurrentOperation.Kind),
			CancelRequested: value.CurrentOperation.CancelRequested,
		}
	}
	if value.Interruption != nil {
		result.Interruption = &runtimeapi.RuntimeInterruption{
			PreviousTurnInterrupted: value.Interruption.PreviousTurnInterrupted, Reason: string(value.Interruption.Reason),
		}
	}
	return result
}

func mapRemoteHistory(value protocol.HistoryPage) runtimeapi.HistoryPage {
	messages := make([]runtimeapi.HistoryMessage, len(value.Messages))
	for index, message := range value.Messages {
		toolCalls := make([]runtimeapi.HistoryToolCall, len(message.ToolCalls))
		for toolIndex, tool := range message.ToolCalls {
			toolCalls[toolIndex] = runtimeapi.HistoryToolCall{
				ID: tool.ID, Name: tool.Name, Arguments: cloneStringPointer(tool.Arguments), Subject: tool.Subject,
				Summary: cloneStringPointer(tool.Summary), Diff: cloneStringPointer(tool.Diff), Added: tool.Added,
				Removed: tool.Removed, ArgumentsArchived: tool.ArgumentsArchived,
			}
		}
		messages[index] = runtimeapi.HistoryMessage{
			Role: message.Role, Content: cloneStringPointer(message.Content), Detail: cloneStringPointer(message.Detail), Code: message.Code,
			SubmitText: cloneStringPointer(message.SubmitText), CheckpointID: runtimeapi.CheckpointID(message.CheckpointID),
			CreatedAtMillis: message.CreatedAtMs, Reasoning: cloneStringPointer(message.Reasoning),
			WorkDurationMillis: message.WorkDurationMs, MemoryCitations: append([]eventwire.MemoryCitation(nil), message.MemoryCitations...),
			Level: message.Level, ToolCalls: toolCalls, ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			ToolResultArchived: message.ToolResultArchived, ToolResultError: cloneStringPointer(message.ToolResultError),
			Pending: message.Pending, Trigger: message.Trigger, Messages: message.Messages,
			Summary: cloneStringPointer(message.Summary), Archive: cloneStringPointer(message.Archive),
		}
	}
	return runtimeapi.HistoryPage{
		Messages: messages, StartTurn: value.StartTurn, EndTurn: value.EndTurn, TotalTurns: value.TotalTurns,
		ActualTurns: value.ActualTurns, HasOlder: value.HasOlder, Next: runtimeapi.Cursor(value.NextCursor),
	}
}

func mapRemotePendingPrompt(value *protocol.PendingPrompt) *runtimeapi.PendingPrompt {
	if value == nil {
		return nil
	}
	result := &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptKind(value.Kind)}
	if value.Approval != nil {
		decisions := make([]runtimeapi.PromptDecision, len(value.Approval.AllowedDecisions))
		for index, decision := range value.Approval.AllowedDecisions {
			decisions[index] = runtimeapi.PromptDecision(decision)
		}
		result.Approval = &runtimeapi.ApprovalPrompt{
			ID: runtimeapi.PromptID(value.Approval.PromptID), Tool: value.Approval.Tool, Subject: value.Approval.Subject,
			Reason: cloneStringPointer(value.Approval.Reason), Fresh: value.Approval.Fresh, AllowedDecisions: decisions,
		}
	}
	if value.Ask != nil {
		questions := make([]runtimeapi.AskQuestion, len(value.Ask.Questions))
		for index, question := range value.Ask.Questions {
			options := make([]runtimeapi.AskOption, len(question.Options))
			for optionIndex, option := range question.Options {
				options[optionIndex] = runtimeapi.AskOption{Label: option.Label, Description: cloneStringPointer(option.Description)}
			}
			questions[index] = runtimeapi.AskQuestion{
				ID: runtimeapi.QuestionID(question.QuestionID), Header: question.Header,
				Prompt: cloneStringPointer(question.Prompt), Options: options, Multi: question.Multi,
			}
		}
		result.Ask = &runtimeapi.AskPrompt{ID: runtimeapi.PromptID(value.Ask.PromptID), Questions: questions}
	}
	return result
}

func mapRemoteTodos(values []protocol.TodoItem) []runtimeapi.TodoItem {
	result := make([]runtimeapi.TodoItem, len(values))
	for index, value := range values {
		result[index] = runtimeapi.TodoItem{
			Content: cloneStringPointer(value.Content), Status: runtimeapi.TodoStatus(value.Status),
			ActiveForm: value.ActiveForm, Level: value.Level,
		}
	}
	return result
}

func mapRemoteContext(value protocol.ContextView) runtimeapi.ContextView {
	sources := make([]runtimeapi.UsageSource, len(value.Sources))
	for index, source := range value.Sources {
		sources[index] = runtimeapi.UsageSource{
			Source: source.Source, PromptTokens: source.PromptTokens, CompletionTokens: source.CompletionTokens,
			TotalTokens: source.TotalTokens, ReasoningTokens: source.ReasoningTokens, CacheHitTokens: source.CacheHitTokens,
			CacheMissTokens: source.CacheMissTokens, RequestCount: source.RequestCount, SessionCost: source.SessionCost,
			SessionCurrency: source.SessionCurrency,
		}
	}
	files := make([]runtimeapi.ReadFileRecord, len(value.ReadFiles))
	for index, file := range value.ReadFiles {
		files[index] = runtimeapi.ReadFileRecord{
			Path: file.Path, Turn: file.Turn, TimeMs: file.TimeMs, Offset: cloneInt64Pointer(file.Offset),
			Limit: cloneInt64Pointer(file.Limit), Truncated: file.Truncated,
		}
	}
	return runtimeapi.ContextView{
		UsedTokens: value.UsedTokens, WindowTokens: value.WindowTokens, PromptTokens: value.PromptTokens,
		CompletionTokens: value.CompletionTokens, TotalTokens: value.TotalTokens, ReasoningTokens: value.ReasoningTokens,
		CacheHitTokens: value.CacheHitTokens, CacheMissTokens: value.CacheMissTokens,
		SessionCacheHitTokens: value.SessionCacheHitTokens, SessionCacheMissTokens: value.SessionCacheMissTokens,
		SessionCompletionTokens: value.SessionCompletionTokens, RequestCount: value.RequestCount,
		ElapsedMillis: value.ElapsedMs, SessionCost: value.SessionCost, SessionCurrency: value.SessionCurrency,
		Sources: sources, ReadFiles: files,
	}
}

func mapRemoteJobs(values []protocol.JobView) []runtimeapi.Job {
	result := make([]runtimeapi.Job, len(values))
	for index, value := range values {
		result[index] = runtimeapi.Job{
			ID: runtimeapi.JobID(value.ID), Kind: runtimeapi.JobKind(value.Kind), Label: value.Label,
			Status: runtimeapi.JobStatus(value.Status), StartedAtMillis: value.StartedAt,
		}
	}
	return result
}

func mapRemoteCheckpoints(values []protocol.CheckpointView) []runtimeapi.Checkpoint {
	result := make([]runtimeapi.Checkpoint, len(values))
	for index, value := range values {
		result[index] = runtimeapi.Checkpoint{
			ID: runtimeapi.CheckpointID(value.CheckpointID), DisplayTurn: value.DisplayTurn,
			Prompt: cloneStringPointer(value.Prompt), Files: append([]string(nil), value.Files...),
			FileCount: value.FileCount, FilesTruncated: value.FilesTruncated, CreatedAtMillis: value.CreatedAtMs,
			CanCode: value.CanCode, CanConversation: value.CanConversation,
		}
	}
	return result
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var (
	_ runtimeapi.RuntimeAPI = (*RemoteRuntimeAdapter)(nil)
	_ TargetAdapter         = (*RemoteRuntimeAdapter)(nil)
	_ TargetFaultSource     = (*RemoteRuntimeAdapter)(nil)
	_ TargetAbandoner       = (*RemoteRuntimeAdapter)(nil)
	_ TargetConnector       = (*RemoteTargetConnector)(nil)
	_ TargetReconnector     = (*RemoteTargetConnector)(nil)
)
