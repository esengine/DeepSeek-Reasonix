package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/contentref"
	remotehistory "reasonix/internal/remote/history"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/sessiondisplay"
)

type snapshotWireController struct {
	*daemonFakeController

	viewMu         sync.Mutex
	workspaceRoot  string
	sessionPath    string
	history        []provider.Message
	todos          []evidence.TodoItem
	usedTokens     int
	windowTokens   int
	lastUsage      *provider.Usage
	jobs           []jobs.View
	checkpointView control.CheckpointSnapshot
}

func (c *snapshotWireController) WorkspaceRoot() string { return c.workspaceRoot }
func (c *snapshotWireController) SessionPath() string   { return c.sessionPath }
func (c *snapshotWireController) SessionDir() string    { return filepath.Dir(c.sessionPath) }

func (c *snapshotWireController) History() []provider.Message {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return cloneSnapshotWireMessages(c.history)
}

func (c *snapshotWireController) Todos() []evidence.TodoItem {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return append([]evidence.TodoItem(nil), c.todos...)
}

func (c *snapshotWireController) ContextSnapshot() (int, int) {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return c.usedTokens, c.windowTokens
}

func (c *snapshotWireController) LastUsage() *provider.Usage {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	if c.lastUsage == nil {
		return nil
	}
	copyUsage := *c.lastUsage
	return &copyUsage
}

func (c *snapshotWireController) Jobs() []jobs.View {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return append([]jobs.View(nil), c.jobs...)
}

func (c *snapshotWireController) CheckpointSnapshot() control.CheckpointSnapshot {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	metas := make([]checkpoint.Meta, len(c.checkpointView.Metas))
	for index, meta := range c.checkpointView.Metas {
		metas[index] = meta
		metas[index].Paths = append([]string(nil), meta.Paths...)
	}
	return control.CheckpointSnapshot{
		Metas:                 metas,
		TurnsByMessageIndex:   cloneIntMap(c.checkpointView.TurnsByMessageIndex),
		ConversationAvailable: cloneBoolMap(c.checkpointView.ConversationAvailable),
	}
}

func (c *snapshotWireController) setHistory(messages []provider.Message) {
	c.viewMu.Lock()
	c.history = cloneSnapshotWireMessages(messages)
	c.viewMu.Unlock()
}

func cloneSnapshotWireMessages(messages []provider.Message) []provider.Message {
	out := make([]provider.Message, len(messages))
	for index, message := range messages {
		out[index] = message
		out[index].Images = append([]string(nil), message.Images...)
		out[index].ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		out[index].MemoryCitations = append([]provider.MemoryCitation(nil), message.MemoryCitations...)
	}
	return out
}

