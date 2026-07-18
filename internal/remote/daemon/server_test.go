package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/skill"
)

const testRequestTimeout = 3 * time.Second

type daemonTestIDs struct{ next atomic.Uint64 }

func (s *daemonTestIDs) value(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, s.next.Add(1))
}

func (s *daemonTestIDs) leaseID() (protocol.LeaseID, error) {
	return protocol.LeaseID(s.value("lease")), nil
}

func (s *daemonTestIDs) runtimeEpoch() (protocol.RuntimeEpoch, error) {
	return protocol.RuntimeEpoch(s.value("runtime")), nil
}

func (s *daemonTestIDs) turnID() (protocol.TurnID, error) {
	return protocol.TurnID(s.value("turn")), nil
}

func (s *daemonTestIDs) subscriptionID() (protocol.SubscriptionID, error) {
	return protocol.SubscriptionID(s.value("subscription")), nil
}

func (s *daemonTestIDs) snapshotID() (protocol.SnapshotID, error) {
	return protocol.SnapshotID(s.value("snapshot")), nil
}

type daemonTestClock struct {
	mu  sync.Mutex
	now time.Time
}

type daemonProfileResolver struct{}

func (daemonProfileResolver) ResolveProfile(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
	return protocol.ResolvedProfile{
		Model: "test/test-model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}, nil
}

func (daemonProfileResolver) WorkspaceCatalog(context.Context, string) (protocol.WorkspaceCatalogResult, error) {
	return protocol.WorkspaceCatalogResult{
		Revision: "test-config-1",
		Models: []protocol.ModelCatalogItem{{
			Ref: "test/test-model", Provider: "test", Model: "test-model",
			Effort: protocol.EffortCatalog{Supported: true, Default: "medium", Levels: []string{"auto", "medium"}},
		}},
		CollaborationModes: []protocol.CollaborationMode{protocol.CollaborationNormal, protocol.CollaborationPlan, protocol.CollaborationGoal},
		TokenModes:         []protocol.TokenMode{protocol.TokenFull, protocol.TokenEconomy, protocol.TokenDelivery},
		ToolApprovalModes:  []protocol.ToolApprovalMode{protocol.ToolApprovalAsk, protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO},
		DefaultProfile: protocol.ResolvedProfile{
			Model: "test/test-model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
			TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
		},
	}, nil
}

func (c *daemonTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *daemonTestClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

type daemonFakeFactory struct {
	mu          sync.Mutex
	controllers []*daemonFakeController
	goal        string
}

func (f *daemonFakeFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	controller := newDaemonFakeController(ctx, sink)
	f.mu.Lock()
	controller.goal = f.goal
	f.controllers = append(f.controllers, controller)
	f.mu.Unlock()
	return controller, nil
}

func (f *daemonFakeFactory) setGoal(goal string) {
	f.mu.Lock()
	f.goal = goal
	f.mu.Unlock()
}

func (f *daemonFakeFactory) controller(t *testing.T, index int) *daemonFakeController {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if index >= len(f.controllers) {
		t.Fatalf("controller %d was not constructed", index)
	}
	return f.controllers[index]
}

func (f *daemonFakeFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.controllers)
}

type daemonFailFirstFactory struct {
	delegate *daemonFakeFactory
	mu       sync.Mutex
	calls    int
	failure  error
}

func (f *daemonFailFirstFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if call == 1 {
		return nil, f.failure
	}
	return f.delegate.CreateController(ctx, target, sink)
}

func (f *daemonFailFirstFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type daemonFakeController struct {
	control.SessionAPI

	ctx  context.Context
	sink event.Sink

	mu              sync.Mutex
	running         bool
	cancelRequested bool
	closeCalls      int
	snapshotCalls   int
	snapshotErr     error
	tryCancelCalls  int
	trySteerCalls   int
	strictSubmits   int
	steerAccepted   bool
	steers          []string
	approvals       []daemonApprovalCall
	answers         []daemonAnswerCall
	release         chan struct{}
	releaseOnce     sync.Once
	started         chan string
	finished        chan struct{}
	finishOnce      sync.Once
	richEvent       event.Event
	goal            string
	operationCore   *control.Controller
}

type daemonApprovalCall struct {
	ID                      string
	Allow, Session, Persist bool
}

type daemonAnswerCall struct {
	ID      string
	Answers []event.AskAnswer
}

func newDaemonFakeController(ctx context.Context, sink event.Sink) *daemonFakeController {
	controller := &daemonFakeController{
		ctx: ctx, sink: sink, release: make(chan struct{}), started: make(chan string, 1),
		finished: make(chan struct{}), steerAccepted: true,
		richEvent: event.Event{Kind: event.ToolResult, Tool: event.Tool{
			ID: "tool-remote", Name: "bash", Args: `{"command":"printf remote"}`,
			Output: "remote output", FileDiff: event.FileDiff{Diff: "@@ -1 +1 @@", Added: 1, Removed: 1},
			Profile: &event.Profile{Model: "test-subagent", Effort: "high"},
		}},
	}
	controller.operationCore = control.New(control.Options{Sink: sink})
	return controller
}

func (c *daemonFakeController) WorkspaceRoot() string { return "" }
func (c *daemonFakeController) SessionPath() string   { return "" }
func (c *daemonFakeController) SessionDir() string    { return "" }
func (c *daemonFakeController) Turn() int             { return 0 }
func (c *daemonFakeController) History() []provider.Message {
	return []provider.Message{}
}
func (c *daemonFakeController) Todos() []evidence.TodoItem  { return []evidence.TodoItem{} }
func (c *daemonFakeController) ContextSnapshot() (int, int) { return 0, 0 }
func (c *daemonFakeController) LastUsage() *provider.Usage  { return nil }
func (c *daemonFakeController) Jobs() []jobs.View           { return []jobs.View{} }
func (c *daemonFakeController) Goal() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.goal
}
func (c *daemonFakeController) GoalStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.goal == "" {
		return ""
	}
	return string(protocol.GoalRunning)
}
func (c *daemonFakeController) CheckpointSnapshot() control.CheckpointSnapshot {
	return control.CheckpointSnapshot{
		Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
	}
}
func (c *daemonFakeController) Commands() []command.Command { return []command.Command{} }
func (c *daemonFakeController) SlashSkills() []skill.Skill  { return []skill.Skill{} }
func (c *daemonFakeController) Host() *plugin.Host          { return nil }

func (c *daemonFakeController) SubmitDisplay(display, input string) {
	if strings.HasPrefix(strings.TrimSpace(input), "/") {
		c.sink.Emit(event.Event{Kind: event.Notice, Text: "unknown or read-only command"})
		return
	}
	c.SubmitUserTurn(input, display)
}

func (c *daemonFakeController) SubmitEditedDisplay(display, input, _ string) {
	c.SubmitUserTurn(input, display)
}

func (c *daemonFakeController) SubmitDeliveryRecovery(display, input string) {
	c.SubmitUserTurn(input, display)
}

func (c *daemonFakeController) SubmitInvocationDisplay(display, input string, _ []control.InvocationRequest) {
	c.SubmitUserTurn(input, display)
}

func (c *daemonFakeController) StartOperation(spec control.OperationSpec) (*control.OperationHandle, error) {
	return c.operationCore.StartOperation(spec)
}

func (c *daemonFakeController) SubmitUserTurn(input, _ string) {
	c.mu.Lock()
	c.strictSubmits++
	c.running = true
	c.cancelRequested = false
	c.mu.Unlock()
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	select {
	case c.started <- input:
	default:
	}
	go func() {
		select {
		case <-c.release:
			c.sink.Emit(c.richEvent)
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			c.sink.Emit(event.Event{Kind: event.TurnDone})
		case <-c.ctx.Done():
		}
		c.finishOnce.Do(func() { close(c.finished) })
	}()
}

func (c *daemonFakeController) TryCancel() control.CancelAttempt {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tryCancelCalls++
	if !c.running {
		return control.CancelNotActive
	}
	if c.cancelRequested {
		return control.CancelAlreadyRequested
	}
	c.cancelRequested = true
	return control.CancelRequestedNow
}

func (c *daemonFakeController) TrySteer(text string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trySteerCalls++
	if !c.running || !c.steerAccepted {
		return false
	}
	c.steers = append(c.steers, text)
	return true
}

func (c *daemonFakeController) Approve(id string, allow, session, persist bool) {
	c.mu.Lock()
	c.approvals = append(c.approvals, daemonApprovalCall{ID: id, Allow: allow, Session: session, Persist: persist})
	c.mu.Unlock()
}

func (c *daemonFakeController) AnswerQuestion(id string, answers []event.AskAnswer) {
	cloned := make([]event.AskAnswer, len(answers))
	for index, answer := range answers {
		cloned[index] = event.AskAnswer{QuestionID: answer.QuestionID, Selected: append([]string(nil), answer.Selected...)}
	}
	c.mu.Lock()
	c.answers = append(c.answers, daemonAnswerCall{ID: id, Answers: cloned})
	c.mu.Unlock()
}

func (c *daemonFakeController) Close() {
	c.mu.Lock()
	c.closeCalls++
	c.running = false
	core := c.operationCore
	c.mu.Unlock()
	if core != nil {
		core.Close()
	}
}

func (c *daemonFakeController) Snapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshotCalls++
	return c.snapshotErr
}

func (c *daemonFakeController) releaseTurn() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func (c *daemonFakeController) emit(value event.Event) { c.sink.Emit(value) }

func (c *daemonFakeController) counts() (closeCalls, tryCancelCalls, strictSubmits int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalls, c.tryCancelCalls, c.strictSubmits
}

func (c *daemonFakeController) promptMutationCalls() (int, []string, []daemonApprovalCall, []daemonAnswerCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	steers := append([]string(nil), c.steers...)
	approvals := append([]daemonApprovalCall(nil), c.approvals...)
	answers := make([]daemonAnswerCall, len(c.answers))
	for index, answer := range c.answers {
		answers[index].ID = answer.ID
		answers[index].Answers = make([]event.AskAnswer, len(answer.Answers))
		for answerIndex, value := range answer.Answers {
			answers[index].Answers[answerIndex] = event.AskAnswer{
				QuestionID: value.QuestionID, Selected: append([]string(nil), value.Selected...),
			}
		}
	}
	return c.trySteerCalls, steers, approvals, answers
}

func daemonTestTarget() protocol.RuntimeTarget {
	return protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"}
}

func daemonTestBuildID(t *testing.T, revisionByte byte) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v-test", strings.Repeat(string(revisionByte), 40))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newDaemonTestServer(t *testing.T) (*Server, *daemonFakeFactory, protocol.BuildID) {
	t.Helper()
	return newDaemonTestServerWithNow(t, nil)
}

