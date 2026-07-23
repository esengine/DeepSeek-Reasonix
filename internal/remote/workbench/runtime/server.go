// Package runtime implements the detached, per-workspace Remote Workbench
// runtime. Session controllers live here across SSH reconnects; provider calls
// are delegated over the active Desktop Broker connection.
package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/billing"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/fileref"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/files"
	"reasonix/internal/rpcwire"
)

const (
	GracePeriod = 5 * time.Minute
	// Keep the socket hierarchy compact. AF_UNIX addresses have a small
	// platform-specific limit (108 bytes on Windows/Linux and less on macOS),
	// and a normal Windows temporary home can already consume half of it.
	defaultSocketDirName  = "wb"
	defaultSocketFileName = "r.sock"
)

type ControllerBuilder func(context.Context, string, *string, event.Sink) (SessionController, error)

type Options struct {
	Workspace       string
	Version         string
	SourceRevision  string
	BuildController ControllerBuilder
	// SessionDir and RegistryPath are injectable for tests. Production defaults
	// keep transcripts in the normal Reasonix session directory and the Remote
	// registry beside the per-workspace runtime socket.
	SessionDir   string
	RegistryPath string
	Logger       io.Writer
}

type SessionController interface {
	ModelRef() string
	Label() string
	History() []provider.Message
	Turn() int
	Running() bool
	RuntimeStatus() control.RuntimeStatus
	Submit(string)
	Cancel()
	Close()
	SessionPath() string
	SetSessionPath(string)
	EnsureSessionPath()
	AdoptHistory([]provider.Message, string)
}

func sessionHasActiveWorkLocked(sess *session) bool {
	if sess == nil || sess.ctrl == nil {
		return false
	}
	if sess.currentTurn != "" || sess.currentOp != nil {
		return true
	}
	status := sess.ctrl.RuntimeStatus()
	return status.Running || status.PendingPrompt || status.BackgroundJobs > 0
}

type Server struct {
	opts Options

	requestMu    sync.Mutex
	connectionMu sync.Mutex
	mu           sync.Mutex
	sessions     map[protocol.SessionID]*session
	subs         map[protocol.SubscriptionID]*subscription
	wires        map[uint64]*rpcwire.Conn
	nextGen      uint64
	gen          uint64
	attached     bool
	lastDetach   time.Time
	activeConn   net.Conn
	explicitGen  map[uint64]bool

	hostEpoch   protocol.HostEpoch
	workspaceID protocol.WorkspaceID
	buildID     protocol.BuildID
	broker      *broker.Host
	contents    map[protocol.ContentRef]contentObject
	ln          net.Listener
	socket      string
	lockPath    string

	registryMu   sync.Mutex
	profileMu    sync.Mutex
	registryRead bool
	dormant      map[protocol.SessionID]runtimeSessionRecord
	shutdownOnce sync.Once
	mutations    *mutationLedger
}

type session struct {
	id            protocol.SessionID
	ctrl          SessionController
	leases        *control.SessionLeaseKeeper
	model         string
	effort        string
	collaboration protocol.CollaborationMode
	tokenMode     protocol.TokenMode
	toolApproval  protocol.ToolApprovalMode
	topicID       protocol.TopicID
	title         string
	runtimeEpoch  protocol.RuntimeEpoch
	createdAt     int64
	updatedAt     int64
	currentTurn   protocol.TurnID
	currentOp     *protocol.OperationState
	operationStop context.CancelFunc
	lastOutcome   protocol.SessionOutcome
	lastError     string
	pendingPrompt *protocol.PendingPrompt
	liveEvents    []eventwire.Event
	sink          *sessionSink
}

func closeRuntimeSession(sess *session) {
	if sess == nil {
		return
	}
	if sess.ctrl != nil {
		sess.ctrl.Close()
	}
	if sess.leases != nil {
		sess.leases.Release()
	}
}

func (sess *session) rebindSessionLease(path string) error {
	if sess == nil {
		return nil
	}
	if sess.leases == nil {
		sess.leases = control.NewSessionLeaseKeeper()
	}
	return sess.leases.Rebind(path)
}

// retireSessionAfterLeaseFailure fails closed after a controller has already
// committed a path rotation that cannot be protected by a session lease. The
// controller mutation cannot be rolled back safely, so the runtime must stop
// serving it: leaving it registered would let the old runtime epoch drive a new,
// unleased transcript. Registry cleanup is best-effort because the primary
// invariant is that no further writes are admitted through this process.
func (s *Server) retireSessionAfterLeaseFailure(sess *session, operation string, leaseErr error) {
	if sess == nil {
		return
	}
	removed := false
	s.mu.Lock()
	if s.sessions[sess.id] == sess {
		delete(s.sessions, sess.id)
		for id, sub := range s.subs {
			if sub.sessionID == sess.id {
				delete(s.subs, id)
			}
		}
		removed = true
	}
	s.mu.Unlock()
	if !removed {
		return
	}
	closeRuntimeSession(sess)
	s.logRegistryError(operation+" session lease", leaseErr)
	if err := s.persistSessionRegistry(); err != nil {
		s.logRegistryError("persist retired session after lease failure", err)
	}
}

type subscription struct {
	id        protocol.SubscriptionID
	gen       uint64
	conn      *rpcwire.Conn
	sessionID protocol.SessionID
	seq       uint64
	active    bool
	pending   []protocol.SessionEvent
}

type sessionSink struct {
	server    *Server
	sessionID protocol.SessionID
}

type contentObject struct {
	data      []byte
	sha256    string
	createdAt time.Time
}

// connectionGate keeps every post-initialize request/notification behind the
// response-commit boundary. Closing ready publishes committed with the channel
// close's happens-before guarantee; rejected candidates wake with a controlled
// stale-connection error instead of falling through to the active runtime.
type connectionGate struct {
	ready     chan struct{}
	once      sync.Once
	committed bool
}

func newConnectionGate() *connectionGate {
	return &connectionGate{ready: make(chan struct{})}
}

func (g *connectionGate) resolve(committed bool) {
	if g == nil {
		return
	}
	g.once.Do(func() {
		g.committed = committed
		close(g.ready)
	})
}

func (g *connectionGate) wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	select {
	case <-g.ready:
		if g.committed {
			return nil
		}
		return protocol.MustRemoteError(protocol.ErrStaleConnection, protocol.ErrorOptions{})
	case <-ctx.Done():
		return ctx.Err()
	}
}

func New(opts Options) *Server {
	workspace := strings.TrimSpace(opts.Workspace)
	sum := sha256.Sum256([]byte(workspace))
	return &Server{
		opts: opts, sessions: make(map[protocol.SessionID]*session),
		subs: make(map[protocol.SubscriptionID]*subscription), wires: make(map[uint64]*rpcwire.Conn),
		explicitGen: make(map[uint64]bool), broker: broker.NewHost(),
		contents:    make(map[protocol.ContentRef]contentObject),
		dormant:     make(map[protocol.SessionID]runtimeSessionRecord),
		mutations:   newMutationLedger(),
		hostEpoch:   protocol.HostEpoch("host_" + randomHex(12)),
		workspaceID: protocol.WorkspaceID("workspace_" + hex.EncodeToString(sum[:8])),
		buildID:     currentBuildID(opts),
	}
}

func SocketPath(home, workspace string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(workspace)))
	dir := filepath.Join(home, ".reasonix", defaultSocketDirName, hex.EncodeToString(sum[:8]))
	return filepath.Join(dir, defaultSocketFileName)
}

func (s *Server) ListenAndServe(ctx context.Context, socket string) error {
	if strings.TrimSpace(s.opts.Workspace) == "" {
		return errors.New("workspace required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return err
	}
	lockPath := socket + ".lock"
	if err := acquireRuntimeLock(ctx, socket, lockPath); err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(socket)
		_ = os.Remove(lockPath)
	}()
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	_ = os.Chmod(socket, 0o600)
	s.mu.Lock()
	s.ln, s.socket, s.lockPath = ln, socket, lockPath
	s.mu.Unlock()
	go s.graceLoop(ctx)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.serveConn(ctx, conn)
	}
}

