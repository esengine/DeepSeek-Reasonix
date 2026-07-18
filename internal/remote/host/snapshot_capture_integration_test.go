package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/sessiondisplay"
	"reasonix/internal/sessiontelemetry"
)

// snapshotTestController is intentionally stateful: these tests exercise the
// production Host barrier against Controller state that changes at admission
// and while a getter is blocked. Embedding SessionAPI keeps the fixture focused
// on the exact production surface used by SessionRuntime.
type snapshotTestController struct {
	control.SessionAPI

	mu   sync.Mutex
	ctx  context.Context
	sink event.Sink

	workspaceRoot string
	sessionPath   string
	sessionDir    string
	history       []provider.Message
	todos         []evidence.TodoItem
	usedTokens    int
	windowTokens  int
	lastUsage     *provider.Usage
	jobs          []jobs.View
	checkpoints   control.CheckpointSnapshot
	turn          int

	appendCurrentTurn bool
	panicHistory      bool
	panicWorkspace    bool
	panicSessionPath  bool
	closeCalls        int
	historyBlock      *snapshotGetterBlock
}

type snapshotGetterBlock struct {
	entered chan struct{}
	release chan struct{}
}

type snapshotTestFactory struct {
	controller *snapshotTestController
	created    atomic.Int32
}

func (f *snapshotTestFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	f.created.Add(1)
	f.controller.mu.Lock()
	f.controller.ctx = ctx
	f.controller.sink = sink
	f.controller.mu.Unlock()
	return f.controller, nil
}

func (c *snapshotTestController) WorkspaceRoot() string {
	c.mu.Lock()
	panicGetter := c.panicWorkspace
	value := c.workspaceRoot
	c.mu.Unlock()
	if panicGetter {
		panic("injected WorkspaceRoot getter failure")
	}
	return value
}

func (c *snapshotTestController) SessionPath() string {
	c.mu.Lock()
	panicGetter := c.panicSessionPath
	value := c.sessionPath
	c.mu.Unlock()
	if panicGetter {
		panic("injected SessionPath getter failure")
	}
	return value
}

func (c *snapshotTestController) SessionDir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionDir
}

func (c *snapshotTestController) Turn() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turn
}

func (c *snapshotTestController) History() []provider.Message {
	c.mu.Lock()
	panicHistory := c.panicHistory
	block := c.historyBlock
	c.historyBlock = nil
	history := cloneProviderMessages(c.history)
	c.mu.Unlock()
	if block != nil {
		close(block.entered)
		<-block.release
	}
	if panicHistory {
		panic("injected History getter failure")
	}
	return history
}

func (c *snapshotTestController) Todos() []evidence.TodoItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]evidence.TodoItem(nil), c.todos...)
}

func (c *snapshotTestController) ContextSnapshot() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usedTokens, c.windowTokens
}

func (c *snapshotTestController) LastUsage() *provider.Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastUsage == nil {
		return nil
	}
	copyValue := *c.lastUsage
	return &copyValue
}

func (c *snapshotTestController) Jobs() []jobs.View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]jobs.View(nil), c.jobs...)
}

func (c *snapshotTestController) CheckpointSnapshot() control.CheckpointSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneCheckpointSnapshot(c.checkpoints)
}

func (c *snapshotTestController) SubmitUserTurn(input, _ string) {
	c.mu.Lock()
	if c.appendCurrentTurn {
		c.history = append(c.history,
			provider.Message{Role: provider.RoleUser, Content: "composed:" + input},
			provider.Message{Role: provider.RoleAssistant, Content: "partial assistant"},
			provider.Message{Role: provider.RoleTool, Name: "read_file", Content: "partial tool"},
		)
	}
	c.turn++
	sink := c.sink
	c.mu.Unlock()
	// Synchronous enqueue establishes the deterministic admission ->
	// TurnStarted -> immediate subscribe actor ordering under test.
	sink.Emit(event.Event{Kind: event.TurnStarted})
}

func (c *snapshotTestController) Close() {
	c.mu.Lock()
	c.closeCalls++
	c.mu.Unlock()
}