func cloneIntMap(in map[int]int) map[int]int {
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBoolMap(in map[int]bool) map[int]bool {
	out := make(map[int]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type snapshotWireFactory struct {
	mu          sync.Mutex
	configure   func(protocol.RuntimeTarget, *snapshotWireController)
	controllers []*snapshotWireController
}

func (f *snapshotWireFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	controller := &snapshotWireController{
		daemonFakeController: newDaemonFakeController(ctx, sink),
		checkpointView: control.CheckpointSnapshot{
			Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	if f.configure != nil {
		f.configure(target, controller)
	}
	f.mu.Lock()
	f.controllers = append(f.controllers, controller)
	f.mu.Unlock()
	return controller, nil
}

func (f *snapshotWireFactory) controller(t *testing.T, index int) *snapshotWireController {
	t.Helper()
	deadline := time.Now().Add(testRequestTimeout)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if index < len(f.controllers) {
			controller := f.controllers[index]
			f.mu.Unlock()
			return controller
		}
		f.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("snapshot controller %d was not constructed", index)
	return nil
}

func newSnapshotWireServer(
	t *testing.T,
	now func() time.Time,
	configure func(protocol.RuntimeTarget, *snapshotWireController),
	mutateOptions func(*Options),
) (*Server, *snapshotWireFactory, protocol.BuildID) {
	t.Helper()
	options, _, buildID := daemonTestServerOptions(t, now)
	factory := &snapshotWireFactory{configure: configure}
	options.ControllerFactory = factory
	options.HistoryOptions = remotehistory.Options{Now: now, SweepInterval: -1}
	if mutateOptions != nil {
		mutateOptions(&options)
	}
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatalf("New snapshot wire server: %v", err)
	}
	t.Cleanup(server.Close)
	return server, factory, buildID
}

func richSnapshotHistory(large string) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "first prompt"},
		{Role: provider.RoleAssistant, Content: "first answer"},
		{Role: provider.RoleUser, Content: "second prompt"},
		{Role: provider.RoleAssistant, Content: large},
		{Role: provider.RoleUser, Content: "/reasonix-develop canonical third prompt"},
		{Role: provider.RoleAssistant, Content: "third answer"},
	}
}

func configureRichSnapshotController(t *testing.T, sessionPath, large string) func(protocol.RuntimeTarget, *snapshotWireController) {
	t.Helper()
	return func(_ protocol.RuntimeTarget, controller *snapshotWireController) {
		controller.workspaceRoot = filepath.Dir(sessionPath)
		controller.sessionPath = sessionPath
		controller.history = richSnapshotHistory(large)
		controller.todos = []evidence.TodoItem{{Content: "wire todo", Status: "in_progress", ActiveForm: "Wiring"}}
		controller.usedTokens = 123
		controller.windowTokens = 4096
		controller.lastUsage = &provider.Usage{PromptTokens: 70, CompletionTokens: 11, TotalTokens: 81, ReasoningTokens: 3, CacheHitTokens: 50, CacheMissTokens: 20}
		controller.jobs = []jobs.View{{ID: "job-wire", Kind: "bash", Label: "wire job", Status: "running", StartedAt: 1700000000000}}
		controller.checkpointView = control.CheckpointSnapshot{
			Metas:               []checkpoint.Meta{{Turn: 3, Time: time.Unix(1700000000, 0), Prompt: "third prompt", Paths: []string{"main.go"}}},
			TurnsByMessageIndex: map[int]int{4: 3}, ConversationAvailable: map[int]bool{3: true},
		}
	}
}

func historyParams(snapshot protocol.SessionSnapshot, cursor protocol.Cursor) protocol.SessionHistoryParams {
	return protocol.SessionHistoryParams{
		RuntimeQuery: protocol.RuntimeQuery{
			ExpectedHostEpoch: snapshot.HostEpoch, Target: snapshot.Target, ExpectedRuntimeEpoch: snapshot.RuntimeEpoch,
		},
		SnapshotID: snapshot.SnapshotID, Cursor: cursor, PageTurns: 1,
	}
}

func TestSubscribeProjectsFrozenCaptureAndPagesOwnedHistory(t *testing.T) {
	large := strings.Repeat("older-large-body-", 5000)
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := sessiondisplay.Record(filepath.Dir(sessionPath), sessionPath, "/reasonix-develop canonical third prompt", "visible third prompt"); err != nil {
		t.Fatal(err)
	}
	server, factory, buildID := newSnapshotWireServer(t, nil, configureRichSnapshotController(t, sessionPath, large), nil)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-snapshot", "")

	subscription := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
	})
	snapshot := subscription.Snapshot
	if snapshot.SnapshotID == "" || snapshot.History.SnapshotID != snapshot.SnapshotID || snapshot.History.TotalTurns != 3 ||
		snapshot.History.ActualTurns != 1 || !snapshot.History.HasOlder || snapshot.History.NextCursor == "" {
		t.Fatalf("initial history boundary = %+v", snapshot.History)
	}
	if len(snapshot.History.Messages) != 2 || snapshot.History.Messages[0].Role != "user" ||
		snapshot.History.Messages[0].Content == nil || *snapshot.History.Messages[0].Content != "visible third prompt" ||
		snapshot.History.Messages[0].SubmitText == nil || *snapshot.History.Messages[0].SubmitText != "/reasonix-develop canonical third prompt" {
		t.Fatalf("display-projected latest turn = %+v", snapshot.History.Messages)
	}
	if len(snapshot.Todos) != 1 || snapshot.Todos[0].Content == nil || *snapshot.Todos[0].Content != "wire todo" ||
		snapshot.Context.UsedTokens != 123 || snapshot.Context.PromptTokens != 70 || len(snapshot.Jobs) != 1 ||
		len(snapshot.Checkpoints) != 1 || snapshot.Checkpoints[0].CheckpointID == "" ||
		snapshot.History.Messages[0].CheckpointID != snapshot.Checkpoints[0].CheckpointID {
		t.Fatalf("captured state was not projected: todos=%+v context=%+v jobs=%+v checkpoints=%+v history=%+v",
			snapshot.Todos, snapshot.Context, snapshot.Jobs, snapshot.Checkpoints, snapshot.History.Messages)
	}

	older := requestResult[protocol.HistoryPage](t, peer, protocol.MethodSessionHistory, historyParams(snapshot, snapshot.History.NextCursor))
	if older.SnapshotID != snapshot.SnapshotID || older.StartTurn != 1 || older.EndTurn != 2 || older.ActualTurns != 1 || len(older.Externalized) != 1 {
		t.Fatalf("older page = %+v", older)
	}
	if got := string(readRemoteContent(t, peer, older.Externalized[0])); got != large {
		t.Fatalf("older contentRef body length=%d, want %d", len(got), len(large))
	}

	wrongCases := []struct {
		name   string
		mutate func(*protocol.SessionHistoryParams)
	}{
		{"host", func(params *protocol.SessionHistoryParams) { params.ExpectedHostEpoch = "wrong-host" }},
		{"target", func(params *protocol.SessionHistoryParams) { params.Target.SessionID = "wrong-session" }},
		{"runtime", func(params *protocol.SessionHistoryParams) { params.ExpectedRuntimeEpoch = "wrong-runtime" }},
		{"snapshot", func(params *protocol.SessionHistoryParams) { params.SnapshotID = "wrong-snapshot" }},
		{"cursor", func(params *protocol.SessionHistoryParams) { params.Cursor = "wrong-cursor" }},
	}
	for _, test := range wrongCases {
		t.Run("mismatch_"+test.name, func(t *testing.T) {
			params := historyParams(snapshot, snapshot.History.NextCursor)
			test.mutate(&params)
			requireRemoteError(t, requestError(t, peer, protocol.MethodSessionHistory, params), protocol.ErrSnapshotExpired)
		})
	}
	invalidPage := historyParams(snapshot, snapshot.History.NextCursor)
	invalidPage.PageTurns = 0
	if response := requestError(t, peer, protocol.MethodSessionHistory, invalidPage); response.Code != rpcwire.ErrInvalidParams {
		t.Fatalf("invalid pageTurns code = %d", response.Code)
	}

	submitted := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: mutation("request-provisional", snapshot.Target, snapshot.RuntimeEpoch),
		Input:           "raw provisional input", DisplayText: "raw provisional input",
	})
	resubscribed := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: snapshot.HostEpoch, Target: snapshot.Target, PageTurns: 20,
		ReplaceSubscriptionID: subscription.SubscriptionID,
	})
	if !resubscribed.Snapshot.Runtime.Running || resubscribed.Snapshot.Runtime.CurrentTurn == nil ||
		resubscribed.Snapshot.Runtime.CurrentTurn.TurnID != submitted.TurnID || resubscribed.Snapshot.History.TotalTurns != 4 {
		t.Fatalf("accepted Turn snapshot = %+v", resubscribed.Snapshot)
	}
	var provisional *protocol.HistoryMessage
	for index := range resubscribed.Snapshot.History.Messages {
		message := &resubscribed.Snapshot.History.Messages[index]
		if message.Role == "user" {
			provisional = message
		}
	}
	if provisional == nil || provisional.Content == nil || *provisional.Content != "raw provisional input" || provisional.Pending {
		t.Fatalf("provisional history message = %+v", provisional)
	}
	factory.controller(t, 0).releaseTurn()
}

