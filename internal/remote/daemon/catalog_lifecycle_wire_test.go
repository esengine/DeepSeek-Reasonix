package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func openLifecycleWorkspace(t *testing.T, peer *daemonPeer, path string) protocol.WorkspaceOpenResult {
	t.Helper()
	browse := browseWorkspacePeer(t, peer, path)
	return openWorkspacePeer(t, peer, protocol.RequestID("open-"+filepath.Base(path)), browse.Directory.DirectoryRef)
}

func receiveCatalogChanged(t *testing.T, changes <-chan protocol.CatalogChanged) protocol.CatalogChanged {
	t.Helper()
	select {
	case change := <-changes:
		return change
	case <-time.After(testRequestTimeout):
		t.Fatal("catalog/changed was not emitted")
		return protocol.CatalogChanged{}
	}
}

func requireNoCatalogChanged(t *testing.T, changes <-chan protocol.CatalogChanged) {
	t.Helper()
	select {
	case change := <-changes:
		t.Fatalf("unexpected catalog/changed: %+v", change)
	case <-time.After(75 * time.Millisecond):
	}
}

func catalogChangedPeer(t *testing.T, server *Server) (*daemonPeer, <-chan protocol.CatalogChanged) {
	t.Helper()
	changes := make(chan protocol.CatalogChanged, 32)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodCatalogChanged), func(_ context.Context, raw json.RawMessage) {
			var change protocol.CatalogChanged
			if json.Unmarshal(raw, &change) == nil {
				changes <- change
			}
		})
	})
	return peer, changes
}

func TestSessionCreateCommittedRuntimeFailureEmitsCatalogChangedOnce(t *testing.T) {
	options, delegate, buildID := daemonTestServerOptions(t, nil)
	factory := &daemonFailFirstFactory{delegate: delegate, failure: errors.New("injected runtime startup failure")}
	options.ControllerFactory = factory
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer, changes := catalogChangedPeer(t, server)
	initializePeer(t, peer, buildID, "client-create-commit-notify", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	receiveCatalogChanged(t, changes) // workspace/open

	precommit := protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "create-precommit-error", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  "workspace-missing", AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionCreate, precommit), protocol.ErrWorkspaceNotFound)
	requireNoCatalogChanged(t, changes)

	params := precommit
	params.RequestID = "create-committed-runtime-failure"
	params.WorkspaceID = opened.Workspace.WorkspaceID
	first := requestError(t, peer, protocol.MethodSessionCreate, params)
	data := requireRemoteError(t, first, protocol.ErrRuntimeStartFailed)
	if data.Target == nil || data.Target.WorkspaceID != opened.Workspace.WorkspaceID || data.Target.SessionID == "" {
		t.Fatalf("runtime failure target = %+v", data.Target)
	}
	change := receiveCatalogChanged(t, changes)
	if change.Scope != protocol.CatalogWorkspace || change.Revision != server.catalog.Revision() ||
		len(change.AffectedWorkspaceIDs) != 1 || change.AffectedWorkspaceIDs[0] != opened.Workspace.WorkspaceID ||
		!reflect.DeepEqual(change.Kinds, []protocol.CatalogKind{protocol.CatalogSessions, protocol.CatalogTopics}) {
		t.Fatalf("committed create catalog/changed = %+v", change)
	}

	replayed := requestError(t, peer, protocol.MethodSessionCreate, params)
	replayedData := requireRemoteError(t, replayed, protocol.ErrRuntimeStartFailed)
	if replayedData.Target == nil || *replayedData.Target != *data.Target || factory.count() != 1 {
		t.Fatalf("runtime failure replay = %+v; factory calls=%d", replayedData, factory.count())
	}
	requireNoCatalogChanged(t, changes)
}

