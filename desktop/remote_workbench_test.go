package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/eventwire"
	"reasonix/internal/runtimeapi"
)

type remoteWorkbenchTestRuntime struct {
	runtimeapi.V1RuntimeAPI

	mu sync.Mutex

	events chan runtimeapi.Event

	browsePage runtimeapi.WorkspacePage
	workspace  runtimeapi.Workspace
	created    runtimeapi.CreatedSession
	snapshot   runtimeapi.SessionSnapshot
	attachErrs []error
	createHook func()
	attachHook func()
	submit     runtimeapi.ComposerSubmitResult

	browseInputs     []runtimeapi.BrowseWorkspaceInput
	openInputs       []runtimeapi.OpenWorkspaceInput
	createInputs     []runtimeapi.CreateSessionInput
	attachInputs     []runtimeapi.AttachAndSubscribeInput
	submitInputs     []runtimeapi.ComposerSubmitInput
	steerInputs      []runtimeapi.SteerInput
	cancelInputs     []runtimeapi.CancelTurnInput
	operationCancels []runtimeapi.CancelOperationInput
	shellInputs      []runtimeapi.RunShellInput
	profileInputs    []runtimeapi.SetProfileInput
	profileResult    runtimeapi.SetProfileResult
	approveInputs    []runtimeapi.ApproveInput
	answerInputs     []runtimeapi.AnswerInput
	unsubscribed     []runtimeapi.SessionRef
	unsubscribeErr   error
}

func newRemoteWorkbenchTestRuntime() *remoteWorkbenchTestRuntime {
	ref := runtimeapi.SessionRef{WorkspaceID: "workspace_opaque", SessionID: "session_opaque"}
	return &remoteWorkbenchTestRuntime{
		events: make(chan runtimeapi.Event, 16),
		browsePage: runtimeapi.WorkspacePage{
			Directory: runtimeapi.Directory{Ref: "dir_parent", Name: "home", DisplayPath: "/srv/home"},
			Entries:   []runtimeapi.Directory{{Ref: "dir_primary", Name: "repo", DisplayPath: "/srv/home/repo", ParentRef: "dir_parent"}},
			HasMore:   true,
			Next:      "cursor_opaque",
		},
		workspace: runtimeapi.Workspace{ID: ref.WorkspaceID, Name: "Remote workspace", DisplayPath: "/srv/home/repo"},
		created: runtimeapi.CreatedSession{
			Session: ref, TopicID: "topic_opaque", TopicTitle: "Remote topic",
			ResolvedProfile: runtimeapi.ResolvedProfile{Model: "remote-model", CollaborationMode: "plan", ToolApprovalMode: "ask", TokenMode: "balanced"},
		},
		snapshot: runtimeapi.SessionSnapshot{
			Session: ref, TopicID: "topic_opaque", Title: "Remote topic",
			Profile: runtimeapi.ResolvedProfile{Model: "remote-model", CollaborationMode: "plan", ToolApprovalMode: "ask", TokenMode: "balanced"},
		},
		submit: runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitTurn, TurnID: "turn_opaque"},
	}
}

func (r *remoteWorkbenchTestRuntime) Connection(context.Context) (runtimeapi.ConnectionView, error) {
	return runtimeapi.ConnectionView{Label: "Remote test Host"}, nil
}

func (r *remoteWorkbenchTestRuntime) BrowseWorkspace(_ context.Context, input runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.browseInputs = append(r.browseInputs, input)
	return r.browsePage, nil
}

func (r *remoteWorkbenchTestRuntime) OpenWorkspace(_ context.Context, input runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openInputs = append(r.openInputs, input)
	return runtimeapi.OpenWorkspaceResult{Workspace: r.workspace}, nil
}

func (r *remoteWorkbenchTestRuntime) CreateSession(_ context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	r.mu.Lock()
	input.AdditionalDirectories = append([]runtimeapi.DirectoryRef(nil), input.AdditionalDirectories...)
	r.createInputs = append(r.createInputs, input)
	created := r.created
	hook := r.createHook
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return created, nil
}

func (r *remoteWorkbenchTestRuntime) AttachAndSubscribe(_ context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	r.mu.Lock()
	r.attachInputs = append(r.attachInputs, input)
	var err error
	if len(r.attachErrs) != 0 {
		err = r.attachErrs[0]
		r.attachErrs = r.attachErrs[1:]
	}
	hook := r.attachHook
	snapshot := cloneRemoteSessionSnapshot(r.snapshot)
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return snapshot, err
}

func (r *remoteWorkbenchTestRuntime) ComposerSubmit(_ context.Context, input runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submitInputs = append(r.submitInputs, input)
	return r.submit, nil
}

func (r *remoteWorkbenchTestRuntime) SteerTurn(_ context.Context, input runtimeapi.SteerInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steerInputs = append(r.steerInputs, input)
	return nil
}

func (r *remoteWorkbenchTestRuntime) CancelTurn(_ context.Context, input runtimeapi.CancelTurnInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelInputs = append(r.cancelInputs, input)
	return nil
}

func (r *remoteWorkbenchTestRuntime) CancelOperation(_ context.Context, input runtimeapi.CancelOperationInput) (runtimeapi.CancelOperationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.operationCancels = append(r.operationCancels, input)
	return runtimeapi.CancelOperationResult{Status: runtimeapi.CancelRequested, OperationID: input.OperationID}, nil
}

func (r *remoteWorkbenchTestRuntime) RunShell(_ context.Context, input runtimeapi.RunShellInput) (runtimeapi.OperationStartedResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shellInputs = append(r.shellInputs, input)
	return runtimeapi.OperationStartedResult{OperationID: "operation_shell", Disposition: "started"}, nil
}

func (r *remoteWorkbenchTestRuntime) SetProfile(_ context.Context, input runtimeapi.SetProfileInput) (runtimeapi.SetProfileResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profileInputs = append(r.profileInputs, input)
	return r.profileResult, nil
}

func (r *remoteWorkbenchTestRuntime) ApprovePrompt(_ context.Context, input runtimeapi.ApproveInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approveInputs = append(r.approveInputs, input)
	return nil
}

func (r *remoteWorkbenchTestRuntime) AnswerPrompt(_ context.Context, input runtimeapi.AnswerInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	input.Answers = append([]runtimeapi.QuestionAnswer(nil), input.Answers...)
	for index := range input.Answers {
		input.Answers[index].Selected = append([]string(nil), input.Answers[index].Selected...)
	}
	r.answerInputs = append(r.answerInputs, input)
	return nil
}

func (r *remoteWorkbenchTestRuntime) Events() <-chan runtimeapi.Event { return r.events }