func (c *snapshotTestController) emit(value event.Event) {
	c.mu.Lock()
	sink := c.sink
	c.mu.Unlock()
	sink.Emit(value)
}

func cloneCheckpointSnapshot(value control.CheckpointSnapshot) control.CheckpointSnapshot {
	result := control.CheckpointSnapshot{
		Metas:                 make([]checkpoint.Meta, len(value.Metas)),
		TurnsByMessageIndex:   make(map[int]int, len(value.TurnsByMessageIndex)),
		ConversationAvailable: make(map[int]bool, len(value.ConversationAvailable)),
	}
	for index, meta := range value.Metas {
		result.Metas[index] = meta
		result.Metas[index].Paths = append([]string(nil), meta.Paths...)
	}
	for index, turn := range value.TurnsByMessageIndex {
		result.TurnsByMessageIndex[index] = turn
	}
	for turn, available := range value.ConversationAvailable {
		result.ConversationAvailable[turn] = available
	}
	return result
}

func newSnapshotTestManager(t *testing.T, controller *snapshotTestController, opts RuntimeManagerOptions) *RuntimeManager {
	t.Helper()
	ids := &testIDSource{}
	if opts.NewRuntimeEpoch == nil {
		opts.NewRuntimeEpoch = ids.runtimeEpoch
	}
	if opts.NewTurnID == nil {
		opts.NewTurnID = ids.turnID
	}
	if opts.NewPromptID == nil {
		opts.NewPromptID = ids.promptID
	}
	if opts.NewCheckpointID == nil {
		opts.NewCheckpointID = func() (protocol.CheckpointID, error) {
			return protocol.CheckpointID(fmt.Sprintf("checkpoint-%d", ids.next.Add(1))), nil
		}
	}
	if opts.NewSubscriptionID == nil {
		opts.NewSubscriptionID = ids.subscriptionID
	}
	if opts.SubscriptionQueue == 0 {
		opts.SubscriptionQueue = 16
	}
	manager, err := NewRuntimeManager(context.Background(), "host-test", &snapshotTestFactory{controller: controller}, opts)
	if err != nil {
		t.Fatalf("NewRuntimeManager: %v", err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func captureSnapshotOnce(t *testing.T, manager *RuntimeManager, snapshotID protocol.SnapshotID) RuntimeSnapshot {
	t.Helper()
	install, err := manager.SubscribeSnapshot(context.Background(), testTarget(), testAttachment(1), "", snapshotID)
	if err != nil {
		t.Fatalf("SubscribeSnapshot(%q): %v", snapshotID, err)
	}
	snapshot := install.Subscription.Snapshot
	if err := install.Abort(); err != nil {
		t.Fatalf("Abort snapshot-only subscription: %v", err)
	}
	return snapshot
}

func TestBoundSnapshotImmediatelyAfterAcceptedSubmitUsesAdmissionPrefix(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	canonicalOld := "/goal --research old canonical"
	controller := &snapshotTestController{
		workspaceRoot:     dir,
		sessionPath:       sessionPath,
		sessionDir:        dir,
		appendCurrentTurn: true,
		history: []provider.Message{
			{Role: provider.RoleSystem, Content: "system"},
			{Role: provider.RoleUser, Content: canonicalOld},
			{Role: provider.RoleAssistant, Content: "old answer"},
		},
		checkpoints: control.CheckpointSnapshot{
			TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	if err := sessiondisplay.Record(dir, sessionPath, canonicalOld, "old visible text"); err != nil {
		t.Fatal(err)
	}
	manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{})
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-admission", ExpectedHostEpoch: "host-test", Target: testTarget(),
			ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		Input: "expanded model input", DisplayText: "visible composer text  ",
	}
	submitted, err := runtime.SubmitMutation(context.Background(), registry, params, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := captureSnapshotOnce(t, manager, "snapshot-admission")
	if snapshot.SnapshotID != "snapshot-admission" || snapshot.Capture.History.Binding.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("snapshot binding = %+v, snapshotId=%q", snapshot.Capture.History.Binding, snapshot.SnapshotID)
	}
	if snapshot.Capture.History.Binding.RuntimeEpoch != runtime.Epoch() || snapshot.Capture.History.Binding.Target != testTarget() {
		t.Fatalf("snapshot binding = %+v", snapshot.Capture.History.Binding)
	}
	if snapshot.BoundarySeq != 1 || !snapshot.Running || snapshot.CurrentTurn != submitted.TurnID {
		t.Fatalf("runtime boundary = seq %d running %v turn %q", snapshot.BoundarySeq, snapshot.Running, snapshot.CurrentTurn)
	}
	wantHistory := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: canonicalOld},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: params.Input},
	}
	if !reflect.DeepEqual(snapshot.Capture.History.Messages, wantHistory) {
		t.Fatalf("captured history = %#v, want admission prefix plus provisional %#v", snapshot.Capture.History.Messages, wantHistory)
	}
	if len(snapshot.Capture.History.Metadata) != 2 {
		t.Fatalf("history metadata = %+v", snapshot.Capture.History.Metadata)
	}
	oldDisplay := snapshot.Capture.History.Metadata[0].DisplayContent
	acceptedDisplay := snapshot.Capture.History.Metadata[1].DisplayContent
	if oldDisplay == nil || *oldDisplay != "old visible text" || acceptedDisplay == nil || *acceptedDisplay != params.DisplayText {
		t.Fatalf("history displays = old %v accepted %v", oldDisplay, acceptedDisplay)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].Event.Kind != "turn_started" {
		t.Fatalf("actor live snapshot = %+v", snapshot.Events)
	}
}