func TestSessionPurgeCommittedCleanupFailureEmitsCatalogChangedOnce(t *testing.T) {
	options, _, buildID := daemonTestServerOptions(t, nil)
	userHome := t.TempDir()
	var failCleanup atomic.Bool
	catalogValue, err := catalog.New("host-test", catalog.Options{
		StateDir: filepath.Join(t.TempDir(), "catalog"), UserHome: userHome,
		SessionDir:      func(string) string { return filepath.Join(userHome, ".sessions") },
		ProfileResolver: daemonProfileResolver{},
		RemoveAll: func(path string) error {
			if failCleanup.Load() && strings.HasSuffix(filepath.Clean(path), ".purging") {
				return errors.New("injected staged purge cleanup failure")
			}
			return os.RemoveAll(path)
		},
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
	peer, changes := catalogChangedPeer(t, server)
	initializePeer(t, peer, buildID, "client-purge-commit-notify", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	receiveCatalogChanged(t, changes) // workspace/open
	created := createSessionPeer(t, peer, "purge-notify-create", opened.Workspace.WorkspaceID)
	receiveCatalogChanged(t, changes) // session/create
	closed := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "purge-notify-close", ExpectedHostEpoch: "host-test", Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
	}})
	if closed.Disposition != protocol.SessionReleased {
		t.Fatalf("session close = %+v", closed)
	}
	receiveCatalogChanged(t, changes)

	precommit := protocol.SessionPurgeParams{
		SessionRecordMutation: protocol.SessionRecordMutation{
			RequestID: "purge-precommit-error", ExpectedHostEpoch: "host-test", Target: created.Target,
		},
		Guard: protocol.TrashNormal,
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionPurge, precommit), protocol.ErrTrashEntryNotFound)
	requireNoCatalogChanged(t, changes)

	trashed := requestResult[protocol.SessionTrashResult](t, peer, protocol.MethodSessionTrash, protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{
			RequestID: "purge-notify-trash", ExpectedHostEpoch: "host-test", Target: created.Target,
		},
		Guard: protocol.TrashNormal,
	})
	if trashed.Disposition != protocol.DispositionTrashed {
		t.Fatalf("session trash = %+v", trashed)
	}
	receiveCatalogChanged(t, changes)

	failCleanup.Store(true)
	params := precommit
	params.RequestID = "purge-committed-cleanup-failure"
	first := requestError(t, peer, protocol.MethodSessionPurge, params)
	requireRemoteError(t, first, protocol.ErrSessionCleanupPending)
	change := receiveCatalogChanged(t, changes)
	if change.Scope != protocol.CatalogWorkspace || change.Revision != server.catalog.Revision() ||
		len(change.AffectedWorkspaceIDs) != 1 || change.AffectedWorkspaceIDs[0] != created.Target.WorkspaceID ||
		!reflect.DeepEqual(change.Kinds, []protocol.CatalogKind{protocol.CatalogTrash, protocol.CatalogTopics}) {
		t.Fatalf("committed purge catalog/changed = %+v", change)
	}
	trash := requestResult[protocol.SessionTrashListResult](t, peer, protocol.MethodSessionTrashList, protocol.SessionTrashListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: created.Target.WorkspaceID,
	})
	if len(trash.Items) != 0 {
		t.Fatalf("logically purged Session remained in trash: %+v", trash.Items)
	}

	replayed := requestError(t, peer, protocol.MethodSessionPurge, params)
	requireRemoteError(t, replayed, protocol.ErrSessionCleanupPending)
	requireNoCatalogChanged(t, changes)
}

