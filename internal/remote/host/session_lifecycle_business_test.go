package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeservice"
)

type lifecycleProfileResolver struct{}

func (lifecycleProfileResolver) ResolveProfile(_ context.Context, _ string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
	profile := protocol.ResolvedProfile{
		Model: "test/model-a", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	if selection.Model != nil {
		profile.Model = strings.TrimSpace(*selection.Model)
	}
	if selection.Effort != nil {
		profile.Effort = strings.TrimSpace(*selection.Effort)
	}
	if selection.CollaborationMode != nil {
		profile.CollaborationMode = *selection.CollaborationMode
	}
	if selection.TokenMode != nil {
		profile.TokenMode = *selection.TokenMode
	}
	if selection.ToolApprovalMode != nil {
		profile.ToolApprovalMode = *selection.ToolApprovalMode
	}
	return profile, nil
}

type lifecycleHostFactory struct {
	catalog  *catalog.Catalog
	mu       sync.Mutex
	byTarget map[protocol.RuntimeTarget]*lifecycleHostController
	calls    int
	failAt   int
}

func (f *lifecycleHostFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if f.failAt != 0 && call == f.failAt {
		return nil, errors.New("injected controller build failure")
	}
	resolved, err := f.catalog.ResolveRuntimeTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	controller := &lifecycleHostController{
		fakeSessionController: newFakeSessionController(ctx, sink), resolved: resolved,
		goalStatus: string(protocol.GoalStopped),
		checkpointState: control.CheckpointSnapshot{
			Metas:               []checkpoint.Meta{{Turn: 3, Time: time.Now().UTC(), Prompt: "checkpoint", Paths: []string{"file.txt"}}},
			TurnsByMessageIndex: map[int]int{0: 3}, ConversationAvailable: map[int]bool{3: true},
		},
	}
	f.mu.Lock()
	f.byTarget[target] = controller
	f.mu.Unlock()
	return controller, nil
}

func (f *lifecycleHostFactory) controller(target protocol.RuntimeTarget) *lifecycleHostController {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byTarget[target]
}

func (f *lifecycleHostFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type lifecycleHostController struct {
	*fakeSessionController
	resolved catalog.ResolvedSession

	domainMu         sync.Mutex
	checkpointState  control.CheckpointSnapshot
	forkCalls        int
	branchCalls      int
	lastForkPath     string
	rewindCalls      int
	rewindResult     control.RewindResult
	rewindErr        error
	dropCheckpoints  bool
	goal             string
	goalStatus       string
	autoApprovalIDs  []string
	planMode         bool
	toolApprovalMode string
	forkSequence     atomic.Uint64
}

func (c *lifecycleHostController) WorkspaceRoot() string { return c.resolved.WorkspaceRoot }
func (c *lifecycleHostController) SessionPath() string   { return c.resolved.SessionPath }
func (c *lifecycleHostController) SessionDir() string    { return c.resolved.SessionDir }
func (c *lifecycleHostController) History() []provider.Message {
	return []provider.Message{{Role: provider.RoleUser, Content: "checkpoint turn"}}
}

func (c *lifecycleHostController) CheckpointSnapshot() control.CheckpointSnapshot {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	return control.CheckpointSnapshot{
		Metas:                 append([]checkpoint.Meta(nil), c.checkpointState.Metas...),
		TurnsByMessageIndex:   cloneTestIntMap(c.checkpointState.TurnsByMessageIndex),
		ConversationAvailable: cloneTestBoolMap(c.checkpointState.ConversationAvailable),
	}
}

func (c *lifecycleHostController) ForkSession(turn int, name string) (string, error) {
	c.domainMu.Lock()
	c.forkCalls++
	c.domainMu.Unlock()
	path := filepath.Join(c.resolved.SessionDir, fmt.Sprintf("fork-%d.jsonl", c.forkSequence.Add(1)))
	c.domainMu.Lock()
	c.lastForkPath = path
	c.domainMu.Unlock()
	if err := agent.NewSession("system").Save(path); err != nil {
		return "", err
	}
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{Name: name, ParentID: agent.BranchID(c.resolved.SessionPath), ForkTurn: turn}); err != nil {
		return "", err
	}
	return path, nil
}

func (c *lifecycleHostController) BranchSession(name string) (string, error) {
	c.domainMu.Lock()
	c.branchCalls++
	c.domainMu.Unlock()
	path := filepath.Join(c.resolved.SessionDir, fmt.Sprintf("branch-%d.jsonl", c.forkSequence.Add(1)))
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "checkpoint turn"})
	if err := session.Save(path); err != nil {
		return "", err
	}
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		Name: name, ParentID: agent.BranchID(c.resolved.SessionPath), ForkTurn: -1, ForkMessageIndex: 1,
	}); err != nil {
		return "", err
	}
	return path, nil
}