func newDaemonTestServerWithNow(t *testing.T, now func() time.Time) (*Server, *daemonFakeFactory, protocol.BuildID) {
	t.Helper()
	options, factory, buildID := daemonTestServerOptions(t, now)
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatalf("New daemon Server: %v", err)
	}
	t.Cleanup(server.Close)
	return server, factory, buildID
}

func daemonTestServerOptions(t *testing.T, now func() time.Time) (Options, *daemonFakeFactory, protocol.BuildID) {
	options, factory, buildID, _ := daemonTestServerOptionsWithCatalogState(t, now)
	return options, factory, buildID
}

func daemonTestServerOptionsWithCatalogState(t *testing.T, now func() time.Time) (Options, *daemonFakeFactory, protocol.BuildID, string) {
	t.Helper()
	ids := &daemonTestIDs{}
	factory := &daemonFakeFactory{}
	buildID := daemonTestBuildID(t, 'a')
	userHome := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "catalog")
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: stateDir, UserHome: userHome,
		SessionDir:      func(string) string { return filepath.Join(userHome, ".sessions") },
		ProfileResolver: daemonProfileResolver{},
	})
	if err != nil {
		t.Fatalf("New daemon test Catalog: %v", err)
	}
	options := Options{
		BuildID: buildID, HostEpoch: "host-test",
		HostInfo:          protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "process"},
		Capabilities:      protocol.FrozenCapabilities(false, false),
		Catalog:           catalogValue,
		ControllerFactory: factory,
		Metadata: func(_ context.Context, _ protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
			return protocol.SessionMetaSnapshot{
				TopicID: "topic-test", Title: "Remote Test Session",
				ResolvedProfile: protocol.ResolvedProfile{
					Model: "test-model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
					TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
				},
			}, nil
		},
		LeaseOptions:      host.LeaseManagerOptions{Now: now, NewLeaseID: ids.leaseID},
		ContentRefOptions: contentref.Config{Now: now},
		RuntimeOptions: host.RuntimeManagerOptions{
			NewRuntimeEpoch: ids.runtimeEpoch, NewTurnID: ids.turnID, NewSubscriptionID: ids.subscriptionID,
			SubscriptionQueue: 16, EventLogLimit: 64,
		},
		NewSnapshotID:                 ids.snapshotID,
		allowUncataloguedTestRuntimes: true,
	}
	return options, factory, buildID, stateDir
}

type daemonPeer struct {
	raw        net.Conn
	wire       *rpcwire.Conn
	cancel     context.CancelFunc
	serverDone chan error
	clientDone chan error
	closeOnce  sync.Once
}

func openDaemonPeer(t *testing.T, server *Server, wrapServerConn func(net.Conn) net.Conn, configure func(*rpcwire.Conn)) *daemonPeer {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	if wrapServerConn != nil {
		serverConn = wrapServerConn(serverConn)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeConn(serverConn) }()

	wire := rpcwire.NewConn(clientConn, clientConn, rpcwire.Options{
		Name: "remote-test-client", MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
		StrictJSONRPC: true,
	})
	if configure != nil {
		configure(wire)
	}
	ctx, cancel := context.WithCancel(context.Background())
	clientDone := make(chan error, 1)
	go func() { clientDone <- wire.Serve(ctx) }()
	peer := &daemonPeer{raw: clientConn, wire: wire, cancel: cancel, serverDone: serverDone, clientDone: clientDone}
	t.Cleanup(func() { peer.close(t) })
	return peer
}

func (p *daemonPeer) close(t *testing.T) {
	t.Helper()
	p.closeOnce.Do(func() {
		p.cancel()
		_ = p.raw.Close()
		waitDone(t, p.serverDone, "daemon transport")
		waitDone(t, p.clientDone, "client transport")
	})
}

func waitDone(t *testing.T, done <-chan error, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(testRequestTimeout):
		t.Fatalf("timed out waiting for %s shutdown", label)
	}
}

func requestResult[T any](t *testing.T, peer *daemonPeer, method protocol.Method, params any) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	defer cancel()
	raw, err := peer.wire.Request(ctx, string(method), params)
	if err != nil {
		t.Fatalf("%s request: %v", method, err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode %s result: %v\n%s", method, err, raw)
	}
	return result
}

func requestError(t *testing.T, peer *daemonPeer, method protocol.Method, params any) *rpcwire.ResponseError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	defer cancel()
	_, err := peer.wire.Request(ctx, string(method), params)
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded", method)
	}
	var response *rpcwire.ResponseError
	if !errors.As(err, &response) {
		t.Fatalf("%s error type = %T, want *rpcwire.ResponseError: %v", method, err, err)
	}
	return response
}

func requireRemoteError(t *testing.T, response *rpcwire.ResponseError, want protocol.ReasonixErrorCode) protocol.RemoteErrorData {
	t.Helper()
	if response.Code != protocol.DomainErrorCode {
		t.Fatalf("JSON-RPC code = %d, want %d (%s)", response.Code, protocol.DomainErrorCode, want)
	}
	var data protocol.RemoteErrorData
	if err := json.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("decode remote error data: %v\n%s", err, response.Data)
	}
	if data.ReasonixCode != want {
		t.Fatalf("reasonixCode = %q, want %q; data=%+v", data.ReasonixCode, want, data)
	}
	if err := data.Validate(); err != nil {
		t.Fatalf("invalid remote error data: %v", err)
	}
	return data
}

func initializePeer(t *testing.T, peer *daemonPeer, buildID protocol.BuildID, client protocol.ClientInstanceID, resume protocol.LeaseID) protocol.InitializeResult {
	t.Helper()
	return requestResult[protocol.InitializeResult](t, peer, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: client, ResumeLeaseID: resume,
	})
}

func subscribePeer(t *testing.T, peer *daemonPeer, target protocol.RuntimeTarget) protocol.SessionSubscribeResult {
	t.Helper()
	return requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
	})
}

func browseWorkspacePeer(t *testing.T, peer *daemonPeer, path string) protocol.WorkspaceBrowseResult {
	t.Helper()
	return requestResult[protocol.WorkspaceBrowseResult](t, peer, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: "host-test", TypedPath: path,
	})
}

func openWorkspacePeer(t *testing.T, peer *daemonPeer, requestID protocol.RequestID, ref protocol.DirectoryRef) protocol.WorkspaceOpenResult {
	t.Helper()
	return requestResult[protocol.WorkspaceOpenResult](t, peer, protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
		HostMutation: protocol.HostMutation{RequestID: requestID, ExpectedHostEpoch: "host-test"}, PrimaryDirectoryRef: ref,
	})
}

func createSessionPeer(t *testing.T, peer *daemonPeer, requestID protocol.RequestID, workspaceID protocol.WorkspaceID) protocol.SessionCreateResult {
	t.Helper()
	return requestResult[protocol.SessionCreateResult](t, peer, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: requestID, ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID,
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Topic: protocol.TopicSelection{Kind: protocol.TopicNew},
		Profile: protocol.ProfileSelection{},
	})
}

func readRemoteContent(t *testing.T, peer *daemonPeer, descriptor protocol.ExternalizedField) []byte {
	t.Helper()
	var out []byte
	for offset := int64(0); ; {
		result := requestResult[protocol.SessionContentResult](t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{
			ContentRef: descriptor.ContentRef, Offset: offset,
		})
		if err := result.Validate(); err != nil {
			t.Fatalf("invalid session/content result: %v", err)
		}
		if result.ContentRef != descriptor.ContentRef || result.Offset != offset ||
			result.TotalBytes != descriptor.TotalBytes || result.SHA256 != descriptor.SHA256 ||
			result.Encoding != protocol.ContentUTF8 {
			t.Fatalf("session/content metadata drifted: result=%+v descriptor=%+v", result, descriptor)
		}
		chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk) > protocol.ContentRefChunkBytes {
			t.Fatalf("content chunk bytes = %d", len(chunk))
		}
		out = append(out, chunk...)
		if result.NextOffset == nil {
			break
		}
		offset = *result.NextOffset
	}
	if int64(len(out)) != descriptor.TotalBytes {
		t.Fatalf("retrieved bytes = %d, want %d", len(out), descriptor.TotalBytes)
	}
	digest := sha256.Sum256(out)
	if got := hex.EncodeToString(digest[:]); got != descriptor.SHA256 {
		t.Fatalf("retrieved SHA-256 = %s, want %s", got, descriptor.SHA256)
	}
	return out
}

func mutation(request protocol.RequestID, target protocol.RuntimeTarget, runtime protocol.RuntimeEpoch) protocol.SessionMutation {
	return protocol.SessionMutation{
		RequestID: request, ExpectedHostEpoch: "host-test", Target: target, ExpectedRuntimeEpoch: runtime,
	}
}

func TestInitializeMustBeFirstAndHandshakeFailureIsTerminal(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)

	first := requestError(t, peer, protocol.MethodRemotePing, protocol.PingParams{LeaseID: "lease-before-init"})
	if first.Code != rpcwire.ErrInvalidRequest {
		t.Fatalf("pre-initialize ping code = %d, want %d", first.Code, rpcwire.ErrInvalidRequest)
	}
	second := requestError(t, peer, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: "client-test",
	})
	if second.Code != rpcwire.ErrInvalidRequest {
		t.Fatalf("late initialize code = %d, want %d", second.Code, rpcwire.ErrInvalidRequest)
	}
	if server.leases.Held() {
		t.Fatal("failed initialize ordering acquired a lease")
	}
}

func TestInitializeRejectsDaemonBuildMismatchBeforeLease(t *testing.T) {
	server, _, _ := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	attachBuild := daemonTestBuildID(t, 'b')

	response := requestError(t, peer, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: attachBuild, ClientInstanceID: "client-test",
	})
	data := requireRemoteError(t, response, protocol.ErrDaemonRestartRequired)
	if data.Expected != strings.Repeat("a", 40) || data.Actual != strings.Repeat("b", 40) {
		t.Fatalf("build mismatch orientation = expected %q actual %q", data.Expected, data.Actual)
	}
	if server.leases.Held() {
		t.Fatal("build mismatch acquired a lease")
	}
}

func TestServerRejectsNonFrozenLeaseTiming(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*host.LeaseManagerOptions)
	}{
		{"ttl", func(options *host.LeaseManagerOptions) { options.TTL = time.Second }},
		{"ping", func(options *host.LeaseManagerOptions) { options.PingInterval = time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options, _, _ := daemonTestServerOptions(t, nil)
			test.configure(&options.LeaseOptions)
			server, err := New(context.Background(), options)
			if server != nil {
				server.Close()
			}
			if err == nil {
				t.Fatal("non-frozen lease timing was accepted")
			}
		})
	}
}