func TestCatalogLifecycleWireUsesRealCatalogAndColdRuntimeTransitions(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-lifecycle", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	workspaceID := opened.Workspace.WorkspaceID

	workspaceCatalog := requestResult[protocol.WorkspaceCatalogResult](t, peer, protocol.MethodCatalogWorkspace, protocol.WorkspaceCatalogParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: workspaceID,
	})
	if workspaceCatalog.Revision == "" || len(workspaceCatalog.Models) != 1 || workspaceCatalog.Models[0].Ref != "test/test-model" {
		t.Fatalf("catalog/workspace = %+v", workspaceCatalog)
	}
	emptyTopics := requestResult[protocol.TopicListResult](t, peer, protocol.MethodTopicList, protocol.TopicListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: workspaceID,
	})
	if emptyTopics.Items == nil || len(emptyTopics.Items) != 0 || emptyTopics.HasMore || emptyTopics.NextCursor != "" {
		t.Fatalf("empty topic/list must return an empty JSON array: %+v", emptyTopics)
	}

	topic := requestResult[protocol.TopicCreateResult](t, peer, protocol.MethodTopicCreate, protocol.TopicCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-create", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  workspaceID, Title: "Lifecycle",
	})
	topics := requestResult[protocol.TopicListResult](t, peer, protocol.MethodTopicList, protocol.TopicListParams{ExpectedHostEpoch: "host-test", WorkspaceID: workspaceID})
	if len(topics.Items) != 1 || topics.Items[0].TopicID != topic.TopicID {
		t.Fatalf("topic/list = %+v", topics)
	}
	renamedTopic := requestResult[protocol.TopicRenameResult](t, peer, protocol.MethodTopicRename, protocol.TopicRenameParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-rename", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  workspaceID, TopicID: topic.TopicID, Title: "Renamed Lifecycle",
	})
	if renamedTopic.Title != "Renamed Lifecycle" {
		t.Fatalf("topic/rename = %+v", renamedTopic)
	}

	created := requestResult[protocol.SessionCreateResult](t, peer, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "session-create-existing", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  workspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: topic.TopicID}, Profile: protocol.ProfileSelection{},
	})
	nonempty := requestError(t, peer, protocol.MethodTopicDelete, protocol.TopicDeleteParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-delete-nonempty", ExpectedHostEpoch: "host-test"},
		WorkspaceID:  workspaceID, TopicID: topic.TopicID,
	})
	requireRemoteError(t, nonempty, protocol.ErrTopicNotEmpty)
	renamedSession := requestResult[protocol.SessionRenameResult](t, peer, protocol.MethodSessionRename, protocol.SessionRenameParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "session-rename", ExpectedHostEpoch: "host-test", Target: created.Target},
		Title:                 "Renamed Session",
	})
	if renamedSession.Title != "Renamed Session" {
		t.Fatalf("session/rename = %+v", renamedSession)
	}

	closed := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "session-close", ExpectedHostEpoch: "host-test", Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
	}})
	if closed.Disposition != protocol.SessionReleased {
		t.Fatalf("session/close = %+v", closed)
	}
	trashed := requestResult[protocol.SessionTrashResult](t, peer, protocol.MethodSessionTrash, protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "session-trash", ExpectedHostEpoch: "host-test", Target: created.Target},
		Guard:                 protocol.TrashNormal,
	})
	if trashed.Disposition != protocol.DispositionTrashed {
		t.Fatalf("session/trash = %+v", trashed)
	}
	trash := requestResult[protocol.SessionTrashListResult](t, peer, protocol.MethodSessionTrashList, protocol.SessionTrashListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: workspaceID,
	})
	if len(trash.Items) != 1 || trash.Items[0].Target != created.Target {
		t.Fatalf("session/trashList = %+v", trash)
	}
	restored := requestResult[protocol.SessionRestoreResult](t, peer, protocol.MethodSessionRestore, protocol.SessionRestoreParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "session-restore", ExpectedHostEpoch: "host-test", Target: created.Target},
	})
	if restored.Target != created.Target || restored.Disposition != protocol.SessionRestored {
		t.Fatalf("session/restore = %+v", restored)
	}
	alreadyClosed := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "session-close-cold", ExpectedHostEpoch: "host-test", Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
	}})
	if alreadyClosed.Disposition != protocol.SessionAlreadyClosed {
		t.Fatalf("cold session/close = %+v", alreadyClosed)
	}
	requestResult[protocol.SessionTrashResult](t, peer, protocol.MethodSessionTrash, protocol.SessionTrashParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "session-trash-again", ExpectedHostEpoch: "host-test", Target: created.Target}, Guard: protocol.TrashNormal,
	})
	purged := requestResult[protocol.SessionPurgeResult](t, peer, protocol.MethodSessionPurge, protocol.SessionPurgeParams{
		SessionRecordMutation: protocol.SessionRecordMutation{RequestID: "session-purge", ExpectedHostEpoch: "host-test", Target: created.Target}, Guard: protocol.TrashNormal,
	})
	if !purged.Purged {
		t.Fatal("session/purge returned false")
	}
	deleted := requestResult[protocol.TopicDeleteResult](t, peer, protocol.MethodTopicDelete, protocol.TopicDeleteParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-delete", ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID, TopicID: topic.TopicID,
	})
	if !deleted.Deleted {
		t.Fatal("topic/delete returned false")
	}

	topicCascade := requestResult[protocol.TopicCreateResult](t, peer, protocol.MethodTopicCreate, protocol.TopicCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-cascade-create", ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID, Title: "Cascade",
	})
	cascadeSession := requestResult[protocol.SessionCreateResult](t, peer, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-cascade-session", ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID,
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Topic: protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: topicCascade.TopicID}, Profile: protocol.ProfileSelection{},
	})
	cascade := requestResult[protocol.TopicTrashResult](t, peer, protocol.MethodTopicTrash, protocol.TopicTrashParams{
		HostMutation: protocol.HostMutation{RequestID: "topic-cascade-trash", ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID, TopicID: topicCascade.TopicID,
	})
	if cascade.Disposition != protocol.DispositionTrashed || cascade.TrashedSessions != 1 {
		t.Fatalf("topic/trash = %+v", cascade)
	}
	if factory.count() != 2 {
		t.Fatalf("lifecycle controllers = %d, want 2", factory.count())
	}
	if _, exists := server.runtimes.Runtime(cascadeSession.Target); exists {
		t.Fatal("topic/trash retained a live member runtime")
	}

	workspaceClosed := requestResult[protocol.WorkspaceCloseResult](t, peer, protocol.MethodWorkspaceClose, protocol.WorkspaceCloseParams{
		HostMutation: protocol.HostMutation{RequestID: "workspace-close", ExpectedHostEpoch: "host-test"}, WorkspaceID: workspaceID,
	})
	if workspaceClosed.Disposition != protocol.WorkspaceClosed {
		t.Fatalf("workspace/close = %+v", workspaceClosed)
	}
}