func (c *lifecycleHostController) RewindDetailed(_ int, _ control.RewindScope) (control.RewindResult, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.rewindCalls++
	if c.dropCheckpoints {
		c.checkpointState = control.CheckpointSnapshot{Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{}}
	}
	return c.rewindResult, c.rewindErr
}

func (c *lifecycleHostController) SetGoal(goal string) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.goal = strings.TrimSpace(goal)
	if c.goal == "" {
		c.goalStatus = string(protocol.GoalStopped)
	} else {
		c.goalStatus = string(protocol.GoalRunning)
	}
}
func (c *lifecycleHostController) ClearGoal() { c.SetGoal("") }
func (c *lifecycleHostController) Goal() string {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	return c.goal
}
func (c *lifecycleHostController) GoalStatus() string {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	return c.goalStatus
}
func (c *lifecycleHostController) ResumeGoal() bool {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	if c.goal == "" || (c.goalStatus != string(protocol.GoalBlocked) && c.goalStatus != string(protocol.GoalStopped)) {
		return false
	}
	c.goalStatus = string(protocol.GoalRunning)
	return true
}
func (c *lifecycleHostController) SetPlanMode(value bool) {
	c.domainMu.Lock()
	c.planMode = value
	c.domainMu.Unlock()
}
func (c *lifecycleHostController) SetToolApprovalMode(mode string) {
	c.domainMu.Lock()
	c.toolApprovalMode = mode
	c.domainMu.Unlock()
}
func (c *lifecycleHostController) ApplyToolApprovalMode(mode string) []string {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.toolApprovalMode = mode
	return append([]string(nil), c.autoApprovalIDs...)
}

type lifecycleHostFixture struct {
	catalog *catalog.Catalog
	manager *RuntimeManager
	factory *lifecycleHostFactory
	service SessionLifecycleService
	source  catalog.CreatedSession
}

func newLifecycleHostFixture(t *testing.T, failAt int, removeAll func(string) error) *lifecycleHostFixture {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	sessionDir := filepath.Join(root, "sessions")
	for _, path := range []string{workspace, sessionDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var next atomic.Uint64
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: filepath.Join(root, "catalog"), UserHome: root,
		SessionDir: func(string) string { return sessionDir }, ProfileResolver: lifecycleProfileResolver{},
		NewOpaqueID: func(kind string) (string, error) { return fmt.Sprintf("%s-%d", kind, next.Add(1)), nil },
		RemoveAll:   removeAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	browse, err := catalogValue.Browse(protocol.WorkspaceBrowseParams{ExpectedHostEpoch: "host-test", TypedPath: workspace})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := catalogValue.OpenWorkspace(protocol.WorkspaceOpenParams{HostMutation: protocol.HostMutation{RequestID: "open", ExpectedHostEpoch: "host-test"}, PrimaryDirectoryRef: browse.Directory.DirectoryRef})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := catalogValue.CreateTopic(protocol.TopicCreateParams{HostMutation: protocol.HostMutation{RequestID: "topic", ExpectedHostEpoch: "host-test"}, WorkspaceID: opened.Workspace.WorkspaceID, Title: "Lifecycle"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := catalogValue.CreateSession(context.Background(), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "create", ExpectedHostEpoch: "host-test"}, WorkspaceID: opened.Workspace.WorkspaceID,
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Topic: protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: topic.TopicID}, Profile: protocol.ProfileSelection{},
	})
	if err != nil {
		t.Fatal(err)
	}
	factory := &lifecycleHostFactory{catalog: catalogValue, byTarget: make(map[protocol.RuntimeTarget]*lifecycleHostController), failAt: failAt}
	ids := &testIDSource{}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch, NewTurnID: ids.turnID, NewOperationID: ids.operationID,
		NewPromptID: ids.promptID, NewSubscriptionID: ids.subscriptionID, SubscriptionQueue: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	catalogValue.SetRuntimeInspector(manager)
	requests, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return &lifecycleHostFixture{
		catalog: catalogValue, manager: manager, factory: factory, source: created,
		service: SessionLifecycleService{Runtimes: manager, Catalog: catalogValue, Requests: requests},
	}
}