func TestSingleClientLeaseResumeStalesOldTransport(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	first := openDaemonPeer(t, server, nil, nil)
	grant := initializePeer(t, first, buildID, "client-a", "")

	busy := openDaemonPeer(t, server, nil, nil)
	busyResponse := requestError(t, busy, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: "client-b",
	})
	busyData := requireRemoteError(t, busyResponse, protocol.ErrHostBusy)
	if busyData.RetryAfterMs == nil || *busyData.RetryAfterMs <= 0 {
		t.Fatalf("HOST_BUSY retryAfterMs = %v", busyData.RetryAfterMs)
	}

	resumed := openDaemonPeer(t, server, nil, nil)
	resumedGrant := initializePeer(t, resumed, buildID, "client-a", grant.Lease.LeaseID)
	if resumedGrant.Lease.LeaseID != grant.Lease.LeaseID {
		t.Fatalf("resume lease = %q, want %q", resumedGrant.Lease.LeaseID, grant.Lease.LeaseID)
	}
	stale := requestError(t, first, protocol.MethodRemotePing, protocol.PingParams{LeaseID: grant.Lease.LeaseID})
	requireRemoteError(t, stale, protocol.ErrStaleConnection)

	ping := requestResult[protocol.PingResult](t, resumed, protocol.MethodRemotePing, protocol.PingParams{LeaseID: grant.Lease.LeaseID})
	if ping.HostEpoch != "host-test" || ping.LeaseTTL != protocol.LeaseTTLMillis {
		t.Fatalf("resumed ping = %+v", ping)
	}
}

func TestCapabilitiesValidateHostEpochAndUnsubscribeIsIdempotent(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-test", "")

	capabilities := requestResult[protocol.HostCapabilitiesResult](t, peer, protocol.MethodHostCapabilities, protocol.HostCapabilitiesParams{
		ExpectedHostEpoch: "host-test",
	})
	if capabilities.HostEpoch != "host-test" || capabilities.Capabilities != protocol.FrozenCapabilities(false, false) {
		t.Fatalf("host capabilities = %+v", capabilities)
	}
	stale := requestError(t, peer, protocol.MethodHostCapabilities, protocol.HostCapabilitiesParams{
		ExpectedHostEpoch: "host-old",
	})
	staleData := requireRemoteError(t, stale, protocol.ErrStaleHostEpoch)
	if staleData.Expected != "host-test" || staleData.Actual != "host-old" {
		t.Fatalf("stale host orientation = expected %q actual %q", staleData.Expected, staleData.Actual)
	}

	subscription := subscribePeer(t, peer, daemonTestTarget())
	for attempt := 0; attempt < 2; attempt++ {
		result := requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{
			SubscriptionID: subscription.SubscriptionID,
		})
		if !result.Unsubscribed {
			t.Fatalf("unsubscribe attempt %d returned false", attempt+1)
		}
	}
}

func TestCatalogWireBrowseOpenCreateListAndSubscribe(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-catalog", "")

	workspacePath := t.TempDir()
	browse := browseWorkspacePeer(t, peer, workspacePath)
	if browse.Directory.DirectoryRef == "" || browse.Directory.DisplayPath != workspacePath || browse.Entries == nil {
		t.Fatalf("workspace/browse = %+v", browse)
	}
	opened := openWorkspacePeer(t, peer, "request-open-catalog", browse.Directory.DirectoryRef)
	if opened.Disposition != protocol.WorkspaceOpened || opened.Workspace.WorkspaceID == "" {
		t.Fatalf("workspace/open = %+v", opened)
	}
	workspaces := requestResult[protocol.WorkspaceListResult](t, peer, protocol.MethodWorkspaceList, protocol.WorkspaceListParams{
		ExpectedHostEpoch: "host-test",
	})
	if len(workspaces.Items) != 1 || workspaces.Items[0] != opened.Workspace || workspaces.HasMore || workspaces.NextCursor != "" {
		t.Fatalf("workspace/list = %+v", workspaces)
	}

	created := createSessionPeer(t, peer, "request-create-catalog", opened.Workspace.WorkspaceID)
	if created.Target.WorkspaceID != opened.Workspace.WorkspaceID || created.Target.SessionID == "" || created.RuntimeEpoch == "" ||
		created.TopicID == "" || created.TopicTitle == "" {
		t.Fatalf("session/create = %+v", created)
	}
	if factory.count() != 1 {
		t.Fatalf("session/create constructed %d controllers, want 1", factory.count())
	}
	sessions := requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 || sessions.Items[0].Target != created.Target || sessions.Items == nil || sessions.HasMore {
		t.Fatalf("session/list = %+v", sessions)
	}
	subscription := subscribePeer(t, peer, created.Target)
	if subscription.Snapshot.Target != created.Target || subscription.Snapshot.RuntimeEpoch != created.RuntimeEpoch {
		t.Fatalf("session/subscribe = %+v", subscription)
	}
	if factory.count() != 1 {
		t.Fatalf("subscribe rebuilt a create-started runtime; controllers = %d", factory.count())
	}
}

func TestCatalogHandlersValidateLeaseBeforeEpochRegistryAndCatalog(t *testing.T) {
	clock := &daemonTestClock{now: time.Unix(12_000, 0)}
	server, _, buildID := newDaemonTestServerWithNow(t, clock.Now)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-catalog-lease", "")
	clock.Advance(time.Duration(protocol.LeaseTTLMillis) * time.Millisecond)

	// A stale epoch and inaccessible path would both fail later checks, but an
	// expired current-client lease must win for every catalog wire handler.
	tests := []struct {
		method protocol.Method
		params any
	}{
		{protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
			ExpectedHostEpoch: "host-stale", TypedPath: filepath.Join(t.TempDir(), "missing-secret"),
		}},
		{protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
			HostMutation:        protocol.HostMutation{RequestID: "request-expired-open", ExpectedHostEpoch: "host-stale"},
			PrimaryDirectoryRef: "directory-missing",
		}},
		{protocol.MethodWorkspaceList, protocol.WorkspaceListParams{ExpectedHostEpoch: "host-stale"}},
		{protocol.MethodSessionList, protocol.SessionListParams{ExpectedHostEpoch: "host-stale", WorkspaceID: "workspace-missing"}},
		{protocol.MethodSessionCreate, protocol.SessionCreateParams{
			HostMutation: protocol.HostMutation{RequestID: "request-expired-create", ExpectedHostEpoch: "host-stale"},
			WorkspaceID:  "workspace-missing", AdditionalDirectoryRefs: []protocol.DirectoryRef{},
			Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
		}},
	}
	for _, test := range tests {
		response := requestError(t, peer, test.method, test.params)
		requireRemoteError(t, response, protocol.ErrLeaseNotHeld)
	}
	if stats := server.requests.Stats(); stats.Entries != 0 {
		t.Fatalf("expired catalog mutations reached idempotency registry: %+v", stats)
	}
}

func TestCatalogErrorsUseFrozenWireMessagesWithoutDetailLeakage(t *testing.T) {
	options, _, buildID := daemonTestServerOptions(t, nil)
	workspacePath := t.TempDir()
	secret := "catalog-secret-/private/host/path"
	var resolverCalls atomic.Int64
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: filepath.Join(t.TempDir(), "catalog"), UserHome: workspacePath,
		SessionDir: func(string) string { return filepath.Join(workspacePath, ".sessions") },
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			resolverCalls.Add(1)
			return protocol.ResolvedProfile{}, errors.New(secret)
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	options.Catalog = catalogValue
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-catalog-errors", "")

	stale := requestError(t, peer, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: "host-stale", TypedPath: filepath.Join(workspacePath, "missing"),
	})
	staleData := requireRemoteError(t, stale, protocol.ErrStaleHostEpoch)
	if staleData.Expected != "host-test" || staleData.Actual != "host-stale" {
		t.Fatalf("stale epoch data = %+v", staleData)
	}
	missingPath := filepath.Join(workspacePath, "missing-sensitive-directory")
	missing := requestError(t, peer, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: "host-test", TypedPath: missingPath,
	})
	requireRemoteError(t, missing, protocol.ErrDirectoryNotFound)
	if strings.Contains(missing.Message, missingPath) || bytes.Contains(missing.Data, []byte(missingPath)) {
		t.Fatalf("directory detail leaked on wire: message=%q data=%s", missing.Message, missing.Data)
	}

	browse := browseWorkspacePeer(t, peer, workspacePath)
	opened := openWorkspacePeer(t, peer, "request-open-error-map", browse.Directory.DirectoryRef)
	create := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-invalid-profile", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}
	invalid := requestError(t, peer, protocol.MethodSessionCreate, create)
	requireRemoteError(t, invalid, protocol.ErrInvalidProfile)
	if strings.Contains(invalid.Message, secret) || bytes.Contains(invalid.Data, []byte(secret)) {
		t.Fatalf("catalog detail leaked on wire: message=%q data=%s", invalid.Message, invalid.Data)
	}
	replayed := requestError(t, peer, protocol.MethodSessionCreate, create)
	requireRemoteError(t, replayed, protocol.ErrInvalidProfile)
	changed := create
	changed.Topic.Title = "changed"
	conflict := requestError(t, peer, protocol.MethodSessionCreate, changed)
	requireRemoteError(t, conflict, protocol.ErrRequestIDConflict)
	if resolverCalls.Load() != 1 {
		t.Fatalf("deterministic INVALID_PROFILE was re-evaluated %d times", resolverCalls.Load())
	}
}

func TestStaleCatalogMutationAbortsClaimBeforeAdmission(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-stale-catalog-mutation", "")
	browse := browseWorkspacePeer(t, peer, t.TempDir())
	params := protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "request-stale-open", ExpectedHostEpoch: "host-old"},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	}
	stale := requestError(t, peer, protocol.MethodWorkspaceOpen, params)
	data := requireRemoteError(t, stale, protocol.ErrStaleHostEpoch)
	if data.Expected != "host-test" || data.Actual != "host-old" {
		t.Fatalf("stale workspace/open = %+v", data)
	}
	if stats := server.requests.Stats(); stats.Entries != 0 {
		t.Fatalf("stale workspace/open entered idempotency registry: %+v", stats)
	}

	// The same requestId can be used for the original operation after the
	// caller refreshes Host epoch because the stale attempt was never admitted.
	params.ExpectedHostEpoch = "host-test"
	opened := requestResult[protocol.WorkspaceOpenResult](t, peer, protocol.MethodWorkspaceOpen, params)
	if opened.Disposition != protocol.WorkspaceOpened {
		t.Fatalf("refreshed workspace/open = %+v", opened)
	}
}

func TestCatalogRejectionCacheClassification(t *testing.T) {
	tests := []struct {
		code protocol.ReasonixErrorCode
		want bool
	}{
		{protocol.ErrWorkspaceNotFound, true},
		{protocol.ErrWorkspaceInUse, true},
		{protocol.ErrInvalidProfile, true},
		{protocol.ErrStaleDirectoryRef, true},
		{protocol.ErrStaleHostEpoch, false},
		{protocol.ErrSessionPersistFailed, false},
		{protocol.ErrQueryFailed, false},
		// RUNTIME_START_FAILED is cached only by session/create after the
		// catalog's durable target commit, never by the generic classifier.
		{protocol.ErrRuntimeStartFailed, false},
	}
	for _, test := range tests {
		if got := cacheableCatalogRejection(test.code); got != test.want {
			t.Errorf("cacheableCatalogRejection(%s) = %t, want %t", test.code, got, test.want)
		}
	}
}