func TestHistoryOwnerExpiresByTTLAndCapacity(t *testing.T) {
	t.Run("ttl", func(t *testing.T) {
		clock := &daemonTestClock{now: time.Unix(20000, 0)}
		large := strings.Repeat("ttl-large-", 8000)
		sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
		server, _, buildID := newSnapshotWireServer(t, clock.Now, configureRichSnapshotController(t, sessionPath, large), func(options *Options) {
			options.HistoryOptions = remotehistory.Options{Now: clock.Now, SnapshotTTL: time.Second, SweepInterval: -1}
		})
		peer := openDaemonPeer(t, server, nil, nil)
		initializePeer(t, peer, buildID, "client-ttl", "")
		subscription := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
		})
		older := requestResult[protocol.HistoryPage](t, peer, protocol.MethodSessionHistory, historyParams(subscription.Snapshot, subscription.Snapshot.History.NextCursor))
		if len(older.Externalized) != 1 {
			t.Fatalf("TTL older descriptors = %+v", older.Externalized)
		}
		clock.Advance(2 * time.Second)
		requireRemoteError(t, requestError(t, peer, protocol.MethodSessionHistory,
			historyParams(subscription.Snapshot, subscription.Snapshot.History.NextCursor)), protocol.ErrSnapshotExpired)
		requireRemoteError(t, requestError(t, peer, protocol.MethodSessionContent,
			protocol.SessionContentParams{ContentRef: older.Externalized[0].ContentRef}), protocol.ErrContentRefExpired)
		if stats := server.histories.Stats(); stats.Snapshots != 0 {
			t.Fatalf("expired histories retained: %+v", stats)
		}
	})

	t.Run("capacity", func(t *testing.T) {
		large := strings.Repeat("capacity-large-", 6000)
		sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
		server, _, buildID := newSnapshotWireServer(t, nil, configureRichSnapshotController(t, sessionPath, large), func(options *Options) {
			options.HistoryOptions = remotehistory.Options{MaxSnapshots: 1, SweepInterval: -1}
		})
		peer := openDaemonPeer(t, server, nil, nil)
		initializePeer(t, peer, buildID, "client-capacity", "")
		first := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
		})
		older := requestResult[protocol.HistoryPage](t, peer, protocol.MethodSessionHistory, historyParams(first.Snapshot, first.Snapshot.History.NextCursor))
		secondTarget := protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-capacity-second"}
		requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-test", Target: secondTarget, PageTurns: 1,
		})
		requireRemoteError(t, requestError(t, peer, protocol.MethodSessionHistory,
			historyParams(first.Snapshot, first.Snapshot.History.NextCursor)), protocol.ErrSnapshotExpired)
		requireRemoteError(t, requestError(t, peer, protocol.MethodSessionContent,
			protocol.SessionContentParams{ContentRef: older.Externalized[0].ContentRef}), protocol.ErrContentRefExpired)
		if stats := server.histories.Stats(); stats.Snapshots != 1 {
			t.Fatalf("capacity histories = %+v", stats)
		}
	})
}

