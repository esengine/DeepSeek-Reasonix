package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

func loadDurableSessionProjection(t *testing.T, legacyPath string) session.Projection {
	t.Helper()
	commits, err := session.Replay(sessionDirectory(legacyPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := session.Project(commits)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func TestRuntimeSnapshotRecoversRecoveryRequiredFromV3Alone(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	recovery := event.RecoveryStatus{State: "recovery_required", Reason: "uncooperative_tool", Phase: "tool"}
	payload, err := json.Marshal(recovery)
	if err != nil {
		t.Fatal(err)
	}
	store := c.sessionEventStore()
	if _, err := store.Append(t.Context(), session.Batch{OperationID: "v3-only-recovery", Events: []session.Event{{Kind: "runtime/recovery", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	c.refreshRuntimeState(event.Event{})
	state := c.RuntimeStateSnapshot()
	if state.Phase != "recovery_required" || state.TurnStatus != event.TurnRecoveryRequired || state.Recovery == nil || state.Recovery.Reason != recovery.Reason {
		t.Fatalf("runtime did not adopt v3 recovery: %+v", state)
	}
}

func TestExclusiveControllerUsesBoundSessionIdentityAndWritesNoLegacyTranscript(t *testing.T) {
	root := t.TempDir()
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "stable-session"})
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(root, "sessions", "legacy-name.jsonl")
	providerMock := testutil.NewMock("test", testutil.Turn{Text: "answer"})
	exec := agent.New(providerMock, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{
		Runner: exec, Executor: exec, Sink: event.Discard,
		SessionPath: legacyPath, SessionDir: filepath.Dir(legacyPath),
		SessionService: service, SessionRuntime: runtime, ExclusiveSession: true,
	})
	if err := c.RunTurn(t.Context(), "question"); err != nil {
		t.Fatal(err)
	}
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	ref, ok := c.SessionRef()
	if !ok || ref.HostID != "desktop" || ref.SessionID != "stable-session" {
		t.Fatalf("session ref = %+v, %v", ref, ok)
	}
	state := c.RuntimeStateSnapshot()
	if state.HostID != ref.HostID || state.SessionID != ref.SessionID || state.SessionCodec != session.Codec {
		t.Fatalf("runtime identity = %+v", state)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("exclusive controller wrote legacy transcript: %v", err)
	}
	if got := c.History(); len(got) < 3 || got[len(got)-1].Content != "answer" {
		t.Fatalf("event-derived history = %#v", got)
	}
	c.Close()
	if cached, ok := service.Runtime(ref); !ok || cached != runtime {
		t.Fatal("terminal controller close did not retain the idle runtime")
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailureDoesNotCloseAnotherControllersRuntime(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	first, err := service.Create(t.Context(), session.CreateOptions{SessionID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), session.CreateOptions{SessionID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	newController := func(runtime *session.Runtime) *Controller {
		exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
		return newOwnedTestController(t, Options{
			Executor: exec, Sink: event.Discard, SessionService: service,
			SessionRuntime: runtime, ExclusiveSession: true,
		})
	}
	owner := newController(second)
	t.Cleanup(owner.Close)
	caller := newController(first)
	t.Cleanup(caller.Close)

	if _, err := second.Session().AppendBatch(t.Context(), "bad-plan", []session.Event{{Kind: "plan/state", Payload: []byte(`{"enabled":"invalid"}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := caller.OpenSession(t.Context(), second.Ref()); err == nil {
		t.Fatal("OpenSession accepted an invalid domain projection")
	}
	if got, ok := service.Runtime(second.Ref()); !ok || got != second {
		t.Fatal("failed client publication disposed another controller's runtime")
	}
	if _, err := second.Session().AppendBatch(t.Context(), "still-owned", []session.Event{{Kind: "diagnostic", Optional: true}}); err != nil {
		t.Fatalf("shared runtime is unusable after another controller failed to bind: %v", err)
	}
}

func TestExclusiveControllerRuntimeSnapshotAndCancelUseExactV3Instance(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "runtime-owned"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	t.Cleanup(c.Close)

	initial := c.RuntimeStateSnapshot()
	runtimeInitial := runtime.Snapshot()
	if initial.RuntimeEpoch != runtimeInitial.Epoch || initial.ActivityRevision != runtimeInitial.ActivityRevision || initial.SessionID != runtime.Ref().SessionID || initial.HeadID != "" {
		t.Fatalf("controller snapshot = %+v, runtime = %+v", initial, runtimeInitial)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	if got := c.runGuarded(func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}); got != turnStarted {
		t.Fatalf("turn admission = %v", got)
	}
	<-started
	receipt := c.CancelSession()
	if !receipt.Accepted || receipt.SessionRef != runtime.Ref().SessionID || receipt.HeadID != "" || receipt.RuntimeEpoch != runtimeInitial.Epoch {
		t.Fatalf("cancel receipt = %+v", receipt)
	}
	close(release)
	for deadline := time.Now().Add(5 * time.Second); c.Running() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if c.Running() {
		t.Fatal("cancelled v3 activity did not settle")
	}
}

func TestExclusiveSessionSwitchRestoresPlanAndGoalWithoutTodo(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	first, err := service.Create(t.Context(), session.CreateOptions{SessionID: "first-domain"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: first, ExclusiveSession: true})
	t.Cleanup(c.Close)
	c.SetPlanMode(true)
	c.SetGoal("finish the migration")
	if _, err := first.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	goalRaw := first.Session().Snapshot().Projection.GoalState
	if string(goalRaw) == "" || strings.Contains(string(goalRaw), `"todos"`) {
		t.Fatalf("goal event retained todo state: %s", goalRaw)
	}

	second, err := service.Create(t.Context(), session.CreateOptions{SessionID: "second-domain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), second.Ref()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.OpenSession(t.Context(), second.Ref()); err != nil {
		t.Fatal(err)
	}
	if c.PlanMode() || c.Goal() != "" || c.GoalStatus() == GoalStatusRunning {
		t.Fatalf("empty target inherited plan/goal: plan=%v goal=%q status=%q", c.PlanMode(), c.Goal(), c.GoalStatus())
	}
	if _, err := c.OpenSession(t.Context(), first.Ref()); err != nil {
		t.Fatal(err)
	}
	if !c.PlanMode() || c.Goal() != "finish the migration" || c.GoalStatus() == GoalStatusRunning {
		t.Fatalf("restored domain state: plan=%v goal=%q status=%q", c.PlanMode(), c.Goal(), c.GoalStatus())
	}
}

func TestExclusiveControllerNewPublishesFreshIdentityAndKeepsOldHistory(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4"))
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard, SessionDir: filepath.Join(root, "sessions"), SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	ref, ok := c.SessionRef()
	if !ok || ref.SessionID == "old-session" {
		t.Fatalf("new identity = %+v, ok=%v", ref, ok)
	}
	oldRef := session.SessionRef{HostID: "desktop", SessionID: "old-session"}
	if cached, ok := service.Runtime(oldRef); !ok || cached != runtime {
		t.Fatal("old runtime was not retained for quick switching")
	}
	read, err := persistence.Open("old-session", session.ReadOnly)
	if err != nil {
		t.Fatalf("old history was removed by NewSession: %v", err)
	}
	_ = read.Close(t.Context())
	if err := service.Close(t.Context(), oldRef); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestExclusiveControllerOpenMissingKeepsCurrentExactRuntime(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4"))
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "current"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	if _, err := c.OpenSession(t.Context(), session.SessionRef{HostID: "desktop", SessionID: "missing"}); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("OpenSession missing error = %v", err)
	}
	ref, ok := c.SessionRef()
	if !ok || ref != runtime.Ref() {
		t.Fatalf("failed open changed binding to %+v", ref)
	}
	if _, err := persistence.Stat(t.Context(), "missing"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("failed open created missing session: %v", err)
	}
	c.Close()
}

func TestExclusiveControllerClearDeletesClosedSourceAfterPublishingFreshIdentity(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4"))
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "clear-source"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard, SessionDir: filepath.Join(root, "sessions"), SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	if err := c.ClearSession(); err != nil {
		t.Fatal(err)
	}
	ref, ok := c.SessionRef()
	if !ok || ref.SessionID == "clear-source" {
		t.Fatalf("clear identity = %+v, ok=%v", ref, ok)
	}
	if _, err := persistence.Stat(t.Context(), "clear-source"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("cleared source remains in catalog: %v", err)
	}
	c.Close()
}

func TestExclusiveControllerDelegatesClearPersistenceToIdentityHost(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(filepath.Join(root, "sessions-v5"))
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "clear-source", CWD: root, Origin: session.SessionOriginNew})
	if err != nil {
		t.Fatal(err)
	}
	var committed SessionRotationRequest
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{
		Executor: exec, Sink: event.Discard, SessionService: service, SessionRuntime: runtime, ExclusiveSession: true,
		OnSessionRotation: func(_ context.Context, request SessionRotationRequest) (SessionRotationPlan, error) {
			committed = request
			return SessionRotationPlan{
				CreateOptions: session.CreateOptions{SessionID: "replacement", CWD: root, Origin: session.SessionOriginNew},
				Commit: func(_ context.Context, ref session.SessionRef) error {
					if ref.SessionID != "replacement" {
						t.Fatalf("replacement ref = %+v", ref)
					}
					return nil
				},
			}, nil
		},
	})
	if err := c.ClearSession(); err != nil {
		t.Fatal(err)
	}
	if committed.Source.SessionID != "clear-source" || committed.Reason != "clear" {
		t.Fatalf("rotation request = %+v", committed)
	}
	if _, err := persistence.Stat(t.Context(), "clear-source"); err != nil {
		t.Fatalf("host-owned clear permanently deleted source: %v", err)
	}
	info, err := persistence.Stat(t.Context(), "replacement")
	if err != nil {
		t.Fatal(err)
	}
	if info.CWD != root || info.Origin != session.SessionOriginNew {
		t.Fatalf("replacement header = %+v", info)
	}
	c.Close()
}

func TestExclusiveControllerForkUsesTypedTurnBoundaryAndNoLegacyTranscript(t *testing.T) {
	root := t.TempDir()
	persistence := session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4"))
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "fork-parent", CWD: root, Origin: session.SessionOriginNew})
	if err != nil {
		t.Fatal(err)
	}
	prov := testutil.NewMock("fork", testutil.Turn{Text: "answer one"}, testutil.Turn{Text: "answer two"})
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Runner: exec, Executor: exec, Sink: event.Discard, SessionDir: filepath.Join(root, "legacy"), SessionService: service, SessionRuntime: runtime, ExclusiveSession: true})
	if err := c.RunTurn(t.Context(), "one"); err != nil {
		t.Fatal(err)
	}
	if err := c.RunTurn(t.Context(), "two"); err != nil {
		t.Fatal(err)
	}
	childID, err := c.ForkNamed(1, "first turn")
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := c.SessionRef()
	if !ok || ref.SessionID != childID || ref.SessionID == "fork-parent" {
		t.Fatalf("fork ref = %+v, child id=%q", ref, childID)
	}
	snapshot := runtimeSnapshotForTest(t, c)
	if got := snapshot.Projection.Title; got != "first turn" {
		t.Fatalf("child title = %q", got)
	}
	if got := snapshot.Projection.Messages; len(got) != 3 || got[1].Content != "one" || got[2].Content != "answer one" {
		t.Fatalf("child history = %#v", got)
	}
	childInfo, err := persistence.Stat(t.Context(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if childInfo.CWD != root || childInfo.ParentSessionID != "fork-parent" || childInfo.Origin != session.SessionOriginFork {
		t.Fatalf("child header = %+v", childInfo)
	}
	entries, err := os.ReadDir(filepath.Join(root, "legacy"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("v3 fork created legacy artifacts: %+v", entries)
	}
	c.Close()
}

func runtimeSnapshotForTest(t *testing.T, c *Controller) session.Snapshot {
	t.Helper()
	_, runtime, ok := c.SessionBinding()
	if !ok {
		t.Fatal("controller has no v3 runtime")
	}
	return runtime.Session().Snapshot()
}

func TestSessionPathBindingSeedsTranscriptBeforeLaterStateEvents(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })

	c.SetSessionPath(legacyPath)
	snapshot, ok := c.sessionEventSnapshot()
	if !ok || len(snapshot.Projection.ModelMessages) != 3 {
		t.Fatalf("bound projection = %#v, available=%v", snapshot.Projection.ModelMessages, ok)
	}
	for i, message := range snapshot.Projection.ModelMessages {
		if message.ID == "" {
			t.Fatalf("bound projection message %d has no stable id", i)
		}
	}
	if snapshot.Projection.ModelMessages[0].Role != provider.RoleSystem {
		t.Fatalf("bound projection lost leading system message: %#v", snapshot.Projection.ModelMessages)
	}

	// A later state-only event must not become the first authoritative v3
	// commit and hide the transcript from History.
	c.SetPlanMode(false)
	if got := c.History(); len(got) != 3 || got[0].Role != provider.RoleSystem {
		t.Fatalf("history after state event = %#v", got)
	}
}

func TestLateManagedHostBindingFencesCandidateUntilLeaseActivation(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	initial := agent.NewSession("system")
	initial.Add(provider.Message{Role: provider.RoleUser, Content: "old"})
	exec := agent.New(nil, tool.NewRegistry(), initial, agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })

	// Serve and other embedders may install their transition owner after the
	// controller is built. From that point onward a private replacement without
	// the final lease may observe, but may not publish, the shared projection.
	c.SetOnSessionTransition(func(SessionTransitionInfo) error { return nil })
	before, _ := c.sessionEventSnapshot()
	candidate := agent.NewSession("system")
	candidate.Add(provider.Message{Role: provider.RoleUser, Content: "old"})
	candidate.Add(provider.Message{Role: provider.RoleAssistant, Content: "candidate"})
	c.executor.SetSession(candidate)
	if err := c.RecordSessionMessages(t.Context(), "unpublished-candidate", []provider.Message{{ID: agent.NewMessageID(), Role: provider.RoleAssistant, Content: "must-not-publish"}}); err != nil {
		t.Fatal(err)
	}
	after, _ := c.sessionEventSnapshot()
	if after.EventSequence != before.EventSequence {
		t.Fatalf("unleased candidate mutated v3: before=%d after=%d", before.EventSequence, after.EventSequence)
	}

	lease, err := agent.TryAcquireSessionLease(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	activated, _ := c.sessionEventSnapshot()
	if got := activated.Projection.ModelMessages; len(got) != 3 || got[2].Content != "candidate" {
		t.Fatalf("activated projection = %#v", got)
	}
}

func TestControllerUsesV3AsBusinessEventStore(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })

	admitted := c.prepareTurnAdmission(func(context.Context) error { return nil })
	if err := admitted(context.Background()); err != nil {
		t.Fatal(err)
	}
	todos := []event.Todo{{Content: "one", Status: "in_progress"}}
	toolMessage := provider.Message{ID: "tool-message-1", Role: provider.RoleTool, ToolCallID: "todo-1", Name: "todo_write", Content: "updated"}
	if err := c.emitTurnEventChecked(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "todo-1", Name: "todo_write", TodoWritten: true, Todos: todos,
		PresentedFiles: []provider.PresentedFile{{Path: "report.txt", Description: "report"}},
		Execution:      &event.ShellExecution{Kind: "shell", State: "completed"},
	}, CommittedMessage: &toolMessage}); err != nil {
		t.Fatal(err)
	}

	snapshot, ok := c.sessionEventSnapshot()
	if !ok || snapshot.EventSequence == 0 {
		t.Fatalf("v3 snapshot = %#v, %v", snapshot, ok)
	}
	if !snapshot.Projection.TodoWritten || len(snapshot.Projection.Todos) != 1 {
		t.Fatalf("v3 todos = %#v, written=%v", snapshot.Projection.Todos, snapshot.Projection.TodoWritten)
	}
	if _, err := os.Stat(store.SessionTurnEventLog(legacyPath)); !os.IsNotExist(err) {
		t.Fatalf("legacy turn ledger was written: %v", err)
	}
	if err := c.CheckpointSession(context.Background(), agent.CheckpointBeforeModel); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = c.sessionEventSnapshot()
	if snapshot.DurableSequence != snapshot.EventSequence {
		t.Fatalf("durable=%d event=%d", snapshot.DurableSequence, snapshot.EventSequence)
	}
	commits, err := session.Replay(sessionDirectory(legacyPath), nil)
	if err != nil || len(commits) == 0 {
		t.Fatalf("Replay = %d commits, %v", len(commits), err)
	}
	foundAtomicTodo := false
	for _, commit := range commits {
		if len(commit.Events) == 3 && commit.Events[0].Kind == "message/complete" && commit.Events[1].Kind == "todo/write" && commit.Events[2].Kind == "tool/result" {
			foundAtomicTodo = true
			var payload struct {
				Todos          []event.Todo             `json:"todos"`
				TodoWritten    bool                     `json:"todoWritten"`
				PresentedFiles []provider.PresentedFile `json:"presentedFiles"`
				Execution      event.ShellExecution     `json:"execution"`
			}
			if err := json.Unmarshal(commit.Events[2].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if !payload.TodoWritten || len(payload.Todos) != 1 || len(payload.PresentedFiles) != 1 || payload.Execution.State != "completed" {
				t.Fatalf("structured tool result metadata was lost: %#v", payload)
			}
			break
		}
	}
	if !foundAtomicTodo {
		t.Fatalf("todo result was not one ordered atomic batch: %#v", commits)
	}
}

func TestCheckpointDoesNotInferMessagesFromLegacyTranscript(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	session := agent.NewSession("system")
	exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	recorded := provider.Message{ID: "recorded", Role: provider.RoleUser, Content: "recorded"}
	if err := c.RecordSessionMessages(context.Background(), "test", []provider.Message{recorded}); err != nil {
		t.Fatal(err)
	}
	session.Add(recorded)
	// Simulate an obsolete caller mutating the display transcript directly.
	// The checkpoint must not mirror or infer that mutation into v3.
	session.Add(provider.Message{ID: "legacy-only", Role: provider.RoleAssistant, Content: "legacy"})
	if err := c.CheckpointSession(context.Background(), agent.CheckpointBeforeModel); err != nil {
		t.Fatal(err)
	}
	messages := c.sessionEventStore().Snapshot().Projection.Messages
	for _, message := range messages {
		if message.ID == "legacy-only" {
			t.Fatalf("checkpoint inferred an unrecorded message: %#v", messages)
		}
	}
}

func TestTurnEndUsesExplicitFinalMessageCommit(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	session := agent.NewSession("system")
	exec := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(context.Background()); err != nil {
		t.Fatal(err)
	}
	assistant := provider.Message{ID: "a1", Role: provider.RoleAssistant, Content: "done"}
	if err := c.RecordSessionMessages(context.Background(), "test-final", []provider.Message{assistant}); err != nil {
		t.Fatal(err)
	}
	session.Add(assistant)
	if err := c.emitTurnEventChecked(event.Event{Kind: event.TurnDone, Status: event.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := c.sessionEventSnapshot()
	if snapshot.Projection.TurnID != "" || len(snapshot.Projection.ModelMessages) == 0 {
		t.Fatalf("terminal projection = %#v", snapshot.Projection)
	}
}

func TestTurnEndClosesPendingInteractionsInTheSameBatch(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.emitTurnEventChecked(event.Event{Kind: event.AskRequest, ItemID: "ask-1", PromptKind: string(PromptAsk)}); err != nil {
		t.Fatal(err)
	}
	if err := c.emitTurnEventChecked(event.Event{Kind: event.TurnDone, Status: event.TurnInterrupted, Cancelled: true}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := c.sessionEventSnapshot()
	if len(snapshot.Projection.Interactions) != 0 {
		t.Fatalf("terminal projection kept pending interactions: %#v", snapshot.Projection.Interactions)
	}
	if _, err := c.flushSessionEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	commits, err := session.Replay(sessionDirectory(legacyPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	last := commits[len(commits)-1]
	found := false
	for _, item := range last.Events {
		if item.Kind == "interaction/resolved" && string(item.Payload) == `{"id":"ask-1","state":"cancelled"}` {
			found = true
		}
	}
	if !found {
		t.Fatalf("turn-end batch did not cancel the interaction: %#v", last.Events)
	}
}

func TestPromptResolutionAndPlanStateShareOneAtomicBatch(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "sessions", "chat.jsonl")
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{Executor: exec, SessionPath: legacyPath, Sink: event.Discard})
	t.Cleanup(func() { c.Close() })
	if err := c.prepareTurnAdmission(func(context.Context) error { return nil })(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.emitTurnEventChecked(event.Event{Kind: event.ApprovalRequest, ItemID: "plan-1", PromptKind: string(PromptPlan)}); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"requestId":"plan-1","decision":"start_execution"}`)
	if err := c.emitTurnEventChecked(event.Event{
		Kind: event.PromptAnswered, ItemID: "plan-1", InteractionState: string(PromptAnswered),
		DomainKind: "plan/state", DomainPayload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.flushSessionEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	commits, err := session.Replay(sessionDirectory(legacyPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	last := commits[len(commits)-1]
	if len(last.Events) != 2 || last.Events[0].Kind != "interaction/resolved" || last.Events[1].Kind != "plan/state" {
		t.Fatalf("resolution split across batches: %#v", last.Events)
	}
}

// A reclaim releases the CLI's writer and closes the runtime's store; the
// TUI stays alive rendering the conversation. A second /takeover re-opens the
// same identity, and the controller's cached turn-event store must follow the
// new runtime instance — the identity path alone matches, so the stale cache
// would keep answering turn admission from the closed recovery database
// ("database not open").
func TestOpenSessionReopensRuntimeClosedByReclaim(t *testing.T) {
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "ping-pong"})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := newOwnedTestController(t, Options{
		Executor: exec, Sink: event.Discard, SessionService: service,
		SessionRuntime: runtime, ExclusiveSession: true,
	})
	// The turn-event store caches the live instance the binding resolved.
	controller.rebindTurnEvents("")
	controller.turnEvents.mu.RLock()
	cached := controller.turnEvents.v3
	controller.turnEvents.mu.RUnlock()
	if cached != runtime.Session() {
		t.Fatal("fixture did not seed the turn-event store cache")
	}

	// The reclaim release: drop every binding, then close the service runtime.
	if err := controller.ReleaseSessionRuntimeBinding(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}

	published, err := controller.OpenSession(t.Context(), runtime.Ref())
	if err != nil {
		t.Fatalf("re-takeover after reclaim failed to re-open the session: %v", err)
	}
	if published != runtime.Ref() {
		t.Fatalf("re-opened ref = %v, want %v", published, runtime.Ref())
	}
	_, reopened, _ := controller.v3Binding()
	if reopened == nil || reopened == runtime {
		t.Fatal("OpenSession kept the closed runtime instead of re-opening")
	}
	// The path that surfaced the bug: the cached store must now be the fresh
	// runtime's session, and a runtime-scoped operation lookup must answer
	// from a live recovery store instead of the closed database.
	controller.turnEvents.mu.RLock()
	recached := controller.turnEvents.v3
	controller.turnEvents.mu.RUnlock()
	if recached != reopened.Session() {
		t.Fatal("turn-event store cache still serves the runtime closed by reclaim")
	}
	if _, err := recached.AppendBatch(t.Context(), "retakeover-probe", []session.Event{{Kind: "diagnostic", Optional: true}}); err != nil {
		t.Fatalf("re-opened runtime store is unusable: %v", err)
	}
}