func TestSessionCreatePersistenceFailureAbortsClaimAndSameRequestRetries(t *testing.T) {
	options, factory, buildID := daemonTestServerOptions(t, nil)
	workspacePath := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "catalog")
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: stateDir, UserHome: workspacePath,
		SessionDir: func(string) string { return filepath.Join(workspacePath, ".sessions") },
		ProfileResolver: catalog.ProfileResolverFunc(func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
			return protocol.ResolvedProfile{
				Model: "test-model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
				TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	options.Catalog = catalogValue
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-persist-retry", "")
	browse := browseWorkspacePeer(t, peer, workspacePath)
	opened := openWorkspacePeer(t, peer, "request-open-persist-retry", browse.Directory.DirectoryRef)
	params := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create-persist-retry", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}

	statePath := filepath.Join(stateDir, "catalog-v1.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	failed := requestError(t, peer, protocol.MethodSessionCreate, params)
	requireRemoteError(t, failed, protocol.ErrSessionPersistFailed)
	if factory.count() != 0 {
		t.Fatalf("runtime started before durable catalog commit; controllers=%d", factory.count())
	}
	if err := os.RemoveAll(statePath); err != nil {
		t.Fatal(err)
	}

	created := requestResult[protocol.SessionCreateResult](t, peer, protocol.MethodSessionCreate, params)
	if created.Target.WorkspaceID != opened.Workspace.WorkspaceID || created.Target.SessionID == "" || created.RuntimeEpoch == "" {
		t.Fatalf("same-request retry after persistence repair = %+v", created)
	}
	if factory.count() != 1 {
		t.Fatalf("repaired retry constructed %d controllers, want 1", factory.count())
	}
	sessions := requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 || sessions.Items[0].Target != created.Target {
		t.Fatalf("persistence retry catalog = %+v", sessions)
	}
}

func TestSessionCreateRuntimeFailurePreservesAndReusesAllocatedTarget(t *testing.T) {
	options, delegate, buildID := daemonTestServerOptions(t, nil)
	factory := &daemonFailFirstFactory{delegate: delegate, failure: errors.New("controller-start-secret-/host/session")}
	options.ControllerFactory = factory
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-create-failure", "")
	workspacePath := t.TempDir()
	browse := browseWorkspacePeer(t, peer, workspacePath)
	opened := openWorkspacePeer(t, peer, "request-open-create-failure", browse.Directory.DirectoryRef)
	params := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create-failure", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}

	first := requestError(t, peer, protocol.MethodSessionCreate, params)
	firstData := requireRemoteError(t, first, protocol.ErrRuntimeStartFailed)
	if firstData.Target == nil || firstData.Target.WorkspaceID != opened.Workspace.WorkspaceID || firstData.Target.SessionID == "" {
		t.Fatalf("runtime failure lost allocated target: %+v", firstData)
	}
	allocated := *firstData.Target
	replayed := requestError(t, peer, protocol.MethodSessionCreate, params)
	replayedData := requireRemoteError(t, replayed, protocol.ErrRuntimeStartFailed)
	if replayedData.Target == nil || *replayedData.Target != allocated || factory.count() != 1 {
		t.Fatalf("runtime failure replay = %+v; factory calls=%d", replayedData, factory.count())
	}
	sessions := requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 || sessions.Items[0].Target != allocated {
		t.Fatalf("persisted failed-start Session = %+v", sessions)
	}

	// subscribe retries cold construction for the same durable target rather
	// than forcing session/create to allocate another target.
	subscription := subscribePeer(t, peer, allocated)
	if subscription.Snapshot.Target != allocated || subscription.Snapshot.RuntimeEpoch == "" || factory.count() != 2 {
		t.Fatalf("failed target recovery = %+v; factory calls=%d", subscription, factory.count())
	}
	sessions = requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 {
		t.Fatalf("failed-start recovery duplicated Session: %+v", sessions)
	}
}

type daemonDropMatchingResponseConn struct {
	net.Conn
	match   []byte
	dropped chan struct{}
	once    sync.Once
}

func (c *daemonDropMatchingResponseConn) Write(value []byte) (int, error) {
	dropped := false
	if bytes.Contains(value, c.match) {
		c.once.Do(func() {
			dropped = true
			close(c.dropped)
			_ = c.Conn.Close()
		})
	}
	if dropped {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(value)
}

func TestCatalogMutationResponseLossReplaysAcrossConnectionsWithoutDuplicates(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	openDropped := make(chan struct{})
	first := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &daemonDropMatchingResponseConn{
			Conn: connection, match: []byte(`"disposition":"opened"`), dropped: openDropped,
		}
	}, nil)
	grant := initializePeer(t, first, buildID, "client-catalog-retry", "")
	ephemeralPath := t.TempDir()
	browse := browseWorkspacePeer(t, first, ephemeralPath)
	openParams := protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "request-open-response-loss", ExpectedHostEpoch: "host-test"},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	}
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	_, openErr := first.wire.Request(ctx, string(protocol.MethodWorkspaceOpen), openParams)
	cancel()
	if openErr == nil {
		t.Fatal("workspace/open unexpectedly delivered the dropped response")
	}
	select {
	case <-openDropped:
	case <-time.After(testRequestTimeout):
		t.Fatal("workspace/open response was not dropped")
	}
	first.close(t)
	if err := os.RemoveAll(ephemeralPath); err != nil {
		t.Fatal(err)
	}

	second := openDaemonPeer(t, server, nil, nil)
	resumed := initializePeer(t, second, buildID, "client-catalog-retry", grant.Lease.LeaseID)
	if resumed.Lease.LeaseID != grant.Lease.LeaseID {
		t.Fatalf("workspace/open retry lease = %q, want %q", resumed.Lease.LeaseID, grant.Lease.LeaseID)
	}
	// Replay must win before catalog/path checks: the original directory no
	// longer exists, yet the exact first "opened" result is retained.
	opened := requestResult[protocol.WorkspaceOpenResult](t, second, protocol.MethodWorkspaceOpen, openParams)
	if opened.Disposition != protocol.WorkspaceOpened || opened.Workspace.DisplayPath != ephemeralPath {
		t.Fatalf("workspace/open replay = %+v", opened)
	}
	workspacePath := t.TempDir()
	createBrowse := browseWorkspacePeer(t, second, workspacePath)
	conflictingOpen := openParams
	conflictingOpen.PrimaryDirectoryRef = createBrowse.Directory.DirectoryRef
	openConflict := requestError(t, second, protocol.MethodWorkspaceOpen, conflictingOpen)
	requireRemoteError(t, openConflict, protocol.ErrRequestIDConflict)
	createWorkspace := openWorkspacePeer(t, second, "request-open-create-workspace", createBrowse.Directory.DirectoryRef)
	workspaces := requestResult[protocol.WorkspaceListResult](t, second, protocol.MethodWorkspaceList, protocol.WorkspaceListParams{
		ExpectedHostEpoch: "host-test",
	})
	if len(workspaces.Items) != 2 {
		t.Fatalf("workspace/open response-loss retry duplicated catalog entries: %+v", workspaces)
	}
	second.close(t)

	createDropped := make(chan struct{})
	third := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &daemonDropMatchingResponseConn{
			Conn: connection, match: []byte(`"runtimeEpoch":`), dropped: createDropped,
		}
	}, nil)
	initializePeer(t, third, buildID, "client-catalog-retry", grant.Lease.LeaseID)
	createParams := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create-response-loss", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  createWorkspace.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}
	ctx, cancel = context.WithTimeout(context.Background(), testRequestTimeout)
	_, createErr := third.wire.Request(ctx, string(protocol.MethodSessionCreate), createParams)
	cancel()
	if createErr == nil {
		t.Fatal("session/create unexpectedly delivered the dropped response")
	}
	select {
	case <-createDropped:
	case <-time.After(testRequestTimeout):
		t.Fatal("session/create response was not dropped")
	}
	third.close(t)

	fourth := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, fourth, buildID, "client-catalog-retry", grant.Lease.LeaseID)
	created := requestResult[protocol.SessionCreateResult](t, fourth, protocol.MethodSessionCreate, createParams)
	if created.Target.WorkspaceID != createWorkspace.Workspace.WorkspaceID || created.Target.SessionID == "" || created.RuntimeEpoch == "" {
		t.Fatalf("session/create replay = %+v", created)
	}
	if factory.count() != 1 {
		t.Fatalf("session/create response-loss retry constructed %d controllers, want 1", factory.count())
	}
	changedCreate := createParams
	changedCreate.Topic.Title = "conflicting retry"
	createConflict := requestError(t, fourth, protocol.MethodSessionCreate, changedCreate)
	requireRemoteError(t, createConflict, protocol.ErrRequestIDConflict)
	crossMethod := createParams
	crossMethod.RequestID = openParams.RequestID
	crossMethodConflict := requestError(t, fourth, protocol.MethodSessionCreate, crossMethod)
	requireRemoteError(t, crossMethodConflict, protocol.ErrRequestIDConflict)
	sessions := requestResult[protocol.SessionListResult](t, fourth, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: createWorkspace.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 || sessions.Items[0].Target != created.Target {
		t.Fatalf("session/create response-loss retry duplicated Session: %+v", sessions)
	}
}

