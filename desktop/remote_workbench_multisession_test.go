package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"reasonix/internal/eventwire"
	"reasonix/internal/runtimeapi"
)

type remoteSessionCreateGateTestRuntime struct {
	*remoteWorkbenchV1TestRuntime

	createStarted chan struct{}
	releaseCreate chan struct{}
	startOnce     sync.Once
	releaseOnce   sync.Once
	attachStarted chan struct{}
	releaseAttach chan struct{}
	attachOnce    sync.Once
	attachRelease sync.Once
	blockAttach   bool
	callsMu       sync.Mutex
	createCalls   int
	createHook    func()
	attachHook    func()
}

func newRemoteSessionCreateGateTestRuntime() *remoteSessionCreateGateTestRuntime {
	return &remoteSessionCreateGateTestRuntime{
		remoteWorkbenchV1TestRuntime: newRemoteWorkbenchV1TestRuntime(),
		createStarted:                make(chan struct{}),
		releaseCreate:                make(chan struct{}),
		attachStarted:                make(chan struct{}),
		releaseAttach:                make(chan struct{}),
	}
}

func (r *remoteSessionCreateGateTestRuntime) CreateSession(ctx context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	r.callsMu.Lock()
	r.createCalls++
	r.callsMu.Unlock()
	created, err := r.remoteWorkbenchV1TestRuntime.CreateSession(ctx, input)
	if err != nil {
		return runtimeapi.CreatedSession{}, err
	}
	r.startOnce.Do(func() { close(r.createStarted) })
	select {
	case <-r.releaseCreate:
		if r.createHook != nil {
			r.createHook()
		}
		return created, nil
	case <-ctx.Done():
		return runtimeapi.CreatedSession{}, ctx.Err()
	}
}

func (r *remoteSessionCreateGateTestRuntime) AttachAndSubscribe(ctx context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	snapshot, err := r.remoteWorkbenchV1TestRuntime.AttachAndSubscribe(ctx, input)
	if r.blockAttach {
		r.attachOnce.Do(func() { close(r.attachStarted) })
		select {
		case <-r.releaseAttach:
		case <-ctx.Done():
			return runtimeapi.SessionSnapshot{}, ctx.Err()
		}
	}
	if r.attachHook != nil {
		r.attachHook()
	}
	return snapshot, err
}

func (r *remoteSessionCreateGateTestRuntime) release() {
	r.releaseOnce.Do(func() { close(r.releaseCreate) })
}

func (r *remoteSessionCreateGateTestRuntime) releaseAttachment() {
	r.attachRelease.Do(func() { close(r.releaseAttach) })
}

func (r *remoteSessionCreateGateTestRuntime) calls() int {
	r.callsMu.Lock()
	defer r.callsMu.Unlock()
	return r.createCalls
}

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
	trashTopicHook   func()
	unsubscribeHook  func()
	blockWorkspaces  bool
	listSessionsHook func()
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
	r.sessionInputs = append(r.sessionInputs, input)
	items := append([]runtimeapi.SessionSummary(nil), r.sessions[input.WorkspaceID]...)
	hook := r.listSessionsHook
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return runtimeapi.SessionListPage{Items: items}, nil
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
	snapshot := r.snapshots[created.Session]
	r.sessions[input.WorkspaceID] = append(r.sessions[input.WorkspaceID], runtimeapi.SessionSummary{
		Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
		Turns: snapshot.History.TotalTurns, LastActivityMillis: int64(snapshot.History.TotalTurns),
	})
	foundTopic := false
	for index := range r.topics[input.WorkspaceID] {
		if r.topics[input.WorkspaceID][index].TopicID != created.TopicID {
			continue
		}
		r.topics[input.WorkspaceID][index].SessionCount++
		foundTopic = true
		break
	}
	if !foundTopic {
		r.topics[input.WorkspaceID] = append(r.topics[input.WorkspaceID], runtimeapi.TopicSummary{
			TopicID: created.TopicID, Title: created.TopicTitle, SessionCount: 1,
		})
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
	r.unsubscribed = append(r.unsubscribed, input.Session)
	r.subscribed[input.Session] = false
	hook := r.unsubscribeHook
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
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
	r.trashTopicInputs = append(r.trashTopicInputs, input)
	err := r.trashTopicErr
	hook := r.trashTopicHook
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return runtimeapi.TrashTopicResult{}, err
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

func TestRemoteSessionCreatePathsExcludeFinalClose(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*App, runtimeapi.Workspace) error
	}{
		{
			name: "workspace create",
			invoke: func(app *App, _ runtimeapi.Workspace) error {
				_, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
					PrimaryDirectoryRef: "directory_gate", TopicTitle: "Gate test",
				})
				return err
			},
		},
		{
			name: "project tab",
			invoke: func(app *App, workspace runtimeapi.Workspace) error {
				_, err := app.remoteOpenProjectTab(string(workspace.ID), "")
				return err
			},
		},
		{
			name: "blank tab",
			invoke: func(app *App, workspace runtimeapi.Workspace) error {
				_, err := app.remoteEnsureBlankTab(string(workspace.ID))
				return err
			},
		},
		{
			name: "workspace switch",
			invoke: func(app *App, workspace runtimeapi.Workspace) error {
				_, err := app.remoteSwitchWorkspaceV1(string(workspace.ID))
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := newRemoteSessionCreateGateTestRuntime()
			runtime.release()
			runtime.blockAttach = true
			defer runtime.releaseAttachment()
			workspace := runtimeapi.Workspace{
				ID: "workspace_create_gate", Name: "Create gate", DisplayPath: "/host/create-gate",
			}
			created := runtimeapi.CreatedSession{
				Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_create_gate"},
				TopicID: "topic_create_gate", TopicTitle: "Create gate",
			}
			runtime.core.workspace = workspace
			runtime.workspaces = []runtimeapi.Workspace{workspace}
			runtime.created = []runtimeapi.CreatedSession{created}
			runtime.snapshots[created.Session] = runtimeapi.SessionSnapshot{
				Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
				Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
			}
			app, _ := newRemoteWorkbenchTestApp(t, TargetDescriptor{
				Kind: TargetRemote, ID: "host_create_gate", Label: "Create gate",
			}, runtime, nil)

			createDone := make(chan error, 1)
			go func() { createDone <- test.invoke(app, workspace) }()
			select {
			case <-runtime.attachStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("Host Session attach did not start after create")
			}
			if app.beginRemoteDesktopClose() {
				t.Fatal("final close passed an admitted Host Session create")
			}

			runtime.releaseAttachment()
			select {
			case err := <-createDone:
				if err != nil {
					t.Fatalf("Host Session create failed: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Host Session create deadlocked after release")
			}
			if calls := runtime.calls(); calls != 1 {
				t.Fatalf("Host Session create calls = %d, want 1", calls)
			}
			if !app.beginRemoteDesktopClose() {
				t.Fatal("final close was not admitted after Host Session create completed")
			}
			if err := test.invoke(app, workspace); !errors.Is(err, errRemoteWorkspaceSessionCreateWhileClosing) {
				t.Fatalf("path admitted after final close: %v", err)
			}
			if calls := runtime.calls(); calls != 1 {
				t.Fatalf("Host Session create reached after final close; calls = %d", calls)
			}
		})
	}
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
	if sessionNode.Root != string(refs[0].WorkspaceID) || sessionNode.SessionPath != remoteSessionToken(refs[0]) || sessionNode.SessionPath == "/host/srv/a" {
		t.Fatalf("Remote tree opaque identity/path boundary = %#v", sessionNode)
	}
	if got := tree[1].Children[0]; len(got.Children) != 0 || !got.Open {
		t.Fatalf("single-Session Remote topic did not own status without a duplicate child: %#v", got)
	}
	trash, err := listAllRemoteTrash(context.Background(), rt, refs[0].WorkspaceID)
	if err != nil || len(trash) != 1 || trash[0].Session.SessionID != "trash_a" {
		t.Fatalf("Remote trash catalog = %#v, %v", trash, err)
	}
}