func TestSessionNewMigratesSubscriptionAndLostResponseReplayDoesNotDuplicateIdentity(t *testing.T) {
	f := newLifecycleHostFixture(t, 0, nil)
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(1)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionNewParams{SessionMutation: mutationEnvelope(runtime, "new-request")}
	result, err := f.service.New(context.Background(), params, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target == result.SourceTarget || !result.SnapshotRequired || f.factory.count() != 2 {
		t.Fatalf("session/new = %+v factory=%d", result, f.factory.count())
	}
	terminal := receiveMessage(t, subscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncTargetReplaced || terminal.Resync.ReplacementTarget == nil || *terminal.Resync.ReplacementTarget != result.Target {
		t.Fatalf("new migration = %+v", terminal)
	}
	replay, err := f.service.New(context.Background(), params, nil)
	if err != nil || replay != result || f.factory.count() != 2 {
		t.Fatalf("new replay = %+v, %v factory=%d", replay, err, f.factory.count())
	}
	listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: "host-test", WorkspaceID: result.Target.WorkspaceID})
	if err != nil || len(listed.Items) != 2 {
		t.Fatalf("new list = %+v, %v", listed, err)
	}
	install, err := f.manager.Subscribe(context.Background(), result.Target, attachment, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := install.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionClearPermanentlyRetiresOldIdentityAndCachesCleanupPending(t *testing.T) {
	var oldPath string
	f := newLifecycleHostFixture(t, 0, func(path string) error {
		if oldPath != "" && filepath.Clean(path) == filepath.Clean(oldPath) {
			return errors.New("injected cleanup failure")
		}
		return os.RemoveAll(path)
	})
	oldPath = f.source.SessionPath
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionClearParams{SessionMutation: mutationEnvelope(runtime, "clear-request")}
	result, err := f.service.Clear(context.Background(), params, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != protocol.SessionCleanupPending || result.Target == result.PreviousTarget {
		t.Fatalf("session/clear = %+v", result)
	}
	if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), f.source.Target); err == nil {
		t.Fatal("cleared identity resolved")
	}
	if !agent.IsCleanupPending(oldPath) {
		t.Fatal("clear cleanup failure lost durable marker")
	}
	replay, err := f.service.Clear(context.Background(), params, nil)
	if err != nil || replay != result || f.factory.count() != 2 {
		t.Fatalf("clear replay = %+v, %v factory=%d", replay, err, f.factory.count())
	}
}

func TestDelegatedComposerLifecycleUsesOriginalSubmitClaimAndResultUnion(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-new"), Input: "/new", DisplayText: "/new",
		}
		route := runtimeservice.ComposerRoute{Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleNew, Input: params.Input}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectSessionReplaced ||
			!result.SnapshotRequired || result.Target == params.Target || result.RuntimeEpoch == "" {
			t.Fatalf("raw /new = %+v, %v", result, err)
		}
		assertRawComposerOutcome(t, f.service.Requests, params, result)
		assertDerivedLifecycleRequestConflicts(t, f.service.Requests, params.RequestID, params.Target,
			protocol.MethodSessionNew, protocol.SessionNewParams{SessionMutation: params.SessionMutation})
		replay, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || replay != result || f.factory.count() != 2 {
			t.Fatalf("raw /new replay = %+v, %v factory=%d", replay, err, f.factory.count())
		}
	})

	t.Run("clear", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-clear"), Input: "/clear", DisplayText: "/clear",
		}
		route := runtimeservice.ComposerRoute{Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleClear, Input: params.Input}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectSessionReplaced ||
			!result.SnapshotRequired || result.Target == params.Target || result.RuntimeEpoch == "" {
			t.Fatalf("raw /clear = %+v, %v", result, err)
		}
		assertRawComposerOutcome(t, f.service.Requests, params, result)
		assertDerivedLifecycleRequestConflicts(t, f.service.Requests, params.RequestID, params.Target,
			protocol.MethodSessionClear, protocol.SessionClearParams{SessionMutation: params.SessionMutation})
		replay, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || replay != result || f.factory.count() != 2 {
			t.Fatalf("raw /clear replay = %+v, %v factory=%d", replay, err, f.factory.count())
		}
	})

	t.Run("effort", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-effort"), Input: "/effort high", DisplayText: "/effort high",
		}
		route := runtimeservice.ComposerRoute{
			Kind: runtimeservice.ComposerCompleted, Completion: runtimeservice.ComposerCompletionProfileEffort,
			Input: params.Input, Argument: "high",
		}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectRuntimeReplaced ||
			!result.SnapshotRequired || result.Target != params.Target || result.RuntimeEpoch == runtime.Epoch() {
			t.Fatalf("raw /effort = %+v, %v", result, err)
		}
		assertRawComposerOutcome(t, f.service.Requests, params, result)
		effort := "high"
		assertDerivedLifecycleRequestConflicts(t, f.service.Requests, params.RequestID, params.Target,
			protocol.MethodSessionProfileSet, protocol.SessionProfileSetParams{
				SessionMutation: params.SessionMutation, Patch: protocol.ProfilePatch{Effort: &effort},
			})
		replay, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || replay != result || f.factory.count() != 2 {
			t.Fatalf("raw /effort replay = %+v, %v factory=%d", replay, err, f.factory.count())
		}
	})

	t.Run("branch exact tip", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-branch-tip"), Input: "/branch tip-child", DisplayText: "/branch tip-child",
		}
		route := runtimeservice.ComposerRoute{
			Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleBranch,
			Input: params.Input, Argument: "tip-child",
		}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectSessionReplaced ||
			!result.SnapshotRequired || result.Target == params.Target || result.RuntimeEpoch == "" {
			t.Fatalf("raw tip /branch = %+v, %v", result, err)
		}
		terminal := receiveMessage(t, subscription.Messages)
		if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncTargetReplaced ||
			terminal.Resync.ReplacementTarget == nil || *terminal.Resync.ReplacementTarget != result.Target ||
			terminal.Resync.ReplacementRuntimeEpoch != result.RuntimeEpoch {
			t.Fatalf("raw tip /branch resync = %+v", terminal)
		}
		if _, ok := f.manager.Runtime(params.Target); ok {
			t.Fatal("raw tip /branch kept source runtime")
		}
		if child, ok := f.manager.Runtime(result.Target); !ok || child.Epoch() != result.RuntimeEpoch {
			t.Fatalf("raw tip /branch child runtime = %v %+v", ok, child)
		}
		resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), result.Target)
		if err != nil {
			t.Fatal(err)
		}
		meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
		if err != nil || !ok || meta.RemoteParentSessionID != string(params.Target.SessionID) ||
			meta.RemoteParentCheckpointID != "" || meta.ForkTurn != -1 {
			t.Fatalf("raw tip /branch metadata = %+v ok=%v err=%v", meta, ok, err)
		}
		assertRawComposerOutcome(t, f.service.Requests, params, result)
		replay, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		controller := f.factory.controller(params.Target)
		if err != nil || replay != result || controller.branchCalls != 1 || f.factory.count() != 2 {
			t.Fatalf("raw tip /branch replay = %+v, %v branchCalls=%d factory=%d", replay, err, controller.branchCalls, f.factory.count())
		}
	})

	t.Run("branch displayed checkpoint turn", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-branch-turn"), Input: "/branch 4 named", DisplayText: "/branch 4 named",
		}
		route := runtimeservice.ComposerRoute{
			Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleBranch,
			Input: params.Input, Argument: "4 named",
		}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Effect != protocol.EffectSessionReplaced || result.Target == params.Target {
			t.Fatalf("raw turn /branch = %+v, %v", result, err)
		}
		resolved, err := f.catalog.ResolveRuntimeTarget(context.Background(), result.Target)
		if err != nil {
			t.Fatal(err)
		}
		meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
		if err != nil || !ok || meta.RemoteParentCheckpointID != string(checkpointID) || meta.ParentID == "" {
			t.Fatalf("raw turn /branch metadata = %+v ok=%v err=%v", meta, ok, err)
		}
		controller := f.factory.controller(params.Target)
		if controller.forkCalls != 1 || controller.branchCalls != 0 {
			t.Fatalf("raw turn /branch controller calls fork=%d branch=%d", controller.forkCalls, controller.branchCalls)
		}
	})

	t.Run("rewind changes state in place and keeps runtime identity", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
		controller := f.factory.controller(f.source.Target)
		controller.rewindResult = control.RewindResult{WorkspaceChanged: true, ConversationRewritten: true}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-rewind"), Input: "/rewind 3 both", DisplayText: "/rewind 3 both",
		}
		route := runtimeservice.ComposerRoute{
			Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleRewind,
			Input: params.Input, Argument: "3 both",
		}
		result, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectStateChanged ||
			!result.SnapshotRequired || result.Target != params.Target || result.RuntimeEpoch != runtime.Epoch() {
			t.Fatalf("raw /rewind = %+v, %v", result, err)
		}
		select {
		case message := <-subscription.Messages:
			t.Fatalf("in-place raw /rewind retired subscription: %+v", message)
		case <-time.After(50 * time.Millisecond):
		}
		if current, ok := f.manager.Runtime(params.Target); !ok || current != runtime || current.Epoch() != result.RuntimeEpoch {
			t.Fatalf("raw /rewind replaced runtime: ok=%v current=%p source=%p", ok, current, runtime)
		}
		assertRawComposerOutcome(t, f.service.Requests, params, result)
		assertDerivedLifecycleRequestConflicts(t, f.service.Requests, params.RequestID, params.Target,
			protocol.MethodSessionRewind, protocol.SessionRewindParams{
				SessionMutation: params.SessionMutation, CheckpointID: checkpointID, Scope: protocol.RewindBoth,
			})
		replay, err := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if err != nil || replay != result || controller.rewindCalls != 1 || f.factory.count() != 1 {
			t.Fatalf("raw /rewind replay = %+v, %v rewindCalls=%d factory=%d", replay, err, controller.rewindCalls, f.factory.count())
		}
	})

	t.Run("rewind partial failure is cached under original submit identity", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 0, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
		controller := f.factory.controller(f.source.Target)
		controller.rewindErr = &control.RewindError{
			Failure: control.RewindFailurePartial, Turn: 3, Scope: control.RewindBoth,
			WorkspaceMayHaveChanged: true, ConversationMayHaveChanged: true, SnapshotRequired: true,
			Cause: errors.New("injected partial write"),
		}
		params := protocol.SessionSubmitParams{
			SessionMutation: mutationEnvelope(runtime, "raw-rewind-partial"), Input: "/rewind 3 both", DisplayText: "/rewind 3 both",
		}
		route := runtimeservice.ComposerRoute{
			Kind: runtimeservice.ComposerLifecycle, Lifecycle: runtimeservice.ComposerLifecycleRewind,
			Input: params.Input, Argument: "3 both",
		}
		_, firstErr := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		var remote *protocol.RemoteError
		if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRewindPartial ||
			remote.Data.WorkspaceMayHaveChanged == nil || !*remote.Data.WorkspaceMayHaveChanged ||
			remote.Data.ConversationMayHaveChanged == nil || !*remote.Data.ConversationMayHaveChanged ||
			remote.Data.SnapshotRequired == nil || !*remote.Data.SnapshotRequired {
			t.Fatalf("raw /rewind partial failure = %#v / %v", remote, firstErr)
		}
		if current, ok := f.manager.Runtime(params.Target); !ok || current != runtime || current.Epoch() != params.ExpectedRuntimeEpoch {
			t.Fatalf("raw /rewind partial failure replaced source runtime: ok=%v current=%p source=%p", ok, current, runtime)
		}
		request := lifecycleRequest(protocol.MethodSessionSubmit, params.RequestID, params.Target, params)
		attempt, found, lookupErr := f.service.Requests.Lookup(request)
		if lookupErr != nil || !found {
			t.Fatalf("raw /rewind cached lookup: found=%v err=%v", found, lookupErr)
		}
		outcome, waitErr := attempt.Wait(context.Background())
		cached := outcome.RemoteError()
		if waitErr != nil || cached == nil || cached.Code != protocol.ErrRewindPartial {
			t.Fatalf("raw /rewind cached rejection = %#v wait=%v", cached, waitErr)
		}
		assertDerivedLifecycleRequestConflicts(t, f.service.Requests, params.RequestID, params.Target,
			protocol.MethodSessionRewind, protocol.SessionRewindParams{
				SessionMutation: params.SessionMutation, CheckpointID: checkpointID, Scope: protocol.RewindBoth,
			})
		_, replayErr := f.service.DelegatedComposerMutation(context.Background(), params, route, nil)
		if !errors.As(replayErr, &remote) || remote.Code != protocol.ErrRewindPartial || controller.rewindCalls != 1 || f.factory.count() != 1 {
			t.Fatalf("raw /rewind partial failure replay = %v rewindCalls=%d factory=%d", replayErr, controller.rewindCalls, f.factory.count())
		}
	})

	t.Run("profile projection", func(t *testing.T) {
		target := testTarget()
		inPlace := projectComposerProfileResult(target, protocol.SessionProfileSetResult{RuntimeEpoch: "epoch-1", Disposition: protocol.ProfileUpdated})
		if inPlace.Effect != protocol.EffectStateChanged || inPlace.SnapshotRequired {
			t.Fatalf("in-place profile projection = %+v", inPlace)
		}
		rebuilt := projectComposerProfileResult(target, protocol.SessionProfileSetResult{RuntimeEpoch: "epoch-2", Disposition: protocol.ProfileRebuilt})
		if rebuilt.Effect != protocol.EffectRuntimeReplaced || !rebuilt.SnapshotRequired {
			t.Fatalf("rebuilt profile projection = %+v", rebuilt)
		}
	})
}