func (r *remoteWorkbenchTestRuntime) ListWorkspaces(context.Context, runtimeapi.ListWorkspacesInput) (runtimeapi.WorkspaceListPage, error) {
	return runtimeapi.WorkspaceListPage{}, runtimeapi.Unavailable(runtimeapi.CapabilitySessionAttach, "catalog not configured in core workbench test")
}

func (r *remoteWorkbenchTestRuntime) UnsubscribeSession(_ context.Context, input runtimeapi.UnsubscribeSessionInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unsubscribed = append(r.unsubscribed, input.Session)
	return r.unsubscribeErr
}

type remoteWorkbenchTestAdapter struct {
	target  TargetDescriptor
	runtime runtimeapi.RuntimeAPI
}

func (a *remoteWorkbenchTestAdapter) Descriptor() TargetDescriptor      { return a.target }
func (a *remoteWorkbenchTestAdapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a.runtime }
func (a *remoteWorkbenchTestAdapter) CanRelease(context.Context) (ReleaseStatus, error) {
	return ReleaseStatus{}, nil
}
func (a *remoteWorkbenchTestAdapter) Detach(context.Context) error { return nil }
func (a *remoteWorkbenchTestAdapter) AbandonTarget() error         { return nil }

func newRemoteWorkbenchTestApp(t *testing.T, target TargetDescriptor, rt runtimeapi.RuntimeAPI, connector TargetConnector) (*App, *TargetManager) {
	t.Helper()
	if connector == nil {
		connector = TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
			return nil, errors.New("unexpected target switch")
		})
	}
	manager, err := NewTargetManager(connector, &remoteWorkbenchTestAdapter{target: target, runtime: rt}, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	setRemoteWorkbenchTestEmitter(app, func(context.Context, string, ...interface{}) {})
	app.readyHook = func() {}
	app.projectTreeChangedHook = func() {}
	installRemoteAppTestState(t, app, newRemoteAppTestStore(t), manager)
	manager.SetEventSink(app.handleTargetRuntimeEvent)
	manager.SetStateSink(app.handleTargetState)
	return app, manager
}

func setRemoteWorkbenchTestEmitter(app *App, emit runtimeEventEmitFunc) {
	app.runtimeEvents.mu.Lock()
	app.runtimeEvents.emit = emit
	app.runtimeEvents.mu.Unlock()
}

func TestRemoteWorkbenchBrowseCreateRetryAndOpaquePathBoundary(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_a", Label: "Host A"}
	rt := newRemoteWorkbenchTestRuntime()
	rt.attachErrs = []error{errors.New("atomic subscribe interrupted")}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)

	page, err := app.BrowseRemoteWorkspace(RemoteWorkspaceBrowseInput{
		DirectoryRef: " dir_parent ", TypedPath: " /srv/home ", Cursor: "cursor_before", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Directory.Ref != "dir_parent" || page.Entries[0].Ref != "dir_primary" || !page.HasMore || page.Next != "cursor_opaque" {
		t.Fatalf("Remote browse projection = %#v", page)
	}
	rt.mu.Lock()
	if len(rt.browseInputs) != 1 || rt.browseInputs[0].DirectoryRef != "dir_parent" || rt.browseInputs[0].TypedPath != "/srv/home" || rt.browseInputs[0].Cursor != "cursor_before" {
		t.Fatalf("Remote browse input = %#v", rt.browseInputs)
	}
	rt.mu.Unlock()

	input := RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef:     " dir_primary ",
		AdditionalDirectoryRefs: []string{"dir_primary", " dir_extra ", "dir_extra"},
		TopicTitle:              " Remote topic ",
	}
	if _, err := app.CreateRemoteWorkspaceSession(input); err == nil || !containsError(err, "atomic subscribe interrupted") {
		t.Fatalf("first create error = %v, want interrupted attach", err)
	}
	status, err := app.CreateRemoteWorkspaceSession(input)
	if err != nil {
		t.Fatal(err)
	}
	if !status.SessionAttached || status.HostID != target.ID || status.WorkspaceName != rt.workspace.Name || status.WorkspaceDisplayPath != rt.workspace.DisplayPath || status.TabID != remoteSessionTabID(rt.created.Session) {
		t.Fatalf("Remote workbench status = %#v", status)
	}
	rt.mu.Lock()
	if len(rt.openInputs) != 1 || len(rt.createInputs) != 1 || len(rt.attachInputs) != 2 {
		t.Fatalf("open/create/attach calls = %d/%d/%d, want 1/1/2", len(rt.openInputs), len(rt.createInputs), len(rt.attachInputs))
	}
	createdInput := rt.createInputs[0]
	rt.mu.Unlock()
	if createdInput.WorkspaceID != rt.workspace.ID || createdInput.Topic.Kind != runtimeapi.TopicNew || createdInput.Topic.Title != "Remote topic" || len(createdInput.AdditionalDirectories) != 1 || createdInput.AdditionalDirectories[0] != "dir_extra" {
		t.Fatalf("CreateSession input = %#v", createdInput)
	}

	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("Remote tabs = %#v", tabs)
	}
	if tabs[0].TargetKind != string(TargetRemote) || tabs[0].WorkspaceID != string(rt.workspace.ID) || tabs[0].SessionID != string(rt.created.Session.SessionID) {
		t.Fatalf("Remote tab opaque identity = %#v", tabs[0])
	}
	if tabs[0].WorkspaceRoot != string(rt.workspace.ID) || tabs[0].SessionPath != remoteSessionToken(rt.created.Session) {
		t.Fatalf("Remote opaque identities missing from compatibility fields: %#v", tabs[0])
	}
	if tabs[0].WorkspacePath != rt.workspace.DisplayPath || tabs[0].Cwd != rt.workspace.DisplayPath {
		t.Fatalf("Remote display-only paths missing: %#v", tabs[0])
	}
	meta := app.MetaForTab(status.TabID)
	if meta.WorkspaceRoot != string(rt.workspace.ID) || meta.WorkspacePath != rt.workspace.DisplayPath || meta.Cwd != rt.workspace.DisplayPath || meta.ImageInputEnabled {
		t.Fatalf("Remote Meta path/media boundary = %#v", meta)
	}
	if tree := app.ListProjectTree(); len(tree) != 0 {
		t.Fatalf("Phase 5 Remote project tree = %#v, want no local path traversal", tree)
	}
}