func TestSubscribeOwnerFailureRollsBackAndRetainsPreviousOwner(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	short := richSnapshotHistory("short second answer")
	internalDiagnostics := make(chan string, 8)
	server, factory, buildID := newSnapshotWireServer(t, nil, func(_ protocol.RuntimeTarget, controller *snapshotWireController) {
		controller.sessionPath = sessionPath
		controller.workspaceRoot = filepath.Dir(sessionPath)
		controller.history = cloneSnapshotWireMessages(short)
	}, func(options *Options) {
		options.ContentRefOptions = contentref.Config{MaxBytes: 1024}
		options.OnInternalError = func(method protocol.Method, err error) {
			internalDiagnostics <- string(method) + ": " + err.Error()
		}
	})
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-owner-rollback", "")
	first := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
	})
	controller := factory.controller(t, 0)
	tooLarge := richSnapshotHistory(strings.Repeat("cannot-retain-", 7000))
	controller.setHistory(tooLarge)
	failed := requestError(t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: first.Snapshot.Target, PageTurns: 20,
		ReplaceSubscriptionID: first.SubscriptionID,
	})
	if failed.Code != rpcwire.ErrInternal {
		t.Fatalf("owner projection failure code = %d, want internal", failed.Code)
	}
	if stats := server.histories.Stats(); stats.Snapshots != 1 {
		t.Fatalf("failed replacement leaked or lost history: %+v", stats)
	}
	requestResult[protocol.HistoryPage](t, peer, protocol.MethodSessionHistory,
		historyParams(first.Snapshot, first.Snapshot.History.NextCursor))
	for len(internalDiagnostics) > 0 {
		<-internalDiagnostics
	}

	controller.setHistory(short)
	retryParams := protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: first.Snapshot.Target, PageTurns: 1,
		ReplaceSubscriptionID: first.SubscriptionID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	raw, retryErr := peer.wire.Request(ctx, string(protocol.MethodSessionSubscribe), retryParams)
	cancel()
	if retryErr != nil {
		select {
		case diagnostic := <-internalDiagnostics:
			t.Fatalf("retry subscribe: %v; internal diagnostic: %s", retryErr, diagnostic)
		default:
			t.Fatalf("retry subscribe: %v; no internal diagnostic", retryErr)
		}
	}
	var retry protocol.SessionSubscribeResult
	if err := json.Unmarshal(raw, &retry); err != nil {
		t.Fatal(err)
	}
	if retry.SubscriptionID == first.SubscriptionID || retry.Snapshot.SnapshotID == first.Snapshot.SnapshotID {
		t.Fatalf("retry did not install a fresh owner: first=%+v retry=%+v", first, retry)
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionHistory,
		historyParams(first.Snapshot, first.Snapshot.History.NextCursor)), protocol.ErrSnapshotExpired)
}