func acquireRuntimeLock(ctx context.Context, socket, lockPath string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			return nil
		} else if !os.IsExist(err) {
			return err
		}
		if conn, err := net.DialTimeout("unix", socket, 150*time.Millisecond); err == nil {
			_ = conn.Close()
			return errors.New("Remote Workbench runtime is already running")
		}
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > 30*time.Second {
			if os.Remove(lockPath) == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return errors.New("Remote Workbench runtime start is already in progress")
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	s.mu.Lock()
	s.nextGen++
	gen := s.nextGen
	s.mu.Unlock()
	gate := newConnectionGate()

	router, err := protocol.NewCompleteRouter(s.handlers(gen, conn, gate), protocol.RouterOptions{})
	if err != nil {
		gate.resolve(false)
		_ = conn.Close()
		return
	}
	wire := rpcwire.NewConn(conn, conn, router.WireOptions())
	s.mu.Lock()
	s.wires[gen] = wire
	s.mu.Unlock()
	if err := s.broker.Bind(wire, gen, gate.ready); err != nil {
		gate.resolve(false)
		_ = conn.Close()
		return
	}
	router.Bind(wire)
	_ = wire.Serve(ctx)

	// Serialize teardown with the initialize after-write callback. Otherwise a
	// peer that closes immediately after the response could reject its gate and
	// delete its wire between commitConnection's liveness check and activation.
	s.connectionMu.Lock()
	gate.resolve(false)
	s.broker.Detach(gen)
	s.mu.Lock()
	delete(s.wires, gen)
	for id, sub := range s.subs {
		if sub.gen == gen {
			delete(s.subs, id)
		}
	}
	if s.gen == gen {
		s.attached = false
		s.activeConn = nil
		if !s.explicitGen[gen] {
			s.lastDetach = time.Now()
		}
	}
	delete(s.explicitGen, gen)
	s.mu.Unlock()
	s.connectionMu.Unlock()
}

func (s *Server) handlers(gen uint64, conn net.Conn, gate *connectionGate) protocol.HandlerSet {
	handlers := protocol.HandlerSet{
		protocol.MethodRemoteInitialize: func(ctx context.Context, value any) (any, error) {
			return s.initialize(gen, conn, gate, value.(protocol.InitializeParams))
		},
		protocol.MethodRemotePing: func(ctx context.Context, value any) (any, error) { return s.ping(gen, value.(protocol.PingParams)) },
		protocol.MethodRemoteDetach: func(ctx context.Context, value any) (any, error) {
			return s.detach(gen, conn, value.(protocol.DetachParams))
		},
		protocol.MethodHostCapabilities: func(ctx context.Context, value any) (any, error) {
			return s.capabilities(value.(protocol.HostCapabilitiesParams))
		},
		protocol.MethodWorkspaceList: func(ctx context.Context, value any) (any, error) {
			return s.workspaceList(value.(protocol.WorkspaceListParams))
		},
		protocol.MethodCatalogWorkspace: func(ctx context.Context, value any) (any, error) {
			return s.workspaceCatalog(value.(protocol.WorkspaceCatalogParams))
		},
		protocol.MethodCatalogSession: func(ctx context.Context, value any) (any, error) {
			return s.sessionCatalog(ctx, value.(protocol.SessionCatalogParams))
		},
		protocol.MethodSessionList: func(ctx context.Context, value any) (any, error) {
			return s.list(ctx, value.(protocol.SessionListParams))
		},
		protocol.MethodSessionCreate: func(ctx context.Context, value any) (any, error) {
			return s.create(ctx, value.(protocol.SessionCreateParams))
		},
		protocol.MethodSessionClose: func(ctx context.Context, value any) (any, error) {
			return s.closeSession(value.(protocol.SessionCloseParams))
		},
		protocol.MethodSessionSubscribe: func(ctx context.Context, value any) (any, error) {
			return s.subscribe(gen, value.(protocol.SessionSubscribeParams))
		},
		protocol.MethodSessionUnsubscribe: func(ctx context.Context, value any) (any, error) {
			return s.unsubscribe(gen, value.(protocol.SessionUnsubscribeParams))
		},
		protocol.MethodSessionHistory: func(ctx context.Context, value any) (any, error) {
			return s.history(value.(protocol.SessionHistoryParams))
		},
		protocol.MethodSessionContent: func(ctx context.Context, value any) (any, error) {
			return s.sessionContent(value.(protocol.SessionContentParams))
		},
		protocol.MethodSessionSubmit: func(ctx context.Context, value any) (any, error) {
			return s.submit(value.(protocol.SessionSubmitParams))
		},
		protocol.MethodTurnSteer: func(ctx context.Context, value any) (any, error) {
			return s.steer(value.(protocol.TurnSteerParams))
		},
		protocol.MethodPromptApprove: func(ctx context.Context, value any) (any, error) {
			return s.approve(value.(protocol.PromptApproveParams))
		},
		protocol.MethodPromptAnswer: func(ctx context.Context, value any) (any, error) {
			return s.answer(value.(protocol.PromptAnswerParams))
		},
		protocol.MethodShellRun: func(ctx context.Context, value any) (any, error) {
			return s.shellRun(value.(protocol.ShellRunParams))
		},
		protocol.MethodOperationCancel: func(ctx context.Context, value any) (any, error) {
			return s.cancelOperation(value.(protocol.OperationCancelParams))
		},
		protocol.MethodSessionNew: func(ctx context.Context, value any) (any, error) {
			return s.newSession(value.(protocol.SessionNewParams))
		},
		protocol.MethodSessionClear: func(ctx context.Context, value any) (any, error) {
			return s.clearSession(value.(protocol.SessionClearParams))
		},
		protocol.MethodSessionCompact: func(ctx context.Context, value any) (any, error) {
			return s.compact(ctx, value.(protocol.SessionCompactParams))
		},
		protocol.MethodSessionFork: func(ctx context.Context, value any) (any, error) {
			return s.forkSession(ctx, value.(protocol.SessionForkParams))
		},
		protocol.MethodSessionRewind: func(ctx context.Context, value any) (any, error) {
			return s.rewindSession(value.(protocol.SessionRewindParams))
		},
		protocol.MethodSessionSummarize: func(ctx context.Context, value any) (any, error) {
			return s.summarizeSession(value.(protocol.SessionSummarizeParams))
		},
		protocol.MethodSessionProfileSet: func(ctx context.Context, value any) (any, error) {
			return s.setProfile(ctx, value.(protocol.SessionProfileSetParams))
		},
		protocol.MethodSessionGoalSet: func(ctx context.Context, value any) (any, error) {
			return s.setGoal(value.(protocol.SessionGoalSetParams))
		},
		protocol.MethodSessionGoalResume: func(ctx context.Context, value any) (any, error) {
			return s.resumeGoal(value.(protocol.SessionGoalResumeParams))
		},
		protocol.MethodSessionGoalClear: func(ctx context.Context, value any) (any, error) {
			return s.clearGoal(value.(protocol.SessionGoalClearParams))
		},
		protocol.MethodSessionContext: func(ctx context.Context, value any) (any, error) {
			return s.sessionContext(value.(protocol.SessionContextParams))
		},
		protocol.MethodSessionBalance: func(ctx context.Context, value any) (any, error) {
			return s.sessionBalance(ctx, value.(protocol.SessionBalanceParams))
		},
		protocol.MethodJobList: func(ctx context.Context, value any) (any, error) {
			return s.jobList(value.(protocol.JobListParams))
		},
		protocol.MethodJobCancel: func(ctx context.Context, value any) (any, error) {
			return s.jobCancel(value.(protocol.JobCancelParams))
		},
		protocol.MethodComposerSlashArgs: func(ctx context.Context, value any) (any, error) {
			return s.composerSlashArgs(value.(protocol.ComposerSlashArgsParams))
		},
		protocol.MethodTurnCancel: func(ctx context.Context, value any) (any, error) { return s.cancel(value.(protocol.TurnCancelParams)) },
		protocol.MethodFileList:   func(ctx context.Context, value any) (any, error) { return s.fileList(value.(protocol.FileListParams)) },
		protocol.MethodFileSearch: func(ctx context.Context, value any) (any, error) {
			return s.fileSearch(value.(protocol.FileSearchParams))
		},
		protocol.MethodFilePreview: func(ctx context.Context, value any) (any, error) {
			return s.filePreview(value.(protocol.FilePreviewParams))
		},
		protocol.MethodWorkspaceChanges: func(ctx context.Context, value any) (any, error) {
			return s.workspaceChanges(value.(protocol.WorkspaceChangesParams))
		},
		protocol.MethodWorkspaceChangeDetail: func(ctx context.Context, value any) (any, error) {
			return s.workspaceChangeDetail(value.(protocol.WorkspaceChangeDetailParams))
		},
		protocol.MethodGitHistory: func(ctx context.Context, value any) (any, error) {
			return s.gitHistory(value.(protocol.GitHistoryParams))
		},
		protocol.MethodGitCommitDetail: func(ctx context.Context, value any) (any, error) {
			return s.gitCommitDetail(value.(protocol.GitCommitDetailParams))
		},
	}
	for _, spec := range protocol.Registry() {
		if spec.Direction != protocol.DirectionClientRequest || handlers[spec.Name] != nil {
			continue
		}
		handlers[spec.Name] = func(context.Context, any) (any, error) {
			return nil, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
		}
	}
	for method, handler := range handlers {
		if method == protocol.MethodRemoteInitialize {
			continue
		}
		handler := handler
		handlers[method] = func(ctx context.Context, value any) (any, error) {
			if err := gate.wait(ctx); err != nil {
				return nil, err
			}
			return handler(ctx, value)
		}
	}
	return s.serializeHandlers(handlers)
}