func TestRemoteWorkspaceSessionCreateBlocksNativeCloseUntilResultIsKnown(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_close_guard", Label: "Close guard"}
	runtime := newRemoteWorkbenchTestRuntime()
	attachStarted := make(chan struct{})
	releaseAttach := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseAttach) }) }
	defer release()
	runtime.attachHook = func() {
		close(attachStarted)
		<-releaseAttach
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)

	createDone := make(chan error, 1)
	go func() {
		_, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
			PrimaryDirectoryRef: "dir_primary",
			TopicTitle:          "Close guard",
		})
		createDone <- err
	}()

	<-attachStarted
	if !app.remoteWorkspaceSessionCreateInFlight() {
		t.Fatal("Remote workspace Session create was not marked in flight")
	}
	consumeSystemQuitRequested()
	t.Cleanup(func() { consumeSystemQuitRequested() })
	app.forceQuit.Store(true)
	markSystemQuitRequested()
	if !app.beforeClose(context.Background()) {
		t.Fatal("native OnBeforeClose did not fail closed during Remote Session creation")
	}
	if app.forceQuit.Load() || consumeSystemQuitRequested() {
		t.Fatal("blocked close retained an explicit quit signal for a later unrelated close")
	}
	app.CloseMainWindow()
	if app.forceQuit.Load() {
		t.Fatal("frameless CloseMainWindow advanced to runtime quit during Remote Session creation")
	}

	release()
	if err := <-createDone; err != nil {
		t.Fatal(err)
	}
	if app.remoteWorkspaceSessionCreateInFlight() {
		t.Fatal("Remote workspace Session create remained marked in flight after completion")
	}

	if !app.beginRemoteDesktopClose() {
		t.Fatal("native close gate did not admit shutdown after Remote Session creation")
	}
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary",
		TopicTitle:          "Must not reach Host",
	}); !errors.Is(err, errRemoteWorkspaceSessionCreateWhileClosing) {
		t.Fatalf("create admitted after native close = %v, want closing rejection", err)
	}
}

func TestRemoteBackgroundCloseCannotCancelAdmittedFinalClose(t *testing.T) {
	app := NewApp()
	if !app.beginRemoteDesktopClose() {
		t.Fatal("final close gate was not admitted")
	}
	if decision := app.decideRemoteDesktopBackgroundClose(); decision != remoteDesktopBackgroundCloseAlreadyClosing {
		t.Fatalf("background close decision = %v, want already closing", decision)
	}
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary",
		TopicTitle:          "Must remain closed",
	}); !errors.Is(err, errRemoteWorkspaceSessionCreateWhileClosing) {
		t.Fatalf("create admitted after competing background close = %v, want closing rejection", err)
	}
}

func TestCreateRemoteWorkspaceSessionPreservesExistingWorkspacesAndSessions(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_create_many", Label: "Host create many"}
	rt := newRemoteWorkbenchTestRuntime()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	type createdFixture struct {
		workspace runtimeapi.Workspace
		created   runtimeapi.CreatedSession
	}
	fixtures := []createdFixture{
		{
			workspace: runtimeapi.Workspace{ID: "workspace_many_a", Name: "A", DisplayPath: "/host/a"},
			created:   runtimeapi.CreatedSession{Session: runtimeapi.SessionRef{WorkspaceID: "workspace_many_a", SessionID: "session_many_a1"}, TopicID: "topic_many_a", TopicTitle: "A one"},
		},
		{
			workspace: runtimeapi.Workspace{ID: "workspace_many_b", Name: "B", DisplayPath: "/host/b"},
			created:   runtimeapi.CreatedSession{Session: runtimeapi.SessionRef{WorkspaceID: "workspace_many_b", SessionID: "session_many_b1"}, TopicID: "topic_many_b", TopicTitle: "B one"},
		},
		{
			workspace: runtimeapi.Workspace{ID: "workspace_many_a", Name: "A", DisplayPath: "/host/a"},
			created:   runtimeapi.CreatedSession{Session: runtimeapi.SessionRef{WorkspaceID: "workspace_many_a", SessionID: "session_many_a2"}, TopicID: "topic_many_a", TopicTitle: "A two"},
		},
	}
	for index, fixture := range fixtures {
		rt.mu.Lock()
		rt.workspace = fixture.workspace
		rt.created = fixture.created
		rt.snapshot = runtimeapi.SessionSnapshot{
			Session: fixture.created.Session, TopicID: fixture.created.TopicID, Title: fixture.created.TopicTitle,
		}
		rt.mu.Unlock()
		if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
			PrimaryDirectoryRef: "directory_many_" + string(rune('a'+index)), TopicTitle: fixture.created.TopicTitle,
		}); err != nil {
			t.Fatal(err)
		}
	}
	tabs := app.ListTabs()
	if len(tabs) != 3 || tabs[0].WorkspaceID != "workspace_many_a" || tabs[1].WorkspaceID != "workspace_many_b" || tabs[2].SessionID != "session_many_a2" || !tabs[2].Active {
		t.Fatalf("multi-create Remote tabs = %#v", tabs)
	}
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	if len(app.remote.workbench.Workspaces) != 2 || len(app.remote.workbench.Sessions) != 3 || len(app.remote.workbench.Pending) != 0 {
		t.Fatalf("multi-create workbench state = %#v", app.remote.workbench)
	}
}

func TestReplayRemotePendingPromptsCoversEveryTab(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_prompt_many", Label: "Host prompt many"}
	rt := newRemoteWorkbenchTestRuntime()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)

	first := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: "workspace_prompt_many", SessionID: "session_prompt_approval"},
		TopicID: "topic_prompt_approval", TopicTitle: "Approval topic",
	}
	second := runtimeapi.CreatedSession{
		Session: runtimeapi.SessionRef{WorkspaceID: "workspace_prompt_many", SessionID: "session_prompt_ask"},
		TopicID: "topic_prompt_ask", TopicTitle: "Ask topic",
	}
	question := "Choose one"
	fixtures := []struct {
		created runtimeapi.CreatedSession
		prompt  *runtimeapi.PendingPrompt
	}{
		{created: first, prompt: &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptApproval, Approval: &runtimeapi.ApprovalPrompt{
			ID: "prompt_many_approval", Tool: "bash", Subject: "run tests",
		}}},
		{created: second, prompt: &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptAsk, Ask: &runtimeapi.AskPrompt{
			ID: "prompt_many_ask", Questions: []runtimeapi.AskQuestion{{ID: "question_many", Prompt: &question}},
		}}},
	}
	for _, fixture := range fixtures {
		rt.mu.Lock()
		rt.workspace = runtimeapi.Workspace{ID: fixture.created.Session.WorkspaceID, Name: "Remote workspace", DisplayPath: "/host/prompts"}
		rt.created = fixture.created
		rt.snapshot = runtimeapi.SessionSnapshot{
			Session: fixture.created.Session, TopicID: fixture.created.TopicID, Title: fixture.created.TopicTitle,
			PendingPrompt: fixture.prompt,
		}
		rt.mu.Unlock()
		if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
			PrimaryDirectoryRef: "directory_" + string(fixture.created.Session.SessionID), TopicTitle: fixture.created.TopicTitle,
		}); err != nil {
			t.Fatal(err)
		}
	}

	emitted := make(chan wireEventTab, 4)
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		if name == eventChannel && len(payload) == 1 {
			if value, ok := payload[0].(wireEventTab); ok {
				emitted <- value
			}
		}
	})
	app.ReplayPendingPrompts()

	approval := waitValue(t, emitted, "non-active Remote approval replay")
	ask := waitValue(t, emitted, "active Remote ask replay")
	if approval.TabID != remoteSessionTabID(first.Session) || approval.Kind != "approval_request" || approval.Approval == nil || approval.Approval.ID != "prompt_many_approval" {
		t.Fatalf("approval replay = %#v", approval)
	}
	if ask.TabID != remoteSessionTabID(second.Session) || ask.Kind != "ask_request" || ask.Ask == nil || ask.Ask.ID != "prompt_many_ask" {
		t.Fatalf("ask replay = %#v", ask)
	}
	if len(emitted) != 0 {
		t.Fatalf("unexpected duplicate pending prompt replays: %d", len(emitted))
	}
}