func TestConcurrentCatalogMutationRetriesShareOneAdmission(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-catalog-concurrent", "")
	browse := browseWorkspacePeer(t, peer, t.TempDir())
	openParams := protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "request-open-concurrent", ExpectedHostEpoch: "host-test"},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	}

	const attempts = 24
	openResults := make(chan protocol.WorkspaceOpenResult, attempts)
	errorsSeen := make(chan error, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
			defer cancel()
			raw, err := peer.wire.Request(ctx, string(protocol.MethodWorkspaceOpen), openParams)
			if err != nil {
				errorsSeen <- err
				return
			}
			var result protocol.WorkspaceOpenResult
			if err := json.Unmarshal(raw, &result); err != nil {
				errorsSeen <- err
				return
			}
			openResults <- result
		}()
	}
	group.Wait()
	close(openResults)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent workspace/open: %v", err)
	}
	var opened protocol.WorkspaceOpenResult
	for result := range openResults {
		if opened.Workspace.WorkspaceID == "" {
			opened = result
		}
		if result != opened || result.Disposition != protocol.WorkspaceOpened {
			t.Fatalf("concurrent workspace/open result drift: first=%+v current=%+v", opened, result)
		}
	}
	if opened.Workspace.WorkspaceID == "" {
		t.Fatal("concurrent workspace/open returned no results")
	}

	createParams := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create-concurrent", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}
	createResults := make(chan protocol.SessionCreateResult, attempts)
	errorsSeen = make(chan error, attempts)
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
			defer cancel()
			raw, err := peer.wire.Request(ctx, string(protocol.MethodSessionCreate), createParams)
			if err != nil {
				errorsSeen <- err
				return
			}
			var result protocol.SessionCreateResult
			if err := json.Unmarshal(raw, &result); err != nil {
				errorsSeen <- err
				return
			}
			createResults <- result
		}()
	}
	group.Wait()
	close(createResults)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent session/create: %v", err)
	}
	var created protocol.SessionCreateResult
	for result := range createResults {
		if created.Target.SessionID == "" {
			created = result
		}
		if result != created {
			t.Fatalf("concurrent session/create result drift: first=%+v current=%+v", created, result)
		}
	}
	if created.Target.SessionID == "" || factory.count() != 1 {
		t.Fatalf("concurrent session/create = %+v; controllers=%d", created, factory.count())
	}
	sessions := requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: opened.Workspace.WorkspaceID,
	})
	if len(sessions.Items) != 1 || sessions.Items[0].Target != created.Target {
		t.Fatalf("concurrent session/create duplicated catalog entries: %+v", sessions)
	}
	if stats := server.requests.Stats(); stats.Entries != 2 || stats.Pending != 0 || stats.Completed != 2 {
		t.Fatalf("catalog idempotency stats = %+v", stats)
	}
}

func TestSnapshotContentChunksAndSubscriptionOwnerLifecycle(t *testing.T) {
	options, factory, buildID := daemonTestServerOptions(t, nil)
	goal := strings.Repeat("远程目标", 100_000)
	factory.setGoal(goal)
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-content", "")
	target := daemonTestTarget()

	first := subscribePeer(t, peer, target)
	if first.Snapshot.Meta.Goal != nil {
		t.Fatal("externalized snapshot goal was not a wire null placeholder")
	}
	if len(first.Snapshot.Externalized) != 1 || first.Snapshot.Externalized[0].JSONPointer != "/meta/goal" {
		t.Fatalf("snapshot descriptors = %#v", first.Snapshot.Externalized)
	}
	firstDescriptor := first.Snapshot.Externalized[0]
	if got := string(readRemoteContent(t, peer, firstDescriptor)); got != goal {
		t.Fatal("snapshot content did not round-trip")
	}
	if firstDescriptor.TotalBytes <= protocol.ContentRefChunkBytes {
		t.Fatalf("test goal only used %d bytes; multi-chunk path was not covered", firstDescriptor.TotalBytes)
	}

	replacement := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: first.SubscriptionID,
	})
	if replacement.Snapshot.SnapshotID == first.Snapshot.SnapshotID || len(replacement.Snapshot.Externalized) != 1 {
		t.Fatalf("replacement snapshot = %+v", replacement.Snapshot)
	}
	oldRef := requestError(t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: firstDescriptor.ContentRef})
	requireRemoteError(t, oldRef, protocol.ErrContentRefExpired)
	secondDescriptor := replacement.Snapshot.Externalized[0]
	if got := string(readRemoteContent(t, peer, secondDescriptor)); got != goal {
		t.Fatal("replacement snapshot content did not round-trip")
	}

	requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{
		SubscriptionID: replacement.SubscriptionID,
	})
	unsubscribedRef := requestError(t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: secondDescriptor.ContentRef})
	requireRemoteError(t, unsubscribedRef, protocol.ErrContentRefExpired)
	if stats := server.contents.Stats(); stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("subscription lifecycle retained snapshot owners: %#v", stats)
	}
}

func TestSameRuntimeSubscribeProjectionFailureRestoresOldSubscription(t *testing.T) {
	options, factory, buildID := daemonTestServerOptions(t, nil)
	baseMetadata := options.Metadata
	var failProjection atomic.Bool
	options.Metadata = func(ctx context.Context, target protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
		if failProjection.Load() {
			return protocol.SessionMetaSnapshot{}, errors.New("injected replacement projection failure")
		}
		return baseMetadata(ctx, target)
	}
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	events := make(chan protocol.SessionEvent, 8)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	initializePeer(t, peer, buildID, "client-projection-abort", "")
	target := daemonTestTarget()
	initial := subscribePeer(t, peer, target)

	failProjection.Store(true)
	requestError(t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	failProjection.Store(false)
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Text: "old-active-after-projection-abort"})
	select {
	case notification := <-events:
		if notification.SubscriptionID != initial.SubscriptionID || notification.Event.Text != "old-active-after-projection-abort" {
			t.Fatalf("old subscription after projection Abort = %+v", notification)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("old subscription was lost after projection failure")
	}

	retry := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	if retry.SubscriptionID == initial.SubscriptionID || retry.Snapshot.RuntimeEpoch != initial.Snapshot.RuntimeEpoch {
		t.Fatalf("replacement retry = %+v", retry)
	}
	missing := requestError(t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	requireRemoteError(t, missing, protocol.ErrSubscriptionNotFound)
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Text: "committed-replacement-only"})
	select {
	case notification := <-events:
		if notification.SubscriptionID != retry.SubscriptionID || notification.Event.Text != "committed-replacement-only" {
			t.Fatalf("committed replacement event = %+v", notification)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("committed replacement did not receive events")
	}
}

func TestRuntimeReplacementWireMigrationRetainsTerminalUntilSameTransportCommit(t *testing.T) {
	options, factory, buildID := daemonTestServerOptions(t, nil)
	goal := strings.Repeat("runtime replacement owner", 20_000)
	factory.setGoal(goal)
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	resyncs := make(chan protocol.SessionResyncRequired, 8)
	events := make(chan protocol.SessionEvent, 8)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionResyncRequired), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionResyncRequired
			if json.Unmarshal(raw, &value) == nil {
				resyncs <- value
			}
		})
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	grant := initializePeer(t, peer, buildID, "client-runtime-migration", "")
	target := daemonTestTarget()
	initial := subscribePeer(t, peer, target)
	if len(initial.Snapshot.Externalized) != 1 {
		t.Fatalf("initial externalized owner = %+v", initial.Snapshot.Externalized)
	}
	oldDescriptor := initial.Snapshot.Externalized[0]
	if stats := server.contents.Stats(); stats.Owners != 1 {
		t.Fatalf("initial snapshot owners = %+v", stats)
	}

	replacement, err := server.runtimes.Replace(target)
	if err != nil {
		t.Fatal(err)
	}
	var terminal protocol.SessionResyncRequired
	select {
	case terminal = <-resyncs:
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for runtime_replaced")
	}
	if terminal.SubscriptionID != initial.SubscriptionID || terminal.Reason != protocol.ResyncRuntimeReplaced ||
		terminal.Target != target || terminal.RuntimeEpoch != initial.Snapshot.RuntimeEpoch || terminal.ReplacementTarget != nil ||
		terminal.ReplacementRuntimeEpoch != replacement.Epoch() {
		t.Fatalf("runtime_replaced payload = %+v", terminal)
	}
	if err := terminal.Validate(); err != nil {
		t.Fatalf("runtime_replaced payload invalid: %v", err)
	}
	if stats := server.contents.Stats(); stats.Owners != 1 {
		t.Fatalf("terminal subscription released owners before migration: %+v", stats)
	}
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Text: "late-old-after-terminal"})
	select {
	case notification := <-events:
		t.Fatalf("late old event crossed terminal resync: %+v", notification)
	case <-time.After(50 * time.Millisecond):
	}

	migrated := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	if migrated.Snapshot.RuntimeEpoch != replacement.Epoch() || migrated.SubscriptionID == initial.SubscriptionID {
		t.Fatalf("runtime migration result = %+v", migrated)
	}
	if stats := server.contents.Stats(); stats.Owners != 1 {
		t.Fatalf("migration did not atomically replace snapshot owner: %+v", stats)
	}
	oldRef := requestError(t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: oldDescriptor.ContentRef})
	requireRemoteError(t, oldRef, protocol.ErrContentRefExpired)
	select {
	case duplicate := <-resyncs:
		t.Fatalf("old subscription emitted a second terminal resync: %+v", duplicate)
	case <-time.After(50 * time.Millisecond):
	}
	factory.controller(t, 1).emit(event.Event{Kind: event.Notice, Text: "new-runtime-event"})
	select {
	case notification := <-events:
		if notification.SubscriptionID != migrated.SubscriptionID || notification.RuntimeEpoch != replacement.Epoch() || notification.Event.Text != "new-runtime-event" {
			t.Fatalf("new runtime event = %+v", notification)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("new runtime subscription did not receive events")
	}

	// A later terminal identity is scoped to this exact transport generation.
	secondReplacement, err := server.runtimes.Replace(target)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case terminal = <-resyncs:
		if terminal.SubscriptionID != migrated.SubscriptionID || terminal.ReplacementRuntimeEpoch != secondReplacement.Epoch() {
			t.Fatalf("second runtime terminal = %+v", terminal)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for second runtime terminal")
	}
	peer.close(t)
	resumed := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, resumed, buildID, "client-runtime-migration", grant.Lease.LeaseID)
	crossTransport := requestError(t, resumed, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: migrated.SubscriptionID,
	})
	requireRemoteError(t, crossTransport, protocol.ErrSubscriptionNotFound)
}

func TestTargetReplacementWireMigrationCarriesReplacementTarget(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	resyncs := make(chan protocol.SessionResyncRequired, 2)
	events := make(chan protocol.SessionEvent, 2)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionResyncRequired), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionResyncRequired
			if json.Unmarshal(raw, &value) == nil {
				resyncs <- value
			}
		})
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	initializePeer(t, peer, buildID, "client-target-migration", "")
	oldTarget := daemonTestTarget()
	newTarget := protocol.RuntimeTarget{WorkspaceID: oldTarget.WorkspaceID, SessionID: "session-target-replacement"}
	initial := subscribePeer(t, peer, oldTarget)
	replacement, err := server.runtimes.ReplaceTarget(oldTarget, newTarget)
	if err != nil {
		t.Fatal(err)
	}
	var terminal protocol.SessionResyncRequired
	select {
	case terminal = <-resyncs:
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for target_replaced")
	}
	if terminal.Reason != protocol.ResyncTargetReplaced || terminal.SubscriptionID != initial.SubscriptionID ||
		terminal.ReplacementTarget == nil || *terminal.ReplacementTarget != newTarget || terminal.ReplacementRuntimeEpoch != replacement.Epoch() {
		t.Fatalf("target_replaced payload = %+v", terminal)
	}
	if err := terminal.Validate(); err != nil {
		t.Fatalf("target_replaced payload invalid: %v", err)
	}
	migrated := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: newTarget, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	if migrated.Snapshot.Target != newTarget || migrated.Snapshot.RuntimeEpoch != replacement.Epoch() {
		t.Fatalf("target migration snapshot = %+v", migrated.Snapshot)
	}
	factory.controller(t, 1).emit(event.Event{Kind: event.Notice, Text: "target-migrated-event"})
	select {
	case notification := <-events:
		if notification.SubscriptionID != migrated.SubscriptionID || notification.Target != newTarget || notification.Event.Text != "target-migrated-event" {
			t.Fatalf("target migration event = %+v", notification)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("target-migrated subscription did not receive events")
	}
}