func (s *Server) initialize(gen uint64, conn net.Conn, gate *connectionGate, p protocol.InitializeParams) (any, error) {
	s.mu.Lock()
	_, live := s.wires[gen]
	activeGen := s.gen
	s.mu.Unlock()
	if !live || gen <= activeGen {
		return nil, protocol.MustRemoteError(protocol.ErrStaleConnection, protocol.ErrorOptions{})
	}
	want, err := filepath.Abs(strings.TrimSpace(s.opts.Workspace))
	if err != nil {
		return nil, protocol.MustRemoteError(protocol.ErrWorkspaceNotFound, protocol.ErrorOptions{})
	}
	got, err := filepath.Abs(strings.TrimSpace(p.Workspace))
	if err != nil || filepath.Clean(got) != filepath.Clean(want) {
		return nil, protocol.MustRemoteError(protocol.ErrWorkspaceNotFound, protocol.ErrorOptions{})
	}
	if err := protocol.CompareBuildID(p.BuildID, s.buildID); err != nil {
		return nil, protocol.MustRemoteError(protocol.ErrDaemonRestartRequired, protocol.ErrorOptions{})
	}
	result := protocol.InitializeResult{
		BuildID: s.buildID, HostEpoch: s.hostEpoch,
		Lease:        protocol.LeaseInfo{LeaseID: leaseID(gen), TTLMillis: protocol.LeaseTTLMillis, PingIntervalMs: protocol.LeasePingIntervalMillis},
		Host:         protocol.HostInfo{OS: goruntime.GOOS, Arch: goruntime.GOARCH, ShellKind: "sh", SandboxBackend: "reasonix"},
		Capabilities: protocol.FrozenCapabilities(false, false),
	}
	return rpcwire.RespondThen(result, func(writeErr error) {
		if writeErr != nil {
			gate.resolve(false)
			_ = conn.Close()
			return
		}
		s.commitConnection(gen, conn, gate)
	}), nil
}

func (s *Server) commitConnection(gen uint64, conn net.Conn, gate *connectionGate) {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()

	s.mu.Lock()
	wire := s.wires[gen]
	if wire == nil || gen <= s.gen {
		s.mu.Unlock()
		gate.resolve(false)
		_ = conn.Close()
		return
	}
	s.mu.Unlock()
	if err := s.broker.Activate(wire, gen); err != nil {
		gate.resolve(false)
		_ = conn.Close()
		return
	}

	s.mu.Lock()
	old := s.activeConn
	s.gen = gen
	s.activeConn = conn
	s.attached = true
	s.lastDetach = time.Time{}
	s.mu.Unlock()
	gate.resolve(true)
	if old != nil && old != conn {
		_ = old.Close()
	}
}

func (s *Server) ping(gen uint64, p protocol.PingParams) (protocol.PingResult, error) {
	if p.LeaseID != leaseID(gen) {
		return protocol.PingResult{}, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	return protocol.PingResult{HostEpoch: s.hostEpoch, LeaseTTL: protocol.LeaseTTLMillis}, nil
}

func (s *Server) detach(gen uint64, conn net.Conn, p protocol.DetachParams) (any, error) {
	if p.LeaseID != leaseID(gen) {
		return nil, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{})
	}
	return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(writeErr error) {
		if writeErr == nil {
			s.mu.Lock()
			s.explicitGen[gen] = true
			if s.gen == gen {
				s.attached = false
				s.lastDetach = time.Now().Add(-GracePeriod)
			}
			s.mu.Unlock()
		}
		_ = conn.Close()
	}), nil
}

func (s *Server) capabilities(p protocol.HostCapabilitiesParams) (protocol.HostCapabilitiesResult, error) {
	if err := s.checkHost(p.ExpectedHostEpoch); err != nil {
		return protocol.HostCapabilitiesResult{}, err
	}
	return protocol.HostCapabilitiesResult{HostEpoch: s.hostEpoch, Capabilities: protocol.FrozenCapabilities(false, false)}, nil
}

func (s *Server) workspaceList(p protocol.WorkspaceListParams) (protocol.WorkspaceListResult, error) {
	if err := s.checkHost(p.ExpectedHostEpoch); err != nil {
		return protocol.WorkspaceListResult{}, err
	}
	return protocol.WorkspaceListResult{Items: []protocol.WorkspaceSummary{{
		WorkspaceID: s.workspaceID, Name: filepath.Base(s.opts.Workspace), DisplayPath: s.opts.Workspace,
	}}, HasMore: false}, nil
}

func (s *Server) workspaceCatalog(p protocol.WorkspaceCatalogParams) (protocol.WorkspaceCatalogResult, error) {
	if err := s.checkHostWorkspace(p.ExpectedHostEpoch, p.WorkspaceID); err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	descriptors := s.broker.Catalog()
	models := make([]protocol.ModelCatalogItem, 0, len(descriptors))
	for _, descriptor := range descriptors {
		providerName, model := splitModelRef(descriptor.Ref)
		if model == "" {
			model = descriptor.Model
		}
		if providerName == "" {
			providerName = descriptor.DisplayName
		}
		models = append(models, protocol.ModelCatalogItem{
			Ref: protocol.ModelRef(descriptor.Ref), Provider: providerName, Model: model,
			Effort: protocol.EffortCatalog{
				Supported: len(descriptor.Efforts) > 0, Default: descriptor.DefaultEffort,
				Levels: append([]string(nil), descriptor.Efforts...),
			},
		})
	}
	defaultModel, defaultEffort := "unavailable", "default"
	if len(descriptors) > 0 {
		defaultModel = descriptors[0].Ref
		if descriptors[0].DefaultEffort != "" {
			defaultEffort = descriptors[0].DefaultEffort
		}
	}
	return protocol.WorkspaceCatalogResult{
		Revision: protocol.CatalogRevision("catalog_" + randomHex(8)), Models: models,
		CollaborationModes: []protocol.CollaborationMode{protocol.CollaborationNormal, protocol.CollaborationPlan, protocol.CollaborationGoal},
		TokenModes:         []protocol.TokenMode{protocol.TokenFull, protocol.TokenEconomy, protocol.TokenDelivery},
		ToolApprovalModes:  []protocol.ToolApprovalMode{protocol.ToolApprovalAsk, protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO},
		DefaultProfile: protocol.ResolvedProfile{
			Model: defaultModel, Effort: defaultEffort, CollaborationMode: protocol.CollaborationNormal,
			TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
		},
	}, nil
}

func (s *Server) sessionCatalog(ctx context.Context, p protocol.SessionCatalogParams) (protocol.SessionCatalogResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionCatalogResult{}, err
	}
	return buildSessionCatalog(ctx, sess.ctrl), nil
}

func (s *Server) list(ctx context.Context, p protocol.SessionListParams) (protocol.SessionListResult, error) {
	if err := s.checkHostWorkspace(p.ExpectedHostEpoch, p.WorkspaceID); err != nil {
		return protocol.SessionListResult{}, err
	}
	if err := s.ensureSessionsRestored(ctx); err != nil {
		return protocol.SessionListResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{})
	}
	s.mu.Lock()
	items := make([]protocol.SessionSummary, 0, len(s.sessions))
	for _, sess := range s.sessions {
		items = append(items, s.summaryLocked(sess))
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].LastActivityAtMs > items[j].LastActivityAtMs })
	return protocol.SessionListResult{Items: items, HasMore: false}, nil
}