func TestRemoteWorkbenchRoutesCurrentSessionAndMapsSnapshot(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_routes", Label: "Host routes"}
	rt := newRemoteWorkbenchTestRuntime()
	content, detail, reasoning, arguments, summary, diff, prompt := "answer", "detail", "thinking", `{\"path\":\"README.md\"}`, "changed", "@@ patch", "checkpoint prompt"
	goal := "ship Remote V1"
	rt.snapshot.Goal = &goal
	rt.snapshot.GoalStatus = runtimeapi.GoalRunning
	rt.snapshot.History = runtimeapi.HistoryPage{
		Messages: []runtimeapi.HistoryMessage{{
			Role: "assistant", Content: &content, Detail: &detail, Reasoning: &reasoning, CheckpointID: "checkpoint_opaque",
			ToolCalls: []runtimeapi.HistoryToolCall{{ID: "call_opaque", Name: "read", Arguments: &arguments, Summary: &summary, Diff: &diff, Added: 2, Removed: 1}},
		}},
		StartTurn: 2, EndTurn: 3, TotalTurns: 3, HasOlder: true,
	}
	rt.snapshot.Checkpoints = []runtimeapi.Checkpoint{{
		ID: "checkpoint_opaque", DisplayTurn: 7, Prompt: &prompt, Files: []string{"README.md"}, FileCount: 2,
		FilesTruncated: true, CreatedAtMillis: 1234, CanCode: true, CanConversation: true,
	}}
	rt.snapshot.PendingPrompt = &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptApproval, Approval: &runtimeapi.ApprovalPrompt{
		ID: "prompt_approval", Tool: "bash", Subject: "run tests", AllowedDecisions: []runtimeapi.PromptDecision{runtimeapi.DecisionAllowSession},
	}}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote topic"})
	if err != nil {
		t.Fatal(err)
	}
	tabID := status.TabID

	history := app.HistoryPageForTab(tabID, 0, 60)
	if history.StartTurn != 2 || history.EndTurn != 3 || history.TotalTurns != 3 || !history.HasOlder || len(history.Messages) != 1 {
		t.Fatalf("Remote history page = %#v", history)
	}
	mapped := history.Messages[0]
	if mapped.Content != content || mapped.Detail != detail || mapped.Reasoning != reasoning || mapped.CheckpointTurn == nil || *mapped.CheckpointTurn != 7 || len(mapped.ToolCalls) != 1 || mapped.ToolCalls[0].Arguments != arguments || mapped.ToolCalls[0].Diff != diff {
		t.Fatalf("Remote history mapping = %#v", mapped)
	}
	checkpoints := app.CheckpointsForTab(tabID)
	if len(checkpoints) != 1 || checkpoints[0].Turn != 7 || checkpoints[0].Prompt != prompt || checkpoints[0].FileCount != 2 || !checkpoints[0].FilesTruncated || !checkpoints[0].CanCode {
		t.Fatalf("Remote checkpoints = %#v", checkpoints)
	}
	meta := app.MetaForTab(tabID)
	if meta.Label != remoteProfileModelLabel(rt.snapshot.Profile.Model) || !meta.Ready || meta.Goal != goal || meta.GoalStatus != string(runtimeapi.GoalRunning) || meta.WorkspaceRoot != string(rt.workspace.ID) {
		t.Fatalf("Remote Meta = %#v", meta)
	}

	emitted := make(chan wireEventTab, 4)
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		if name == eventChannel && len(payload) == 1 {
			if value, ok := payload[0].(wireEventTab); ok {
				emitted <- value
			}
		}
	})
	app.ReplayPendingPrompts()
	replayed := waitValue(t, emitted, "Remote pending approval replay")
	if replayed.TabID != tabID || replayed.Kind != "approval_request" || replayed.Approval == nil || replayed.Approval.ID != "prompt_approval" {
		t.Fatalf("Remote replay event = %#v", replayed)
	}

	if err := app.SubmitToTab(tabID, "hello Remote"); err != nil {
		t.Fatal(err)
	}
	if err := app.SteerForTab(tabID, "focus on tests"); err != nil {
		t.Fatal(err)
	}
	app.CancelTab(tabID)
	app.ApproveTab(tabID, "prompt_approval", true, true, false)

	questionPrompt := "Pick one"
	app.remote.workbenchMu.Lock()
	app.remote.workbench.Sessions[rt.created.Session].Snapshot.PendingPrompt = &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptAsk, Ask: &runtimeapi.AskPrompt{
		ID: "prompt_ask", Questions: []runtimeapi.AskQuestion{{ID: "question_opaque", Prompt: &questionPrompt}},
	}}
	app.remote.workbenchMu.Unlock()
	app.AnswerQuestionForTab(tabID, "prompt_ask", []QuestionAnswer{{QuestionID: "question_opaque", Selected: []string{"yes"}}})

	rt.mu.Lock()
	if len(rt.submitInputs) != 1 || rt.submitInputs[0].Session != rt.created.Session || rt.submitInputs[0].Input != "hello Remote" {
		t.Fatalf("Remote submit calls = %#v", rt.submitInputs)
	}
	if len(rt.steerInputs) != 1 || rt.steerInputs[0].TurnID != "turn_opaque" || rt.steerInputs[0].Text != "focus on tests" {
		t.Fatalf("Remote steer calls = %#v", rt.steerInputs)
	}
	if len(rt.cancelInputs) != 1 || rt.cancelInputs[0].TurnID != "turn_opaque" {
		t.Fatalf("Remote cancel calls = %#v", rt.cancelInputs)
	}
	if len(rt.approveInputs) != 1 || rt.approveInputs[0].PromptID != "prompt_approval" || rt.approveInputs[0].Decision != runtimeapi.DecisionAllowSession {
		t.Fatalf("Remote approval calls = %#v", rt.approveInputs)
	}
	if len(rt.answerInputs) != 1 || rt.answerInputs[0].PromptID != "prompt_ask" || len(rt.answerInputs[0].Answers) != 1 || rt.answerInputs[0].Answers[0].QuestionID != "question_opaque" || len(rt.answerInputs[0].Answers[0].Selected) != 1 || rt.answerInputs[0].Answers[0].Selected[0] != "yes" {
		t.Fatalf("Remote answer calls = %#v", rt.answerInputs)
	}
	rt.mu.Unlock()

	if err := app.CloseTab(tabID); err == nil || !containsError(err, "cannot close the last tab") {
		t.Fatalf("CloseTab(last) error = %v", err)
	}
	rt.mu.Lock()
	if len(rt.unsubscribed) != 0 {
		t.Fatalf("last Remote tab unexpectedly unsubscribed = %#v", rt.unsubscribed)
	}
	rt.mu.Unlock()
	if tabs := app.ListTabs(); len(tabs) != 1 {
		t.Fatalf("Remote last tab was removed = %#v", tabs)
	}
}

