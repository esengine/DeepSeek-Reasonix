package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"reasonix/internal/runtimeapi"
)

// remoteWorkbenchV1TestRuntime embeds the complete interface so focused tests
// can override only the exercised domains. The core methods are forwarded to
// the Phase-5 test runtime; any accidental call to an unimplemented V1 domain
// panics, making missing production routing visible.
type remoteWorkbenchV1TestRuntime struct {
	runtimeapi.V1RuntimeAPI
	core *remoteWorkbenchTestRuntime

	mu             sync.Mutex
	workspaces     []runtimeapi.Workspace
	workspacePages map[runtimeapi.Cursor]runtimeapi.WorkspaceListPage
	topics         map[runtimeapi.WorkspaceID][]runtimeapi.TopicSummary
	sessions       map[runtimeapi.WorkspaceID][]runtimeapi.SessionSummary
	trash          map[runtimeapi.WorkspaceID][]runtimeapi.TrashEntry
	trashPages     map[runtimeapi.WorkspaceID]map[runtimeapi.Cursor]runtimeapi.TrashPage
	snapshots      map[runtimeapi.SessionRef]runtimeapi.SessionSnapshot
	attachErrors   map[runtimeapi.SessionRef]error
	subscribed     map[runtimeapi.SessionRef]bool
	created        []runtimeapi.CreatedSession

	attachInputs     []runtimeapi.AttachAndSubscribeInput
	unsubscribed     []runtimeapi.SessionRef
	closedSessions   []runtimeapi.SessionRef
	workspaceInputs  []runtimeapi.ListWorkspacesInput
	topicInputs      []runtimeapi.ListTopicsInput
	sessionInputs    []runtimeapi.ListSessionsInput
	trashInputs      []runtimeapi.ListTrashedSessionsInput
	renameTopicInput []runtimeapi.RenameTopicInput
	trashTopicInputs []runtimeapi.TrashTopicInput
	trashTopicErr    error
	blockWorkspaces  bool
}

var _ runtimeapi.V1RuntimeAPI = (*remoteWorkbenchV1TestRuntime)(nil)

func newRemoteWorkbenchV1TestRuntime() *remoteWorkbenchV1TestRuntime {
	return &remoteWorkbenchV1TestRuntime{
		core:         newRemoteWorkbenchTestRuntime(),
		topics:       make(map[runtimeapi.WorkspaceID][]runtimeapi.TopicSummary),
		sessions:     make(map[runtimeapi.WorkspaceID][]runtimeapi.SessionSummary),
		trash:        make(map[runtimeapi.WorkspaceID][]runtimeapi.TrashEntry),
		snapshots:    make(map[runtimeapi.SessionRef]runtimeapi.SessionSnapshot),
		attachErrors: make(map[runtimeapi.SessionRef]error),
		subscribed:   make(map[runtimeapi.SessionRef]bool),
	}
}