func TestBoundSnapshotBarrierExcludesEventsQueuedDuringGetterCapture(t *testing.T) {
	block := &snapshotGetterBlock{entered: make(chan struct{}), release: make(chan struct{})}
	controller := &snapshotTestController{
		historyBlock: block,
		checkpoints: control.CheckpointSnapshot{
			TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{})
	if _, err := manager.GetOrCreate(testTarget()); err != nil {
		t.Fatal(err)
	}

	type subscribeResult struct {
		install *SubscriptionInstall
		err     error
	}
	result := make(chan subscribeResult, 1)
	go func() {
		install, err := manager.SubscribeSnapshot(context.Background(), testTarget(), testAttachment(1), "", "snapshot-boundary")
		result <- subscribeResult{install: install, err: err}
	}()
	select {
	case <-block.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot History getter did not enter")
	}
	controller.emit(event.Event{Kind: event.Notice, Text: "queued-after-freeze"})
	close(block.release)

	var subscribed subscribeResult
	select {
	case subscribed = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("SubscribeSnapshot did not return")
	}
	if subscribed.err != nil {
		t.Fatal(subscribed.err)
	}
	defer func() {
		if err := subscribed.install.Abort(); err != nil {
			t.Errorf("Abort: %v", err)
		}
	}()
	snapshot := subscribed.install.Subscription.Snapshot
	if snapshot.BoundarySeq != 0 || len(snapshot.Events) != 0 {
		t.Fatalf("snapshot crossed actor boundary: seq=%d events=%+v", snapshot.BoundarySeq, snapshot.Events)
	}
	message := receiveMessage(t, subscribed.install.Subscription.Messages)
	if message.Event == nil || message.Event.Seq != snapshot.BoundarySeq+1 || message.Event.Event.Text != "queued-after-freeze" {
		t.Fatalf("first post-boundary notification = %+v", message)
	}
}

func TestBoundSnapshotFailuresAreTransactionalAndRestoreReplacement(t *testing.T) {
	controller := &snapshotTestController{
		checkpoints: control.CheckpointSnapshot{
			TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{})
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(1)
	initial, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, "", "snapshot-initial")
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(); err != nil {
		t.Fatal(err)
	}
	if activity, err := runtime.activity(context.Background()); err != nil || activity.subscriptions != 1 {
		t.Fatalf("initial activity = %+v, %v", activity, err)
	}

	controller.mu.Lock()
	controller.panicHistory = true
	controller.mu.Unlock()
	if _, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, initial.Subscription.ID, "snapshot-panic"); err == nil {
		t.Fatal("panicking getter produced a subscription")
	}
	if activity, err := runtime.activity(context.Background()); err != nil || activity.subscriptions != 1 {
		t.Fatalf("getter failure installed state: %+v, %v", activity, err)
	}

	// Reaching a projection error through the same replacement ID proves the
	// getter failure restored previous.migrating. This failure must restore it
	// again and must not install the generated subscription either.
	controller.mu.Lock()
	controller.panicHistory = false
	controller.usedTokens = -1
	controller.mu.Unlock()
	if _, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, initial.Subscription.ID, "snapshot-invalid"); err == nil {
		t.Fatal("invalid getter projection produced a subscription")
	}
	if activity, err := runtime.activity(context.Background()); err != nil || activity.subscriptions != 1 {
		t.Fatalf("projection failure installed state: %+v, %v", activity, err)
	}

	controller.mu.Lock()
	controller.usedTokens = 0
	controller.mu.Unlock()
	retry, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, initial.Subscription.ID, "snapshot-retry")
	if err != nil {
		t.Fatalf("replacement retry after failures: %v", err)
	}
	if activity, err := runtime.activity(context.Background()); err != nil || activity.subscriptions != 2 {
		t.Fatalf("pending replacement activity = %+v, %v", activity, err)
	}
	if err := retry.Abort(); err != nil {
		t.Fatal(err)
	}
	if activity, err := runtime.activity(context.Background()); err != nil || activity.subscriptions != 1 {
		t.Fatalf("replacement Abort did not restore old subscription: %+v, %v", activity, err)
	}
}

