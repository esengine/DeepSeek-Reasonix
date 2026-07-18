package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func (c *snapshotWireController) BranchSession(name string) (string, error) {
	return c.persistRemoteWireBranch(-1, name)
}

func (c *snapshotWireController) ForkSession(turn int, name string) (string, error) {
	return c.persistRemoteWireBranch(turn, name)
}

func (c *snapshotWireController) persistRemoteWireBranch(turn int, name string) (string, error) {
	history := c.History()
	session := agent.NewSession("")
	session.Messages = append([]provider.Message(nil), history...)
	path := agent.NewSessionPath(c.SessionDir(), "wire-branch")
	if err := session.Save(path); err != nil {
		return "", err
	}
	preview, turns := agent.SessionPreviewFromMessages(history)
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		Name: name, ParentID: agent.BranchID(c.SessionPath()), ForkTurn: turn,
		ForkMessageIndex: len(history), Preview: preview, Turns: turns, SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		return "", err
	}
	return path, nil
}

func (c *snapshotWireController) RewindDetailed(_ int, scope control.RewindScope) (control.RewindResult, error) {
	return control.RewindResult{
		WorkspaceChanged:      scope != control.RewindConversation,
		ConversationRewritten: scope != control.RewindCode,
	}, nil
}

func newLifecycleComposerWireServer(t *testing.T) (*Server, *snapshotWireFactory, protocol.BuildID) {
	t.Helper()
	options, _, buildID := daemonTestServerOptions(t, nil)
	factory := &snapshotWireFactory{}
	factory.configure = func(target protocol.RuntimeTarget, controller *snapshotWireController) {
		resolved, err := options.Catalog.ResolveRuntimeTarget(context.Background(), target)
		if err != nil {
			panic(fmt.Sprintf("resolve lifecycle wire target: %v", err))
		}
		controller.workspaceRoot = resolved.WorkspaceRoot
		controller.sessionPath = resolved.SessionPath
		controller.history = []provider.Message{
			{Role: provider.RoleUser, Content: "wire branch prompt"},
			{Role: provider.RoleAssistant, Content: "wire branch answer"},
		}
		controller.checkpointView = control.CheckpointSnapshot{
			Metas:               []checkpoint.Meta{{Turn: 0, Time: time.Now().UTC(), Prompt: "wire branch prompt", Paths: []string{"file.txt"}}},
			TurnsByMessageIndex: map[int]int{0: 0}, ConversationAvailable: map[int]bool{0: true},
		}
	}
	options.ControllerFactory = factory
	options.allowUncataloguedTestRuntimes = false
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, factory, buildID
}

func (f *snapshotWireFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.controllers)
}

func TestRawComposerLifecycleWireUsesSubmitUnionExactReplayAndCatalogNotifications(t *testing.T) {
	server, factory, buildID := newDaemonTestServer(t)
	changes := make(chan protocol.CatalogChanged, 32)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodCatalogChanged), func(_ context.Context, raw json.RawMessage) {
			var change protocol.CatalogChanged
			if json.Unmarshal(raw, &change) == nil {
				changes <- change
			}
		})
	})
	initializePeer(t, peer, buildID, "raw-lifecycle-client", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	created := createSessionPeer(t, peer, "raw-lifecycle-create", opened.Workspace.WorkspaceID)
	drainCatalogChanges(changes)

	newParams := protocol.SessionSubmitParams{
		SessionMutation: mutation("raw-wire-new", created.Target, created.RuntimeEpoch),
		Input:           "/new", DisplayText: "/new",
	}
	newResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, newParams)
	requireComposerReplacementResult(t, newResult, protocol.EffectSessionReplaced, created.Target, false)
	requireCatalogChange(t, changes, newResult.Target.WorkspaceID, protocol.CatalogSessions)
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, newParams); replay != newResult {
		t.Fatalf("raw /new replay = %+v, want %+v", replay, newResult)
	}
	requireNoCatalogChanged(t, changes)
	newConflict := newParams
	newConflict.DisplayText = "changed display"
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionSubmit, newConflict), protocol.ErrRequestIDConflict)
	if factory.count() != 2 {
		t.Fatalf("raw /new controllers = %d, want 2", factory.count())
	}

	effortParams := protocol.SessionSubmitParams{
		SessionMutation: mutation("raw-wire-effort", newResult.Target, newResult.RuntimeEpoch),
		Input:           "/effort high", DisplayText: "/effort high",
	}
	effortResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, effortParams)
	if effortResult.Kind != protocol.SubmitCompleted || effortResult.Effect != protocol.EffectRuntimeReplaced ||
		!effortResult.SnapshotRequired || effortResult.Target != newResult.Target || effortResult.RuntimeEpoch == newResult.RuntimeEpoch {
		t.Fatalf("raw /effort result = %+v", effortResult)
	}
	if err := effortResult.Validate(); err != nil {
		t.Fatalf("raw /effort invalid result: %v", err)
	}
	change := requireCatalogChange(t, changes, effortResult.Target.WorkspaceID, protocol.CatalogSessions)
	if !containsCatalogKind(change.Kinds, protocol.CatalogSessionCatalog) {
		t.Fatalf("raw /effort catalog kinds = %+v", change.Kinds)
	}
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, effortParams); replay != effortResult {
		t.Fatalf("raw /effort replay = %+v, want %+v", replay, effortResult)
	}
	requireNoCatalogChanged(t, changes)
	if factory.count() != 3 {
		t.Fatalf("raw /effort controllers = %d, want 3", factory.count())
	}

	clearParams := protocol.SessionSubmitParams{
		SessionMutation: mutation("raw-wire-clear", effortResult.Target, effortResult.RuntimeEpoch),
		Input:           "/clear", DisplayText: "/clear",
	}
	clearResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, clearParams)
	requireComposerReplacementResult(t, clearResult, protocol.EffectSessionReplaced, effortResult.Target, false)
	requireCatalogChange(t, changes, clearResult.Target.WorkspaceID, protocol.CatalogSessions)
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, clearParams); replay != clearResult {
		t.Fatalf("raw /clear replay = %+v, want %+v", replay, clearResult)
	}
	requireNoCatalogChanged(t, changes)
	clearConflict := clearParams
	clearConflict.Input = "/clear changed"
	clearConflict.DisplayText = clearConflict.Input
	requireRemoteError(t, requestError(t, peer, protocol.MethodSessionSubmit, clearConflict), protocol.ErrRequestIDConflict)
	if factory.count() != 4 {
		t.Fatalf("raw /clear controllers = %d, want 4", factory.count())
	}
}