func TestWorkspaceCloseBusyAndPersistenceFailureAreAtomicAndRetryable(t *testing.T) {
	options, factory, buildID, stateDir := daemonTestServerOptionsWithCatalogState(t, nil)
	options.allowUncataloguedTestRuntimes = false
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-workspace-close", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	created := createSessionPeer(t, peer, "workspace-close-session", opened.Workspace.WorkspaceID)
	subscription := subscribePeer(t, peer, created.Target)
	busyParams := protocol.WorkspaceCloseParams{
		HostMutation: protocol.HostMutation{RequestID: "workspace-close-busy", ExpectedHostEpoch: "host-test"}, WorkspaceID: opened.Workspace.WorkspaceID,
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodWorkspaceClose, busyParams), protocol.ErrWorkspaceInUse)
	requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{SubscriptionID: subscription.SubscriptionID})
	// The first deterministic rejection is replayed even though the workspace is
	// now idle; a new semantic operation needs a new requestId.
	requireRemoteError(t, requestError(t, peer, protocol.MethodWorkspaceClose, busyParams), protocol.ErrWorkspaceInUse)

	backup := stateDir + ".backup"
	if err := os.Rename(stateDir, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, []byte("block catalog directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	retryParams := busyParams
	retryParams.RequestID = "workspace-close-persist-retry"
	requireRemoteError(t, requestError(t, peer, protocol.MethodWorkspaceClose, retryParams), protocol.ErrSessionPersistFailed)
	// Abort reopened the exact actor admission; the failed durable close did not
	// release or replace the runtime.
	resubscribed := subscribePeer(t, peer, created.Target)
	if factory.count() != 1 || resubscribed.Snapshot.RuntimeEpoch != created.RuntimeEpoch {
		t.Fatalf("failed close changed runtime: controllers=%d snapshot=%+v", factory.count(), resubscribed.Snapshot)
	}
	requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{SubscriptionID: resubscribed.SubscriptionID})
	if err := os.Remove(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, stateDir); err != nil {
		t.Fatal(err)
	}
	closed := requestResult[protocol.WorkspaceCloseResult](t, peer, protocol.MethodWorkspaceClose, retryParams)
	if closed.Disposition != protocol.WorkspaceClosed {
		t.Fatalf("workspace close retry = %+v", closed)
	}
	if _, exists := server.runtimes.Runtime(created.Target); exists {
		t.Fatal("successful workspace close retained runtime")
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: "host-test", Target: created.Target, PageTurns: 60,
	}), protocol.ErrWorkspaceNotFound)
}