func TestSnapshotContentExpiryReleasesOwner(t *testing.T) {
	contentClock := &daemonTestClock{now: time.Unix(20_000, 0)}
	options, factory, buildID := daemonTestServerOptions(t, nil)
	options.ContentRefOptions = contentref.Config{Now: contentClock.Now}
	goal := strings.Repeat("expiry", 20_000)
	factory.setGoal(goal)
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-expiry", "")
	subscription := subscribePeer(t, peer, daemonTestTarget())
	descriptor := subscription.Snapshot.Externalized[0]

	contentClock.Advance(15 * time.Minute)
	expired := requestError(t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: descriptor.ContentRef})
	requireRemoteError(t, expired, protocol.ErrContentRefExpired)
	if stats := server.contents.Stats(); stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("expired snapshot retained owner metadata: %#v", stats)
	}
}

func TestEventContentSurvivesLeaseResumeButNotLeaseReplacement(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	events := make(chan protocol.SessionEvent, 8)
	first := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	grant := initializePeer(t, first, buildID, "client-event-content", "")
	subscribePeer(t, first, daemonTestTarget())
	text := strings.Repeat("事件正文", 100_000)
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Text: text})

	var envelope protocol.SessionEvent
	select {
	case envelope = <-events:
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for externalized event")
	}
	if envelope.Event.Text != "" || len(envelope.Externalized) != 1 || envelope.Externalized[0].JSONPointer != "/event/text" {
		t.Fatalf("event externalization = %+v", envelope)
	}
	descriptor := envelope.Externalized[0]
	first.close(t)

	resumed := openDaemonPeer(t, server, nil, nil)
	resumedGrant := initializePeer(t, resumed, buildID, "client-event-content", grant.Lease.LeaseID)
	if resumedGrant.Lease.LeaseID != grant.Lease.LeaseID {
		t.Fatalf("resumed lease = %q, want %q", resumedGrant.Lease.LeaseID, grant.Lease.LeaseID)
	}
	if got := string(readRemoteContent(t, resumed, descriptor)); got != text {
		t.Fatal("event content did not survive transport-generation reconnect")
	}
	badOffset := requestError(t, resumed, protocol.MethodSessionContent, protocol.SessionContentParams{
		ContentRef: descriptor.ContentRef, Offset: descriptor.TotalBytes + 1,
	})
	requireRemoteError(t, badOffset, protocol.ErrContentRefExpired)

	// The daemon closes the transport immediately after the detach response is
	// written. rpcwire may therefore observe the response and EOF in the same
	// scheduling quantum and report either outcome to Request. The dedicated
	// detach ordering test verifies the response-before-release contract; this
	// test only needs to wait until the lease has actually been released before
	// acquiring its replacement.
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	_, detachErr := resumed.wire.Request(ctx, string(protocol.MethodRemoteDetach), protocol.DetachParams{
		LeaseID: resumedGrant.Lease.LeaseID,
	})
	cancel()
	if detachErr != nil && !strings.Contains(detachErr.Error(), "connection closed") {
		t.Fatalf("remote/detach request: %v", detachErr)
	}
	deadline := time.Now().Add(testRequestTimeout)
	for server.leases.Held() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.leases.Held() {
		t.Fatal("detached lease remained held")
	}
	resumed.close(t)
	fresh := openDaemonPeer(t, server, nil, nil)
	freshGrant := initializePeer(t, fresh, buildID, "client-fresh-content", "")
	if freshGrant.Lease.LeaseID == grant.Lease.LeaseID {
		t.Fatalf("replacement lease reused %q", grant.Lease.LeaseID)
	}
	wrongLease := requestError(t, fresh, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: descriptor.ContentRef})
	requireRemoteError(t, wrongLease, protocol.ErrContentRefExpired)

	server.Close()
	if stats := server.contents.Stats(); !stats.Closed || stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("daemon Close did not clear content store: %#v", stats)
	}
	fresh.close(t)
}

func TestEventContentRejectsReplacedRuntimeOwner(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	events := make(chan protocol.SessionEvent, 1)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	initializePeer(t, peer, buildID, "client-runtime-owner", "")
	target := daemonTestTarget()
	subscription := subscribePeer(t, peer, target)
	text := strings.Repeat("old-runtime", 10_000)
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Text: text})

	var descriptor protocol.ExternalizedField
	select {
	case envelope := <-events:
		if envelope.RuntimeEpoch != subscription.Snapshot.RuntimeEpoch || len(envelope.Externalized) != 1 {
			t.Fatalf("old runtime event = %+v", envelope)
		}
		descriptor = envelope.Externalized[0]
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for old-runtime event")
	}
	replacement, err := server.runtimes.Replace(target)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Epoch() == subscription.Snapshot.RuntimeEpoch {
		t.Fatal("runtime replacement reused its epoch")
	}

	stale := requestError(t, peer, protocol.MethodSessionContent, protocol.SessionContentParams{ContentRef: descriptor.ContentRef})
	requireRemoteError(t, stale, protocol.ErrContentRefExpired)
	if stats := server.contents.Stats(); stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("replaced runtime retained event owner: %#v", stats)
	}
}

func TestExpiredHalfOpenTransportLosesNotificationsAndFreshClientOwnership(t *testing.T) {
	clock := &daemonTestClock{now: time.Unix(10_000, 0)}
	server, factory, buildID := newDaemonTestServerWithNow(t, clock.Now)
	oldEvents := make(chan protocol.SessionEvent, 8)
	old := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				oldEvents <- value
			}
		})
	})
	oldGrant := initializePeer(t, old, buildID, "client-old", "")
	firstTarget := daemonTestTarget()
	secondTarget := protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-second"}
	subscribePeer(t, old, firstTarget)
	subscribePeer(t, old, secondTarget)
	firstController := factory.controller(t, 0)
	secondController := factory.controller(t, 1)

	clock.Advance(30 * time.Second)
	firstController.emit(event.Event{Kind: event.Notice, Text: "must not cross expired lease"})
	select {
	case value := <-oldEvents:
		t.Fatalf("expired transport received event: %+v", value)
	case <-time.After(100 * time.Millisecond):
	}

	freshEvents := make(chan protocol.SessionEvent, 8)
	fresh := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				freshEvents <- value
			}
		})
	})
	freshGrant := initializePeer(t, fresh, buildID, "client-fresh", "")
	if freshGrant.Lease.LeaseID == oldGrant.Lease.LeaseID {
		t.Fatalf("fresh client reused expired lease %q", freshGrant.Lease.LeaseID)
	}
	stale := requestError(t, old, protocol.MethodRemotePing, protocol.PingParams{LeaseID: oldGrant.Lease.LeaseID})
	requireRemoteError(t, stale, protocol.ErrLeaseNotHeld)

	freshSubscription := subscribePeer(t, fresh, secondTarget)
	submitted := requestResult[protocol.SessionSubmitResult](t, fresh, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-fresh-turn", secondTarget, freshSubscription.Snapshot.RuntimeEpoch),
		Input:           "fresh client owns notifications", DisplayText: "fresh client owns notifications",
	})
	select {
	case value := <-freshEvents:
		if value.Event.Kind != "turn_started" || value.TurnID != submitted.TurnID {
			t.Fatalf("fresh notification = %+v", value)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("fresh client did not receive turn_started")
	}
	select {
	case value := <-oldEvents:
		t.Fatalf("old transport received event after fresh attachment: %+v", value)
	case <-time.After(100 * time.Millisecond):
	}
	secondController.releaseTurn()
}

type detachObservingConn struct {
	net.Conn
	onDetachWrite func()
	once          sync.Once
}

type detachFailingConn struct{ net.Conn }

type submitResponseDroppingConn struct {
	net.Conn
	dropped chan struct{}
	once    sync.Once
}

type eventFrameObservingConn struct {
	net.Conn
	sizes chan int
}

func (c *eventFrameObservingConn) Write(value []byte) (int, error) {
	n, err := c.Conn.Write(value)
	// Observe only a frame the transport has actually accepted. net.Pipe writes
	// synchronously with the peer's read, so reporting before Write made the
	// test's client-acceptance deadline include delivery of the entire 8 MiB
	// boundary frame. Under race instrumentation that could expire before the
	// peer had even received the newline, despite both frame limits accepting
	// the exact boundary.
	if err == nil && n == len(value) && bytes.Contains(value, []byte(`"method":"session/event"`)) {
		select {
		case c.sizes <- len(value):
		default:
		}
	}
	return n, err
}