func TestRemoteWorkbenchCancelTargetsCurrentOperationIdentity(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_operation", Label: "Host operation"}
	rt := newRemoteWorkbenchTestRuntime()
	rt.snapshot.Runtime = runtimeapi.RuntimeState{
		Running: true,
		CurrentOperation: &runtimeapi.OperationState{
			ID: "operation_opaque", Kind: string(runtimeapi.OperationShell),
		},
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote operation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.remoteCancel(status.TabID); err != nil {
		t.Fatal(err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.cancelInputs) != 0 {
		t.Fatalf("Operation cancel was sent through Turn API: %#v", rt.cancelInputs)
	}
	if len(rt.operationCancels) != 1 || rt.operationCancels[0].Session != rt.created.Session || rt.operationCancels[0].OperationID != "operation_opaque" {
		t.Fatalf("Remote Operation cancel calls = %#v", rt.operationCancels)
	}
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	state := app.remote.workbench.Sessions[rt.created.Session]
	operation := state.Snapshot.Runtime.CurrentOperation
	if operation == nil || !operation.CancelRequested || !state.Snapshot.Runtime.CancelRequested {
		t.Fatalf("Remote Operation cancel state = %+v", state.Snapshot.Runtime)
	}
}

func TestRemoteWorkbenchImmediateShellThenCancelUsesReturnedOperationIdentity(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_shell_cancel", Label: "Host shell cancel"}
	rt := newRemoteWorkbenchTestRuntime()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote shell",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RunShellForTab(status.TabID, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	if err := app.remoteCancel(status.TabID); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.shellInputs) != 1 || rt.shellInputs[0].Session != rt.created.Session || rt.shellInputs[0].Command != "go test ./..." {
		t.Fatalf("Remote shell inputs = %#v", rt.shellInputs)
	}
	if len(rt.operationCancels) != 1 || rt.operationCancels[0].OperationID != "operation_shell" {
		t.Fatalf("Remote shell cancel = %#v", rt.operationCancels)
	}
}

func TestRemoteWorkbenchProjectsResolvedProfileOntoLegacyModeAxes(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_profile_axes", Label: "Host profile axes"}
	rt := newRemoteWorkbenchTestRuntime()
	rt.created.ResolvedProfile.CollaborationMode = "plan"
	rt.created.ResolvedProfile.ToolApprovalMode = "yolo"
	rt.snapshot.Profile = rt.created.ResolvedProfile
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	tab, ok := app.remoteTabMeta(status.TabID)
	if !ok || tab.Mode != "plan-yolo" || tab.Mode == rt.snapshot.Profile.Model {
		t.Fatalf("Remote tab profile axes = %#v", tab)
	}
	meta, ok := app.remoteMeta(status.TabID)
	if !ok || !meta.AutoApproveTools || !meta.Bypass || meta.CollaborationMode != "plan" || meta.ToolApprovalMode != "yolo" {
		t.Fatalf("Remote Meta profile axes = %#v", meta)
	}
}

func TestRemoteWorkbenchMetaProjectsResolvedModelForDesktopSelector(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_model_label", Label: "Host model label"}
	rt := newRemoteWorkbenchTestRuntime()
	rt.created.TopicTitle = "Topic title must not become a model"
	rt.created.ResolvedProfile.Model = "openrouter/anthropic/claude-sonnet-4"
	rt.snapshot.Title = rt.created.TopicTitle
	rt.snapshot.Profile = rt.created.ResolvedProfile
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: rt.created.TopicTitle,
	})
	if err != nil {
		t.Fatal(err)
	}

	meta := app.MetaForTab(status.TabID)
	if meta.Label != "anthropic/claude-sonnet-4" {
		t.Fatalf("Remote model selector label = %q, want catalog model name", meta.Label)
	}
	if meta.Label == rt.created.TopicTitle {
		t.Fatalf("Remote model selector label leaked topic title %q", meta.Label)
	}
	tab, ok := app.remoteTabMeta(status.TabID)
	if !ok || tab.Label != meta.Label {
		t.Fatalf("Remote optimistic tab model label = %q, ok=%v; want %q", tab.Label, ok, meta.Label)
	}
}