func (s *Server) create(ctx context.Context, p protocol.SessionCreateParams) (protocol.SessionCreateResult, error) {
	if err := s.checkHostWorkspace(p.ExpectedHostEpoch, p.WorkspaceID); err != nil {
		return protocol.SessionCreateResult{}, err
	}
	if err := s.ensureSessionsRestored(ctx); err != nil {
		return protocol.SessionCreateResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{})
	}
	model := ""
	if p.Profile.Model != nil {
		model = strings.TrimSpace(*p.Profile.Model)
	}
	if model == "" {
		if catalog := s.broker.Catalog(); len(catalog) > 0 {
			model = catalog[0].Ref
		}
	}
	if model == "" {
		return protocol.SessionCreateResult{}, protocol.MustRemoteError(protocol.ErrModelNotAvailable, protocol.ErrorOptions{})
	}
	effort := "default"
	var effortOverride *string
	if p.Profile.Effort != nil {
		effort = strings.TrimSpace(*p.Profile.Effort)
		effortOverride = &effort
	} else {
		for _, descriptor := range s.broker.Catalog() {
			if descriptor.Ref == model && descriptor.DefaultEffort != "" {
				effort = descriptor.DefaultEffort
				break
			}
		}
	}
	id := protocol.SessionID("session_" + randomHex(12))
	sink := &sessionSink{server: s, sessionID: id}
	collaboration := protocol.CollaborationNormal
	if p.Profile.CollaborationMode != nil {
		collaboration = *p.Profile.CollaborationMode
	}
	tokenMode := protocol.TokenFull
	if p.Profile.TokenMode != nil {
		tokenMode = *p.Profile.TokenMode
	}
	toolApproval := protocol.ToolApprovalAsk
	if p.Profile.ToolApprovalMode != nil {
		toolApproval = *p.Profile.ToolApprovalMode
	}
	ctrl, err := s.buildController(ctx, model, effortOverride, sink, tokenMode)
	if err != nil {
		return protocol.SessionCreateResult{}, protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{})
	}
	if ctrl == nil {
		return protocol.SessionCreateResult{}, errors.New("controller builder returned nil")
	}
	ctrl.EnsureSessionPath()
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		ctrl.Close()
		leases.Release()
		return protocol.SessionCreateResult{}, protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{})
	}
	now := time.Now().UnixMilli()
	title := strings.TrimSpace(p.Topic.Title)
	if title == "" {
		title = "New session"
	}
	topicID := p.Topic.TopicID
	if topicID == "" {
		topicID = protocol.TopicID("topic_" + randomHex(8))
	}
	sess := &session{
		id: id, ctrl: ctrl, leases: leases, model: ctrl.ModelRef(), effort: effort,
		collaboration: collaboration, tokenMode: tokenMode, toolApproval: toolApproval,
		topicID: topicID, title: title,
		runtimeEpoch: protocol.RuntimeEpoch("runtime_" + randomHex(12)),
		createdAt:    now, updatedAt: now, sink: sink,
	}
	applyControllerProfile(ctrl, collaboration, toolApproval)
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	if err := s.persistSessionRegistry(); err != nil {
		s.mu.Lock()
		if s.sessions[id] == sess {
			delete(s.sessions, id)
		}
		s.mu.Unlock()
		closeRuntimeSession(sess)
		return protocol.SessionCreateResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{})
	}
	return protocol.SessionCreateResult{
		Target: s.target(id), RuntimeEpoch: sess.runtimeEpoch,
		TopicID: topicID, TopicTitle: title, ResolvedProfile: resolvedProfile(sess),
	}, nil
}

func (s *Server) buildController(ctx context.Context, model string, effort *string, sink event.Sink, tokenMode protocol.TokenMode) (SessionController, error) {
	var ctrl SessionController
	var err error
	if s.opts.BuildController != nil {
		ctrl, err = s.opts.BuildController(ctx, model, effort, sink)
	} else {
		ctrl, err = boot.Build(ctx, boot.Options{
			Model: model, EffortOverride: effort, RequireKey: false,
			WorkspaceRoot: s.opts.Workspace, ProviderResolver: s.broker, Sink: sink, TokenMode: string(tokenMode),
			SessionDir: s.sessionDir(),
		})
	}
	if nilSessionController(ctrl) {
		ctrl = nil
	}
	return ctrl, err
}

func nilSessionController(ctrl SessionController) bool {
	if ctrl == nil {
		return true
	}
	value := reflect.ValueOf(ctrl)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *Server) closeSession(p protocol.SessionCloseParams) (protocol.SessionCloseResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionCloseResult{}, err
	}
	s.mu.Lock()
	busy := sessionHasActiveWorkLocked(sess)
	s.mu.Unlock()
	if busy {
		return protocol.SessionCloseResult{Disposition: protocol.SessionRetainedActive}, nil
	}
	s.mu.Lock()
	if s.sessions[sess.id] != sess {
		s.mu.Unlock()
		return protocol.SessionCloseResult{Disposition: protocol.SessionAlreadyClosed}, nil
	}
	delete(s.sessions, sess.id)
	removedSubs := make(map[protocol.SubscriptionID]*subscription)
	for id, sub := range s.subs {
		if sub.sessionID == sess.id {
			removedSubs[id] = sub
			delete(s.subs, id)
		}
	}
	s.mu.Unlock()
	if err := s.persistSessionRegistry(); err != nil {
		s.mu.Lock()
		if s.sessions[sess.id] == nil {
			s.sessions[sess.id] = sess
			for id, sub := range removedSubs {
				s.subs[id] = sub
			}
		}
		s.mu.Unlock()
		return protocol.SessionCloseResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{})
	}
	closeRuntimeSession(sess)
	return protocol.SessionCloseResult{Disposition: protocol.SessionReleased}, nil
}

func (s *Server) subscribe(gen uint64, p protocol.SessionSubscribeParams) (any, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, "")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if old := s.subs[p.ReplaceSubscriptionID]; old != nil && old.gen == gen {
		delete(s.subs, p.ReplaceSubscriptionID)
	}
	conn := s.currentWireConnectionLocked(gen)
	if conn == nil {
		s.mu.Unlock()
		return nil, protocol.MustRemoteError(protocol.ErrStaleConnection, protocol.ErrorOptions{})
	}
	id := protocol.SubscriptionID("subscription_" + randomHex(10))
	sub := &subscription{id: id, gen: gen, conn: conn, sessionID: sess.id}
	s.subs[id] = sub
	snapshot := s.snapshotLocked(sess, p.PageTurns)
	s.mu.Unlock()
	return rpcwire.RespondThen(protocol.SessionSubscribeResult{SubscriptionID: id, Snapshot: snapshot}, func(writeErr error) {
		s.activateSubscription(id, sub, writeErr)
		if writeErr == nil {
			if controller, ok := sess.ctrl.(interface{ ReplayPendingPrompts() }); ok {
				controller.ReplayPendingPrompts()
			}
		}
	}), nil
}

// currentWireConnectionLocked resolves the rpcwire peer indirectly from an
// existing subscription or the generation-owned Broker. The serve connection
// installs the actual wire in connectionWires before requests are served.
func (s *Server) currentWireConnectionLocked(gen uint64) *rpcwire.Conn {
	return s.wires[gen]
}

func (s *Server) activateSubscription(id protocol.SubscriptionID, sub *subscription, writeErr error) {
	s.mu.Lock()
	if writeErr != nil || s.subs[id] != sub {
		delete(s.subs, id)
		s.mu.Unlock()
		return
	}
	sub.active = true
	pending := append([]protocol.SessionEvent(nil), sub.pending...)
	sub.pending = nil
	s.mu.Unlock()
	for _, event := range pending {
		_ = sub.conn.Notify(string(protocol.MethodSessionEvent), event)
	}
}

func (s *Server) unsubscribe(gen uint64, p protocol.SessionUnsubscribeParams) (protocol.SessionUnsubscribeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := s.subs[p.SubscriptionID]
	if sub == nil || sub.gen != gen {
		return protocol.SessionUnsubscribeResult{}, protocol.MustRemoteError(protocol.ErrSubscriptionNotFound, protocol.ErrorOptions{})
	}
	delete(s.subs, p.SubscriptionID)
	return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
}

func (s *Server) history(p protocol.SessionHistoryParams) (protocol.HistoryPage, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	beforeTurn, err := historyCursorTurn(p.Cursor)
	if err != nil {
		return protocol.HistoryPage{}, protocol.MustRemoteError(protocol.ErrStaleCursor, protocol.ErrorOptions{Target: &p.Target})
	}
	return historyPage(sess, p.SnapshotID, beforeTurn, p.PageTurns), nil
}

func (s *Server) sessionContent(p protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	s.mu.Lock()
	object, ok := s.contents[p.ContentRef]
	s.mu.Unlock()
	if !ok {
		return protocol.SessionContentResult{}, protocol.MustRemoteError(protocol.ErrContentRefExpired, protocol.ErrorOptions{})
	}
	if p.Offset < 0 || p.Offset > int64(len(object.data)) {
		return protocol.SessionContentResult{}, protocol.MustRemoteError(protocol.ErrContentRefExpired, protocol.ErrorOptions{})
	}
	end := p.Offset + protocol.ContentRefChunkBytes
	if end > int64(len(object.data)) {
		end = int64(len(object.data))
	}
	var next *int64
	if end < int64(len(object.data)) {
		value := end
		next = &value
	}
	return protocol.SessionContentResult{
		ContentRef: p.ContentRef, Offset: p.Offset,
		DataBase64: base64.StdEncoding.EncodeToString(object.data[p.Offset:end]), NextOffset: next,
		TotalBytes: int64(len(object.data)), SHA256: object.sha256, Encoding: protocol.ContentUTF8,
	}, nil
}

