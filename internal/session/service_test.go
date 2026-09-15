package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestServiceForkAndRewindUsePersistedTurnBoundaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	parent, err := service.Create(t.Context(), CreateOptions{SessionID: "parent"})
	if err != nil {
		t.Fatal(err)
	}
	appendTurn := func(operation, turnID, messageID string) {
		payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: messageID, Role: provider.RoleUser, Content: messageID}})
		_, appendErr := parent.Session().Append(t.Context(), Batch{OperationID: operation, TurnID: turnID, Events: []Event{
			{Kind: "turn/start"}, {Kind: "message/complete", Payload: payload}, {Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
		}})
		if appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	appendTurn("same-size-a", "turn-a", "message-a")
	appendTurn("same-size-b", "turn-b", "message-b")

	child, err := service.Fork(t.Context(), parent.Ref(), "turn-a", "child-after-a")
	if err != nil {
		t.Fatal(err)
	}
	childMessages := child.Session().Snapshot().Projection.ModelMessages
	if len(childMessages) != 1 || childMessages[0].ID != "message-a" {
		t.Fatalf("fork messages = %#v", childMessages)
	}
	if got := child.Session().Manifest().InheritedEvents; got != 3 {
		t.Fatalf("inherited event count = %d", got)
	}

	rewound, err := service.Rewind(t.Context(), parent.Ref(), "turn-b", "child-before-b")
	if err != nil {
		t.Fatal(err)
	}
	rewoundMessages := rewound.Session().Snapshot().Projection.ModelMessages
	if len(rewoundMessages) != 1 || rewoundMessages[0].ID != "message-a" {
		t.Fatalf("rewind messages = %#v", rewoundMessages)
	}

	for _, runtime := range []*Runtime{child, rewound, parent} {
		if err := service.Close(t.Context(), runtime.Ref()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServiceConcurrentOpenPublishesOneExactRuntime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	persistence := NewFilesystemPersistence(root)
	handle, err := persistence.Create(CreateOptions{SessionID: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	ref := SessionRef{HostID: "local", SessionID: "shared"}

	const callers = 16
	results := make(chan *Runtime, callers)
	errorsFound := make(chan error, callers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			<-start
			runtime, openErr := service.Open(context.Background(), ref)
			if openErr != nil {
				errorsFound <- openErr
				return
			}
			results <- runtime.Runtime()
		})
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)
	for openErr := range errorsFound {
		t.Fatalf("Open: %v", openErr)
	}
	var published *Runtime
	for runtime := range results {
		if published == nil {
			published = runtime
		} else if runtime != published {
			t.Fatal("concurrent Open returned multiple runtime instances")
		}
	}
	if published == nil {
		t.Fatal("no runtime published")
	}
	if !service.Detach(published) {
		t.Fatal("exact runtime did not detach")
	}
	if service.Detach(published) {
		t.Fatal("stale callback detached twice")
	}
	if err := published.close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestServicePrepareCreateIsInvisibleUntilExactPublish(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	prepared, err := service.PrepareCreate(t.Context(), CreateOptions{SessionID: "prepared"})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared.Runtime().Ref()
	if _, ok := service.Runtime(ref); ok {
		t.Fatal("prepared runtime was visible before publish")
	}
	if _, err := prepared.Runtime().Session().AppendBatch(t.Context(), "seed", []Event{{Kind: "diagnostic", Optional: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Runtime().Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	published, err := service.Publish(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if current, ok := service.Runtime(ref); !ok || current != published.Runtime() {
		t.Fatal("exact prepared runtime was not published")
	}
	if err := service.Discard(t.Context(), prepared); err == nil {
		t.Fatal("published runtime was discarded outside service close")
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestQueryListProjectsEventBackedTitleAndCompletedTurns(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("host-a", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "listed"})
	if err != nil {
		t.Fatal(err)
	}
	title, _ := json.Marshal(map[string]string{"title": "Event title"})
	if _, err := runtime.Session().AppendBatch(t.Context(), "title", []Event{{Kind: "session/title", Payload: title}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Append(t.Context(), Batch{OperationID: "turn", TurnID: "turn-1", Events: []Event{
		{Kind: "turn/start"}, {Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
	page, err := service.Query().List(t.Context(), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 {
		t.Fatalf("sessions = %+v", page.Sessions)
	}
	got := page.Sessions[0]
	if got.Ref != (SessionRef{HostID: "host-a", SessionID: "listed"}) || got.Codec != Codec || got.Title != "Event title" || got.Turns != 1 {
		t.Fatalf("listed session = %+v", got)
	}
}

func TestServiceDiscardPreparedCreateReleasesWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	prepared, err := service.PrepareCreate(t.Context(), CreateOptions{SessionID: "discarded"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Discard(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	handle, err := NewFilesystemPersistence(root).Open("discarded", ReadWrite)
	if err != nil {
		t.Fatalf("writer lease remained held after discard: %v", err)
	}
	if err := handle.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCancellationUsesOwnedActivityWithoutTurnID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "cancel"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, exec := bindTestExecution(t, runtime, "model")
	ref := runtime.Ref()
	snapshot, err := service.Cancel(ref)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != RuntimeCancelling {
		t.Fatalf("phase after cancel = %s", snapshot.Phase)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancel signal was not delivered")
	}
	exec.Finish()
	if got := runtime.Snapshot().Phase; got != RuntimeIdle {
		t.Fatalf("phase after activity exit = %s", got)
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func TestServiceObserveDistinguishesLiveAcceptedAndColdDurablePrefixes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "observe"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "accepted", []Event{{Kind: "diagnostic", Optional: true}}); err != nil {
		t.Fatal(err)
	}
	ref := runtime.Ref()
	live, err := service.Observe(t.Context(), ref, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if live.Runtime == nil || live.Runtime.Session.EventSequence != 1 || live.Runtime.Session.DurableSequence != 0 || len(live.Events.Commits) != 1 {
		t.Fatalf("live observe = %+v", live)
	}
	if !service.Detach(runtime) {
		t.Fatal("detach runtime")
	}
	// The detached writer still owns the unflushed prefix. A competing cold
	// read sees the durable prefix without creating an Agent or taking a lease.
	cold, err := service.Observe(t.Context(), ref, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if cold.Runtime != nil || len(cold.Events.Commits) != 0 {
		t.Fatalf("cold observe exposed unflushed events: %+v", cold)
	}
	if err := runtime.close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceClosePreservesBusyRuntime(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	_, exec := bindTestExecution(t, runtime, "tool")
	if err := service.Close(t.Context(), runtime.Ref()); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("Close busy error = %v", err)
	}
	if current, ok := service.Runtime(runtime.Ref()); !ok || current != runtime {
		t.Fatal("busy close released the exact runtime")
	}
	exec.Finish()
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceOpenClosesPersistedRuntimeWithoutRestoringAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	persistence := NewFilesystemPersistence(root)
	handle, err := persistence.Create(CreateOptions{SessionID: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	store := handle
	if _, err := store.Append(t.Context(), Batch{OperationID: "interrupted", TurnID: "turn", Events: []Event{
		{Kind: "turn/start"},
		{Kind: "tool/start", Payload: json.RawMessage(`{"id":"call","name":"bash"}`)},
		{Kind: "interaction/created", Payload: json.RawMessage(`{"id":"approval","state":"pending"}`)},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	binding, err := service.Open(t.Context(), SessionRef{HostID: "local", SessionID: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := binding.Runtime()
	snapshot := runtime.Snapshot()
	if snapshot.Phase != RuntimeIdle || snapshot.Session.Projection.TurnID != "" || len(snapshot.Session.Projection.ActiveTools) != 0 || len(snapshot.Session.Projection.Interactions) != 0 {
		t.Fatalf("restored stale runtime authority: %+v", snapshot)
	}
	if snapshot.Session.Projection.TurnStatus != "interrupted" {
		t.Fatalf("turn status = %q", snapshot.Session.Projection.TurnStatus)
	}
	if err := binding.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionQueryColdReadDoesNotAcquireWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	persistence := NewFilesystemPersistence(root)
	handle, err := persistence.Create(CreateOptions{SessionID: "cold"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "m", Role: provider.RoleUser, Content: "hello"}})
	if _, err := handle.Append(t.Context(), Batch{OperationID: "message", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", persistence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	ref := SessionRef{HostID: "local", SessionID: "cold"}
	history, err := service.Query().History(t.Context(), ref)
	if err != nil || len(history) != 1 || history[0].ID != "m" {
		t.Fatalf("cold history = %#v, %v", history, err)
	}
	// A query did not retain the writer lease; the execution runtime can attach.
	binding, err := service.Open(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
}

func TestServiceContinueLegacyPublishesNewIdentityAndLeavesSourceUnchanged(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "sessions", "old.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	source := agent.NewSession("system")
	source.Add(provider.Message{ID: "user", Role: provider.RoleUser, Content: "hello"})
	if err := source.Save(legacy); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(root, "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, migration, err := service.ContinueLegacy(t.Context(), legacy, "")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Ref().SessionID != migration.TargetID || runtime.Ref().SessionID == "old" {
		t.Fatalf("runtime=%+v migration=%+v", runtime.Ref(), migration)
	}
	after, err := os.ReadFile(legacy)
	if err != nil || string(after) != string(before) {
		t.Fatal("legacy source changed during continue")
	}
	if got := runtime.Session().Snapshot().Projection.ModelMessages; len(got) != 2 || got[1].ID != "user" {
		t.Fatalf("migrated history = %#v", got)
	}
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceExportAndDeleteAreSessionDirectoryAtomic(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "m", Role: provider.RoleUser, Content: strings.Repeat("exported", 20_000)}})
	if _, err := runtime.Session().AppendBatch(t.Context(), "message", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(t.TempDir(), "exported-session")
	if err := service.Export(t.Context(), runtime.Ref(), exported); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(exported, nil); err != nil {
		t.Fatalf("export is not self-contained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exported, "writer.lock")); !os.IsNotExist(err) {
		t.Fatalf("export copied writer ownership artifact: %v", err)
	}
	ref := runtime.Ref()
	if err := service.Delete(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ref.SessionID)); !os.IsNotExist(err) {
		t.Fatalf("deleted session remains visible: %v", err)
	}
	if _, err := service.Query().Snapshot(t.Context(), ref); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("deleted session was revived by query cache: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(exported, nil); err != nil {
		t.Fatalf("export depended on the source content store: %v", err)
	}
}

func TestServiceImportValidatesSelfContainedContentAndPublishesAtomically(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source", "sessions-v4")
	source, err := NewService("source", NewFilesystemPersistence(sourceRoot))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.CloseAll(context.Background()) })
	runtime, err := source.Create(t.Context(), CreateOptions{SessionID: "portable"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "large", Role: provider.RoleUser, Content: strings.Repeat("portable", 20_000)}})
	if _, err := runtime.Session().AppendBatch(t.Context(), "large", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err := source.Export(t.Context(), runtime.Ref(), bundle); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(sourceRoot)); err != nil {
		t.Fatal(err)
	}

	targetRoot := filepath.Join(t.TempDir(), "target", "sessions-v4")
	target, err := NewService("target", NewFilesystemPersistence(targetRoot))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.CloseAll(context.Background()) })
	ref, err := target.Import(t.Context(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	page := historyPageReady(t, target.Query(), ref, "", 10)
	if len(page.Messages) != 1 || page.Messages[0].ContentRef == nil {
		t.Fatalf("imported history = %+v", page)
	}
	if _, err := target.Import(t.Context(), bundle); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("duplicate import = %v", err)
	}
	entries, err := os.ReadDir(targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".portable.import-") {
			t.Fatalf("failed import left staging directory %q", entry.Name())
		}
	}
	headerTarget, err := NewService("desktop", NewFilesystemPersistence(filepath.Join(t.TempDir(), "desktop-sessions-v5", "by-id")))
	if err != nil {
		t.Fatal(err)
	}
	headerRef, err := headerTarget.ImportWithHeader(t.Context(), bundle, CreateOptions{SessionID: "portable", CWD: "/workspace", Origin: SessionOriginCanonicalImport})
	if err != nil {
		t.Fatal(err)
	}
	headerInfo, err := headerTarget.persistence.Stat(t.Context(), headerRef.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	// Headers record CWD in OS-native form; clean the expectation the same way.
	if headerInfo.CWD != filepath.Clean("/workspace") || headerInfo.Origin != SessionOriginCanonicalImport {
		t.Fatalf("imported header = %+v", headerInfo)
	}
	remapped, err := headerTarget.ImportWithHeader(t.Context(), bundle, CreateOptions{SessionID: "migr-conflict", CWD: "/workspace", Origin: SessionOriginCanonicalImport})
	if err != nil {
		t.Fatal(err)
	}
	if remapped.SessionID != "migr-conflict" {
		t.Fatalf("remapped import = %+v", remapped)
	}
	remappedPage := historyPageReady(t, headerTarget.Query(), remapped, "", 10)
	if len(remappedPage.Messages) != 1 || remappedPage.Messages[0].ContentRef == nil {
		t.Fatalf("remapped history = %+v", remappedPage)
	}
}

func TestDeleteRefusesAnOwnedSession(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	persistence := NewFilesystemPersistence(root)
	handle, err := persistence.Create(CreateOptions{SessionID: "owned"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := persistence.Delete(ctx, "owned"); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete owned session error = %v", err)
	}
	if err := handle.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledActivityCannotCommitLateBusinessResult(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "fenced"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, exec := bindTestExecution(t, runtime, "tool")
	if receipt, err := service.CancelSession(runtime.Ref()); err != nil || !receipt.Accepted || receipt.Phase != RuntimeCancelling {
		t.Fatalf("cancel receipt = %+v, %v", receipt, err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("cancel context = %v", ctx.Err())
	}
	if _, err := runtime.Session().Append(t.Context(), Batch{
		OperationID: "turn-end",
		TurnID:      "turn-1",
		Events:      []Event{{Kind: "turn/end", Payload: json.RawMessage(`{"status":"interrupted"}`)}},
	}); err != nil {
		t.Fatalf("terminal truth during cancel: %v", err)
	}
	exec.Finish()
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryOwnerCanCommitOnlyTerminalRecoveryFacts(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "recovery-terminal"})
	if err != nil {
		t.Fatal(err)
	}
	bindTestExecution(t, runtime, "tool")
	runtime.RequireRecovery("tool")
	if err := service.Close(t.Context(), runtime.Ref()); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("close during recovery = %v", err)
	}
	terminal := Batch{OperationID: "recovery-terminal", TurnID: "turn-1", Events: []Event{
		{Kind: "runtime/recovery", Payload: json.RawMessage(`{"state":"recovery_required","phase":"tool","reason":"cancellation_grace_expired","requires_user_decision":true}`)},
		{Kind: "turn/end", Payload: json.RawMessage(`{"status":"recovery_required"}`)},
	}}
	if _, err := runtime.Session().Append(t.Context(), terminal); err != nil {
		t.Fatalf("record recovery: %v", err)
	}
	projection := runtime.Session().Snapshot().Projection
	if projection.Recovery == nil || projection.Recovery.State != "recovery_required" || projection.TurnStatus != "recovery_required" {
		t.Fatalf("recovery projection = %#v, turn status = %q", projection.Recovery, projection.TurnStatus)
	}
	runtime.mu.Lock()
	runtime.phase = RuntimeIdle
	runtime.activity = ""
	runtime.mu.Unlock()
	if err := service.Close(t.Context(), runtime.Ref()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelSessionIsIdempotentWithoutAttachedRuntime(t *testing.T) {
	service, err := NewService("local", NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	ref := SessionRef{HostID: "local", SessionID: "not-attached"}
	receipt, err := service.CancelSession(ref)
	if err != nil || !receipt.Accepted || receipt.Phase != RuntimeIdle {
		t.Fatalf("idle cancel = %+v, %v", receipt, err)
	}
}