func TestRemoteWorkbenchProfileProjectionDistinguishesInPlaceAndRebuilt(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_profile_projection", Label: "Host profile projection"}
	rt := newRemoteWorkbenchTestRuntime()
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote profile projection",
	})
	if err != nil {
		t.Fatal(err)
	}
	app.remote.workbenchMu.Lock()
	app.remote.workbench.Sessions[rt.created.Session].Snapshot.PendingPrompt = &runtimeapi.PendingPrompt{
		Kind:     runtimeapi.PromptApproval,
		Approval: &runtimeapi.ApprovalPrompt{ID: "prompt_auto", Tool: "bash", Subject: "test"},
	}
	app.remote.workbenchMu.Unlock()

	inPlace := runtimeapi.ResolvedProfile{
		Model: "remote-updated", Effort: "high", CollaborationMode: "plan", TokenMode: "full", ToolApprovalMode: "yolo",
	}
	rt.mu.Lock()
	rt.profileResult = runtimeapi.SetProfileResult{
		ResolvedProfile: inPlace, Disposition: runtimeapi.ProfileUpdated,
		AutoResolvedPromptIDs: []runtimeapi.PromptID{"prompt_auto"}, SnapshotRequired: false,
	}
	attachBefore := len(rt.attachInputs)
	rt.mu.Unlock()
	mode := "plan"
	result, err := app.remoteSetProfileV1(status.TabID, runtimeapi.ProfilePatch{CollaborationMode: &mode})
	if err != nil || result.ResolvedProfile != inPlace {
		t.Fatalf("in-place profile result = %#v, %v", result, err)
	}
	app.remote.workbenchMu.RLock()
	projected := app.remote.workbench.Sessions[rt.created.Session]
	if projected == nil || projected.Snapshot.Profile != inPlace || projected.Created.ResolvedProfile != inPlace || projected.Snapshot.PendingPrompt != nil {
		t.Fatalf("in-place profile projection = %#v", projected)
	}
	app.remote.workbenchMu.RUnlock()
	if meta, ok := app.remoteMeta(status.TabID); !ok || meta.Label != "remote-updated" {
		t.Fatalf("Remote Meta model after profile update = %#v, ok=%v", meta, ok)
	}
	rt.mu.Lock()
	if len(rt.attachInputs) != attachBefore {
		t.Fatalf("in-place profile unexpectedly resubscribed: %#v", rt.attachInputs)
	}
	rt.profileResult = runtimeapi.SetProfileResult{
		ResolvedProfile: runtimeapi.ResolvedProfile{Model: "rebuilt-model"},
		Disposition:     runtimeapi.ProfileRebuilt, SnapshotRequired: true,
	}
	rt.mu.Unlock()
	if _, err := app.remoteSetProfileV1(status.TabID, runtimeapi.ProfilePatch{}); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.attachInputs) != attachBefore {
		t.Fatalf("rebuilt profile refreshed the old target: %#v", rt.attachInputs)
	}
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	if got := app.remote.workbench.Sessions[rt.created.Session].Snapshot.Profile.Model; got != inPlace.Model {
		t.Fatalf("rebuilt profile mutated old snapshot model = %q", got)
	}
}

func TestRemoteWorkbenchAtomicallyCommitsReplacementSnapshot(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_snapshot", Label: "Host snapshot"}
	rt := newRemoteWorkbenchTestRuntime()
	app, manager := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote source",
	})
	if err != nil {
		t.Fatal(err)
	}
	source := rt.created.Session
	replacement := runtimeapi.SessionRef{WorkspaceID: source.WorkspaceID, SessionID: "session_replacement"}
	replacementSnapshot := cloneRemoteSessionSnapshot(rt.snapshot)
	replacementSnapshot.Session = replacement
	replacementSnapshot.TopicID = "topic_replacement"
	replacementSnapshot.Title = "Remote replacement"
	replacementSnapshot.Runtime.CurrentTurn = nil
	replacementSnapshot.Runtime.Running = false

	emitted := make(chan wireEventTab, 1)
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		if name == eventChannel && len(payload) == 1 {
			if value, ok := payload[0].(wireEventTab); ok {
				emitted <- value
			}
		}
	})
	app.emitRemoteRuntimeEvent(TargetRuntimeEvent{
		Generation: manager.Snapshot().Generation,
		Target:     target,
		Event: runtimeapi.Event{
			Session: replacement,
			Snapshot: &runtimeapi.SnapshotUpdate{
				Previous: source,
				Snapshot: replacementSnapshot,
			},
		},
	})
	select {
	case ordinary := <-emitted:
		t.Fatalf("replacement snapshot leaked as an ordinary frontend event: %#v", ordinary)
	default:
	}
	updated := app.RemoteWorkbenchStatus()
	if !updated.SessionAttached || updated.TabID == status.TabID || updated.TabID != remoteSessionTabID(replacement) || updated.TopicTitle != replacementSnapshot.Title {
		t.Fatalf("replacement workbench status = %#v, source=%#v", updated, status)
	}
	app.remote.workbenchMu.RLock()
	current := app.remote.workbench.Sessions[replacement]
	app.remote.workbenchMu.RUnlock()
	if current == nil || current.Created.Session != replacement || current.Created.TopicID != replacementSnapshot.TopicID ||
		current.Snapshot.Session != replacement || current.Snapshot.Title != replacementSnapshot.Title {
		t.Fatalf("replacement workbench binding = %#v", current)
	}
}

func TestRemoteWorkbenchRefreshesInPlaceComposerRewriteWithoutEpochReplacementEvent(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_rewind", Label: "Host rewind"}
	rt := newRemoteWorkbenchTestRuntime()
	rt.submit = runtimeapi.ComposerSubmitResult{
		Kind: runtimeapi.SubmitCompleted, Effect: string(runtimeapi.EffectStateChanged),
		Session: rt.created.Session, SnapshotRequired: true,
	}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote source",
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.snapshot.Title = "Remote rewound"
	rt.snapshot.History.TotalTurns = 1
	rt.snapshot.History.ActualTurns = 1
	rt.mu.Unlock()
	if err := app.SubmitToTab(status.TabID, "/rewind"); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	attachInputs := append([]runtimeapi.AttachAndSubscribeInput(nil), rt.attachInputs...)
	rt.mu.Unlock()
	if len(attachInputs) != 2 || attachInputs[1].Session != rt.created.Session || attachInputs[1].HistoryTurns != runtimeapi.DefaultAttachHistoryTurns {
		t.Fatalf("rewind snapshot refresh inputs = %#v", attachInputs)
	}
	app.remote.workbenchMu.RLock()
	current := app.remote.workbench.Sessions[rt.created.Session]
	app.remote.workbenchMu.RUnlock()
	if current == nil || current.Snapshot.Title != "Remote rewound" || current.Snapshot.History.TotalTurns != 1 {
		t.Fatalf("rewind workbench snapshot = %#v", current)
	}
}