func TestRemoteSessionSurfacesPreserveFullRefAcrossWorkspaces(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_session_surfaces", Label: "Host session surfaces"}
	rt, refs := remoteWorkbenchV1Fixture()
	originalB := refs[2]
	refs[2] = runtimeapi.SessionRef{WorkspaceID: originalB.WorkspaceID, SessionID: refs[0].SessionID}
	rt.sessions[refs[2].WorkspaceID][0].Session = refs[2]
	snapshotB := rt.snapshots[originalB]
	delete(rt.snapshots, originalB)
	snapshotB.Session = refs[2]
	rt.snapshots[refs[2]] = snapshotB
	rt.trash[refs[2].WorkspaceID] = []runtimeapi.TrashEntry{{
		Session: runtimeapi.SessionRef{WorkspaceID: refs[2].WorkspaceID, SessionID: "trash_b"},
		TopicID: "topic_old_b", Title: "Newer trash", TrashedAtMillis: 90,
	}}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	if _, err := app.attachRemoteWorkbenchSession(refs[0], true); err != nil {
		t.Fatal(err)
	}

	sessions := app.ListSessions()
	if len(sessions) != 3 {
		t.Fatalf("Remote all-workspace sessions = %#v", sessions)
	}
	wantOrder := []runtimeapi.SessionRef{refs[2], refs[1], refs[0]}
	for index, want := range wantOrder {
		got, encoded, err := parseRemoteSessionToken(sessions[index].Path)
		if err != nil || !encoded || got != want {
			t.Fatalf("session[%d] token = %q => %#v, encoded=%v, err=%v; want %#v", index, sessions[index].Path, got, encoded, err, want)
		}
		if sessions[index].WorkspaceRoot != string(want.WorkspaceID) {
			t.Fatalf("session[%d] WorkspaceRoot = %q, want %q", index, sessions[index].WorkspaceRoot, want.WorkspaceID)
		}
	}
	if !sessions[2].Current || sessions[0].Current || sessions[1].Current {
		t.Fatalf("Remote current Session projection = %#v", sessions)
	}
	api, expected, err := app.remoteConnectedV1RuntimeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.remoteSessionRefForToken(api, expected, string(refs[0].SessionID)); err == nil || !containsError(err, "ambiguous across workspaces") {
		t.Fatalf("duplicate raw SessionID was not rejected: %v", err)
	}
	if resolved, err := app.remoteSessionRefForToken(api, expected, sessions[0].Path); err != nil || resolved != refs[2] {
		t.Fatalf("encoded full SessionRef resolved as %#v, %v; want %#v", resolved, err, refs[2])
	}

	tabs := app.ListTabs()
	tree := app.ListProjectTree()
	if len(tabs) != 1 || tabs[0].SessionPath != remoteSessionToken(refs[0]) ||
		tree[0].Children[0].Children[0].SessionPath != tabs[0].SessionPath || sessions[2].Path != tabs[0].SessionPath {
		t.Fatalf("Remote Session identity diverged across tab/tree/history: tab=%#v tree=%#v history=%#v", tabs, tree, sessions)
	}

	trash := app.ListTrashedSessions()
	if len(trash) != 2 || trash[0].WorkspaceRoot != string(refs[2].WorkspaceID) || trash[1].WorkspaceRoot != string(refs[0].WorkspaceID) {
		t.Fatalf("Remote all-workspace trash = %#v", trash)
	}
	for _, item := range trash {
		if ref, encoded, err := parseRemoteSessionToken(item.Path); err != nil || !encoded || ref.WorkspaceID != runtimeapi.WorkspaceID(item.WorkspaceRoot) {
			t.Fatalf("Remote trash token/WorkspaceRoot mismatch: %#v => %#v, encoded=%v, err=%v", item, ref, encoded, err)
		}
	}

	beforeAttach := len(rt.attachInputs)
	beforeUnsubscribe := len(rt.unsubscribed)
	if _, err := app.remotePreviewSessionV1("", sessions[0].Path); err != nil {
		t.Fatal(err)
	}
	if len(rt.attachInputs) != beforeAttach+1 || rt.attachInputs[len(rt.attachInputs)-1].Session != refs[2] ||
		len(rt.unsubscribed) != beforeUnsubscribe+1 || rt.unsubscribed[len(rt.unsubscribed)-1] != refs[2] {
		t.Fatalf("cross-workspace preview lost SessionRef pair: attach=%#v unsubscribe=%#v", rt.attachInputs, rt.unsubscribed)
	}
}