func (c *detachFailingConn) Write(value []byte) (int, error) {
	if bytes.Contains(value, []byte(`"detached":true`)) {
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(value)
}

func TestSessionEventExactFrameBoundaryPassesDaemonTransport(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	frameSizes := make(chan int, 1)
	events := make(chan protocol.SessionEvent, 1)
	peer := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &eventFrameObservingConn{Conn: connection, sizes: frameSizes}
	}, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	initializePeer(t, peer, buildID, "client-frame-boundary", "")
	subscription := subscribePeer(t, peer, daemonTestTarget())

	probeEvent := eventwire.ToWire(event.Event{Kind: event.Notice, Code: "c"})
	probePayload := protocol.SessionEvent{
		SubscriptionID: subscription.SubscriptionID, HostEpoch: "host-test", Target: daemonTestTarget(),
		RuntimeEpoch: subscription.Snapshot.RuntimeEpoch, Seq: 1, Event: probeEvent,
		Externalized: []protocol.ExternalizedField{},
	}
	probeWire, err := json.Marshal(probePayload)
	if err != nil {
		t.Fatal(err)
	}
	frameOverhead := len(`{"jsonrpc":"2.0","method":"session/event","params":`) + len("}\n")
	codeBytes := protocol.FrameBytes - frameOverhead - (len(probeWire) - 1)
	if codeBytes <= protocol.SnapshotHistoryBytes {
		t.Fatalf("computed exact-frame event code bytes = %d", codeBytes)
	}
	factory.controller(t, 0).emit(event.Event{Kind: event.Notice, Code: strings.Repeat("c", codeBytes)})

	select {
	case frameBytes := <-frameSizes:
		if frameBytes != protocol.FrameBytes {
			t.Fatalf("session/event frame bytes = %d, want %d", frameBytes, protocol.FrameBytes)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out observing exact-boundary event frame")
	}
	select {
	case envelope := <-events:
		if len(envelope.Event.Code) != codeBytes || len(envelope.Externalized) != 0 {
			t.Fatalf("exact-boundary event = code %d, descriptors %d", len(envelope.Event.Code), len(envelope.Externalized))
		}
	case <-time.After(5 * testRequestTimeout):
		// The frame has already been fully accepted by net.Pipe and its exact
		// byte size asserted above. Give the race-instrumented client enough
		// time to decode and unmarshal the maximum permitted 8 MiB JSON value
		// when the complete repository race suite is running concurrently.
		t.Fatal("client rejected exact-boundary event frame")
	}
}

func (c *submitResponseDroppingConn) Write(value []byte) (int, error) {
	if bytes.Contains(value, []byte(`"result":{"kind":"turn"`)) {
		c.once.Do(func() { close(c.dropped) })
		_ = c.Conn.Close()
		return 0, io.ErrClosedPipe
	}
	return c.Conn.Write(value)
}

func (c *detachObservingConn) Write(value []byte) (int, error) {
	if bytes.Contains(value, []byte(`"detached":true`)) {
		c.once.Do(c.onDetachWrite)
	}
	return c.Conn.Write(value)
}

func TestDetachReleasesLeaseOnlyAfterResponseWrite(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	heldDuringWrite := make(chan bool, 1)
	peer := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &detachObservingConn{Conn: connection, onDetachWrite: func() { heldDuringWrite <- server.leases.Held() }}
	}, nil)
	grant := initializePeer(t, peer, buildID, "client-test", "")

	result := requestResult[protocol.DetachResult](t, peer, protocol.MethodRemoteDetach, protocol.DetachParams{LeaseID: grant.Lease.LeaseID})
	if !result.Detached {
		t.Fatal("detach result was false")
	}
	select {
	case held := <-heldDuringWrite:
		if !held {
			t.Fatal("lease was released before detach response write")
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("detach response write was not observed")
	}
	deadline := time.Now().Add(testRequestTimeout)
	for server.leases.Held() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.leases.Held() {
		t.Fatal("lease remained held after successful detach response")
	}
}

func TestDetachResponseWriteFailureKeepsLeaseUntilTTL(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &detachFailingConn{Conn: connection}
	}, nil)
	grant := initializePeer(t, peer, buildID, "client-test", "")
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	defer cancel()
	if _, err := peer.wire.Request(ctx, string(protocol.MethodRemoteDetach), protocol.DetachParams{LeaseID: grant.Lease.LeaseID}); err == nil {
		t.Fatal("detach unexpectedly succeeded despite response write failure")
	}
	peer.close(t)
	if !server.leases.Held() {
		t.Fatal("failed detach response released lease before TTL")
	}
}

func TestAcceptedTurnSurvivesEOFAndReconnectSnapshotRestoresState(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	events := make(chan protocol.SessionEvent, 8)
	first := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var value protocol.SessionEvent
			if json.Unmarshal(raw, &value) == nil {
				events <- value
			}
		})
	})
	grant := initializePeer(t, first, buildID, "client-test", "")
	target := daemonTestTarget()
	initial := subscribePeer(t, first, target)

	submitted := requestResult[protocol.SessionSubmitResult](t, first, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-submit", target, initial.Snapshot.RuntimeEpoch),
		Input:           "continue after SSH EOF", DisplayText: "continue after SSH EOF",
	})
	if submitted.Kind != protocol.SubmitTurn || submitted.TurnID == "" {
		t.Fatalf("submit result = %+v", submitted)
	}
	select {
	case value := <-events:
		if value.Event.Kind != "turn_started" || value.TurnID != submitted.TurnID || value.Seq != 1 {
			t.Fatalf("first event = %+v", value)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("timed out waiting for turn_started notification")
	}

	controller := factory.controller(t, 0)
	first.close(t)
	closeCalls, cancelCalls, strictSubmits := controller.counts()
	if closeCalls != 0 || cancelCalls != 0 || strictSubmits != 1 {
		t.Fatalf("EOF controller calls = close %d cancel %d submit %d", closeCalls, cancelCalls, strictSubmits)
	}
	select {
	case <-controller.ctx.Done():
		t.Fatal("transport EOF cancelled daemon-owned Controller context")
	default:
	}

	controller.releaseTurn()
	select {
	case <-controller.finished:
	case <-time.After(testRequestTimeout):
		t.Fatal("accepted turn did not finish after transport EOF")
	}

	second := openDaemonPeer(t, server, nil, nil)
	resumed := initializePeer(t, second, buildID, "client-test", grant.Lease.LeaseID)
	if resumed.Lease.LeaseID != grant.Lease.LeaseID {
		t.Fatalf("reconnect lease = %q, want %q", resumed.Lease.LeaseID, grant.Lease.LeaseID)
	}
	recovered := subscribePeer(t, second, target).Snapshot
	if recovered.RuntimeEpoch != initial.Snapshot.RuntimeEpoch || recovered.BoundarySeq != 3 {
		t.Fatalf("recovered identity/boundary = %q/%d, want %q/3", recovered.RuntimeEpoch, recovered.BoundarySeq, initial.Snapshot.RuntimeEpoch)
	}
	if recovered.Runtime.Running || recovered.Runtime.CurrentTurn != nil || recovered.Runtime.LastOutcome != protocol.OutcomeCompleted {
		t.Fatalf("recovered runtime state = %+v", recovered.Runtime)
	}
	if len(recovered.Runtime.LiveEvents) != 0 {
		t.Fatalf("completed Turn retained %d live events; canonical history owns completed content", len(recovered.Runtime.LiveEvents))
	}
}

func TestWireSnapshotUsesCompleteSemanticLiveProjection(t *testing.T) {
	const deltas = 12001
	const notices = 4100
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-live-projection", "")
	target := daemonTestTarget()
	initial := subscribePeer(t, peer, target)
	requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-live-projection", target, initial.Snapshot.RuntimeEpoch),
		Input:           "large live projection", DisplayText: "large live projection",
	})
	requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{
		SubscriptionID: initial.SubscriptionID,
	})

	controller := factory.controller(t, 0)
	for index := 0; index < deltas; index++ {
		if index%2 == 0 {
			controller.emit(event.Event{Kind: event.Reasoning, Text: "r"})
		} else {
			controller.emit(event.Event{Kind: event.Text, Text: "t"})
		}
	}
	for index := 0; index < notices; index++ {
		controller.emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("wire-notice-%04d", index)})
	}

	recovered := subscribePeer(t, peer, target).Snapshot
	if recovered.BoundarySeq != 1+deltas+notices {
		t.Fatalf("wire boundary = %d, want %d", recovered.BoundarySeq, 1+deltas+notices)
	}
	if len(recovered.Runtime.LiveEvents) != 1+2+notices {
		t.Fatalf("wire live events = %d, want TurnStarted + two streams + %d notices", len(recovered.Runtime.LiveEvents), notices)
	}
	var textBytes, reasoningBytes, noticeCount int
	for _, liveEvent := range recovered.Runtime.LiveEvents {
		switch liveEvent.Kind {
		case "text":
			textBytes += len(liveEvent.Text)
		case "reasoning":
			reasoningBytes += len(liveEvent.Text)
		case "notice":
			noticeCount++
		}
	}
	if textBytes != deltas/2 || reasoningBytes != deltas/2+1 || noticeCount != notices {
		t.Fatalf("wire projection text=%d reasoning=%d notices=%d", textBytes, reasoningBytes, noticeCount)
	}
	if recovered.Runtime.LiveEvents[3].Text != "wire-notice-0000" || recovered.Runtime.LiveEvents[len(recovered.Runtime.LiveEvents)-1].Text != "wire-notice-4099" {
		t.Fatalf("wire notice order first=%+v last=%+v", recovered.Runtime.LiveEvents[3], recovered.Runtime.LiveEvents[len(recovered.Runtime.LiveEvents)-1])
	}
}

func TestSubmitResponseLossReconnectReplaysWithoutDuplicateExecution(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	dropped := make(chan struct{})
	first := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &submitResponseDroppingConn{Conn: connection, dropped: dropped}
	}, nil)
	grant := initializePeer(t, first, buildID, "client-test", "")
	target := daemonTestTarget()
	subscription := subscribePeer(t, first, target)
	params := protocol.SessionSubmitParams{
		SessionMutation: mutation("request-response-loss", target, subscription.Snapshot.RuntimeEpoch),
		Input:           "execute once despite lost response", DisplayText: "execute once despite lost response",
	}
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	_, requestErr := first.wire.Request(ctx, string(protocol.MethodSessionSubmit), params)
	cancel()
	if requestErr == nil {
		t.Fatal("submit unexpectedly received the intentionally dropped response")
	}
	select {
	case <-dropped:
	case <-time.After(testRequestTimeout):
		t.Fatal("submit response write was not dropped")
	}
	controller := factory.controller(t, 0)
	select {
	case input := <-controller.started:
		if input != params.Input {
			t.Fatalf("Controller input = %q", input)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("response-loss submit was not admitted")
	}
	first.close(t)

	second := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, second, buildID, "client-test", grant.Lease.LeaseID)
	replayed := requestResult[protocol.SessionSubmitResult](t, second, protocol.MethodSessionSubmit, params)
	if replayed.Kind != protocol.SubmitTurn || replayed.TurnID == "" || replayed.RuntimeEpoch != subscription.Snapshot.RuntimeEpoch {
		t.Fatalf("replayed SubmitResult = %+v", replayed)
	}
	_, _, strictSubmits := controller.counts()
	if strictSubmits != 1 {
		t.Fatalf("response-loss retry executed Controller %d times", strictSubmits)
	}

	// Registry lookup precedes epoch validation. Altering the caller's expected
	// epoch changes the canonical fingerprint and is a conflict, not a stale
	// retry that could be forwarded to another runtime.
	conflict := params
	conflict.ExpectedRuntimeEpoch = "runtime-forged"
	response := requestError(t, second, protocol.MethodSessionSubmit, conflict)
	requireRemoteError(t, response, protocol.ErrRequestIDConflict)
	_, _, strictSubmits = controller.counts()
	if strictSubmits != 1 {
		t.Fatalf("conflicting retry executed Controller %d times", strictSubmits)
	}
	controller.releaseTurn()
}

func TestMissingSessionBusinessRejectionIsCachedAcrossLaterRuntimeCreation(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-test", "")
	target := daemonTestTarget()
	params := protocol.SessionSubmitParams{
		SessionMutation: mutation("request-missing-session", target, "runtime-not-created"),
		Input:           "must not appear after Session creation", DisplayText: "must not appear after Session creation",
	}
	first := requestError(t, peer, protocol.MethodSessionSubmit, params)
	requireRemoteError(t, first, protocol.ErrSessionNotFound)

	created := subscribePeer(t, peer, target)
	if created.Snapshot.RuntimeEpoch == "" {
		t.Fatal("subscribe did not create the runtime")
	}
	replayed := requestError(t, peer, protocol.MethodSessionSubmit, params)
	requireRemoteError(t, replayed, protocol.ErrSessionNotFound)

	changed := params
	changed.ExpectedRuntimeEpoch = created.Snapshot.RuntimeEpoch
	conflict := requestError(t, peer, protocol.MethodSessionSubmit, changed)
	requireRemoteError(t, conflict, protocol.ErrRequestIDConflict)
}