func TestRemoteWorkbenchRejectsAttachAndEventABA(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_a", Label: "Host A"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_b", Label: "Host B"}
	rtA := newRemoteWorkbenchTestRuntime()
	rtB := newRemoteWorkbenchTestRuntime()
	rtB.created.Session = runtimeapi.SessionRef{WorkspaceID: "workspace_b", SessionID: "session_b"}
	rtB.snapshot.Session = rtB.created.Session
	adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: rtB}
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if sameTarget(target, targetB) {
			return adapterB, nil
		}
		return nil, errors.New("unexpected target switch")
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, rtA, connector)
	rtA.attachHook = func() {
		if err := manager.Switch(context.Background(), targetB, SwitchTargetOptions{}); err != nil {
			t.Errorf("switch during attach: %v", err)
		}
	}
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "dir_primary"}); !errors.Is(err, ErrTargetTransitionSuperseded) {
		t.Fatalf("create after target ABA error = %v, want ErrTargetTransitionSuperseded", err)
	}
	if status := app.RemoteWorkbenchStatus(); status.HostID != targetB.ID || status.SessionAttached || status.TabID != "" {
		t.Fatalf("workbench published stale Host A Session after ABA: %#v", status)
	}

	// Install a Host B binding and prove only the exact target generation and
	// Session identity may mutate or emit it.
	current := manager.Snapshot()
	app.remote.workbenchMu.Lock()
	app.remote.workbench = remoteWorkbenchState{
		HostID:     targetB.ID,
		Workspaces: map[runtimeapi.WorkspaceID]runtimeapi.Workspace{rtB.workspace.ID: rtB.workspace},
		Sessions: map[runtimeapi.SessionRef]*remoteWorkbenchSession{
			rtB.created.Session: {Created: rtB.created, Snapshot: cloneRemoteSessionSnapshot(rtB.snapshot), AttachedGeneration: current.Generation},
		},
		SessionTabs: map[string]runtimeapi.SessionRef{remoteSessionTabID(rtB.created.Session): rtB.created.Session},
		TabOrder:    []string{remoteSessionTabID(rtB.created.Session)}, ActiveTabID: remoteSessionTabID(rtB.created.Session),
	}
	app.remote.workbenchMu.Unlock()
	emitted := make(chan wireEventTab, 4)
	setRemoteWorkbenchTestEmitter(app, func(_ context.Context, name string, payload ...interface{}) {
		if name == eventChannel && len(payload) == 1 {
			if value, ok := payload[0].(wireEventTab); ok {
				emitted <- value
			}
		}
	})
	valid := TargetRuntimeEvent{Generation: current.Generation, Target: targetB, Event: runtimeapi.Event{
		Session: rtB.created.Session, TurnID: "turn_b", Value: eventwire.Event{Kind: "turn_started"},
	}}
	wrongGeneration := valid
	wrongGeneration.Generation++
	wrongHost := valid
	wrongHost.Target = targetA
	wrongSession := valid
	wrongSession.Event.Session.SessionID = "session_stale"
	app.emitRemoteRuntimeEvent(wrongGeneration)
	app.emitRemoteRuntimeEvent(wrongHost)
	app.emitRemoteRuntimeEvent(wrongSession)
	select {
	case event := <-emitted:
		t.Fatalf("stale Remote event was emitted: %#v", event)
	default:
	}
	app.emitRemoteRuntimeEvent(valid)
	delivered := waitValue(t, emitted, "generation-matched Remote event")
	if delivered.TabID != remoteSessionTabID(rtB.created.Session) || delivered.Kind != "turn_started" {
		t.Fatalf("valid Remote event = %#v", delivered)
	}
	app.remote.workbenchMu.RLock()
	running := app.remote.workbench.Sessions[rtB.created.Session].Snapshot.Runtime.Running
	turn := app.remote.workbench.Sessions[rtB.created.Session].Snapshot.Runtime.CurrentTurn
	app.remote.workbenchMu.RUnlock()
	if !running || turn == nil || turn.ID != "turn_b" {
		t.Fatalf("Remote runtime projection running=%v turn=%#v", running, turn)
	}
}

func TestRemoteWorkspaceCreatePendingSurvivesHostABA(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_pending_old", Label: "Old Host"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_pending_new", Label: "New Host"}
	runtimeA := newRemoteWorkbenchTestRuntime()
	runtimeB := newRemoteWorkbenchTestRuntime()
	adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
	adapterA := &remoteWorkbenchTestAdapter{target: targetA, runtime: runtimeA}
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		switch {
		case sameTarget(target, targetB):
			return adapterB, nil
		case sameTarget(target, targetA):
			return adapterA, nil
		default:
			return nil, errors.New("unexpected target switch")
		}
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)
	var switchErr error
	var switchOnce sync.Once
	runtimeA.createHook = func() {
		switchOnce.Do(func() {
			switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{})
		})
	}
	input := RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Must stay on Host A",
	}

	if _, err := app.CreateRemoteWorkspaceSession(input); !errors.Is(err, ErrTargetTransitionSuperseded) {
		t.Fatalf("workspace create after newer Host StateSink = %v, want ErrTargetTransitionSuperseded", err)
	}
	if switchErr != nil {
		t.Fatal(switchErr)
	}
	current := manager.Snapshot()
	if !sameTarget(current.Target, targetB) || current.State != TargetRemoteConnected {
		t.Fatalf("current target = %#v, want connected Host B", current)
	}
	app.remote.workbenchMu.RLock()
	hostID := app.remote.workbench.HostID
	pending := len(app.remote.workbench.Pending)
	sessions := len(app.remote.workbench.Sessions)
	app.remote.workbenchMu.RUnlock()
	if hostID == targetA.ID || pending != 0 || sessions != 0 {
		t.Fatalf("stale Host A state overwrote Host B: host=%q pending=%d sessions=%d", hostID, pending, sessions)
	}
	app.remote.workbenchMu.RLock()
	retained := clonePendingRemoteCreate(app.remote.workspacePending[targetA.ID][remoteSessionCreateFingerprint("dir_primary", nil, "Must stay on Host A")])
	app.remote.workbenchMu.RUnlock()
	if retained == nil || retained.HostID != targetA.ID || retained.Created.Session != runtimeA.created.Session {
		t.Fatalf("Host A pending create was not retained across switch to B: %#v", retained)
	}

	if err := manager.Switch(context.Background(), targetA, SwitchTargetOptions{}); err != nil {
		t.Fatal(err)
	}
	status, err := app.CreateRemoteWorkspaceSession(input)
	if err != nil {
		t.Fatalf("retry after returning to Host A: %v", err)
	}
	if !status.SessionAttached || status.HostID != targetA.ID || status.TabID != remoteSessionTabID(runtimeA.created.Session) {
		t.Fatalf("Host A retry status = %#v", status)
	}
	runtimeA.mu.Lock()
	createCalls := len(runtimeA.createInputs)
	openCalls := len(runtimeA.openInputs)
	attachCalls := len(runtimeA.attachInputs)
	runtimeA.mu.Unlock()
	if createCalls != 1 || openCalls != 1 || attachCalls != 1 {
		t.Fatalf("Host A open/create/attach calls after retry = %d/%d/%d, want 1/1/1", openCalls, createCalls, attachCalls)
	}
	app.remote.workbenchMu.RLock()
	remaining := len(app.remote.workspacePending[targetA.ID])
	app.remote.workbenchMu.RUnlock()
	if remaining != 0 {
		t.Fatalf("successful Host A retry retained %d pending creates", remaining)
	}
}