func (s *Server) submit(p protocol.SessionSubmitParams) (protocol.SessionSubmitResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionSubmitResult{}, err
	}
	s.mu.Lock()
	busy := sess.currentTurn != "" || sess.currentOp != nil || sess.ctrl.Running()
	s.mu.Unlock()
	if busy {
		return protocol.SessionSubmitResult{}, protocol.MustRemoteError(protocol.ErrTurnAlreadyRunning, protocol.ErrorOptions{Target: &p.Target})
	}
	turnID := protocol.TurnID("turn_" + randomHex(10))
	s.mu.Lock()
	if s.sessions[sess.id] != sess {
		s.mu.Unlock()
		return protocol.SessionSubmitResult{}, protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{})
	}
	sess.currentTurn = turnID
	sess.updatedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	sess.ctrl.Submit(p.Input)
	return protocol.SessionSubmitResult{
		Kind: protocol.SubmitTurn, TurnID: turnID,
		Target: p.Target, RuntimeEpoch: sess.runtimeEpoch,
	}, nil
}

func (s *Server) steer(p protocol.TurnSteerParams) (protocol.TurnSteerResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.TurnSteerResult{}, err
	}
	s.mu.Lock()
	current := sess.currentTurn
	s.mu.Unlock()
	if current == "" {
		return protocol.TurnSteerResult{}, protocol.MustRemoteError(protocol.ErrTurnNotActive, protocol.ErrorOptions{Target: &p.Target})
	}
	if current != p.ExpectedTurnID {
		return protocol.TurnSteerResult{}, protocol.MustRemoteError(protocol.ErrTurnMismatch, protocol.ErrorOptions{Target: &p.Target, Expected: string(p.ExpectedTurnID), Actual: string(current)})
	}
	controller, ok := sess.ctrl.(interface{ Steer(string) })
	if !ok {
		return protocol.TurnSteerResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	controller.Steer(p.Text)
	return protocol.TurnSteerResult{Accepted: true, TurnID: current}, nil
}

func (s *Server) approve(p protocol.PromptApproveParams) (protocol.PromptResolvedResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	controller, ok := sess.ctrl.(interface {
		Approve(string, bool, bool, bool)
	})
	if !ok {
		return protocol.PromptResolvedResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	s.mu.Lock()
	pending := sess.pendingPrompt
	if pending == nil || pending.Kind != protocol.PromptApproval || pending.Approval == nil || pending.Approval.PromptID != p.PromptID {
		s.mu.Unlock()
		return protocol.PromptResolvedResult{}, protocol.MustRemoteError(protocol.ErrPromptNotPending, protocol.ErrorOptions{Target: &p.Target})
	}
	sess.pendingPrompt = nil
	s.mu.Unlock()
	allow := p.Decision != protocol.DecisionDeny
	controller.Approve(string(p.PromptID), allow, p.Decision == protocol.DecisionAllowSession, p.Decision == protocol.DecisionAllowPersistent)
	return protocol.PromptResolvedResult{Resolved: true, PromptID: p.PromptID}, nil
}

func (s *Server) answer(p protocol.PromptAnswerParams) (protocol.PromptResolvedResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	controller, ok := sess.ctrl.(interface {
		AnswerQuestion(string, []event.AskAnswer)
	})
	if !ok {
		return protocol.PromptResolvedResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	s.mu.Lock()
	pending := sess.pendingPrompt
	if pending == nil || pending.Kind != protocol.PromptAsk || pending.Ask == nil || pending.Ask.PromptID != p.PromptID {
		s.mu.Unlock()
		return protocol.PromptResolvedResult{}, protocol.MustRemoteError(protocol.ErrPromptNotPending, protocol.ErrorOptions{Target: &p.Target})
	}
	sess.pendingPrompt = nil
	s.mu.Unlock()
	answers := make([]event.AskAnswer, 0, len(p.Answers))
	for _, answer := range p.Answers {
		answers = append(answers, event.AskAnswer{QuestionID: string(answer.QuestionID), Selected: append([]string(nil), answer.Selected...)})
	}
	controller.AnswerQuestion(string(p.PromptID), answers)
	return protocol.PromptResolvedResult{Resolved: true, PromptID: p.PromptID}, nil
}

func (s *Server) shellRun(p protocol.ShellRunParams) (protocol.OperationStartedResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.OperationStartedResult{}, err
	}
	s.mu.Lock()
	hasOperation := sess.currentOp != nil
	s.mu.Unlock()
	if sess.ctrl.Running() || hasOperation {
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &p.Target})
	}
	controller, ok := sess.ctrl.(interface{ RunShell(string) })
	if !ok {
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	operationID := protocol.OperationID("operation_" + randomHex(10))
	s.mu.Lock()
	sess.currentOp = &protocol.OperationState{OperationID: operationID, Kind: protocol.OperationShell}
	s.mu.Unlock()
	controller.RunShell(p.Command)
	return protocol.OperationStartedResult{OperationID: operationID, Disposition: "started"}, nil
}