func TestSessionCloseSnapshotFailureRetryAlreadyClosedAndResponseLoss(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	peer := openDaemonPeer(t, server, nil, nil)
	grant := initializePeer(t, peer, buildID, "client-session-close", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	created := createSessionPeer(t, peer, "close-session-create", opened.Workspace.WorkspaceID)
	controller := factory.controller(t, 0)
	controller.mu.Lock()
	controller.snapshotErr = errors.New("disk full")
	controller.mu.Unlock()
	params := protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "close-snapshot-retry", ExpectedHostEpoch: "host-test", Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
	}}
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionClose, params), protocol.ErrSessionPersistFailed)
	controller.mu.Lock()
	controller.snapshotErr = nil
	controller.mu.Unlock()
	closed := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, params)
	if closed.Disposition != protocol.SessionReleased {
		t.Fatalf("close retry = %+v", closed)
	}
	already := params
	already.RequestID = "close-already"
	if result := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, already); result.Disposition != protocol.SessionAlreadyClosed {
		t.Fatalf("already closed = %+v", result)
	}

	second := createSessionPeer(t, peer, "close-loss-create", opened.Workspace.WorkspaceID)
	peer.close(t)
	dropped := make(chan struct{})
	dropPeer := openDaemonPeer(t, server, func(connection net.Conn) net.Conn {
		return &daemonDropMatchingResponseConn{Conn: connection, match: []byte(`"disposition":"released"`), dropped: dropped}
	}, nil)
	initializePeer(t, dropPeer, buildID, "client-session-close", grant.Lease.LeaseID)
	lossParams := protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "close-response-loss", ExpectedHostEpoch: "host-test", Target: second.Target, ExpectedRuntimeEpoch: second.RuntimeEpoch,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), testRequestTimeout)
	_, err := dropPeer.wire.Request(ctx, string(protocol.MethodSessionClose), lossParams)
	cancel()
	if err == nil {
		t.Fatal("session/close unexpectedly delivered dropped response")
	}
	select {
	case <-dropped:
	case <-time.After(testRequestTimeout):
		t.Fatal("session/close response was not dropped")
	}
	dropPeer.close(t)
	retryPeer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, retryPeer, buildID, "client-session-close", grant.Lease.LeaseID)
	if result := requestResult[protocol.SessionCloseResult](t, retryPeer, protocol.MethodSessionClose, lossParams); result.Disposition != protocol.SessionReleased {
		t.Fatalf("response-loss replay = %+v", result)
	}
	secondController := factory.controller(t, 1)
	secondController.mu.Lock()
	snapshots, closes := secondController.snapshotCalls, secondController.closeCalls
	secondController.mu.Unlock()
	if snapshots != 1 || closes != 1 {
		t.Fatalf("response-loss snapshot/close = %d/%d", snapshots, closes)
	}
}