func TestRemoteProjectTreeNewSessionExpandsOneToTwoAndPublishesRefresh(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_tree_new", Label: "Host tree new"}
	rt := newRemoteWorkbenchV1TestRuntime()
	workspace := runtimeapi.Workspace{ID: "workspace_tree", Name: "Tree", DisplayPath: "/host/tree"}
	source := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_source"}
	topicID := runtimeapi.TopicID("topic_tree")
	rt.workspaces = []runtimeapi.Workspace{workspace}
	rt.topics[workspace.ID] = []runtimeapi.TopicSummary{{TopicID: topicID, Title: "Tree topic", SessionCount: 1, LastActivityMillis: 10}}
	rt.sessions[workspace.ID] = []runtimeapi.SessionSummary{{Session: source, TopicID: topicID, Title: "Source", Turns: 2, LastActivityMillis: 10}}
	rt.snapshots[source] = runtimeapi.SessionSnapshot{
		Session: source, TopicID: topicID, Title: "Tree topic", Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
		History: runtimeapi.HistoryPage{TotalTurns: 2},
	}
	app, manager := newRemoteWorkbenchTestApp(t, target, rt, nil)
	if _, err := app.attachRemoteWorkbenchSession(source, true); err != nil {
		t.Fatal(err)
	}
	initial := app.ListProjectTree()
	if len(initial) != 1 || len(initial[0].Children) != 1 || len(initial[0].Children[0].Children) != 0 || !initial[0].Children[0].Open {
		t.Fatalf("single-Session topic projection = %#v", initial)
	}

	replacement := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_replacement"}
	replacementSnapshot := runtimeapi.SessionSnapshot{
		Session: replacement, TopicID: topicID, Title: "Tree topic", Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
	}
	rt.mu.Lock()
	rt.sessions[workspace.ID] = append(rt.sessions[workspace.ID], runtimeapi.SessionSummary{
		Session: replacement, TopicID: topicID, Title: "Replacement", LastActivityMillis: 20,
	})
	rt.topics[workspace.ID][0].SessionCount = 2
	rt.topics[workspace.ID][0].LastActivityMillis = 20
	rt.snapshots[replacement] = replacementSnapshot
	rt.mu.Unlock()
	refreshes := 0
	app.projectTreeChangedHook = func() { refreshes++ }
	app.emitRemoteRuntimeEvent(TargetRuntimeEvent{
		Generation: manager.Snapshot().Generation, Target: target,
		Event: runtimeapi.Event{Session: replacement, Snapshot: &runtimeapi.SnapshotUpdate{Previous: source, Snapshot: replacementSnapshot}},
	})
	if refreshes != 1 {
		t.Fatalf("replacement SnapshotUpdate project-tree refreshes = %d, want 1", refreshes)
	}
	updated := app.ListProjectTree()
	if len(updated) != 1 || len(updated[0].Children) != 1 || len(updated[0].Children[0].Children) != 2 || updated[0].Children[0].Open {
		t.Fatalf("two-Session topic projection = %#v", updated)
	}
	paths := map[string]ProjectNode{}
	for _, child := range updated[0].Children[0].Children {
		paths[child.SessionPath] = child
	}
	if paths[remoteSessionToken(source)].Open || !paths[remoteSessionToken(replacement)].Open {
		t.Fatalf("replacement child status projection = %#v", paths)
	}
	if tabs := app.ListTabs(); len(tabs) != 1 || tabs[0].SessionPath != remoteSessionToken(replacement) {
		t.Fatalf("replacement tab identity = %#v", tabs)
	}
}