func (s *Server) newSession(p protocol.SessionNewParams) (protocol.SessionNewResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionNewResult{}, err
	}
	controller, ok := sess.ctrl.(interface{ NewSession() error })
	if !ok {
		return protocol.SessionNewResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	if err := controller.NewSession(); err != nil {
		return protocol.SessionNewResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	if err := sess.rebindSessionLease(sess.ctrl.SessionPath()); err != nil {
		s.retireSessionAfterLeaseFailure(sess, "new", err)
		return protocol.SessionNewResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	epoch := s.commitSessionRotation(sess)
	if err := s.persistSessionRegistry(); err != nil {
		// NewSession already rotated the controller and cannot be rolled back
		// safely. Return the committed epoch so the client stays usable; a later
		// registry write (including shutdown) can persist the new transcript path.
		s.logRegistryError("persist committed new session", err)
	}
	return protocol.SessionNewResult{SourceTarget: p.Target, Target: p.Target, RuntimeEpoch: epoch, Disposition: "created", SnapshotRequired: true}, nil
}

func (s *Server) clearSession(p protocol.SessionClearParams) (protocol.SessionClearResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionClearResult{}, err
	}
	controller, ok := sess.ctrl.(interface{ ClearSession() error })
	if !ok {
		return protocol.SessionClearResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	if err := controller.ClearSession(); err != nil {
		return protocol.SessionClearResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	if err := sess.rebindSessionLease(sess.ctrl.SessionPath()); err != nil {
		s.retireSessionAfterLeaseFailure(sess, "clear", err)
		return protocol.SessionClearResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	epoch := s.commitSessionRotation(sess)
	if err := s.persistSessionRegistry(); err != nil {
		// ClearSession has already discarded the old controller state. Reporting
		// failure here would leave the client on the previous epoch even though the
		// destructive mutation committed, so keep the result authoritative.
		s.logRegistryError("persist committed cleared session", err)
	}
	return protocol.SessionClearResult{PreviousTarget: p.Target, Target: p.Target, RuntimeEpoch: epoch, Disposition: protocol.SessionCleared, SnapshotRequired: true}, nil
}

func (s *Server) commitSessionRotation(sess *session) protocol.RuntimeEpoch {
	s.mu.Lock()
	sess.runtimeEpoch = protocol.RuntimeEpoch("runtime_" + randomHex(12))
	sess.currentTurn = ""
	sess.lastOutcome = ""
	sess.lastError = ""
	sess.pendingPrompt = nil
	sess.liveEvents = nil
	sess.updatedAt = time.Now().UnixMilli()
	epoch := sess.runtimeEpoch
	s.mu.Unlock()
	return epoch
}

func (s *Server) compact(ctx context.Context, p protocol.SessionCompactParams) (protocol.OperationStartedResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.OperationStartedResult{}, err
	}
	controller, ok := sess.ctrl.(interface {
		Compact(context.Context, string) error
	})
	if !ok {
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	operationID := protocol.OperationID("operation_" + randomHex(10))
	opCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if sess.currentOp != nil || sess.ctrl.Running() {
		s.mu.Unlock()
		cancel()
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &p.Target})
	}
	sess.currentOp = &protocol.OperationState{OperationID: operationID, Kind: protocol.OperationCompact}
	sess.operationStop = cancel
	s.mu.Unlock()
	go func() {
		_ = controller.Compact(opCtx, p.Instructions)
		s.finishOperation(sess.id, operationID)
	}()
	return protocol.OperationStartedResult{OperationID: operationID, Disposition: "started"}, nil
}

func (s *Server) setProfile(ctx context.Context, p protocol.SessionProfileSetParams) (protocol.SessionProfileSetResult, error) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()

	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionProfileSetResult{}, err
	}
	s.mu.Lock()
	busy := sessionHasActiveWorkLocked(sess)
	s.mu.Unlock()
	if busy {
		return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &p.Target})
	}
	model, effort := sess.model, sess.effort
	collaboration, tokenMode, toolApproval := sess.collaboration, sess.tokenMode, sess.toolApproval
	if p.Patch.Model != nil {
		model = strings.TrimSpace(*p.Patch.Model)
	}
	if p.Patch.Effort != nil {
		effort = strings.TrimSpace(*p.Patch.Effort)
	}
	if p.Patch.CollaborationMode != nil {
		collaboration = *p.Patch.CollaborationMode
	}
	if p.Patch.TokenMode != nil {
		tokenMode = *p.Patch.TokenMode
	}
	if p.Patch.ToolApprovalMode != nil {
		toolApproval = *p.Patch.ToolApprovalMode
	}
	rebuild := model != sess.model || effort != sess.effort || tokenMode != sess.tokenMode
	if !rebuild {
		s.mu.Lock()
		if s.sessions[sess.id] != sess {
			s.mu.Unlock()
			return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{})
		}
		previousCollaboration, previousToolApproval := sess.collaboration, sess.toolApproval
		sess.collaboration, sess.toolApproval = collaboration, toolApproval
		ctrl := sess.ctrl
		profile, epoch := resolvedProfile(sess), sess.runtimeEpoch
		s.mu.Unlock()
		applyControllerProfile(ctrl, collaboration, toolApproval)
		if err := s.persistSessionRegistry(); err != nil {
			s.mu.Lock()
			var rollbackCtrl SessionController
			if s.sessions[sess.id] == sess && sess.collaboration == collaboration && sess.toolApproval == toolApproval {
				sess.collaboration, sess.toolApproval = previousCollaboration, previousToolApproval
				rollbackCtrl = sess.ctrl
			}
			s.mu.Unlock()
			if rollbackCtrl != nil {
				applyControllerProfile(rollbackCtrl, previousCollaboration, previousToolApproval)
			}
			return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
		}
		return protocol.SessionProfileSetResult{
			ResolvedProfile: profile, RuntimeEpoch: epoch,
			Disposition: protocol.ProfileUpdated, AutoResolvedPromptIDs: []protocol.PromptID{},
		}, nil
	}
	effortOverride := &effort
	newController, buildErr := s.buildController(ctx, model, effortOverride, sess.sink, tokenMode)
	if buildErr != nil || newController == nil {
		return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrInvalidProfile, protocol.ErrorOptions{Target: &p.Target})
	}
	newController.AdoptHistory(sess.ctrl.History(), sess.ctrl.SessionPath())
	applyControllerProfile(newController, collaboration, toolApproval)
	s.mu.Lock()
	if s.sessions[sess.id] != sess || sessionHasActiveWorkLocked(sess) {
		s.mu.Unlock()
		newController.Close()
		return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &p.Target})
	}
	old := sess.ctrl
	previousModel, previousEffort := sess.model, sess.effort
	previousCollaboration, previousTokenMode, previousToolApproval := sess.collaboration, sess.tokenMode, sess.toolApproval
	previousEpoch, previousUpdatedAt := sess.runtimeEpoch, sess.updatedAt
	sess.ctrl = newController
	sess.model = newController.ModelRef()
	sess.effort = effort
	sess.collaboration, sess.tokenMode, sess.toolApproval = collaboration, tokenMode, toolApproval
	sess.runtimeEpoch = protocol.RuntimeEpoch("runtime_" + randomHex(12))
	sess.updatedAt = time.Now().UnixMilli()
	profile, epoch := resolvedProfile(sess), sess.runtimeEpoch
	s.mu.Unlock()
	if err := s.persistSessionRegistry(); err != nil {
		s.mu.Lock()
		restored := s.sessions[sess.id] == sess && sess.ctrl == newController
		if restored {
			sess.ctrl = old
			sess.model, sess.effort = previousModel, previousEffort
			sess.collaboration, sess.tokenMode, sess.toolApproval = previousCollaboration, previousTokenMode, previousToolApproval
			sess.runtimeEpoch, sess.updatedAt = previousEpoch, previousUpdatedAt
		}
		s.mu.Unlock()
		newController.Close()
		return protocol.SessionProfileSetResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	old.Close()
	return protocol.SessionProfileSetResult{
		ResolvedProfile: profile, RuntimeEpoch: epoch,
		Disposition: protocol.ProfileRebuilt, AutoResolvedPromptIDs: []protocol.PromptID{},
	}, nil
}

func (s *Server) sessionContext(p protocol.SessionContextParams) (protocol.SessionContextResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionContextResult{}, err
	}
	return protocol.SessionContextResult{Context: sessionContextView(sess)}, nil
}

func sessionContextView(sess *session) protocol.ContextView {
	view := emptyContext()
	if controller, ok := sess.ctrl.(interface{ ContextSnapshot() (int, int) }); ok {
		view.UsedTokens, view.WindowTokens = controller.ContextSnapshot()
	}
	if controller, ok := sess.ctrl.(interface{ LastUsage() *provider.Usage }); ok {
		if usage := controller.LastUsage(); usage != nil {
			view.PromptTokens = usage.PromptTokens
			view.CompletionTokens = usage.CompletionTokens
			view.TotalTokens = usage.TotalTokens
			view.ReasoningTokens = usage.ReasoningTokens
			view.CacheHitTokens = usage.CacheHitTokens
			view.CacheMissTokens = usage.CacheMissTokens
		}
	}
	if controller, ok := sess.ctrl.(interface{ SessionCache() (int, int) }); ok {
		view.SessionCacheHitTokens, view.SessionCacheMissTokens = controller.SessionCache()
	}
	return view
}

func (s *Server) sessionBalance(ctx context.Context, p protocol.SessionBalanceParams) (protocol.SessionBalanceResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionBalanceResult{}, err
	}
	controller, ok := sess.ctrl.(interface {
		Balance(context.Context) (*billing.Balance, error)
	})
	if !ok {
		return protocol.SessionBalanceResult{Available: false}, nil
	}
	balance, err := controller.Balance(ctx)
	if err != nil {
		return protocol.SessionBalanceResult{}, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	if balance == nil {
		return protocol.SessionBalanceResult{Available: false}, nil
	}
	return protocol.SessionBalanceResult{Available: true, Display: balance.Display()}, nil
}

func (s *Server) jobList(p protocol.JobListParams) (protocol.JobListResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.JobListResult{}, err
	}
	return protocol.JobListResult{Jobs: sessionJobViews(sess), HasMore: false}, nil
}

func sessionJobViews(sess *session) []protocol.JobView {
	controller, ok := sess.ctrl.(interface{ Jobs() []jobs.View })
	if !ok {
		return []protocol.JobView{}
	}
	values := controller.Jobs()
	out := make([]protocol.JobView, 0, len(values))
	for _, job := range values {
		out = append(out, protocol.JobView{ID: protocol.JobID(job.ID), Kind: protocol.JobKind(job.Kind), Label: job.Label, Status: protocol.JobStatus(job.Status), StartedAt: job.StartedAt})
	}
	return out
}

func (s *Server) jobCancel(p protocol.JobCancelParams) (protocol.JobCancelResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.JobCancelResult{}, err
	}
	controller, ok := sess.ctrl.(interface{ CancelJob(string) bool })
	if !ok {
		return protocol.JobCancelResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	disposition := protocol.JobNotRunning
	if controller.CancelJob(string(p.JobID)) {
		disposition = protocol.JobCancelled
	}
	return protocol.JobCancelResult{Disposition: disposition}, nil
}

func (s *Server) cancel(p protocol.TurnCancelParams) (protocol.TurnCancelResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.TurnCancelResult{}, err
	}
	s.mu.Lock()
	current := sess.currentTurn
	s.mu.Unlock()
	if current == "" {
		return protocol.TurnCancelResult{}, protocol.MustRemoteError(protocol.ErrTurnNotActive, protocol.ErrorOptions{Target: &p.Target})
	}
	if p.ExpectedTurnID != current {
		return protocol.TurnCancelResult{}, protocol.MustRemoteError(protocol.ErrTurnMismatch, protocol.ErrorOptions{Target: &p.Target, Expected: string(p.ExpectedTurnID), Actual: string(current)})
	}
	sess.ctrl.Cancel()
	return protocol.TurnCancelResult{Status: protocol.CancelRequested, TurnID: current}, nil
}