func TestRemoteWorkbenchCommitEventsPrecedeNewerStateSink(t *testing.T) {
	targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_event_old", Label: "Old event Host"}
	targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_event_new", Label: "New event Host"}
	runtimeA := newRemoteWorkbenchTestRuntime()
	runtimeB := newRemoteWorkbenchTestRuntime()
	adapterB := &remoteWorkbenchTestAdapter{target: targetB, runtime: runtimeB}
	connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		if sameTarget(target, targetB) {
			return adapterB, nil
		}
		return nil, errors.New("unexpected target switch")
	})
	app, manager := newRemoteWorkbenchTestApp(t, targetA, runtimeA, connector)

	type observedEvent struct {
		name     string
		hostID   string
		attached bool
		tabID    string
	}
	var eventsMu sync.Mutex
	var events []observedEvent
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
		if name == "test:workbench-event-barrier" {
			close(barrier)
		}
	})

	// This hook sits exactly between workbench-state and rebuilt in the old
	// publication path. The fixed path invokes it only after the complete event
	// batch has been enqueued and stateDispatchMu has been released.
	var switched atomic.Bool
	var switchErr error
	app.projectTreeChangedHook = func() {
		if switched.CompareAndSwap(false, true) {
			switchErr = manager.Switch(context.Background(), targetB, SwitchTargetOptions{})
		}
	}
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary", TopicTitle: "Old Host result",
	})
	if err != nil {
		t.Fatal(err)
	}
	if switchErr != nil {
		t.Fatal(switchErr)
	}
	if !switched.Load() {
		t.Fatal("adversarial target switch was not triggered")
	}
	app.emitRuntimeEvent("test:workbench-event-barrier")
	waitSignal(t, barrier, "workbench event queue barrier")

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
		return event.name == remoteWorkbenchStateEvent && event.hostID == targetA.ID && event.attached && event.tabID == status.TabID
	})
	aRebuilt := index(func(event observedEvent) bool { return event.name == "runtime:rebuilt" && event.tabID == status.TabID })
	aReady := index(func(event observedEvent) bool { return event.name == "agent:ready" && event.tabID == status.TabID })
	bState := index(func(event observedEvent) bool {
		return event.name == remoteWorkbenchStateEvent && event.hostID == targetB.ID && !event.attached
	})
	if aState < 0 || aRebuilt < 0 || aReady < 0 || bState < 0 {
		t.Fatalf("missing ordered workbench events: %#v", got)
	}
	if !(aState < aRebuilt && aRebuilt < aReady && aReady < bState) {
		t.Fatalf("old Host events crossed newer StateSink: state=%d rebuilt=%d ready=%d newState=%d events=%#v", aState, aRebuilt, aReady, bState, got)
	}
	for i := bState + 1; i < len(got); i++ {
		if got[i].tabID == status.TabID && (got[i].name == remoteWorkbenchStateEvent || got[i].name == "runtime:rebuilt" || got[i].name == "agent:ready") {
			t.Fatalf("old Host event published after Host B StateSink at index %d: %#v", i, got)
		}
	}
}

func TestRemoteWorkbenchDoesNotRouteWhenTargetIsLocal(t *testing.T) {
	rt := newRemoteWorkbenchTestRuntime()
	local := TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"}
	app, manager := newRemoteWorkbenchTestApp(t, local, rt, nil)
	current := manager.Snapshot()
	app.remote.workbenchMu.Lock()
	app.remote.workbench = remoteWorkbenchState{
		HostID:     "stale_remote_host",
		Workspaces: map[runtimeapi.WorkspaceID]runtimeapi.Workspace{rt.workspace.ID: rt.workspace},
		Sessions: map[runtimeapi.SessionRef]*remoteWorkbenchSession{
			rt.created.Session: {Created: rt.created, Snapshot: cloneRemoteSessionSnapshot(rt.snapshot), AttachedGeneration: current.Generation},
		},
		SessionTabs: map[string]runtimeapi.SessionRef{remoteSessionTabID(rt.created.Session): rt.created.Session},
		TabOrder:    []string{remoteSessionTabID(rt.created.Session)}, ActiveTabID: remoteSessionTabID(rt.created.Session),
	}
	app.remote.workbenchMu.Unlock()

	if app.remoteTargetSelected() {
		t.Fatal("Local target was classified as Remote")
	}
	if tabs := app.ListTabs(); len(tabs) != 0 {
		t.Fatalf("Local ListTabs exposed stale Remote Session: %#v", tabs)
	}
	if history := app.HistoryForTab(remoteSessionTabID(rt.created.Session)); len(history) != 0 {
		t.Fatalf("Local History routed to stale Remote Session: %#v", history)
	}
	if meta := app.MetaForTab(remoteSessionTabID(rt.created.Session)); meta.WorkspacePath == rt.workspace.DisplayPath {
		t.Fatalf("Local Meta routed to stale Remote Session: %#v", meta)
	}
	if err := app.SubmitToTab(remoteSessionTabID(rt.created.Session), "must stay Local"); err == nil {
		t.Fatal("Local submission without a Local tab unexpectedly succeeded")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.submitInputs) != 0 || len(rt.attachInputs) != 0 || len(rt.openInputs) != 0 || len(rt.createInputs) != 0 {
		t.Fatalf("Local target invoked stale Remote RuntimeAPI: submit=%#v attach=%#v open=%#v create=%#v", rt.submitInputs, rt.attachInputs, rt.openInputs, rt.createInputs)
	}
}

func TestPickWorkspaceDoesNotOpenLocalDialogForRemoteTarget(t *testing.T) {
	rt := newRemoteWorkbenchTestRuntime()
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_remote_picker", Label: "Remote picker"}
	app, _ := newRemoteWorkbenchTestApp(t, target, rt, nil)

	path, err := app.PickWorkspace()
	if path != "" || !containsError(err, "Host workspace setup") {
		t.Fatalf("PickWorkspace() = %q, %v; want Remote setup rejection", path, err)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.browseInputs) != 0 || len(rt.openInputs) != 0 || len(rt.createInputs) != 0 {
		t.Fatalf("PickWorkspace unexpectedly mutated Remote runtime: browse=%#v open=%#v create=%#v", rt.browseInputs, rt.openInputs, rt.createInputs)
	}
}

func containsError(err error, fragment string) bool {
	return err != nil && strings.Contains(err.Error(), fragment)
}