func TestRemoteEnsureBlankAfterUsedSessionCreatesNewTopicVisibleInTree(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_tree_plus", Label: "Host tree plus"}
	rt := newRemoteWorkbenchV1TestRuntime()
	workspace := runtimeapi.Workspace{ID: "workspace_plus", Name: "Plus", DisplayPath: "/host/plus"}
	source := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_used"}
	createdRef := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_new_topic"}
	rt.workspaces = []runtimeapi.Workspace{workspace}
	rt.topics[workspace.ID] = []runtimeapi.TopicSummary{{TopicID: "topic_used", Title: "Used", SessionCount: 1}}
	rt.sessions[workspace.ID] = []runtimeapi.SessionSummary{{Session: source, TopicID: "topic_used", Title: "Used", Turns: 3}}
	rt.snapshots[source] = runtimeapi.SessionSnapshot{
		Session: source, TopicID: "topic_used", Title: "Used", Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
		History: runtimeapi.HistoryPage{TotalTurns: 3},
	}
	created := runtimeapi.CreatedSession{Session: createdRef, TopicID: "topic_new", TopicTitle: defaultTopicTitle}
	rt.created = []runtimeapi.CreatedSession{created}
	rt.snapshots[createdRef] = runtimeapi.SessionSnapshot{
		Session: createdRef, TopicID: created.TopicID, Title: created.TopicTitle, Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	if _, err := app.attachRemoteWorkbenchSession(source, true); err != nil {
		t.Fatal(err)
	}
	meta, err := app.remoteEnsureBlankTab(string(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionID != string(createdRef.SessionID) || meta.SessionPath != remoteSessionToken(createdRef) || meta.TopicID != string(created.TopicID) {
		t.Fatalf("new blank Remote tab = %#v", meta)
	}
	tree := app.ListProjectTree()
	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("new blank topic was not visible in Remote tree: %#v", tree)
	}
	for _, topic := range tree[0].Children {
		if len(topic.Children) != 0 {
			t.Fatalf("single-Session topic rendered duplicate child: %#v", topic)
		}
	}
}

func TestRemoteEnsureBlankRetriesPendingCreatedSessionAfterAttachFailure(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_retry", Label: "Blank retry"}
	runtime := newRemoteSessionCreateGateTestRuntime()
	runtime.release()
	workspace := runtimeapi.Workspace{ID: "workspace_blank_retry", Name: "Blank retry", DisplayPath: "/host/blank-retry"}
	created := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_opaque_blank_retry"},
		TopicID: "topic_opaque_blank_retry", TopicTitle: defaultTopicTitle,
	}
	runtime.workspaces = []runtimeapi.Workspace{workspace}
	runtime.created = []runtimeapi.CreatedSession{created}
	runtime.snapshots[created.Session] = runtimeapi.SessionSnapshot{
		Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
		Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
	}
	runtime.attachErrors[created.Session] = errors.New("atomic attach interrupted")
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)

	if _, err := app.remoteEnsureBlankTab(string(workspace.ID)); err == nil || !containsError(err, "atomic attach interrupted") {
		t.Fatalf("first /new attach error = %v", err)
	}
	if calls := runtime.calls(); calls != 1 {
		t.Fatalf("Host Session create calls after failed attach = %d, want 1", calls)
	}
	fingerprint := remoteBlankSessionCreateFingerprint(workspace.ID)
	app.remote.workbenchMu.RLock()
	pending := clonePendingRemoteCreate(app.remote.workspacePending[target.ID][fingerprint])
	app.remote.workbenchMu.RUnlock()
	if pending == nil || pending.HostID != target.ID || pending.Created.Session != created.Session || pending.Workspace.ID != workspace.ID {
		t.Fatalf("pending /new create = %#v", pending)
	}

	runtime.mu.Lock()
	delete(runtime.attachErrors, created.Session)
	runtime.mu.Unlock()
	meta, err := app.remoteEnsureBlankTab(string(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionID != string(created.Session.SessionID) || meta.SessionPath != remoteSessionToken(created.Session) {
		t.Fatalf("retried /new tab = %#v", meta)
	}
	if calls := runtime.calls(); calls != 1 {
		t.Fatalf("retry created a second Host Session; calls = %d", calls)
	}
	runtime.mu.Lock()
	attachInputs := append([]runtimeapi.AttachAndSubscribeInput(nil), runtime.attachInputs...)
	runtime.mu.Unlock()
	if len(attachInputs) != 2 || attachInputs[0].Session != created.Session || attachInputs[1].Session != created.Session {
		t.Fatalf("/new attach retries = %#v", attachInputs)
	}
	app.remote.workbenchMu.RLock()
	pending = clonePendingRemoteCreate(app.remote.workspacePending[target.ID][fingerprint])
	app.remote.workbenchMu.RUnlock()
	if pending != nil {
		t.Fatalf("successful /new retained pending create = %#v", pending)
	}
}

func TestRemoteEnsureBlankRejectsTargetABABeforePublishing(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_aba_a", Label: "Blank ABA A"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_aba_b", Label: "Blank ABA B"}
	runtimeA := newRemoteSessionCreateGateTestRuntime()
	runtimeA.release()
	workspace := runtimeapi.Workspace{ID: "workspace_blank_aba", Name: "Blank ABA", DisplayPath: "/host/blank-aba"}
	created := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_opaque_blank_aba"},
		TopicID: "topic_opaque_blank_aba", TopicTitle: defaultTopicTitle,
	}
	runtimeA.workspaces = []runtimeapi.Workspace{workspace}
	runtimeA.created = []runtimeapi.CreatedSession{created}
	runtimeA.snapshots[created.Session] = runtimeapi.SessionSnapshot{
		Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
		Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
	}
	runtimeB := newRemoteWorkbenchV1TestRuntime()
	adapterA := &remoteWorkbenchTestAdapter{target: targetA, runtime: runtimeA}
	adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		switch {
		case sameTarget(target, targetA):
			return adapterA, nil
		case sameTarget(target, targetB):
			return adapterB, nil
		default:
			return nil, errors.New("unexpected target switch")
		}
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
	initial := manager.Snapshot()
	var switchErr error
	var switchOnce sync.Once
	runtimeA.attachHook = func() {
		switchOnce.Do(func() {
			if err := manager.Switch(context.Background(), targetB, SwitchTargetOptions{}); err != nil {
				switchErr = err
				return
			}
			switchErr = manager.Switch(context.Background(), targetA, SwitchTargetOptions{})
		})
	}

	if _, err := app.remoteEnsureBlankTab(string(workspace.ID)); !errors.Is(err, ErrTargetTransitionSuperseded) {
		t.Fatalf("/new after target ABA = %v, want ErrTargetTransitionSuperseded", err)
	}
	if switchErr != nil {
		t.Fatal(switchErr)
	}
	current := manager.Snapshot()
	if !sameTarget(current.Target, targetA) || current.Generation == initial.Generation {
		t.Fatalf("target after ABA = %#v, initial = %#v", current, initial)
	}
	if calls := runtimeA.calls(); calls != 1 {
		t.Fatalf("Host Session create calls = %d, want 1", calls)
	}
	app.remote.workbenchMu.RLock()
	hostID := app.remote.workbench.HostID
	sessions := len(app.remote.workbench.Sessions)
	pending := len(app.remote.workbench.Pending)
	hostPending := clonePendingRemoteCreate(app.remote.workspacePending[targetA.ID][remoteBlankSessionCreateFingerprint(workspace.ID)])
	app.remote.workbenchMu.RUnlock()
	if (hostID != "" && hostID != targetA.ID) || sessions != 0 || pending != 0 {
		t.Fatalf("stale /new state published after target ABA: host=%q sessions=%d pending=%d", hostID, sessions, pending)
	}
	if hostPending == nil || hostPending.Created.Session != created.Session || hostPending.HostID != targetA.ID {
		t.Fatalf("Host A /new pending create was not retained across ABA: %#v", hostPending)
	}
	runtimeA.mu.Lock()
	if len(runtimeA.unsubscribed) != 1 || runtimeA.unsubscribed[0] != created.Session || runtimeA.subscribed[created.Session] {
		t.Fatalf("superseded /new subscription rollback = unsubscribed %#v subscribed=%v", runtimeA.unsubscribed, runtimeA.subscribed[created.Session])
	}
	// Make the Host catalog deliberately lag the successful create. The retry
	// must use Host-scoped pending state, not rely on list visibility.
	runtimeA.sessions[workspace.ID] = nil
	runtimeA.topics[workspace.ID] = nil
	runtimeA.mu.Unlock()
	meta, err := app.remoteEnsureBlankTab(string(workspace.ID))
	if err != nil {
		t.Fatalf("recover catalog-created blank after target ABA: %v", err)
	}
	if meta.SessionPath != remoteSessionToken(created.Session) || runtimeA.calls() != 1 {
		t.Fatalf("catalog recovery created a duplicate Session: meta=%#v createCalls=%d", meta, runtimeA.calls())
	}
	app.remote.workbenchMu.RLock()
	remaining := len(app.remote.workspacePending[targetA.ID])
	app.remote.workbenchMu.RUnlock()
	if remaining != 0 {
		t.Fatalf("successful /new retry retained %d Host-scoped pending creates", remaining)
	}
}