func assertRawComposerOutcome(t *testing.T, registry *idempotency.Registry, params protocol.SessionSubmitParams, want protocol.SessionSubmitResult) {
	t.Helper()
	request := lifecycleRequest(protocol.MethodSessionSubmit, params.RequestID, params.Target, params)
	attempt, found, err := registry.Lookup(request)
	if err != nil || !found {
		t.Fatalf("raw composer registry lookup: found=%v err=%v", found, err)
	}
	outcome, err := attempt.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.SessionSubmitResult
	if err := outcome.Decode(&got); err != nil || got != want {
		t.Fatalf("raw composer cached outcome = %+v, %v; want %+v", got, err, want)
	}
}

func assertDerivedLifecycleRequestConflicts(t *testing.T, registry *idempotency.Registry, requestID protocol.RequestID, target protocol.RuntimeTarget, method protocol.Method, params any) {
	t.Helper()
	_, found, err := registry.Lookup(lifecycleRequest(method, requestID, target, params))
	var remote *protocol.RemoteError
	if !found || !errors.As(err, &remote) || remote.Code != protocol.ErrRequestIDConflict {
		t.Fatalf("derived lifecycle request did not conflict with raw claim: found=%v err=%v", found, err)
	}
}

func TestLifecycleBuildFailuresDoNotDuplicateOrPartiallyReplaceState(t *testing.T) {
	t.Run("new keeps one cold allocated target and replays failure", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 2, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionNewParams{SessionMutation: mutationEnvelope(runtime, "new-build-failure")}
		_, firstErr := f.service.New(context.Background(), params, nil)
		var remote *protocol.RemoteError
		if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRuntimeStartFailed || remote.Data.Target == nil {
			t.Fatalf("new build failure = %v", firstErr)
		}
		coldTarget := *remote.Data.Target
		listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: "host-test", WorkspaceID: f.source.Target.WorkspaceID})
		if err != nil || len(listed.Items) != 2 {
			t.Fatalf("new failure list = %+v, %v", listed, err)
		}
		_, replayErr := f.service.New(context.Background(), params, nil)
		if !errors.As(replayErr, &remote) || remote.Data.Target == nil || *remote.Data.Target != coldTarget || f.factory.count() != 2 {
			t.Fatalf("new failure replay = %v factory=%d", replayErr, f.factory.count())
		}
	})

	t.Run("clear restores old identity on replacement build failure", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 2, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		params := protocol.SessionClearParams{SessionMutation: mutationEnvelope(runtime, "clear-build-failure")}
		_, firstErr := f.service.Clear(context.Background(), params, nil)
		var remote *protocol.RemoteError
		if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRuntimeStartFailed {
			t.Fatalf("clear failure = %v", firstErr)
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), f.source.Target); err != nil {
			t.Fatalf("clear failure retired source: %v", err)
		}
		if agent.IsCleanupPending(f.source.SessionPath) {
			t.Fatal("clear rollback left source hidden")
		}
		listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: "host-test", WorkspaceID: f.source.Target.WorkspaceID})
		if err != nil || len(listed.Items) != 1 || listed.Items[0].Target != f.source.Target {
			t.Fatalf("clear rollback list = %+v, %v", listed, err)
		}
		_, replayErr := f.service.Clear(context.Background(), params, nil)
		if !errors.As(replayErr, &remote) || remote.Code != protocol.ErrRuntimeStartFailed || f.factory.count() != 2 {
			t.Fatalf("clear failure replay = %v factory=%d", replayErr, f.factory.count())
		}
	})

	t.Run("profile rollback preserves old epoch and full profile", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 2, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		before, err := f.catalog.ResolveRuntimeTarget(context.Background(), f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		model := "test/model-b"
		params := protocol.SessionProfileSetParams{SessionMutation: mutationEnvelope(runtime, "profile-build-failure"), Patch: protocol.ProfilePatch{Model: &model}}
		_, firstErr := f.service.SetProfile(context.Background(), params, nil)
		var remote *protocol.RemoteError
		if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRuntimeStartFailed {
			t.Fatalf("profile failure = %v", firstErr)
		}
		after, err := f.catalog.ResolveRuntimeTarget(context.Background(), f.source.Target)
		if err != nil || after.ResolvedProfile != before.ResolvedProfile {
			t.Fatalf("profile rollback = %+v, %v", after.ResolvedProfile, err)
		}
		if current, ok := f.manager.Runtime(f.source.Target); !ok || current != runtime || current.Epoch() != runtime.Epoch() {
			t.Fatal("profile build failure replaced runtime")
		}
	})

	t.Run("fork removes adopted child when child runtime build fails", func(t *testing.T) {
		f := newLifecycleHostFixture(t, 2, nil)
		runtime, err := f.manager.GetOrCreate(f.source.Target)
		if err != nil {
			t.Fatal(err)
		}
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
		params := protocol.SessionForkParams{SessionMutation: mutationEnvelope(runtime, "fork-build-failure"), CheckpointID: checkpointID}
		_, firstErr := f.service.Fork(context.Background(), params, nil)
		var remote *protocol.RemoteError
		if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRuntimeStartFailed || remote.Data.Target == nil {
			t.Fatalf("fork failure = %v", firstErr)
		}
		controller := f.factory.controller(f.source.Target)
		controller.domainMu.Lock()
		forkPath := controller.lastForkPath
		controller.domainMu.Unlock()
		if _, statErr := os.Stat(f.source.SessionPath); statErr != nil {
			t.Fatalf("fork rollback damaged source artifact %s: %v", f.source.SessionPath, statErr)
		}
		if _, statErr := os.Stat(forkPath); !os.IsNotExist(statErr) {
			t.Fatalf("fork rollback left child transcript %s: %v", forkPath, statErr)
		}
		entries, err := os.ReadDir(filepath.Dir(forkPath))
		if err != nil {
			t.Fatal(err)
		}
		forkStem := strings.TrimSuffix(filepath.Base(forkPath), ".jsonl")
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), forkStem) {
				t.Fatalf("fork rollback left child artifact %s", entry.Name())
			}
		}
		if _, ok := f.manager.Runtime(*remote.Data.Target); ok {
			t.Fatal("fork rollback left child runtime")
		}
		if _, err := f.catalog.ResolveRuntimeTarget(context.Background(), *remote.Data.Target); err == nil {
			t.Fatal("failed fork child remained in catalog")
		} else if code, ok := catalog.ErrorCode(err); !ok || code != protocol.ErrSessionNotFound {
			t.Fatalf("failed fork child catalog lookup = %v", err)
		}
		listed, err := f.catalog.ListSessions(context.Background(), protocol.SessionListParams{ExpectedHostEpoch: "host-test", WorkspaceID: f.source.Target.WorkspaceID})
		if err != nil || len(listed.Items) != 1 {
			t.Fatalf("fork failure list = %+v, %v", listed, err)
		}
		_, replayErr := f.service.Fork(context.Background(), params, nil)
		if !errors.As(replayErr, &remote) || controller.forkCalls != 1 || f.factory.count() != 2 {
			t.Fatalf("fork failure replay = %v forkCalls=%d factory=%d", replayErr, controller.forkCalls, f.factory.count())
		}
	})
}