func TestRuntimeConstructionGetterPanicsDiscardControllerAndReleaseManager(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*snapshotTestController, bool)
	}{
		{
			name: "workspace root",
			inject: func(controller *snapshotTestController, value bool) {
				controller.panicWorkspace = value
			},
		},
		{
			name: "session path",
			inject: func(controller *snapshotTestController, value bool) {
				controller.panicSessionPath = value
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := &snapshotTestController{}
			controller.mu.Lock()
			test.inject(controller, true)
			controller.mu.Unlock()
			manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{})
			if _, err := manager.GetOrCreate(testTarget()); err == nil {
				t.Fatal("panicking construction getter created a runtime")
			}
			controller.mu.Lock()
			if controller.closeCalls != 1 {
				t.Fatalf("discard Close calls = %d, want 1", controller.closeCalls)
			}
			test.inject(controller, false)
			controller.mu.Unlock()
			if _, err := manager.GetOrCreate(testTarget()); err != nil {
				t.Fatalf("manager remained poisoned after construction failure: %v", err)
			}
		})
	}
}

func TestEmptySnapshotIDIsRejectedBeforeRuntimeConstruction(t *testing.T) {
	controller := &snapshotTestController{}
	factory := &snapshotTestFactory{controller: controller}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if _, err := manager.SubscribeSnapshot(context.Background(), testTarget(), testAttachment(1), "", " \t\n"); err == nil {
		t.Fatal("blank snapshotId was accepted")
	}
	if created := factory.created.Load(); created != 0 {
		t.Fatalf("blank snapshotId constructed %d Controllers", created)
	}
}