func TestRemoteAttachSuccessRollsBackEverySupersededWorkbenchPath(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		invoke   func(*App, runtimeapi.Workspace, runtimeapi.CreatedSession) error
	}{
		{
			name: "workspace create",
			invoke: func(app *App, _ runtimeapi.Workspace, _ runtimeapi.CreatedSession) error {
				_, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
					PrimaryDirectoryRef: "directory_superseded", TopicTitle: "Superseded workspace create",
				})
				return err
			},
		},
		{
			name:     "project existing",
			existing: true,
			invoke: func(app *App, workspace runtimeapi.Workspace, _ runtimeapi.CreatedSession) error {
				_, err := app.remoteOpenProjectTab(string(workspace.ID), "")
				return err
			},
		},
		{
			name: "project create",
			invoke: func(app *App, workspace runtimeapi.Workspace, _ runtimeapi.CreatedSession) error {
				_, err := app.remoteOpenProjectTab(string(workspace.ID), "")
				return err
			},
		},
		{
			name:     "topic existing",
			existing: true,
			invoke: func(app *App, workspace runtimeapi.Workspace, created runtimeapi.CreatedSession) error {
				_, err := app.remoteOpenTopicSession(
					string(workspace.ID), string(created.TopicID), remoteSessionToken(created.Session),
				)
				return err
			},
		},
		{
			name:     "workspace switch existing",
			existing: true,
			invoke: func(app *App, workspace runtimeapi.Workspace, _ runtimeapi.CreatedSession) error {
				_, err := app.remoteSwitchWorkspaceV1(string(workspace.ID))
				return err
			},
		},
		{
			name: "workspace switch create",
			invoke: func(app *App, workspace runtimeapi.Workspace, _ runtimeapi.CreatedSession) error {
				_, err := app.remoteSwitchWorkspaceV1(string(workspace.ID))
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_attach_rollback_a", Label: "Rollback A"}
			targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_attach_rollback_b", Label: "Rollback B"}
			runtimeA := newRemoteSessionCreateGateTestRuntime()
			runtimeA.release()
			workspace := runtimeapi.Workspace{
				ID: "workspace_attach_rollback", Name: "Attach rollback", DisplayPath: "/host/attach-rollback",
			}
			created := runtimeapi.CreatedSession{
				Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_attach_rollback"},
				TopicID: "topic_attach_rollback", TopicTitle: "Attach rollback",
			}
			snapshot := runtimeapi.SessionSnapshot{
				Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
				Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
			}
			runtimeA.workspaces = []runtimeapi.Workspace{workspace}
			runtimeA.created = []runtimeapi.CreatedSession{created}
			runtimeA.snapshots[created.Session] = snapshot
			runtimeA.core.workspace = workspace
			runtimeA.core.created = created
			runtimeA.core.snapshot = snapshot
			if test.existing {
				runtimeA.sessions[workspace.ID] = []runtimeapi.SessionSummary{{
					Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
				}}
			}
			runtimeB := newRemoteWorkbenchV1TestRuntime()
			adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
			connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
				if sameTarget(target, targetB) {
					return adapterB, nil
				}
				return nil, errors.New("unexpected target switch")
			})
			app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
			var switchErr error
			var switchOnce sync.Once
			runtimeA.attachHook = func() {
				switchOnce.Do(func() {
					switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{})
				})
			}

			if err := test.invoke(app, workspace, created); !errors.Is(err, ErrTargetTransitionSuperseded) {
				t.Fatalf("successful attach after target supersede = %v, want ErrTargetTransitionSuperseded", err)
			}
			if switchErr != nil {
				t.Fatal(switchErr)
			}
			runtimeA.mu.Lock()
			unsubscribed := append([]runtimeapi.SessionRef(nil), runtimeA.unsubscribed...)
			subscribed := runtimeA.subscribed[created.Session]
			runtimeA.mu.Unlock()
			if len(unsubscribed) != 1 || unsubscribed[0] != created.Session || subscribed {
				t.Fatalf("superseded attach rollback = unsubscribed %#v subscribed=%v", unsubscribed, subscribed)
			}
		})
	}
}

func TestNewestReusableRemoteBlankSessionRejectsNonBlankOrActiveCandidates(t *testing.T) {
	workspace := runtimeapi.WorkspaceID("workspace_blank_candidate")
	summary := func(id string, activity int64) runtimeapi.SessionSummary {
		return runtimeapi.SessionSummary{
			Session: runtimeapi.SessionRef{WorkspaceID: workspace, SessionID: runtimeapi.SessionID(id)},
			TopicID: runtimeapi.TopicID("topic_" + id), Title: id,
			CreatedAtMillis: activity, LastActivityMillis: activity,
		}
	}
	used := summary("used", 100)
	used.Turns = 1
	preview := summary("preview", 101)
	preview.Preview = "unfinished input"
	branched := summary("branched", 102)
	branched.BranchSource = &runtimeapi.BranchSource{Parent: summary("parent", 1).Session, ParentCheckpointID: "checkpoint_parent"}
	recovering := summary("recovering", 103)
	recovering.RecoveryInterrupted = true
	running := summary("running", 104)
	running.Runtime = &runtimeapi.SessionRuntimeSummary{Running: true}
	prompt := summary("prompt", 105)
	prompt.Runtime = &runtimeapi.SessionRuntimeSummary{PendingPrompt: true}
	jobs := summary("jobs", 106)
	jobs.Runtime = &runtimeapi.SessionRuntimeSummary{ActiveJobs: 1}
	sharedTopic := summary("shared_topic", 107)
	olderBlank := summary("older_blank", 10)
	newerBlank := summary("newer_blank", 20)
	candidates := []runtimeapi.SessionSummary{
		used, preview, branched, recovering, running, prompt, jobs, sharedTopic, olderBlank, newerBlank,
	}
	topics := make([]runtimeapi.TopicSummary, 0, len(candidates))
	for _, candidate := range candidates {
		count := 1
		if candidate.Session == sharedTopic.Session {
			count = 2
		}
		topics = append(topics, runtimeapi.TopicSummary{TopicID: candidate.TopicID, SessionCount: count})
	}

	selected, ok := newestReusableRemoteBlankSession(candidates, topics)
	if !ok || selected.Session != newerBlank.Session {
		t.Fatalf("reusable blank selection = %#v, %v", selected, ok)
	}
}

func TestRemoteReusableBlankSnapshotRejectsEveryActiveOrNonFreshField(t *testing.T) {
	if !remoteSnapshotIsReusableBlank(runtimeapi.SessionSnapshot{}) {
		t.Fatal("zero fresh snapshot was rejected")
	}
	text := "state"
	tests := []struct {
		name   string
		mutate func(*runtimeapi.SessionSnapshot)
	}{
		{name: "history total", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.TotalTurns = 1 }},
		{name: "history actual", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.ActualTurns = 1 }},
		{name: "history start", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.StartTurn = 1 }},
		{name: "history end", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.EndTurn = 1 }},
		{name: "history message", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.Messages = []runtimeapi.HistoryMessage{{}} }},
		{name: "history older", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.HasOlder = true }},
		{name: "history cursor", mutate: func(s *runtimeapi.SessionSnapshot) { s.History.Next = "cursor_nonfresh" }},
		{name: "running", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.Running = true }},
		{name: "turn", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.CurrentTurn = &runtimeapi.TurnState{} }},
		{name: "operation", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.CurrentOperation = &runtimeapi.OperationState{} }},
		{name: "cancel", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.CancelRequested = true }},
		{name: "outcome", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.LastOutcome = runtimeapi.OutcomeCompleted }},
		{name: "error", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.LastError = &text }},
		{name: "interruption", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.Interruption = &runtimeapi.RuntimeInterruption{} }},
		{name: "live event", mutate: func(s *runtimeapi.SessionSnapshot) { s.Runtime.LiveEvents = []eventwire.Event{{Kind: "turn_started"}} }},
		{name: "prompt", mutate: func(s *runtimeapi.SessionSnapshot) { s.PendingPrompt = &runtimeapi.PendingPrompt{} }},
		{name: "goal", mutate: func(s *runtimeapi.SessionSnapshot) { s.Goal = &text }},
		{name: "goal status", mutate: func(s *runtimeapi.SessionSnapshot) { s.GoalStatus = runtimeapi.GoalComplete }},
		{name: "todo", mutate: func(s *runtimeapi.SessionSnapshot) { s.Todos = []runtimeapi.TodoItem{{}} }},
		{name: "job", mutate: func(s *runtimeapi.SessionSnapshot) { s.Jobs = []runtimeapi.Job{{}} }},
		{name: "checkpoint", mutate: func(s *runtimeapi.SessionSnapshot) { s.Checkpoints = []runtimeapi.Checkpoint{{}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var snapshot runtimeapi.SessionSnapshot
			test.mutate(&snapshot)
			if remoteSnapshotIsReusableBlank(snapshot) {
				t.Fatalf("snapshot with %s was treated as reusable blank: %#v", test.name, snapshot)
			}
		})
	}
}