func TestProfileInPlaceMapsOnlyCurrentOpaquePromptAndRebuildMigratesSubscription(t *testing.T) {
	f := newLifecycleHostFixture(t, 0, nil)
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	controller := f.factory.controller(f.source.Target)
	controller.autoApprovalIDs = []string{"controller-approval"}
	controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "controller-approval", Tool: "bash", Subject: "run"}})
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil || snapshot.PendingPrompt == nil {
		t.Fatalf("prompt snapshot = %+v, %v", snapshot.PendingPrompt, err)
	}
	opaque := snapshot.PendingPrompt.Approval.PromptID
	yolo := protocol.ToolApprovalYOLO
	updated, err := f.service.SetProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: mutationEnvelope(runtime, "profile-in-place"), Patch: protocol.ProfilePatch{ToolApprovalMode: &yolo},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Disposition != protocol.ProfileUpdated || len(updated.AutoResolvedPromptIDs) != 1 || updated.AutoResolvedPromptIDs[0] != opaque || updated.RuntimeEpoch != runtime.Epoch() {
		t.Fatalf("in-place profile = %+v", updated)
	}
	if after, _ := runtime.Snapshot(context.Background()); after.PendingPrompt != nil {
		t.Fatalf("auto-resolved prompt remained: %+v", after.PendingPrompt)
	}

	attachment := testAttachment(2)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	model := "test/model-b"
	rebuilt, err := f.service.SetProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: mutationEnvelope(runtime, "profile-rebuild"), Patch: protocol.ProfilePatch{Model: &model},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Disposition != protocol.ProfileRebuilt || rebuilt.RuntimeEpoch == runtime.Epoch() || rebuilt.ResolvedProfile.Model != model {
		t.Fatalf("rebuilt profile = %+v", rebuilt)
	}
	terminal := receiveMessage(t, subscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced || terminal.Resync.ReplacementTarget != nil {
		t.Fatalf("profile migration = %+v", terminal)
	}
	replay, err := f.service.SetProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: mutationEnvelope(runtime, "profile-rebuild"), Patch: protocol.ProfilePatch{Model: &model},
	}, nil)
	if err != nil || !reflect.DeepEqual(replay, rebuilt) || f.factory.count() != 2 {
		t.Fatalf("profile replay = %+v, %v factory=%d", replay, err, f.factory.count())
	}
}