func (s *Server) fileList(p protocol.FileListParams) (protocol.FileListResult, error) {
	if _, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch); err != nil {
		return protocol.FileListResult{}, err
	}
	entries, _, err := files.ListDir(s.opts.Workspace, p.Path)
	if err != nil {
		return protocol.FileListResult{}, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	out := make([]protocol.FileEntry, 0, len(entries))
	for _, entry := range entries {
		path := filepath.ToSlash(filepath.Join(p.Path, entry.Name()))
		out = append(out, protocol.FileEntry{Name: entry.Name(), Path: path, IsDir: entry.IsDir()})
	}
	return protocol.FileListResult{Entries: out, HasMore: false}, nil
}

func (s *Server) fileSearch(p protocol.FileSearchParams) (protocol.FileSearchResult, error) {
	if _, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch); err != nil {
		return protocol.FileSearchResult{}, err
	}
	limit := protocol.SearchDefaultItems
	if p.Limit != nil {
		limit = *p.Limit
	}
	results := fileref.Search(s.opts.Workspace, p.Query, limit+1)
	truncated := len(results) > limit
	if truncated {
		results = results[:limit]
	}
	entries := make([]protocol.FileEntry, 0, len(results))
	for _, result := range results {
		entries = append(entries, protocol.FileEntry{Name: filepath.Base(result.Path), Path: filepath.ToSlash(result.Path), IsDir: result.IsDir})
	}
	result := protocol.FileSearchResult{Entries: entries, Truncated: truncated, ReturnedItems: len(entries)}
	if truncated {
		result.TruncationReason = protocol.SearchResultLimit
	}
	return result, nil
}

func (s *Server) filePreview(p protocol.FilePreviewParams) (protocol.FilePreviewResult, error) {
	if _, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch); err != nil {
		return protocol.FilePreviewResult{}, err
	}
	full, err := files.ResolveRel(s.opts.Workspace, p.Path)
	if err != nil {
		return protocol.FilePreviewResult{}, protocol.MustRemoteError(protocol.ErrPathNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return protocol.FilePreviewResult{}, protocol.MustRemoteError(protocol.ErrNotFile, protocol.ErrorOptions{Target: &p.Target})
	}
	data, err := files.ReadFile(s.opts.Workspace, p.Path, protocol.PreviewBytes)
	if err != nil {
		return protocol.FilePreviewResult{}, protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	kind := previewKind(p.Path, data)
	if kind != protocol.FileText {
		return protocol.FilePreviewResult{
			Name: filepath.Base(p.Path), Path: p.Path, Kind: kind,
			SizeBytes: info.Size(), Binary: true,
		}, nil
	}
	// A preview can end in the middle of a UTF-8 rune. Trim only that partial
	// suffix; invalid data elsewhere is treated as binary metadata.
	for len(data) > 0 && !utf8.Valid(data) && info.Size() > int64(len(data)) && len(data) >= protocol.PreviewBytes-utf8.UTFMax {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return protocol.FilePreviewResult{
			Name: filepath.Base(p.Path), Path: p.Path, Kind: protocol.FileBinary,
			SizeBytes: info.Size(), Binary: true,
		}, nil
	}
	body := string(data)
	truncated := info.Size() > int64(len(data))
	result := protocol.FilePreviewResult{
		Name: filepath.Base(p.Path), Path: p.Path, Kind: protocol.FileText,
		SizeBytes: info.Size(), ReturnedBytes: int64(len(data)), Binary: false,
		Truncated: truncated, Body: &body,
	}
	if truncated {
		result.TruncationReason = protocol.ByteLimit
	}
	return result, nil
}

func previewKind(path string, data []byte) protocol.FileKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".svg":
		return protocol.FileImage
	case ".pdf":
		return protocol.FilePDF
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return protocol.FileBinary
	}
	return protocol.FileText
}

func (s *Server) sessionForQuery(host protocol.HostEpoch, target protocol.RuntimeTarget, epoch protocol.RuntimeEpoch) (*session, error) {
	if err := s.checkHostWorkspace(host, target.WorkspaceID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess := s.sessions[target.SessionID]
	if sess == nil {
		s.mu.Unlock()
		return nil, protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{})
	}
	actualEpoch := sess.runtimeEpoch
	s.mu.Unlock()
	if epoch != "" && actualEpoch != epoch {
		return nil, protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{Target: &target, Expected: string(epoch), Actual: string(actualEpoch)})
	}
	return sess, nil
}

func (s *Server) sessionForMutation(host protocol.HostEpoch, target protocol.RuntimeTarget, epoch protocol.RuntimeEpoch) (*session, error) {
	return s.sessionForQuery(host, target, epoch)
}

func (s *Server) checkHost(host protocol.HostEpoch) error {
	if host != s.hostEpoch {
		return protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{Expected: string(host), Actual: string(s.hostEpoch)})
	}
	return nil
}

func (s *Server) checkHostWorkspace(host protocol.HostEpoch, workspace protocol.WorkspaceID) error {
	if err := s.checkHost(host); err != nil {
		return err
	}
	if workspace != s.workspaceID {
		return protocol.MustRemoteError(protocol.ErrWorkspaceNotFound, protocol.ErrorOptions{})
	}
	return nil
}

func (s *Server) target(id protocol.SessionID) protocol.RuntimeTarget {
	return protocol.RuntimeTarget{WorkspaceID: s.workspaceID, SessionID: id}
}

func (s *Server) summaryLocked(sess *session) protocol.SessionSummary {
	preview := ""
	for _, message := range sess.ctrl.History() {
		if message.Role == provider.RoleUser && strings.TrimSpace(message.Content) != "" {
			preview = message.Content
		}
	}
	return protocol.SessionSummary{
		Target: s.target(sess.id), TopicID: sess.topicID, Title: sess.title,
		Preview: preview, Turns: sess.ctrl.Turn(), CreatedAtMs: sess.createdAt,
		LastActivityAtMs: sess.updatedAt, RecoveryInterrupted: false,
		Runtime: &protocol.SessionRuntimeSummary{RuntimeEpoch: sess.runtimeEpoch, Running: sess.ctrl.Running()},
	}
}

func resolvedProfile(sess *session) protocol.ResolvedProfile {
	return protocol.ResolvedProfile{
		Model: sess.model, Effort: sess.effort,
		CollaborationMode: sess.collaboration,
		TokenMode:         sess.tokenMode, ToolApprovalMode: sess.toolApproval,
	}
}

func applyControllerProfile(ctrl SessionController, collaboration protocol.CollaborationMode, approval protocol.ToolApprovalMode) {
	if controller, ok := ctrl.(interface{ SetPlanMode(bool) }); ok {
		controller.SetPlanMode(collaboration == protocol.CollaborationPlan)
	}
	if controller, ok := ctrl.(interface{ SetToolApprovalMode(string) }); ok {
		controller.SetToolApprovalMode(string(approval))
	}
}

func (s *Server) snapshotLocked(sess *session, pageTurns int) protocol.SessionSnapshot {
	snapshotID := protocol.SnapshotID("snapshot_" + randomHex(10))
	history := historyPage(sess, snapshotID, 0, pageTurns)
	lastError := (*string)(nil)
	if sess.lastError != "" {
		copy := sess.lastError
		lastError = &copy
	}
	var current *protocol.TurnState
	if sess.currentTurn != "" {
		current = &protocol.TurnState{TurnID: sess.currentTurn}
	}
	var currentOperation *protocol.OperationState
	if sess.currentOp != nil {
		copy := *sess.currentOp
		currentOperation = &copy
	}
	mirror, externalized := s.sessionMirrorArtifactLocked(sess)
	var goal *string
	var goalStatus protocol.GoalStatus
	if controller, ok := sess.ctrl.(goalController); ok {
		value, status := protocolGoal(controller)
		if value != "" {
			goal = &value
		}
		goalStatus = status
	}
	return protocol.SessionSnapshot{
		SnapshotID: snapshotID, HostEpoch: s.hostEpoch, Target: s.target(sess.id),
		RuntimeEpoch: sess.runtimeEpoch, BoundarySeq: 0,
		Meta: protocol.SessionMetaSnapshot{
			TopicID: sess.topicID, Title: sess.title, ResolvedProfile: resolvedProfile(sess),
			Goal: goal, GoalStatus: goalStatus, Capabilities: protocol.FrozenCapabilities(false, false),
		},
		Runtime: protocol.SessionRuntimeState{
			Running: sess.ctrl.Running() || currentOperation != nil, CurrentTurn: current, CurrentOperation: currentOperation,
			CancelRequested: false, LastOutcome: sess.lastOutcome,
			LastError: lastError, LiveEvents: append([]eventwire.Event{}, sess.liveEvents...),
		},
		History: history, Todos: sessionTodoViews(sess.ctrl),
		PendingPrompt: clonePendingPrompt(sess.pendingPrompt),
		Context:       sessionContextView(sess), Jobs: sessionJobViews(sess),
		Checkpoints: sessionCheckpointViews(s.opts.Workspace, sess.ctrl), Mirror: mirror, Externalized: externalized,
	}
}