func TestRemoteEnsureBlankRechecksCatalogCandidateAtomicSnapshot(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_recheck", Label: "Blank recheck"}
	runtime := newRemoteSessionCreateGateTestRuntime()
	runtime.release()
	workspace := runtimeapi.Workspace{ID: "workspace_blank_recheck", Name: "Blank recheck", DisplayPath: "/host/blank-recheck"}
	candidate := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_stale_blank"}
	replacement := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_fresh_replacement"},
		TopicID: "topic_fresh_replacement", TopicTitle: defaultTopicTitle,
	}
	runtime.workspaces = []runtimeapi.Workspace{workspace}
	runtime.topics[workspace.ID] = []runtimeapi.TopicSummary{{TopicID: "topic_stale_blank", Title: "Stale blank", SessionCount: 1}}
	runtime.sessions[workspace.ID] = []runtimeapi.SessionSummary{{Session: candidate, TopicID: "topic_stale_blank", Title: "Stale blank"}}
	runtime.snapshots[candidate] = runtimeapi.SessionSnapshot{
		Session: candidate, TopicID: "topic_stale_blank", Title: "Stale blank",
		Runtime: runtimeapi.RuntimeState{CurrentOperation: &runtimeapi.OperationState{ID: "operation_started_after_list"}},
	}
	runtime.created = []runtimeapi.CreatedSession{replacement}
	runtime.snapshots[replacement.Session] = runtimeapi.SessionSnapshot{
		Session: replacement.Session, TopicID: replacement.TopicID, Title: replacement.TopicTitle,
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)

	meta, err := app.remoteEnsureBlankTab(string(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionPath != remoteSessionToken(replacement.Session) || runtime.calls() != 1 {
		t.Fatalf("/new reused stale catalog candidate: meta=%#v createCalls=%d", meta, runtime.calls())
	}
	runtime.mu.Lock()
	unsubscribed := append([]runtimeapi.SessionRef(nil), runtime.unsubscribed...)
	attachInputs := append([]runtimeapi.AttachAndSubscribeInput(nil), runtime.attachInputs...)
	runtime.mu.Unlock()
	if len(unsubscribed) != 1 || unsubscribed[0] != candidate {
		t.Fatalf("rejected catalog candidate unsubscribe = %#v", unsubscribed)
	}
	if len(attachInputs) != 2 || attachInputs[0].Session != candidate || attachInputs[1].Session != replacement.Session {
		t.Fatalf("catalog candidate/replacement attaches = %#v", attachInputs)
	}
}

func TestRemoteEnsureBlankFastPathUsesCanonicalSnapshot(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_fast_path", Label: "Blank fast path"}
	runtime := newRemoteSessionCreateGateTestRuntime()
	runtime.release()
	workspace := runtimeapi.Workspace{ID: "workspace_blank_fast_path", Name: "Blank fast path", DisplayPath: "/host/blank-fast-path"}
	source := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_fast_path_active"}
	replacement := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_fast_path_fresh"},
		TopicID: "topic_fast_path_fresh", TopicTitle: defaultTopicTitle,
	}
	runtime.workspaces = []runtimeapi.Workspace{workspace}
	runtime.topics[workspace.ID] = []runtimeapi.TopicSummary{{TopicID: "topic_fast_path_active", Title: "Active", SessionCount: 1}}
	runtime.sessions[workspace.ID] = []runtimeapi.SessionSummary{{
		Session: source, TopicID: "topic_fast_path_active", Title: "Active",
	}}
	runtime.snapshots[source] = runtimeapi.SessionSnapshot{
		Session: source, TopicID: "topic_fast_path_active", Title: "Active",
	}
	runtime.created = []runtimeapi.CreatedSession{replacement}
	runtime.snapshots[replacement.Session] = runtimeapi.SessionSnapshot{
		Session: replacement.Session, TopicID: replacement.TopicID, Title: replacement.TopicTitle,
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)
	if _, err := app.attachRemoteWorkbenchSession(source, true); err != nil {
		t.Fatal(err)
	}
	// The cached workbench snapshot is still blank, but the Host becomes active
	// before /new makes its decision. Only a fresh atomic attach can observe it.
	runtime.mu.Lock()
	active := runtime.snapshots[source]
	active.Runtime.CurrentOperation = &runtimeapi.OperationState{ID: "operation_active"}
	active.Jobs = []runtimeapi.Job{{ID: "job_active"}}
	runtime.snapshots[source] = active
	runtime.mu.Unlock()

	meta, err := app.remoteEnsureBlankTab(string(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionPath != remoteSessionToken(replacement.Session) || runtime.calls() != 1 {
		t.Fatalf("/new fast path reused active Session: meta=%#v createCalls=%d", meta, runtime.calls())
	}
	runtime.mu.Lock()
	unsubscribed := append([]runtimeapi.SessionRef(nil), runtime.unsubscribed...)
	runtime.mu.Unlock()
	if len(unsubscribed) != 0 {
		t.Fatalf("/new catalog retry unsubscribed an already-open active Session: %#v", unsubscribed)
	}
	app.remote.workbenchMu.RLock()
	refreshed := app.remote.workbench.Sessions[source]
	app.remote.workbenchMu.RUnlock()
	if refreshed == nil || refreshed.Snapshot.Runtime.CurrentOperation == nil || len(refreshed.Snapshot.Jobs) != 1 {
		t.Fatalf("open Session did not retain authoritative active snapshot: %#v", refreshed)
	}
}

func TestRemoteBlankPendingCommitCannotOverwriteNewerHostState(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_pending_old", Label: "Old blank Host"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_blank_pending_new", Label: "New blank Host"}
	runtimeA := newRemoteSessionCreateGateTestRuntime()
	runtimeA.release()
	workspace := runtimeapi.Workspace{ID: "workspace_blank_pending", Name: "Blank pending", DisplayPath: "/host/blank-pending"}
	created := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_blank_pending_old"},
		TopicID: "topic_blank_pending_old", TopicTitle: defaultTopicTitle,
	}
	runtimeA.workspaces = []runtimeapi.Workspace{workspace}
	runtimeA.created = []runtimeapi.CreatedSession{created}
	runtimeA.snapshots[created.Session] = runtimeapi.SessionSnapshot{Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle}
	runtimeB := newRemoteWorkbenchV1TestRuntime()
	adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if sameTarget(target, targetB) {
			return adapterB, nil
		}
		return nil, errors.New("unexpected target switch")
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
	var switchErr error
	runtimeA.createHook = func() {
		switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{})
	}

	if _, err := app.remoteEnsureBlankTab(string(workspace.ID)); !errors.Is(err, ErrTargetTransitionSuperseded) {
		t.Fatalf("/new after newer Host StateSink = %v, want ErrTargetTransitionSuperseded", err)
	}
	if switchErr != nil {
		t.Fatal(switchErr)
	}
	app.remote.workbenchMu.RLock()
	hostID := app.remote.workbench.HostID
	pending := len(app.remote.workbench.Pending)
	sessions := len(app.remote.workbench.Sessions)
	app.remote.workbenchMu.RUnlock()
	if hostID == targetA.ID || pending != 0 || sessions != 0 {
		t.Fatalf("stale blank Host state overwrote Host B: host=%q pending=%d sessions=%d", hostID, pending, sessions)
	}
}