func (r *remoteWorkbenchV1TestRuntime) Connection(ctx context.Context) (runtimeapi.ConnectionView, error) {
	return r.core.Connection(ctx)
}
func (r *remoteWorkbenchV1TestRuntime) BrowseWorkspace(ctx context.Context, input runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	return r.core.BrowseWorkspace(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) OpenWorkspace(ctx context.Context, input runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	return r.core.OpenWorkspace(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) ComposerSubmit(ctx context.Context, input runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	return r.core.ComposerSubmit(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) SteerTurn(ctx context.Context, input runtimeapi.SteerInput) error {
	return r.core.SteerTurn(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) CancelTurn(ctx context.Context, input runtimeapi.CancelTurnInput) error {
	return r.core.CancelTurn(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) ApprovePrompt(ctx context.Context, input runtimeapi.ApproveInput) error {
	return r.core.ApprovePrompt(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) AnswerPrompt(ctx context.Context, input runtimeapi.AnswerInput) error {
	return r.core.AnswerPrompt(ctx, input)
}
func (r *remoteWorkbenchV1TestRuntime) Events() <-chan runtimeapi.Event { return r.core.Events() }

func (r *remoteWorkbenchV1TestRuntime) ListWorkspaces(ctx context.Context, input runtimeapi.ListWorkspacesInput) (runtimeapi.WorkspaceListPage, error) {
	r.mu.Lock()
	r.workspaceInputs = append(r.workspaceInputs, input)
	block := r.blockWorkspaces
	items := append([]runtimeapi.Workspace(nil), r.workspaces...)
	page, paged := r.workspacePages[input.Cursor]
	usePages := r.workspacePages != nil
	r.mu.Unlock()
	if block {
		<-ctx.Done()
		return runtimeapi.WorkspaceListPage{}, ctx.Err()
	}
	if usePages {
		if !paged {
			return runtimeapi.WorkspaceListPage{}, errors.New("unexpected workspace cursor")
		}
		page.Items = append([]runtimeapi.Workspace(nil), page.Items...)
		return page, nil
	}
	return runtimeapi.WorkspaceListPage{Items: items}, nil
}

func (r *remoteWorkbenchV1TestRuntime) ListTopics(_ context.Context, input runtimeapi.ListTopicsInput) (runtimeapi.TopicPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topicInputs = append(r.topicInputs, input)
	return runtimeapi.TopicPage{Items: append([]runtimeapi.TopicSummary(nil), r.topics[input.WorkspaceID]...)}, nil
}

func (r *remoteWorkbenchV1TestRuntime) ListSessions(_ context.Context, input runtimeapi.ListSessionsInput) (runtimeapi.SessionListPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionInputs = append(r.sessionInputs, input)
	return runtimeapi.SessionListPage{Items: append([]runtimeapi.SessionSummary(nil), r.sessions[input.WorkspaceID]...)}, nil
}

func (r *remoteWorkbenchV1TestRuntime) ListTrashedSessions(_ context.Context, input runtimeapi.ListTrashedSessionsInput) (runtimeapi.TrashPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trashInputs = append(r.trashInputs, input)
	if pages := r.trashPages[input.WorkspaceID]; pages != nil {
		page, ok := pages[input.Cursor]
		if !ok {
			return runtimeapi.TrashPage{}, errors.New("unexpected trash cursor")
		}
		page.Items = append([]runtimeapi.TrashEntry(nil), page.Items...)
		return page, nil
	}
	return runtimeapi.TrashPage{Items: append([]runtimeapi.TrashEntry(nil), r.trash[input.WorkspaceID]...)}, nil
}

func (r *remoteWorkbenchV1TestRuntime) CreateSession(_ context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.created) == 0 {
		return runtimeapi.CreatedSession{}, errors.New("no created Remote Session queued")
	}
	created := r.created[0]
	r.created = r.created[1:]
	if created.Session.WorkspaceID != input.WorkspaceID {
		return runtimeapi.CreatedSession{}, errors.New("created Remote Session workspace mismatch")
	}
	return created, nil
}

func (r *remoteWorkbenchV1TestRuntime) AttachAndSubscribe(_ context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attachInputs = append(r.attachInputs, input)
	if err := r.attachErrors[input.Session]; err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	snapshot, ok := r.snapshots[input.Session]
	if !ok {
		return runtimeapi.SessionSnapshot{}, errors.New("unknown Remote Session snapshot")
	}
	r.subscribed[input.Session] = true
	return cloneRemoteSessionSnapshot(snapshot), nil
}

func (r *remoteWorkbenchV1TestRuntime) UnsubscribeSession(_ context.Context, input runtimeapi.UnsubscribeSessionInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unsubscribed = append(r.unsubscribed, input.Session)
	r.subscribed[input.Session] = false
	return nil
}

func (r *remoteWorkbenchV1TestRuntime) CloseSession(_ context.Context, input runtimeapi.CloseSessionInput) (runtimeapi.CloseSessionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closedSessions = append(r.closedSessions, input.Session)
	return runtimeapi.CloseSessionResult{Disposition: runtimeapi.SessionReleased}, nil
}

func (r *remoteWorkbenchV1TestRuntime) CreateTopic(_ context.Context, input runtimeapi.CreateTopicInput) (runtimeapi.CreatedTopic, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	created := runtimeapi.CreatedTopic{TopicID: "topic_created", Title: input.Title, CreatedAtMillis: 900}
	r.topics[input.WorkspaceID] = append(r.topics[input.WorkspaceID], runtimeapi.TopicSummary{
		TopicID: created.TopicID, Title: created.Title, CreatedAtMillis: created.CreatedAtMillis,
	})
	return created, nil
}

func (r *remoteWorkbenchV1TestRuntime) RenameTopic(_ context.Context, input runtimeapi.RenameTopicInput) (runtimeapi.RenameTopicResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renameTopicInput = append(r.renameTopicInput, input)
	for index := range r.topics[input.WorkspaceID] {
		if r.topics[input.WorkspaceID][index].TopicID == input.TopicID {
			r.topics[input.WorkspaceID][index].Title = input.Title
		}
	}
	return runtimeapi.RenameTopicResult{Title: input.Title}, nil
}

func (r *remoteWorkbenchV1TestRuntime) DeleteTopic(_ context.Context, input runtimeapi.DeleteTopicInput) (runtimeapi.DeleteTopicResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.topics[input.WorkspaceID][:0]
	for _, topic := range r.topics[input.WorkspaceID] {
		if topic.TopicID != input.TopicID {
			next = append(next, topic)
		}
	}
	r.topics[input.WorkspaceID] = next
	return runtimeapi.DeleteTopicResult{Deleted: true}, nil
}

func (r *remoteWorkbenchV1TestRuntime) TrashTopic(_ context.Context, input runtimeapi.TrashTopicInput) (runtimeapi.TrashTopicResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trashTopicInputs = append(r.trashTopicInputs, input)
	if r.trashTopicErr != nil {
		return runtimeapi.TrashTopicResult{}, r.trashTopicErr
	}
	return runtimeapi.TrashTopicResult{Disposition: runtimeapi.CleanupTrashed}, nil
}

func remoteWorkbenchV1Fixture() (*remoteWorkbenchV1TestRuntime, []runtimeapi.SessionRef) {
	rt := newRemoteWorkbenchV1TestRuntime()
	wsA := runtimeapi.Workspace{ID: "workspace_a", Name: "Workspace A", DisplayPath: "/host/srv/a"}
	wsB := runtimeapi.Workspace{ID: "workspace_b", Name: "Workspace B", DisplayPath: "/host/srv/b"}
	rt.workspaces = []runtimeapi.Workspace{wsA, wsB}
	refs := []runtimeapi.SessionRef{
		{WorkspaceID: wsA.ID, SessionID: "session_a1"},
		{WorkspaceID: wsA.ID, SessionID: "session_a2"},
		{WorkspaceID: wsB.ID, SessionID: "session_b1"},
	}
	rt.topics[wsA.ID] = []runtimeapi.TopicSummary{
		{TopicID: "topic_a", Title: "Topic A", CreatedAtMillis: 10, SessionCount: 2, LastActivityMillis: 40},
	}
	rt.topics[wsB.ID] = []runtimeapi.TopicSummary{
		{TopicID: "topic_b", Title: "Topic B", CreatedAtMillis: 20, SessionCount: 1, LastActivityMillis: 50},
	}
	rt.sessions[wsA.ID] = []runtimeapi.SessionSummary{
		{Session: refs[0], TopicID: "topic_a", Title: "A one", Turns: 2, CreatedAtMillis: 11, LastActivityMillis: 31},
		{Session: refs[1], TopicID: "topic_a", Title: "A two", Turns: 4, CreatedAtMillis: 12, LastActivityMillis: 41},
	}
	rt.sessions[wsB.ID] = []runtimeapi.SessionSummary{
		{Session: refs[2], TopicID: "topic_b", Title: "B one", Turns: 3, CreatedAtMillis: 21, LastActivityMillis: 51},
	}
	for index, ref := range refs {
		summary, _ := remoteSessionByRef(context.Background(), rt, ref)
		rt.snapshots[ref] = runtimeapi.SessionSnapshot{
			Session: ref, TopicID: summary.TopicID, Title: summary.Title,
			Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
			History: runtimeapi.HistoryPage{TotalTurns: summary.Turns, StartTurn: summary.Turns - 1, HasOlder: true, Next: runtimeapi.Cursor("cursor_" + string(rune('a'+index)))},
		}
	}
	rt.trash[wsA.ID] = []runtimeapi.TrashEntry{{
		Session: runtimeapi.SessionRef{WorkspaceID: wsA.ID, SessionID: "trash_a"}, TopicID: "topic_old", Title: "Old", TrashedAtMillis: 70,
	}}
	return rt, refs
}

func TestRemoteWorkbenchMultipleWorkspacesTabsAndCatalogTree(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_multi", Label: "Host multi"}
	rt, refs := remoteWorkbenchV1Fixture()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)

	for _, ref := range refs {
		if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
			t.Fatalf("attach %v: %v", ref, err)
		}
	}
	tabs := app.ListTabs()
	if len(tabs) != 3 || !tabs[2].Active || tabs[2].WorkspaceID != string(refs[2].WorkspaceID) {
		t.Fatalf("Remote multi tabs = %#v", tabs)
	}
	for _, tab := range tabs {
		if tab.WorkspacePath == tab.WorkspaceRoot || tab.Cwd == tab.WorkspaceRoot {
			t.Fatalf("Host DisplayPath was used as opaque/local identity: %#v", tab)
		}
	}

	order := []string{tabs[2].ID, tabs[0].ID, tabs[1].ID}
	if err := app.ReorderTabs(order); err != nil {
		t.Fatal(err)
	}
	if err := app.SetActiveTab(tabs[0].ID); err != nil {
		t.Fatal(err)
	}
	ordered := app.ListTabs()
	if ordered[0].ID != tabs[2].ID || ordered[1].ID != tabs[0].ID || !ordered[1].Active {
		t.Fatalf("Remote reordered tabs = %#v", ordered)
	}
	if err := app.CloseTab(tabs[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := app.ListTabs(); len(got) != 2 {
		t.Fatalf("Remote tabs after close = %#v", got)
	}

	tree := app.ListProjectTree()
	if len(tree) != 2 || tree[0].Root != string(refs[0].WorkspaceID) || tree[0].Label != "Workspace A" || len(tree[0].Children) != 1 || len(tree[0].Children[0].Children) != 2 {
		t.Fatalf("Remote project tree = %#v", tree)
	}
	sessionNode := tree[0].Children[0].Children[0]
	if sessionNode.Root != string(refs[0].WorkspaceID) || sessionNode.SessionPath != string(refs[0].SessionID) || sessionNode.SessionPath == "/host/srv/a" {
		t.Fatalf("Remote tree opaque identity/path boundary = %#v", sessionNode)
	}
	trash, err := listAllRemoteTrash(context.Background(), rt, refs[0].WorkspaceID)
	if err != nil || len(trash) != 1 || trash[0].Session.SessionID != "trash_a" {
		t.Fatalf("Remote trash catalog = %#v, %v", trash, err)
	}
}

func TestRemoteWorkbenchReconnectsEveryOpenSessionIndependently(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_reconnect_all", Label: "Host reconnect all"}
	rt, refs := remoteWorkbenchV1Fixture()
	app, manager := newRemoteWorkbenchTestApp(t, target, rt, nil)
	for _, ref := range refs {
		if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
			t.Fatal(err)
		}
	}
	expected := manager.Snapshot()
	app.remote.workbenchMu.Lock()
	for _, session := range app.remote.workbench.Sessions {
		session.AttachedGeneration = 0
		session.ReattachGeneration = expected.Generation
	}
	app.remote.workbenchMu.Unlock()
	rt.mu.Lock()
	rt.attachInputs = nil
	rt.mu.Unlock()
	app.reattachRemoteWorkbench(expected)
	rt.mu.Lock()
	inputs := append([]runtimeapi.AttachAndSubscribeInput(nil), rt.attachInputs...)
	rt.mu.Unlock()
	if len(inputs) != len(refs) {
		t.Fatalf("reattach calls = %#v", inputs)
	}
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	for _, ref := range refs {
		session := app.remote.workbench.Sessions[ref]
		if session == nil || session.AttachedGeneration != expected.Generation || session.ReattachGeneration != 0 || session.LastAttachError != "" {
			t.Fatalf("reattached Session %v = %#v", ref, session)
		}
	}
}

func TestRemoteWorkbenchReconnectFailureIsScopedToOneSession(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_reconnect_partial", Label: "Host reconnect partial"}
	rt, refs := remoteWorkbenchV1Fixture()
	app, manager := newRemoteWorkbenchTestApp(t, target, rt, nil)
	for _, ref := range refs {
		if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
			t.Fatal(err)
		}
	}
	expected := manager.Snapshot()
	app.remote.workbenchMu.Lock()
	for _, session := range app.remote.workbench.Sessions {
		session.AttachedGeneration = 0
		session.ReattachGeneration = expected.Generation
	}
	app.remote.workbenchMu.Unlock()
	rt.mu.Lock()
	rt.attachInputs = nil
	rt.attachErrors[refs[1]] = errors.New("one Session could not reattach")
	rt.mu.Unlock()
	app.reattachRemoteWorkbench(expected)
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	for index, ref := range refs {
		session := app.remote.workbench.Sessions[ref]
		if index == 1 {
			if session.AttachedGeneration != 0 || session.LastAttachError != "one Session could not reattach" {
				t.Fatalf("failed reattach Session = %#v", session)
			}
			continue
		}
		if session.AttachedGeneration != expected.Generation || session.LastAttachError != "" {
			t.Fatalf("healthy reattach Session %v = %#v", ref, session)
		}
	}
}

func TestRemoteWorkbenchAttachUsesDesktopHistorySetting(t *testing.T) {
	isolateDesktopUserDirs(t)
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_history_setting", Label: "Host history setting"}
	rt, refs := remoteWorkbenchV1Fixture()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	if err := app.SetDesktopHistoryPageTurns(125); err != nil {
		t.Fatal(err)
	}
	if _, err := app.attachRemoteWorkbenchSession(refs[0], true); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.attachInputs) != 1 || rt.attachInputs[0].HistoryTurns != 125 {
		t.Fatalf("Remote attach history setting = %#v", rt.attachInputs)
	}
}

func TestRemoteWorkbenchReplacementMovesOnlyMatchingTabAndHistoryCursor(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_migrate_one", Label: "Host migrate one"}
	rt, refs := remoteWorkbenchV1Fixture()
	app, manager := newRemoteWorkbenchTestApp(t, target, rt, nil)
	for _, ref := range refs[:2] {
		if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
			t.Fatal(err)
		}
	}
	sourceTab := remoteSessionTabID(refs[0])
	replacement := runtimeapi.SessionRef{WorkspaceID: refs[0].WorkspaceID, SessionID: "session_a_replacement"}
	snapshot := cloneRemoteSessionSnapshot(rt.snapshots[refs[0]])
	snapshot.Session = replacement
	snapshot.Title = "A replacement"
	snapshot.History.StartTurn = 7
	snapshot.History.Next = "cursor_replacement"
	if !app.updateRemoteWorkbenchSnapshot(TargetRuntimeEvent{
		Generation: manager.Snapshot().Generation, Target: target,
		Event: runtimeapi.Event{Session: replacement, Snapshot: &runtimeapi.SnapshotUpdate{Previous: refs[0], Snapshot: snapshot}},
	}) {
		t.Fatal("replacement migration rejected")
	}
	tabs := app.ListTabs()
	if len(tabs) != 2 || tabs[0].ID == sourceTab || tabs[1].SessionID != string(refs[1].SessionID) {
		t.Fatalf("replacement migrated wrong tabs = %#v", tabs)
	}
	cursor, ref, ok := app.remoteHistoryCursor(tabs[0].ID, 7)
	if !ok || cursor != "cursor_replacement" || ref != replacement {
		t.Fatalf("replacement history cursor = %q, %#v, %v", cursor, ref, ok)
	}
	older := runtimeapi.HistoryPage{StartTurn: 3, HasOlder: true, Next: "cursor_older"}
	if !app.recordRemoteHistoryPage(replacement, older) {
		t.Fatal("record Remote history page failed")
	}
	if cursor, _, ok := app.remoteHistoryCursor(tabs[0].ID, 3); !ok || cursor != "cursor_older" {
		t.Fatalf("older history cursor = %q, %v", cursor, ok)
	}
}

func TestRemoteWorkbenchTrashOpenIdleTopicAndRollback(t *testing.T) {
	t.Run("success unsubscribes before Host mutation", func(t *testing.T) {
		target := TargetDescriptor{Kind: TargetRemote, ID: "host_trash", Label: "Host trash"}
		rt, refs := remoteWorkbenchV1Fixture()
		app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
		for _, ref := range refs {
			if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
				t.Fatal(err)
			}
		}
		if err := app.remoteTrashTopic("topic_a"); err != nil {
			t.Fatal(err)
		}
		rt.mu.Lock()
		unsubscribed := append([]runtimeapi.SessionRef(nil), rt.unsubscribed...)
		mutations := append([]runtimeapi.TrashTopicInput(nil), rt.trashTopicInputs...)
		rt.mu.Unlock()
		if len(unsubscribed) != 2 || len(mutations) != 1 || mutations[0].WorkspaceID != refs[0].WorkspaceID {
			t.Fatalf("trash preflight calls unsubscribed=%#v mutations=%#v", unsubscribed, mutations)
		}
		remaining := app.ListTabs()
		if len(remaining) != 1 || remaining[0].SessionID != string(refs[2].SessionID) || !remaining[0].Active {
			t.Fatalf("tabs after Remote topic trash = %#v", remaining)
		}
	})

	t.Run("Host failure restores every subscription", func(t *testing.T) {
		target := TargetDescriptor{Kind: TargetRemote, ID: "host_trash_rollback", Label: "Host trash rollback"}
		rt, refs := remoteWorkbenchV1Fixture()
		rt.trashTopicErr = errors.New("Host refused trash")
		app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
		for _, ref := range refs[:2] {
			if _, err := app.attachRemoteWorkbenchSession(ref, true); err != nil {
				t.Fatal(err)
			}
		}
		before := app.ListTabs()
		rt.mu.Lock()
		rt.attachInputs = nil
		rt.mu.Unlock()
		if err := app.remoteTrashTopic("topic_a"); err == nil || !containsError(err, "Host refused trash") {
			t.Fatalf("trash failure = %v", err)
		}
		rt.mu.Lock()
		rollbackAttaches := append([]runtimeapi.AttachAndSubscribeInput(nil), rt.attachInputs...)
		unsubscribed := append([]runtimeapi.SessionRef(nil), rt.unsubscribed...)
		subscribed := make(map[runtimeapi.SessionRef]bool, len(rt.subscribed))
		for ref, active := range rt.subscribed {
			subscribed[ref] = active
		}
		rt.mu.Unlock()
		if len(unsubscribed) != 2 || len(rollbackAttaches) != 2 {
			t.Fatalf("trash rollback unsubscribed=%#v reattached=%#v", unsubscribed, rollbackAttaches)
		}
		after := app.ListTabs()
		if len(after) != len(before) {
			t.Fatalf("trash rollback lost tabs before=%#v after=%#v", before, after)
		}
		for index := range before {
			if after[index].ID != before[index].ID || after[index].Active != before[index].Active || !subscribed[refs[index]] || rollbackAttaches[index].Session != refs[index] {
				t.Fatalf("trash rollback changed order/active/subscription before=%#v after=%#v subscribed=%#v attaches=%#v", before, after, subscribed, rollbackAttaches)
			}
		}
	})

	t.Run("active work is rejected before unsubscribe", func(t *testing.T) {
		target := TargetDescriptor{Kind: TargetRemote, ID: "host_trash_busy", Label: "Host trash busy"}
		rt, refs := remoteWorkbenchV1Fixture()
		snapshot := rt.snapshots[refs[0]]
		snapshot.Runtime.Running = true
		snapshot.Runtime.CurrentTurn = &runtimeapi.TurnState{ID: "turn_busy"}
		rt.snapshots[refs[0]] = snapshot
		app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
		if _, err := app.attachRemoteWorkbenchSession(refs[0], true); err != nil {
			t.Fatal(err)
		}
		if err := app.remoteTrashTopic("topic_a"); err == nil || !containsError(err, "active work") {
			t.Fatalf("busy trash error = %v", err)
		}
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if len(rt.unsubscribed) != 0 || len(rt.trashTopicInputs) != 0 {
			t.Fatalf("busy topic reached Host lifecycle: unsub=%#v trash=%#v", rt.unsubscribed, rt.trashTopicInputs)
		}
	})
}