func TestSubscribeHostCommitFailureRestoresTransportAndPreviousOwner(t *testing.T) {
	var commitCalls atomic.Int32
	server, factory, buildID := newSnapshotWireServer(t, nil, func(_ protocol.RuntimeTarget, controller *snapshotWireController) {
		controller.sessionPath = filepath.Join(t.TempDir(), "session.jsonl")
		controller.history = richSnapshotHistory("short owner")
	}, func(options *Options) {
		options.commitSubscription = func(install *host.SubscriptionInstall) error {
			if commitCalls.Add(1) == 2 {
				return errors.New("injected Host subscription commit failure")
			}
			return install.Commit()
		}
	})
	events := make(chan protocol.SessionEvent, 4)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
			var envelope protocol.SessionEvent
			if json.Unmarshal(raw, &envelope) == nil {
				events <- envelope
			}
		})
	})
	initializePeer(t, peer, buildID, "client-host-commit-rollback", "")
	first := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
	})
	failed := requestError(t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: first.Snapshot.Target, PageTurns: 1,
		ReplaceSubscriptionID: first.SubscriptionID,
	})
	if failed.Code != rpcwire.ErrInternal {
		t.Fatalf("Host commit failure code = %d, want internal", failed.Code)
	}
	if stats := server.histories.Stats(); stats.Snapshots != 1 {
		t.Fatalf("Host commit rollback owner count = %+v", stats)
	}
	requestResult[protocol.HistoryPage](t, peer, protocol.MethodSessionHistory,
		historyParams(first.Snapshot, first.Snapshot.History.NextCursor))

	controller := factory.controller(t, 0)
	controller.emit(event.Event{Kind: event.Notice, Text: "old-pump-after-commit-rollback"})
	select {
	case envelope := <-events:
		if envelope.SubscriptionID != first.SubscriptionID || envelope.Event.Text != "old-pump-after-commit-rollback" {
			t.Fatalf("restored old pump event = %+v", envelope)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("old event pump was not restored after Host commit failure")
	}

	retry := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: first.Snapshot.Target, PageTurns: 1,
		ReplaceSubscriptionID: first.SubscriptionID,
	})
	if retry.SubscriptionID == first.SubscriptionID || commitCalls.Load() != 3 {
		t.Fatalf("Host commit retry = %+v; calls=%d", retry, commitCalls.Load())
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionHistory,
		historyParams(first.Snapshot, first.Snapshot.History.NextCursor)), protocol.ErrSnapshotExpired)
}