func TestRemoteListFirstAttachRejectsCrossHostOpaqueIDCollision(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*App, runtimeapi.Workspace) error
	}{
		{name: "project tab", invoke: func(app *App, workspace runtimeapi.Workspace) error {
			_, err := app.remoteOpenProjectTab(string(workspace.ID), "")
			return err
		}},
		{name: "workspace switch", invoke: func(app *App, workspace runtimeapi.Workspace) error {
			_, err := app.remoteSwitchWorkspaceV1(string(workspace.ID))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_collision_a", Label: "Collision A"}
			targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_collision_b", Label: "Collision B"}
			workspace := runtimeapi.Workspace{ID: "workspace_collision", Name: "Collision", DisplayPath: "/host/collision"}
			collision := runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_collision"}
			runtimeA := newRemoteWorkbenchV1TestRuntime()
			runtimeA.workspaces = []runtimeapi.Workspace{workspace}
			runtimeA.sessions[workspace.ID] = []runtimeapi.SessionSummary{{Session: collision, TopicID: "topic_a", Title: "Host A Session"}}
			runtimeA.snapshots[collision] = runtimeapi.SessionSnapshot{Session: collision, TopicID: "topic_a", Title: "Host A Session"}
			runtimeB := newRemoteWorkbenchV1TestRuntime()
			runtimeB.workspaces = []runtimeapi.Workspace{workspace}
			runtimeB.sessions[workspace.ID] = []runtimeapi.SessionSummary{{Session: collision, TopicID: "topic_b", Title: "Host B unrelated Session"}}
			runtimeB.snapshots[collision] = runtimeapi.SessionSnapshot{Session: collision, TopicID: "topic_b", Title: "Host B unrelated Session"}
			adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
			connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
				if sameTarget(target, targetB) {
					return adapterB, nil
				}
				return nil, errors.New("unexpected target switch")
			})
			app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
			var switchErr error
			var once sync.Once
			runtimeA.listSessionsHook = func() {
				once.Do(func() { switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{}) })
			}

			if err := test.invoke(app, workspace); !errors.Is(err, ErrTargetTransitionSuperseded) {
				t.Fatalf("list-first attach after Host switch = %v, want ErrTargetTransitionSuperseded", err)
			}
			if switchErr != nil {
				t.Fatal(switchErr)
			}
			app.remote.workbenchMu.RLock()
			sessions := len(app.remote.workbench.Sessions)
			app.remote.workbenchMu.RUnlock()
			if sessions != 0 {
				t.Fatalf("opaque ID collision attached unrelated Host B Session: sessions=%d", sessions)
			}
		})
	}
}