func TestCheckpointOpaqueIDsAreStableReservedAndNeverReused(t *testing.T) {
	created := time.Unix(10, 0)
	controller := &snapshotTestController{
		history: []provider.Message{{Role: provider.RoleUser, Content: "turn seven"}},
		checkpoints: control.CheckpointSnapshot{
			Metas:               []checkpoint.Meta{{Turn: 7, Time: created, Prompt: "turn seven", Paths: []string{"a.go"}}},
			TurnsByMessageIndex: map[int]int{0: 7}, ConversationAvailable: map[int]bool{7: true},
		},
	}
	var generatorMu sync.Mutex
	generated := []protocol.CheckpointID{"checkpoint-A", "checkpoint-A", "checkpoint-B", "checkpoint-C", "checkpoint-C", "checkpoint-D"}
	newCheckpointID := func() (protocol.CheckpointID, error) {
		generatorMu.Lock()
		defer generatorMu.Unlock()
		if len(generated) == 0 {
			return "", errors.New("checkpoint generator exhausted")
		}
		id := generated[0]
		generated = generated[1:]
		return id, nil
	}
	manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{NewCheckpointID: newCheckpointID})
	if _, err := manager.GetOrCreate(testTarget()); err != nil {
		t.Fatal(err)
	}

	first := captureSnapshotOnce(t, manager, "snapshot-checkpoint-1")
	second := captureSnapshotOnce(t, manager, "snapshot-checkpoint-2")
	if got := first.Capture.Checkpoints[0].CheckpointID; got != "checkpoint-A" {
		t.Fatalf("first checkpointId = %q", got)
	}
	if got := second.Capture.Checkpoints[0].CheckpointID; got != "checkpoint-A" {
		t.Fatalf("stable checkpointId = %q", got)
	}

	controller.mu.Lock()
	controller.checkpoints = control.CheckpointSnapshot{TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{}}
	controller.mu.Unlock()
	removed := captureSnapshotOnce(t, manager, "snapshot-checkpoint-removed")
	if len(removed.Capture.Checkpoints) != 0 {
		t.Fatalf("removed checkpoints = %+v", removed.Capture.Checkpoints)
	}

	controller.mu.Lock()
	controller.checkpoints = control.CheckpointSnapshot{
		Metas:               []checkpoint.Meta{{Turn: 7, Time: created, Prompt: "turn seven", Paths: []string{"a.go"}}},
		TurnsByMessageIndex: map[int]int{0: 7}, ConversationAvailable: map[int]bool{7: true},
	}
	controller.mu.Unlock()
	readded := captureSnapshotOnce(t, manager, "snapshot-checkpoint-readded")
	if got := readded.Capture.Checkpoints[0].CheckpointID; got != "checkpoint-B" {
		t.Fatalf("re-added checkpoint reused retired opaque ID: %q", got)
	}

	// Allocate checkpoint-C, then fail projection after ID reconciliation. The
	// actor mapping must stay at turn 7 only, while C remains globally reserved.
	controller.mu.Lock()
	controller.checkpoints = control.CheckpointSnapshot{
		Metas: []checkpoint.Meta{
			{Turn: 7, Time: created, Prompt: "turn seven", Paths: []string{"a.go"}},
			{Turn: 8, Time: created.Add(time.Second), Prompt: "turn eight", Paths: []string{"b.go"}},
		},
		TurnsByMessageIndex: map[int]int{0: 7, 99: 8}, ConversationAvailable: map[int]bool{7: true, 8: true},
	}
	controller.mu.Unlock()
	if _, err := manager.SubscribeSnapshot(context.Background(), testTarget(), testAttachment(1), "", "snapshot-checkpoint-failed"); err == nil {
		t.Fatal("invalid checkpoint boundary produced a snapshot")
	}

	controller.mu.Lock()
	controller.history = append(controller.history, provider.Message{Role: provider.RoleUser, Content: "turn eight"})
	controller.checkpoints.TurnsByMessageIndex = map[int]int{0: 7, 1: 8}
	controller.mu.Unlock()
	recovered := captureSnapshotOnce(t, manager, "snapshot-checkpoint-recovered")
	if len(recovered.Capture.Checkpoints) != 2 ||
		recovered.Capture.Checkpoints[0].CheckpointID != "checkpoint-B" ||
		recovered.Capture.Checkpoints[1].CheckpointID != "checkpoint-D" {
		t.Fatalf("checkpoint IDs after failed capture = %+v", recovered.Capture.Checkpoints)
	}
}