func TestSessionHistoryStrictLeaseAndNoCrossTransportOwner(t *testing.T) {
	clock := &daemonTestClock{now: time.Unix(30000, 0)}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	server, _, buildID := newSnapshotWireServer(t, clock.Now, configureRichSnapshotController(t, sessionPath, "large"), nil)
	old := openDaemonPeer(t, server, nil, nil)
	oldGrant := initializePeer(t, old, buildID, "client-old-history", "")
	subscription := requestResult[protocol.SessionSubscribeResult](t, old, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
	})
	clock.Advance(time.Duration(protocol.LeaseTTLMillis) * time.Millisecond)
	fresh := openDaemonPeer(t, server, nil, nil)
	freshGrant := initializePeer(t, fresh, buildID, "client-fresh-history", "")
	if freshGrant.Lease.LeaseID == oldGrant.Lease.LeaseID {
		t.Fatal("expired lease was reused")
	}
	requireRemoteError(t, requestError(t, old, protocol.MethodSessionHistory,
		historyParams(subscription.Snapshot, subscription.Snapshot.History.NextCursor)), protocol.ErrLeaseNotHeld)
	requireRemoteError(t, requestError(t, fresh, protocol.MethodSessionHistory,
		historyParams(subscription.Snapshot, subscription.Snapshot.History.NextCursor)), protocol.ErrSnapshotExpired)
}

func TestSubscribeResponseWriteFailureAndDisconnectReleaseOwners(t *testing.T) {
	newServer := func(t *testing.T) (*Server, protocol.BuildID) {
		t.Helper()
		latestLarge := strings.Repeat("latest-owner-body-", 5000)
		server, _, buildID := newSnapshotWireServer(t, nil, func(_ protocol.RuntimeTarget, controller *snapshotWireController) {
			controller.sessionPath = filepath.Join(t.TempDir(), "session.jsonl")
			controller.history = richSnapshotHistory("older answer")
			controller.history[len(controller.history)-1].Content = latestLarge
		}, nil)
		return server, buildID
	}
	waitReleased := func(t *testing.T, server *Server) {
		t.Helper()
		deadline := time.Now().Add(testRequestTimeout)
		for time.Now().Before(deadline) {
			if server.histories.Stats().Snapshots == 0 && server.contents.Stats().Entries == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("snapshot owners remained: history=%+v content=%+v", server.histories.Stats(), server.contents.Stats())
	}

	t.Run("response_write_failure", func(t *testing.T) {
		server, buildID := newServer(t)
		dropped := make(chan struct{})
		peer := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
			return &daemonDropMatchingResponseConn{Conn: connection, match: []byte(`"subscriptionId"`), dropped: dropped}
		}, nil)
		initializePeer(t, peer, buildID, "client-write-failure", "")
		ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
		_, err := peer.wire.Request(ctx, string(protocol.MethodSessionSubscribe), protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
		})
		cancel()
		if err == nil {
			t.Fatal("subscribe unexpectedly survived response write failure")
		}
		select {
		case <-dropped:
		case <-time.After(testRequestTimeout):
			t.Fatal("subscription response was not dropped")
		}
		peer.close(t)
		waitReleased(t, server)
	})

	t.Run("transport_disconnect", func(t *testing.T) {
		server, buildID := newServer(t)
		peer := openDaemonPeer(t, server, nil, nil)
		initializePeer(t, peer, buildID, "client-disconnect-owner", "")
		subscription := requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
		})
		if len(subscription.Snapshot.Externalized) == 0 || server.histories.Stats().Snapshots != 1 || server.contents.Stats().Entries == 0 {
			t.Fatalf("subscription did not retain both owners: snapshot=%+v history=%+v content=%+v",
				subscription.Snapshot.Externalized, server.histories.Stats(), server.contents.Stats())
		}
		peer.close(t)
		waitReleased(t, server)
	})
}