func TestRemoteCatalogPaginationHonorsCallerCancellation(t *testing.T) {
	rt := newRemoteWorkbenchV1TestRuntime()
	rt.blockWorkspaces = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listAllRemoteWorkspaces(ctx, rt); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled catalog error = %v", err)
	}
}

func TestRemoteCatalogPaginationExhaustsAndRejectsNonAdvancingCursors(t *testing.T) {
	rt := newRemoteWorkbenchV1TestRuntime()
	rt.workspacePages = map[runtimeapi.Cursor]runtimeapi.WorkspaceListPage{
		"": {
			Items:   []runtimeapi.Workspace{{ID: "workspace_page_1", Name: "One"}},
			HasMore: true, Next: "workspace_cursor_2",
		},
		"workspace_cursor_2": {
			Items: []runtimeapi.Workspace{{ID: "workspace_page_2", Name: "Two"}},
		},
	}
	workspaces, err := listAllRemoteWorkspaces(context.Background(), rt)
	if err != nil || len(workspaces) != 2 || workspaces[1].ID != "workspace_page_2" {
		t.Fatalf("paged Remote workspaces = %#v, %v", workspaces, err)
	}
	workspaceID := runtimeapi.WorkspaceID("workspace_page_1")
	rt.trashPages = map[runtimeapi.WorkspaceID]map[runtimeapi.Cursor]runtimeapi.TrashPage{
		workspaceID: {
			"":               {Items: []runtimeapi.TrashEntry{{Title: "One"}}, HasMore: true, Next: "trash_cursor_2"},
			"trash_cursor_2": {Items: []runtimeapi.TrashEntry{{Title: "Two"}}},
		},
	}
	trash, err := listAllRemoteTrash(context.Background(), rt, workspaceID)
	if err != nil || len(trash) != 2 || trash[1].Title != "Two" {
		t.Fatalf("paged Remote trash = %#v, %v", trash, err)
	}

	rt.workspacePages = map[runtimeapi.Cursor]runtimeapi.WorkspaceListPage{
		"":      {HasMore: true, Next: "stuck"},
		"stuck": {HasMore: true, Next: "stuck"},
	}
	if _, err := listAllRemoteWorkspaces(context.Background(), rt); err == nil || !containsError(err, "invalid cursor") {
		t.Fatalf("non-advancing workspace cursor error = %v", err)
	}
}