func TestCatalogChangedIsEmittedOncePerOwnedMutationNotReplay(t *testing.T) {
	server, _, buildID := newDaemonTestServer(t)
	changes := make(chan protocol.CatalogChanged, 32)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodCatalogChanged), func(_ context.Context, raw json.RawMessage) {
			var change protocol.CatalogChanged
			if json.Unmarshal(raw, &change) == nil {
				changes <- change
			}
		})
	})
	initializePeer(t, peer, buildID, "client-catalog-change", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	time.Sleep(25 * time.Millisecond)
	for len(changes) > 0 {
		<-changes
	}
	params := protocol.TopicCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "notify-once", ExpectedHostEpoch: "host-test"}, WorkspaceID: opened.Workspace.WorkspaceID, Title: "Notify",
	}
	first := requestResult[protocol.TopicCreateResult](t, peer, protocol.MethodTopicCreate, params)
	var observed protocol.CatalogChanged
	select {
	case observed = <-changes:
	case <-time.After(testRequestTimeout):
		t.Fatal("catalog/changed was not emitted")
	}
	if observed.Scope != protocol.CatalogWorkspace || len(observed.AffectedWorkspaceIDs) != 1 || observed.AffectedWorkspaceIDs[0] != opened.Workspace.WorkspaceID || len(observed.Kinds) != 1 || observed.Kinds[0] != protocol.CatalogTopics {
		t.Fatalf("catalog/changed = %+v", observed)
	}
	if replay := requestResult[protocol.TopicCreateResult](t, peer, protocol.MethodTopicCreate, params); replay != first {
		t.Fatalf("topic/create replay = %+v, want %+v", replay, first)
	}
	select {
	case duplicate := <-changes:
		t.Fatalf("replayed mutation emitted duplicate catalog/changed: %+v", duplicate)
	case <-time.After(75 * time.Millisecond):
	}

	created := requestResult[protocol.SessionCreateResult](t, peer, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "notify-close-create", ExpectedHostEpoch: "host-test"}, WorkspaceID: opened.Workspace.WorkspaceID,
		AdditionalDirectoryRefs: []protocol.DirectoryRef{}, Topic: protocol.TopicSelection{Kind: protocol.TopicExisting, TopicID: first.TopicID}, Profile: protocol.ProfileSelection{},
	})
	select {
	case <-changes:
	case <-time.After(testRequestTimeout):
		t.Fatal("session/create catalog notification missing")
	}
	for len(changes) > 0 {
		<-changes
	}
	subscription := subscribePeer(t, peer, created.Target)
	closeParams := protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		RequestID: "notify-close-active", ExpectedHostEpoch: "host-test", Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
	}}
	if result := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, closeParams); result.Disposition != protocol.SessionRetainedActive {
		t.Fatalf("active session/close = %+v", result)
	}
	select {
	case change := <-changes:
		t.Fatalf("retained_active emitted catalog/changed: %+v", change)
	case <-time.After(50 * time.Millisecond):
	}
	requestResult[protocol.SessionUnsubscribeResult](t, peer, protocol.MethodSessionUnsubscribe, protocol.SessionUnsubscribeParams{SubscriptionID: subscription.SubscriptionID})
	closeParams.RequestID = "notify-close-released"
	if result := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, closeParams); result.Disposition != protocol.SessionReleased {
		t.Fatalf("released session/close = %+v", result)
	}
	select {
	case change := <-changes:
		if len(change.Kinds) != 1 || change.Kinds[0] != protocol.CatalogSessions {
			t.Fatalf("released catalog/changed = %+v", change)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("released session/close catalog notification missing")
	}
	closeParams.RequestID = "notify-close-already"
	if result := requestResult[protocol.SessionCloseResult](t, peer, protocol.MethodSessionClose, closeParams); result.Disposition != protocol.SessionAlreadyClosed {
		t.Fatalf("already_closed session/close = %+v", result)
	}
	select {
	case change := <-changes:
		t.Fatalf("already_closed emitted catalog/changed: %+v", change)
	case <-time.After(50 * time.Millisecond):
	}
}