func TestSubscribeResponsePrecedesQueuedBoundaryEvent(t *testing.T) {
	options, factory, buildID := daemonTestServerOptions(t, nil)
	metadata := options.Metadata
	metadataEntered := make(chan struct{})
	releaseMetadata := make(chan struct{})
	options.Metadata = func(ctx context.Context, target protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
		close(metadataEntered)
		select {
		case <-releaseMetadata:
			return metadata(ctx, target)
		case <-ctx.Done():
			return protocol.SessionMetaSnapshot{}, ctx.Err()
		}
	}
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	serverConn, clientConn := net.Pipe()
	deadline := time.Now().Add(testRequestTimeout)
	_ = serverConn.SetDeadline(deadline)
	_ = clientConn.SetDeadline(deadline)
	done := make(chan error, 1)
	go func() { done <- server.ServeConn(serverConn) }()
	reader := bufio.NewReader(clientConn)
	encoder := json.NewEncoder(clientConn)
	writeRawRequest := func(id int, method protocol.Method, params any) {
		t.Helper()
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
	}
	readFrame := func() map[string]json.RawMessage {
		t.Helper()
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read raw frame: %v", err)
		}
		var frame map[string]json.RawMessage
		if err := json.Unmarshal(line, &frame); err != nil {
			t.Fatalf("decode raw frame: %v\n%s", err, line)
		}
		return frame
	}

	writeRawRequest(1, protocol.MethodRemoteInitialize, protocol.InitializeParams{BuildID: buildID, ClientInstanceID: "client-order"})
	if frame := readFrame(); string(frame["id"]) != "1" || len(frame["result"]) == 0 {
		t.Fatalf("initialize frame = %s", mustJSON(frame))
	}
	writeRawRequest(2, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: daemonTestTarget(), PageTurns: 1,
	})
	select {
	case <-metadataEntered:
	case <-time.After(testRequestTimeout):
		t.Fatal("subscribe did not reach metadata projection")
	}
	controller := factory.controller(t, 0)
	controller.emit(event.Event{Kind: event.Notice, Text: "queued-after-boundary"})
	runtime, ok := server.runtimes.Runtime(daemonTestTarget())
	if !ok {
		t.Fatal("runtime missing during response-order test")
	}
	actorSnapshot, err := runtime.Snapshot(context.Background())
	if err != nil || actorSnapshot.BoundarySeq == 0 {
		t.Fatalf("queued event was not actor-ordered: snapshot=%+v err=%v", actorSnapshot, err)
	}
	close(releaseMetadata)

	response := readFrame()
	if string(response["id"]) != "2" || len(response["result"]) == 0 || len(response["method"]) != 0 {
		t.Fatalf("first subscribe frame was not the response: %s", mustJSON(response))
	}
	notification := readFrame()
	var method protocol.Method
	if err := json.Unmarshal(notification["method"], &method); err != nil || method != protocol.MethodSessionEvent || len(notification["id"]) != 0 {
		t.Fatalf("second subscribe frame was not the queued event: %s", mustJSON(notification))
	}
	_ = clientConn.Close()
	select {
	case serveErr := <-done:
		if serveErr != nil && !errors.Is(serveErr, net.ErrClosed) {
			// net.Pipe commonly reports the peer close through the rpcwire read
			// path. The exact wrapper is transport-only and not a protocol result.
			var opErr *net.OpError
			if !errors.As(serveErr, &opErr) {
				t.Fatalf("ServeConn: %v", serveErr)
			}
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("raw ordering transport did not stop")
	}
}

func mustJSON(value any) string {
	body, _ := json.Marshal(value)
	return string(body)
}