func TestRawComposerBranchMigratesTargetAndRewindRefreshesInPlaceWithoutFakeTurn(t *testing.T) {
	server, factory, buildID := newLifecycleComposerWireServer(t)
	changes := make(chan protocol.CatalogChanged, 16)
	resyncs := make(chan protocol.SessionResyncRequired, 16)
	peer := openDaemonPeer(t, server, nil, func(connection *rpcwire.Conn) {
		connection.HandleNotify(string(protocol.MethodCatalogChanged), func(_ context.Context, raw json.RawMessage) {
			var change protocol.CatalogChanged
			if json.Unmarshal(raw, &change) == nil {
				changes <- change
			}
		})
		connection.HandleNotify(string(protocol.MethodSessionResyncRequired), func(_ context.Context, raw json.RawMessage) {
			var resync protocol.SessionResyncRequired
			if json.Unmarshal(raw, &resync) == nil {
				resyncs <- resync
			}
		})
	})
	initializePeer(t, peer, buildID, "raw-branch-rewind-client", "")
	opened := openLifecycleWorkspace(t, peer, t.TempDir())
	created := createSessionPeer(t, peer, "raw-branch-create", opened.Workspace.WorkspaceID)
	subscription := subscribePeer(t, peer, created.Target)
	if len(subscription.Snapshot.Checkpoints) != 1 {
		t.Fatalf("raw branch source checkpoints = %+v", subscription.Snapshot.Checkpoints)
	}
	parentCheckpointID := subscription.Snapshot.Checkpoints[0].CheckpointID
	drainCatalogChanges(changes)

	branchParams := protocol.SessionSubmitParams{
		SessionMutation: mutation("raw-wire-branch", created.Target, created.RuntimeEpoch),
		Input:           "/branch 1 wire child", DisplayText: "/branch 1 wire child",
	}
	branchResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, branchParams)
	requireComposerReplacementResult(t, branchResult, protocol.EffectSessionReplaced, created.Target, false)
	if branchResult.TurnID != "" || branchResult.OperationID != "" {
		t.Fatalf("raw /branch fabricated execution identity: %+v", branchResult)
	}
	select {
	case resync := <-resyncs:
		if resync.SubscriptionID != subscription.SubscriptionID || resync.Reason != protocol.ResyncTargetReplaced ||
			resync.Target != created.Target || resync.RuntimeEpoch != created.RuntimeEpoch ||
			resync.ReplacementTarget == nil || *resync.ReplacementTarget != branchResult.Target ||
			resync.ReplacementRuntimeEpoch != branchResult.RuntimeEpoch {
			t.Fatalf("raw /branch target replacement resync = %+v", resync)
		}
	case <-time.After(testRequestTimeout):
		t.Fatal("raw /branch did not emit target replacement resync")
	}
	requireCatalogChange(t, changes, branchResult.Target.WorkspaceID, protocol.CatalogSessions)
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, branchParams); replay != branchResult {
		t.Fatalf("raw /branch replay = %+v, want %+v", replay, branchResult)
	}
	requireNoCatalogChanged(t, changes)
	if factory.controller(t, 0).SessionPath() == factory.controller(t, 1).SessionPath() {
		t.Fatal("raw /branch child controller reused source transcript")
	}
	listed := requestResult[protocol.SessionListResult](t, peer, protocol.MethodSessionList, protocol.SessionListParams{
		ExpectedHostEpoch: "host-test", WorkspaceID: branchResult.Target.WorkspaceID,
	})
	var child *protocol.SessionSummary
	for index := range listed.Items {
		if listed.Items[index].Target == branchResult.Target {
			child = &listed.Items[index]
			break
		}
	}
	if child == nil || child.BranchSource == nil || child.BranchSource.ParentTarget != created.Target ||
		child.BranchSource.ParentCheckpointID != parentCheckpointID {
		t.Fatalf("raw /branch catalog ancestry = %+v", child)
	}

	childSubscription := subscribePeer(t, peer, branchResult.Target)
	if len(childSubscription.Snapshot.Checkpoints) != 1 {
		t.Fatalf("raw rewind child checkpoints = %+v", childSubscription.Snapshot.Checkpoints)
	}
	rewindParams := protocol.SessionSubmitParams{
		SessionMutation: mutation("raw-wire-rewind", branchResult.Target, branchResult.RuntimeEpoch),
		Input:           "/rewind 0 both", DisplayText: "/rewind 0 both",
	}
	rewindResult := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, rewindParams)
	if rewindResult.Kind != protocol.SubmitCompleted || rewindResult.Effect != protocol.EffectStateChanged ||
		!rewindResult.SnapshotRequired || rewindResult.Target != branchResult.Target ||
		rewindResult.RuntimeEpoch != branchResult.RuntimeEpoch || rewindResult.TurnID != "" || rewindResult.OperationID != "" {
		t.Fatalf("raw /rewind did not refresh in place or fabricated identity: %+v", rewindResult)
	}
	if err := rewindResult.Validate(); err != nil {
		t.Fatalf("raw /rewind invalid result: %v", err)
	}
	if replay := requestResult[protocol.SessionSubmitResult](t, peer, protocol.MethodSessionSubmit, rewindParams); replay != rewindResult {
		t.Fatalf("raw /rewind replay = %+v, want %+v", replay, rewindResult)
	}
	requireNoCatalogChanged(t, changes)
	select {
	case resync := <-resyncs:
		t.Fatalf("in-place raw /rewind emitted replacement resync: %+v", resync)
	case <-time.After(75 * time.Millisecond):
	}
	if factory.count() != 2 {
		t.Fatalf("raw /rewind rebuilt Controller: factory count=%d", factory.count())
	}
	if _, exists := server.runtimes.Runtime(created.Target); exists {
		t.Fatal("raw /branch retained source runtime after target migration")
	}
	current, exists := server.runtimes.Runtime(rewindResult.Target)
	if !exists || current.Epoch() != rewindResult.RuntimeEpoch {
		t.Fatalf("raw /rewind current runtime = %+v exists=%v", current, exists)
	}
}