func TestTelemetryIsPersistedBeforeNotificationAndCapturedAtBoundary(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "telemetry-session.jsonl")
	controller := &snapshotTestController{
		workspaceRoot: dir,
		sessionPath:   sessionPath,
		sessionDir:    dir,
		turn:          3,
		checkpoints: control.CheckpointSnapshot{
			TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		},
	}
	var now atomic.Int64
	now.Store(900)
	manager := newSnapshotTestManager(t, controller, RuntimeManagerOptions{NowMillis: now.Load})
	if _, err := manager.GetOrCreate(testTarget()); err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(1)
	initial, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, "", "snapshot-telemetry-initial")
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(); err != nil {
		t.Fatal(err)
	}
	requireTelemetry := func(wantSeq uint64) sessiontelemetry.Snapshot {
		t.Helper()
		message := receiveMessage(t, initial.Subscription.Messages)
		if message.Event == nil || message.Event.Seq != wantSeq {
			t.Fatalf("telemetry notification = %+v, want seq %d", message, wantSeq)
		}
		if _, err := os.Stat(sessiontelemetry.Path(sessionPath)); err != nil {
			t.Fatalf("telemetry was not persisted before seq %d delivery: %v", wantSeq, err)
		}
		return sessiontelemetry.Load(sessiontelemetry.Path(sessionPath))
	}

	now.Store(1000)
	controller.emit(event.Event{Kind: event.TurnStarted})
	started := requireTelemetry(1)
	if started.Usage.ElapsedMs != 0 || started.Usage.RequestCount != 0 {
		t.Fatalf("TurnStarted telemetry = %+v", started.Usage)
	}

	now.Store(1100)
	controller.emit(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7,
			CacheHitTokens: 1, CacheMissTokens: 3, ReasoningTokens: 2,
		},
		UsageSource: event.UsageSourceExecutor, SessionHit: 2, SessionMiss: 3,
	})
	used := requireTelemetry(2)
	if used.Usage.RequestCount != 1 || used.Usage.TotalTokens != 7 || used.Usage.CacheHitTokens != 2 || used.Usage.CacheMissTokens != 3 || used.Usage.ElapsedMs != 100 {
		t.Fatalf("Usage telemetry = %+v", used.Usage)
	}

	now.Store(1200)
	readPath := filepath.Join(dir, "internal", "remote", "host", "runtime.go")
	controller.emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "tool-read", Name: "read_file",
		Args:   fmt.Sprintf(`{"path":%q,"offset":2,"limit":10}`, readPath),
		Output: "File truncated", Truncated: true,
	}})
	read := requireTelemetry(3)
	if len(read.ReadFiles) != 1 || read.ReadFiles[0].Path != "internal/remote/host/runtime.go" || read.ReadFiles[0].Turn != 3 || read.ReadFiles[0].Time != 1200 || !read.ReadFiles[0].Truncated {
		t.Fatalf("read-file telemetry = %+v", read.ReadFiles)
	}

	now.Store(1500)
	controller.emit(event.Event{Kind: event.TurnDone})
	done := requireTelemetry(4)
	if done.Usage.ElapsedMs != 500 || done.Usage.RequestCount != 1 || len(done.ReadFiles) != 1 {
		t.Fatalf("TurnDone telemetry = %+v", done)
	}

	replacement, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, initial.Subscription.ID, "snapshot-telemetry-final")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := replacement.Abort(); err != nil {
			t.Errorf("Abort: %v", err)
		}
	}()
	snapshot := replacement.Subscription.Snapshot
	if snapshot.BoundarySeq != 4 || snapshot.Capture.Context.RequestCount != 1 || snapshot.Capture.Context.TotalTokens != 7 || snapshot.Capture.Context.ElapsedMs != 500 {
		t.Fatalf("captured context = boundary %d %+v", snapshot.BoundarySeq, snapshot.Capture.Context)
	}
	if len(snapshot.Capture.Context.ReadFiles) != 1 || snapshot.Capture.Context.ReadFiles[0].Path != "internal/remote/host/runtime.go" {
		t.Fatalf("captured read files = %+v", snapshot.Capture.Context.ReadFiles)
	}
}