func TestForkKeepsSourceRuntimeAndPersistsOpaqueParentMetadata(t *testing.T) {
	f := newLifecycleHostFixture(t, 0, nil)
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
	result, err := f.service.Fork(context.Background(), protocol.SessionForkParams{
		SessionMutation: mutationEnvelope(runtime, "fork-request"), CheckpointID: checkpointID, Name: "child",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceTarget != f.source.Target || result.SourceRuntimeEpoch != runtime.Epoch() || result.ChildTarget == result.SourceTarget {
		t.Fatalf("fork result = %+v", result)
	}
	if current, ok := f.manager.Runtime(f.source.Target); !ok || current != runtime {
		t.Fatal("fork replaced source runtime")
	}
	child, err := f.catalog.ResolveRuntimeTarget(context.Background(), result.ChildTarget)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok, err := agent.LoadBranchMeta(child.SessionPath)
	if err != nil || !ok || meta.RemoteParentSessionID != string(f.source.Target.SessionID) || meta.RemoteParentCheckpointID != string(checkpointID) {
		t.Fatalf("fork meta = %+v ok=%v err=%v", meta, ok, err)
	}
}

func TestRewindPartialIsCachedWithExactFlagsAndDeletedCheckpointImmediatelyStales(t *testing.T) {
	f := newLifecycleHostFixture(t, 0, nil)
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
	controller := f.factory.controller(f.source.Target)
	controller.rewindErr = &control.RewindError{
		Failure: control.RewindFailurePartial, Turn: 3, Scope: control.RewindBoth,
		WorkspaceMayHaveChanged: true, ConversationMayHaveChanged: false, SnapshotRequired: true,
		Cause: errors.New("injected partial write"),
	}
	controller.dropCheckpoints = true
	params := protocol.SessionRewindParams{SessionMutation: mutationEnvelope(runtime, "rewind-partial"), CheckpointID: checkpointID, Scope: protocol.RewindBoth}
	_, firstErr := runtime.RewindMutation(context.Background(), f.service.Requests, params, nil)
	var remote *protocol.RemoteError
	if !errors.As(firstErr, &remote) || remote.Code != protocol.ErrRewindPartial || remote.Data.WorkspaceMayHaveChanged == nil || !*remote.Data.WorkspaceMayHaveChanged || remote.Data.ConversationMayHaveChanged == nil || *remote.Data.ConversationMayHaveChanged || remote.Data.SnapshotRequired == nil || !*remote.Data.SnapshotRequired {
		t.Fatalf("partial rewind error = %#v / %v", remote, firstErr)
	}
	_, replayErr := runtime.RewindMutation(context.Background(), f.service.Requests, params, nil)
	if !errors.As(replayErr, &remote) || remote.Code != protocol.ErrRewindPartial || controller.rewindCalls != 1 {
		t.Fatalf("partial replay = %v calls=%d", replayErr, controller.rewindCalls)
	}
	params.RequestID = "rewind-stale-checkpoint"
	_, staleErr := runtime.RewindMutation(context.Background(), f.service.Requests, params, nil)
	if !errors.As(staleErr, &remote) || remote.Code != protocol.ErrCheckpointNotFound || controller.rewindCalls != 1 {
		t.Fatalf("deleted checkpoint = %v calls=%d", staleErr, controller.rewindCalls)
	}
}

func TestGoalMutationsAndSnapshotShareActorBarrier(t *testing.T) {
	f := newLifecycleHostFixture(t, 0, nil)
	runtime, err := f.manager.GetOrCreate(f.source.Target)
	if err != nil {
		t.Fatal(err)
	}
	setParams := protocol.SessionGoalSetParams{SessionMutation: mutationEnvelope(runtime, "goal-set"), Goal: "ship Remote V1"}
	set, err := runtime.GoalSetMutation(context.Background(), f.service.Requests, setParams, nil)
	if err != nil || set.Goal != "ship Remote V1" || set.Status != protocol.GoalRunning {
		t.Fatalf("goal set = %+v, %v", set, err)
	}
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil || snapshot.Goal == nil || *snapshot.Goal != set.Goal || snapshot.GoalStatus != set.Status {
		t.Fatalf("goal snapshot = goal=%v status=%q err=%v", snapshot.Goal, snapshot.GoalStatus, err)
	}
	replay, err := runtime.GoalSetMutation(context.Background(), f.service.Requests, setParams, nil)
	if err != nil || replay != set {
		t.Fatalf("goal replay = %+v, %v", replay, err)
	}
	controller := f.factory.controller(f.source.Target)
	controller.domainMu.Lock()
	controller.goalStatus = string(protocol.GoalBlocked)
	controller.domainMu.Unlock()
	resumed, err := runtime.GoalResumeMutation(context.Background(), f.service.Requests, protocol.SessionGoalResumeParams{SessionMutation: mutationEnvelope(runtime, "goal-resume")}, nil)
	if err != nil || !resumed.Resumed || resumed.Status != protocol.GoalRunning {
		t.Fatalf("goal resume = %+v, %v", resumed, err)
	}
	cleared, err := runtime.GoalClearMutation(context.Background(), f.service.Requests, protocol.SessionGoalClearParams{SessionMutation: mutationEnvelope(runtime, "goal-clear")}, nil)
	if err != nil || !cleared.Cleared {
		t.Fatalf("goal clear = %+v, %v", cleared, err)
	}
	snapshot, err = runtime.Snapshot(context.Background())
	if err != nil || snapshot.Goal != nil || snapshot.GoalStatus != "" {
		t.Fatalf("cleared goal snapshot = %+v, %v", snapshot, err)
	}
}