func requireComposerReplacementResult(
	t *testing.T,
	result protocol.SessionSubmitResult,
	effect protocol.SubmitEffect,
	previous protocol.RuntimeTarget,
	sameTarget bool,
) {
	t.Helper()
	if result.Kind != protocol.SubmitCompleted || result.Effect != effect || !result.SnapshotRequired || result.RuntimeEpoch == "" {
		t.Fatalf("composer replacement result = %+v", result)
	}
	if sameTarget && result.Target != previous {
		t.Fatalf("composer target = %+v, want unchanged %+v", result.Target, previous)
	}
	if !sameTarget && result.Target == previous {
		t.Fatalf("composer target was not replaced: %+v", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("invalid composer replacement result: %v", err)
	}
}

func requireCatalogChange(
	t *testing.T,
	changes <-chan protocol.CatalogChanged,
	workspaceID protocol.WorkspaceID,
	want protocol.CatalogKind,
) protocol.CatalogChanged {
	t.Helper()
	select {
	case change := <-changes:
		if change.Scope != protocol.CatalogWorkspace || len(change.AffectedWorkspaceIDs) != 1 ||
			change.AffectedWorkspaceIDs[0] != workspaceID || !containsCatalogKind(change.Kinds, want) {
			t.Fatalf("catalog/changed = %+v, want workspace=%s kind=%s", change, workspaceID, want)
		}
		return change
	case <-time.After(testRequestTimeout):
		t.Fatal("catalog/changed was not emitted")
		return protocol.CatalogChanged{}
	}
}

func containsCatalogKind(values []protocol.CatalogKind, want protocol.CatalogKind) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func drainCatalogChanges(changes <-chan protocol.CatalogChanged) {
	time.Sleep(25 * time.Millisecond)
	for {
		select {
		case <-changes:
		default:
			return
		}
	}
}