func TestMissingTargetGuardRejectsQueuedOldLeaseGenerationBeforeBegin(t *testing.T) {
	server, _, _ := newDaemonTestServer(t)
	first, err := server.leases.Acquire("client-missing-generation", "")
	if err != nil {
		t.Fatal(err)
	}
	oldTransport := &transport{
		server: server, phase: transportReady, binding: first.Binding,
	}
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-missing", SessionID: "session-missing"}
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-missing-generation", ExpectedHostEpoch: "host-test",
			Target: target, ExpectedRuntimeEpoch: "runtime-never-created",
		},
		Input: "must remain retryable", DisplayText: "must remain retryable",
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionSubmit),
		Target: idempotency.SessionTarget(target), Params: params,
	}

	server.missingMutationMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		var destination protocol.SessionSubmitResult
		done <- server.rejectMissingSessionMutation(
			context.Background(), request, params.ExpectedHostEpoch, target, &destination,
			oldTransport.sessionMutationGuard(),
		)
	}()
	<-started
	second, err := server.leases.Acquire("client-missing-generation", first.Binding.LeaseID)
	if err != nil {
		server.missingMutationMu.Unlock()
		t.Fatal(err)
	}
	server.missingMutationMu.Unlock()
	select {
	case guardErr := <-done:
		var remote *protocol.RemoteError
		if !errors.As(guardErr, &remote) || remote.Code != protocol.ErrStaleConnection {
			t.Fatalf("old-generation missing-target guard = %v", guardErr)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("old-generation missing-target request did not finish")
	}
	if stats := server.requests.Stats(); stats.Entries != 0 || stats.Pending != 0 || stats.Completed != 0 {
		t.Fatalf("old-generation missing-target request created requestId state: %+v", stats)
	}

	resumedTransport := &transport{
		server: server, phase: transportReady, binding: second.Binding,
	}
	var destination protocol.SessionSubmitResult
	err = server.rejectMissingSessionMutation(
		context.Background(), request, params.ExpectedHostEpoch, target, &destination,
		resumedTransport.sessionMutationGuard(),
	)
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != protocol.ErrSessionNotFound {
		t.Fatalf("same requestId from resumed generation = %v", err)
	}
	if stats := server.requests.Stats(); stats.Entries != 1 || stats.Pending != 0 || stats.Completed != 1 {
		t.Fatalf("resumed missing-target requestId state = %+v", stats)
	}
}

func TestSubmitRoutesComposerOperationsAndCancelRequiresExactIdentity(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-test", "")
	target := daemonTestTarget()
	subscription := subscribePeer(t, peer, target)
	epoch := subscription.Snapshot.RuntimeEpoch

	// The shared composer now routes compact to the real Operation boundary.
	// This narrow fake Controller has no executor, so capability rejection is
	// the business result rather than the former blanket slash rejection.
	compact := protocol.SessionSubmitParams{
		SessionMutation: mutation("request-compact-unavailable", target, epoch),
		Input:           "/compact", DisplayText: "/compact",
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionSubmit, compact), protocol.ErrCapabilityUnavailable)
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionSubmit, compact), protocol.ErrCapabilityUnavailable)

	shell := protocol.SessionSubmitParams{
		SessionMutation: mutation("request-shell-operation", target, epoch),
		Input:           "!printf remote-v1", DisplayText: "!printf remote-v1",
	}
	shellResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, shell)
	if shellResult.Kind != protocol.SubmitOperation || shellResult.Operation != protocol.OperationShell ||
		shellResult.OperationID == "" || shellResult.Target != target || shellResult.RuntimeEpoch != epoch || shellResult.SnapshotRequired {
		t.Fatalf("shell composer result = %+v", shellResult)
	}
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, shell); replay != shellResult {
		t.Fatalf("shell composer replay = %+v, want %+v", replay, shellResult)
	}
	runtime, ok := server.runtimes.Runtime(target)
	if !ok {
		t.Fatal("shell composer lost runtime")
	}
	deadline := time.Now().Add(testRequestTimeout)
	for {
		snapshot, err := runtime.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.CurrentOperation == nil && !snapshot.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell composer operation did not finish: %+v", snapshot.CurrentOperation)
		}
		time.Sleep(time.Millisecond)
	}
	controller := factory.controller(t, 0)
	_, _, strictSubmits := controller.counts()
	if strictSubmits != 0 {
		t.Fatalf("operation composer input reached normal Turn primitive %d times", strictSubmits)
	}

	submitted := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-turn", target, epoch), Input: "ordinary model prompt", DisplayText: "ordinary model prompt",
	})
	staleEpoch := requestError(t, peer, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: mutation("request-stale-epoch", target, "runtime-forged"), ExpectedTurnID: submitted.TurnID,
	})
	staleData := requireRemoteError(t, staleEpoch, protocol.ErrStaleRuntimeEpoch)
	if staleData.Expected != string(epoch) || staleData.Actual != "runtime-forged" {
		t.Fatalf("stale runtime orientation = expected %q actual %q", staleData.Expected, staleData.Actual)
	}

	wrongTurn := requestError(t, peer, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: mutation("request-wrong-turn", target, epoch), ExpectedTurnID: "turn-forged",
	})
	turnData := requireRemoteError(t, wrongTurn, protocol.ErrTurnMismatch)
	if turnData.Expected != string(submitted.TurnID) || turnData.Actual != "turn-forged" {
		t.Fatalf("turn mismatch orientation = expected %q actual %q", turnData.Expected, turnData.Actual)
	}
	_, cancelCalls, _ := controller.counts()
	if cancelCalls != 0 {
		t.Fatalf("forged turn reached Controller.TryCancel %d times", cancelCalls)
	}

	cancelled := requestResult[protocol.TurnCancelResult](t, peer, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: mutation("request-exact-turn", target, epoch), ExpectedTurnID: submitted.TurnID,
	})
	if cancelled.Status != protocol.CancelRequested || cancelled.TurnID != submitted.TurnID {
		t.Fatalf("exact cancel result = %+v", cancelled)
	}
	controller.releaseTurn()
}

func TestWireSteerApproveAndAnswerAreStrictAndIdempotent(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-prompt-mutations", "")
	target := daemonTestTarget()
	initial := subscribePeer(t, peer, target)
	epoch := initial.Snapshot.RuntimeEpoch
	submitted := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-wire-prompt-turn", target, epoch),
		Input:           "keep this Turn active", DisplayText: "keep this Turn active",
	})
	controller := factory.controller(t, 0)

	steerParams := protocol.TurnSteerParams{
		SessionMutation: mutation("request-wire-steer", target, epoch),
		ExpectedTurnID:  submitted.TurnID,
		Text:            "use the smaller approach",
	}
	steered := requestResult[protocol.TurnSteerResult](t, peer, protocol.MethodTurnSteer, steerParams)
	replayedSteer := requestResult[protocol.TurnSteerResult](t, peer, protocol.MethodTurnSteer, steerParams)
	if !steered.Accepted || steered.TurnID != submitted.TurnID || replayedSteer != steered {
		t.Fatalf("wire Steer results = first %+v replay %+v", steered, replayedSteer)
	}

	controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: "private-wire-approval", Tool: "bash", Subject: "go test ./...",
	}})
	approvalSubscription := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: initial.SubscriptionID,
	})
	approval := approvalSubscription.Snapshot.PendingPrompt
	if approval == nil || approval.Kind != protocol.PromptApproval || approval.Approval == nil || approval.Approval.PromptID == "" {
		t.Fatalf("wire Approval snapshot = %+v", approval)
	}
	approveParams := protocol.PromptApproveParams{
		SessionMutation: mutation("request-wire-approve", target, epoch),
		PromptID:        approval.Approval.PromptID,
		Decision:        protocol.DecisionAllowOnce,
	}
	approved := requestResult[protocol.PromptResolvedResult](t, peer, protocol.MethodPromptApprove, approveParams)
	replayedApproval := requestResult[protocol.PromptResolvedResult](t, peer, protocol.MethodPromptApprove, approveParams)
	if !approved.Resolved || approved.PromptID != approval.Approval.PromptID || replayedApproval != approved {
		t.Fatalf("wire Approval results = first %+v replay %+v", approved, replayedApproval)
	}

	controller.emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{
		ID: "private-wire-ask",
		Questions: []event.AskQuestion{{
			ID: "wire-choice", Prompt: "Choose one", Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}},
	}})
	askSubscription := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: target, PageTurns: 60,
		ReplaceSubscriptionID: approvalSubscription.SubscriptionID,
	})
	ask := askSubscription.Snapshot.PendingPrompt
	if ask == nil || ask.Kind != protocol.PromptAsk || ask.Ask == nil || ask.Ask.PromptID == "" {
		t.Fatalf("wire Ask snapshot = %+v", ask)
	}
	answerParams := protocol.PromptAnswerParams{
		SessionMutation: mutation("request-wire-answer", target, epoch),
		PromptID:        ask.Ask.PromptID,
		Answers:         []protocol.QuestionAnswer{{QuestionID: "wire-choice", Selected: []string{"B"}}},
	}
	answered := requestResult[protocol.PromptResolvedResult](t, peer, protocol.MethodPromptAnswer, answerParams)
	replayedAnswer := requestResult[protocol.PromptResolvedResult](t, peer, protocol.MethodPromptAnswer, answerParams)
	if !answered.Resolved || answered.PromptID != ask.Ask.PromptID || replayedAnswer != answered {
		t.Fatalf("wire Answer results = first %+v replay %+v", answered, replayedAnswer)
	}

	trySteerCalls, steers, approvals, answers := controller.promptMutationCalls()
	if trySteerCalls != 1 || !reflect.DeepEqual(steers, []string{"use the smaller approach"}) {
		t.Fatalf("wire Steer Controller calls = count %d values %v", trySteerCalls, steers)
	}
	if len(approvals) != 1 || approvals[0] != (daemonApprovalCall{ID: "private-wire-approval", Allow: true}) {
		t.Fatalf("wire Approval Controller calls = %+v", approvals)
	}
	wantAnswers := []daemonAnswerCall{{
		ID: "private-wire-ask", Answers: []event.AskAnswer{{QuestionID: "wire-choice", Selected: []string{"B"}}},
	}}
	if !reflect.DeepEqual(answers, wantAnswers) {
		t.Fatalf("wire Answer Controller calls = %+v", answers)
	}
	controller.releaseTurn()
}