func TestRemoteListFirstCreatePathsRetryExistingSessionAfterAttachFailure(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*App, runtimeapi.Workspace) error
	}{
		{
			name: "project tab",
			invoke: func(app *App, workspace runtimeapi.Workspace) error {
				_, err := app.remoteOpenProjectTab(string(workspace.ID), "")
				return err
			},
		},
		{
			name: "workspace switch",
			invoke: func(app *App, workspace runtimeapi.Workspace) error {
				_, err := app.remoteSwitchWorkspaceV1(string(workspace.ID))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := newRemoteSessionCreateGateTestRuntime()
			runtime.release()
			workspace := runtimeapi.Workspace{ID: "workspace_list_retry", Name: "List retry", DisplayPath: "/host/list-retry"}
			created := runtimeapi.CreatedSession{
				Session: runtimeapi.SessionRef{WorkspaceID: workspace.ID, SessionID: "session_opaque_list_retry"},
				TopicID: "topic_opaque_list_retry", TopicTitle: defaultTopicTitle,
			}
			runtime.workspaces = []runtimeapi.Workspace{workspace}
			runtime.created = []runtimeapi.CreatedSession{created}
			runtime.snapshots[created.Session] = runtimeapi.SessionSnapshot{
				Session: created.Session, TopicID: created.TopicID, Title: created.TopicTitle,
				Profile: runtimeapi.ResolvedProfile{Model: "remote-model"},
			}
			runtime.attachErrors[created.Session] = errors.New("atomic attach interrupted")
			app, _ := newRemoteWorkbenchTestApp(t, TargetDescriptor{
				Kind: TargetRemote, ID: "host_list_retry", Label: "List retry",
			}, runtime, nil)

			if err := test.invoke(app, workspace); err == nil || !containsError(err, "atomic attach interrupted") {
				t.Fatalf("first attach error = %v", err)
			}
			runtime.mu.Lock()
			delete(runtime.attachErrors, created.Session)
			runtime.mu.Unlock()
			if err := test.invoke(app, workspace); err != nil {
				t.Fatal(err)
			}
			if calls := runtime.calls(); calls != 1 {
				t.Fatalf("retry created a second Host Session; calls = %d", calls)
			}
			runtime.mu.Lock()
			attachInputs := append([]runtimeapi.AttachAndSubscribeInput(nil), runtime.attachInputs...)
			runtime.mu.Unlock()
			if len(attachInputs) != 2 || attachInputs[0].Session != created.Session || attachInputs[1].Session != created.Session {
				t.Fatalf("attach retries = %#v", attachInputs)
			}
		})
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

func TestRemoteWorkbenchReattachReadyPrecedesNewerTargetState(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_reattach_ready_a", Label: "Reattach ready A"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_reattach_ready_b", Label: "Reattach ready B"}
	runtimeA, refs := remoteWorkbenchV1Fixture()
	runtimeB := newRemoteWorkbenchV1TestRuntime()
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if sameTarget(target, targetB) {
			return &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}, nil
		}
		return nil, errors.New("unexpected target switch")
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
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
	activeTabID := app.remote.workbench.ActiveTabID
	app.remote.workbenchMu.Unlock()

	type observedEvent struct {
		name     string
		hostID   string
		attached bool
		tabID    string
	}
	var eventsMu sync.Mutex
	var events []observedEvent
	baseline := make(chan struct{})
	barrier := make(chan struct{})
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		event := observedEvent{name: name}
		if len(payload) != 0 {
			switch value := payload[0].(type) {
			case RemoteWorkbenchStatusView:
				event.hostID = value.HostID
				event.attached = value.SessionAttached
				event.tabID = value.TabID
			case string:
				event.tabID = value
			}
		}
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
		if name == "test:reattach-ready-baseline" {
			close(baseline)
		}
		if name == "test:reattach-ready-barrier" {
			close(barrier)
		}
	})
	app.emitRuntimeEvent("test:reattach-ready-baseline")
	waitSignal(t, baseline, "initial reattach-ready event queue barrier")
	eventsMu.Lock()
	events = nil
	eventsMu.Unlock()

	var switchMu sync.Mutex
	switched := false
	var switchErr error
	app.projectTreeChangedHook = func() {
		switchMu.Lock()
		if switched {
			switchMu.Unlock()
			return
		}
		switched = true
		switchMu.Unlock()
		switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{})
	}

	app.reattachRemoteWorkbench(expected)
	if switchErr != nil {
		t.Fatal(switchErr)
	}
	app.emitRuntimeEvent("test:reattach-ready-barrier")
	waitSignal(t, barrier, "reattach ready event queue barrier")

	eventsMu.Lock()
	got := append([]observedEvent(nil), events...)
	eventsMu.Unlock()
	index := func(match func(observedEvent) bool) int {
		for i, event := range got {
			if match(event) {
				return i
			}
		}
		return -1
	}
	aState := index(func(event observedEvent) bool {
		return event.name == remoteWorkbenchStateEvent && event.hostID == targetA.ID && event.attached && event.tabID == activeTabID
	})
	aRebuilt := index(func(event observedEvent) bool { return event.name == "runtime:rebuilt" && event.tabID == activeTabID })
	aReady := index(func(event observedEvent) bool { return event.name == "agent:ready" && event.tabID == activeTabID })
	bState := index(func(event observedEvent) bool {
		return event.name == remoteWorkbenchStateEvent && event.hostID == targetB.ID && !event.attached
	})
	if aState < 0 || aRebuilt < 0 || aReady < 0 || bState < 0 {
		t.Fatalf("missing ordered reattach events: %#v", got)
	}
	if !(aState < aRebuilt && aRebuilt < aReady && aReady < bState) {
		t.Fatalf("reattach ready crossed newer StateSink: state=%d rebuilt=%d ready=%d newState=%d events=%#v", aState, aRebuilt, aReady, bState, got)
	}
}

func TestRemoteWorkbenchReattachDoesNotPublishReadyAfterNewerTargetWins(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_reattach_lost_a", Label: "Reattach lost A"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_reattach_won_b", Label: "Reattach won B"}
	fixture, refs := remoteWorkbenchV1Fixture()
	runtimeA := newRemoteSessionCreateGateTestRuntime()
	runtimeA.remoteWorkbenchV1TestRuntime = fixture
	runtimeB := newRemoteWorkbenchV1TestRuntime()
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if sameTarget(target, targetB) {
			return &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}, nil
		}
		return nil, errors.New("unexpected target switch")
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
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
	activeTabID := app.remote.workbench.ActiveTabID
	app.remote.workbenchMu.Unlock()

	type observedEvent struct {
		name   string
		hostID string
		tabID  string
	}
	var eventsMu sync.Mutex
	var events []observedEvent
	baseline := make(chan struct{})
	barrier := make(chan struct{})
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		event := observedEvent{name: name}
		if len(payload) != 0 {
			switch value := payload[0].(type) {
			case RemoteWorkbenchStatusView:
				event.hostID = value.HostID
				event.tabID = value.TabID
			case string:
				event.tabID = value
			}
		}
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
		if name == "test:reattach-lost-baseline" {
			close(baseline)
		}
		if name == "test:reattach-lost-barrier" {
			close(barrier)
		}
	})
	// Initial attach ready events were queued before the recording emitter was
	// installed. Drain them so this assertion covers only the superseded
	// reattach attempt below.
	app.emitRuntimeEvent("test:reattach-lost-baseline")
	waitSignal(t, baseline, "initial attach event queue barrier")
	eventsMu.Lock()
	events = nil
	eventsMu.Unlock()

	runtimeA.blockAttach = true
	done := make(chan struct{})
	go func() {
		app.reattachRemoteWorkbench(expected)
		close(done)
	}()
	waitSignal(t, runtimeA.attachStarted, "blocked Host A reattach")
	if err := manager.Switch(context.Background(), targetB, SwitchTargetOptions{}); err != nil {
		t.Fatal(err)
	}
	runtimeA.releaseAttachment()
	waitSignal(t, done, "superseded Host A reattach")
	app.emitRuntimeEvent("test:reattach-lost-barrier")
	waitSignal(t, barrier, "superseded reattach event queue barrier")

	eventsMu.Lock()
	got := append([]observedEvent(nil), events...)
	eventsMu.Unlock()
	bState := -1
	for index, event := range got {
		if event.name == remoteWorkbenchStateEvent && event.hostID == targetB.ID {
			bState = index
		}
	}
	if bState < 0 {
		t.Fatalf("newer Host B state was not published: %#v", got)
	}
	for index := bState + 1; index < len(got); index++ {
		event := got[index]
		if event.tabID == activeTabID && (event.name == "runtime:rebuilt" || event.name == "agent:ready") {
			t.Fatalf("superseded Host A published %s after Host B won: %#v", event.name, got)
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