func (s *Server) sessionMirrorArtifactLocked(sess *session) (protocol.SessionMirrorSnapshot, []protocol.ExternalizedField) {
	path := strings.TrimSpace(sess.ctrl.SessionPath())
	if path == "" {
		return protocol.SessionMirrorSnapshot{}, []protocol.ExternalizedField{}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > protocol.ContentRefObjectBytes {
		return protocol.SessionMirrorSnapshot{}, []protocol.ExternalizedField{}
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	ref := protocol.ContentRef("content_" + randomHex(12))
	s.contents[ref] = contentObject{data: append([]byte(nil), data...), sha256: digest, createdAt: time.Now()}
	if len(s.contents) > 64 {
		var oldestRef protocol.ContentRef
		var oldest time.Time
		for candidate, object := range s.contents {
			if candidate == ref {
				continue
			}
			if oldestRef == "" || object.createdAt.Before(oldest) {
				oldestRef, oldest = candidate, object.createdAt
			}
		}
		delete(s.contents, oldestRef)
	}
	body := string(data)
	return protocol.SessionMirrorSnapshot{SessionJSONL: &body}, []protocol.ExternalizedField{{
		JSONPointer: "/mirror/session.jsonl", ContentRef: ref, TotalBytes: int64(len(data)),
		SHA256: digest,
	}}
}

func emptyContext() protocol.ContextView {
	return protocol.ContextView{Sources: []protocol.UsageSourceView{}, ReadFiles: []protocol.ReadFileRecord{}}
}

func approvalPendingPrompt(approval event.Approval) *protocol.PendingPrompt {
	return &protocol.PendingPrompt{Kind: protocol.PromptApproval, Approval: &protocol.ApprovalPrompt{
		PromptID: protocol.PromptID(approval.ID), Tool: approval.Tool, Subject: approval.Subject,
		Reason: stringPtrOrNil(approval.Reason), Fresh: approval.Fresh,
		AllowedDecisions: []protocol.PromptDecision{
			protocol.DecisionAllowOnce, protocol.DecisionAllowSession,
			protocol.DecisionAllowPersistent, protocol.DecisionDeny,
		},
	}}
}

func askPendingPrompt(ask event.Ask) *protocol.PendingPrompt {
	questions := make([]protocol.AskQuestion, 0, len(ask.Questions))
	for _, question := range ask.Questions {
		options := make([]protocol.AskOption, 0, len(question.Options))
		for _, option := range question.Options {
			options = append(options, protocol.AskOption{Label: option.Label, Description: stringPtrOrNil(option.Description)})
		}
		questions = append(questions, protocol.AskQuestion{
			QuestionID: protocol.QuestionID(question.ID), Header: question.Header,
			Prompt: stringPtrOrNil(question.Prompt), Options: options, Multi: question.Multi,
		})
	}
	return &protocol.PendingPrompt{Kind: protocol.PromptAsk, Ask: &protocol.AskPrompt{PromptID: protocol.PromptID(ask.ID), Questions: questions}}
}

func clonePendingPrompt(value *protocol.PendingPrompt) *protocol.PendingPrompt {
	if value == nil {
		return nil
	}
	copy := *value
	if value.Approval != nil {
		approval := *value.Approval
		approval.AllowedDecisions = append([]protocol.PromptDecision(nil), value.Approval.AllowedDecisions...)
		copy.Approval = &approval
	}
	if value.Ask != nil {
		ask := *value.Ask
		ask.Questions = append([]protocol.AskQuestion(nil), value.Ask.Questions...)
		copy.Ask = &ask
	}
	return &copy
}

func (sink *sessionSink) Emit(e event.Event) {
	if sink == nil || sink.server == nil {
		return
	}
	s := sink.server
	wired := eventwire.ToWire(e)
	s.mu.Lock()
	sess := s.sessions[sink.sessionID]
	if sess == nil {
		s.mu.Unlock()
		return
	}
	turnID := sess.currentTurn
	if e.Kind == event.TurnStarted {
		sess.liveEvents = nil
	}
	sess.liveEvents = append(sess.liveEvents, wired)
	if len(sess.liveEvents) > 256 {
		sess.liveEvents = append([]eventwire.Event(nil), sess.liveEvents[len(sess.liveEvents)-256:]...)
	}
	switch e.Kind {
	case event.ApprovalRequest:
		sess.pendingPrompt = approvalPendingPrompt(e.Approval)
	case event.AskRequest:
		sess.pendingPrompt = askPendingPrompt(e.Ask)
	}
	if e.Kind == event.TurnDone {
		sess.currentTurn = ""
		// Approval and ask prompts belong to the completed turn. Keeping one here
		// makes a reconnect or snapshot refresh replay a decision that can no
		// longer be answered safely.
		sess.pendingPrompt = nil
		if sess.currentOp != nil {
			sess.currentOp = nil
			if sess.operationStop != nil {
				sess.operationStop()
				sess.operationStop = nil
			}
		}
		sess.updatedAt = time.Now().UnixMilli()
		if e.Cancelled {
			sess.lastOutcome = protocol.OutcomeCancelled
			sess.lastError = ""
		} else if e.Err != nil {
			sess.lastOutcome = protocol.OutcomeFailed
			sess.lastError = e.Err.Error()
		} else {
			sess.lastOutcome = protocol.OutcomeCompleted
			sess.lastError = ""
		}
		sess.liveEvents = nil
	}
	persistRegistry := e.Kind == event.TurnDone
	ready := make([]struct {
		conn  *rpcwire.Conn
		event protocol.SessionEvent
	}, 0)
	for _, sub := range s.subs {
		if sub.sessionID != sink.sessionID {
			continue
		}
		sub.seq++
		envelope := protocol.SessionEvent{
			SubscriptionID: sub.id, HostEpoch: s.hostEpoch, Target: s.target(sess.id),
			RuntimeEpoch: sess.runtimeEpoch, Seq: sub.seq, TurnID: turnID,
			Event: wired, Externalized: []protocol.ExternalizedField{},
		}
		if !sub.active {
			sub.pending = append(sub.pending, envelope)
			continue
		}
		ready = append(ready, struct {
			conn  *rpcwire.Conn
			event protocol.SessionEvent
		}{sub.conn, envelope})
	}
	s.mu.Unlock()
	if persistRegistry {
		go func() {
			s.requestMu.Lock()
			defer s.requestMu.Unlock()
			if err := s.persistSessionRegistry(); err != nil {
				s.logRegistryError("persist completed turn", err)
			}
		}()
	}
	for _, notification := range ready {
		_ = notification.conn.Notify(string(protocol.MethodSessionEvent), notification.event)
	}
}

func (s *Server) graceLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			exit := !s.attached && !s.lastDetach.IsZero() && time.Since(s.lastDetach) >= GracePeriod && !s.hasBusyLocked()
			ln := s.ln
			s.mu.Unlock()
			if exit {
				s.snapshotAndClose()
				if ln != nil {
					_ = ln.Close()
				}
				return
			}
		}
	}
}

func (s *Server) hasBusyLocked() bool {
	for _, sess := range s.sessions {
		if sess.currentOp != nil || (sess.ctrl != nil && sess.ctrl.Running()) {
			return true
		}
	}
	return false
}

func (s *Server) snapshotAndClose() {
	s.shutdownOnce.Do(func() {
		s.requestMu.Lock()
		defer s.requestMu.Unlock()
		if err := s.persistSessionRegistry(); err != nil {
			s.logRegistryError("persist before shutdown", err)
		}
		s.mu.Lock()
		sessions := make([]*session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			if sess != nil {
				sessions = append(sessions, sess)
			}
		}
		s.sessions = make(map[protocol.SessionID]*session)
		s.subs = make(map[protocol.SubscriptionID]*subscription)
		s.mu.Unlock()
		for _, sess := range sessions {
			closeRuntimeSession(sess)
		}
	})
}

func (s *Server) Close() {
	s.mu.Lock()
	ln, conn := s.ln, s.activeConn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
	s.snapshotAndClose()
}

func (s *Server) ForceDetachForTest() {
	s.mu.Lock()
	s.attached = false
	s.lastDetach = time.Now()
	s.mu.Unlock()
}

func (s *Server) Attached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
}

func leaseID(gen uint64) protocol.LeaseID {
	return protocol.LeaseID("lease_" + strings.TrimSpace(time.Unix(0, int64(gen)).Format("150405.000000000")))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func splitModelRef(ref string) (string, string) {
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ref
}

func currentBuildID(opts Options) protocol.BuildID {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}
	if revision := strings.TrimSpace(opts.SourceRevision); revision != "" {
		if id, err := protocol.NewBuildID(version, revision); err == nil {
			return id
		}
	}
	return protocol.CurrentBuildID(version)
}
