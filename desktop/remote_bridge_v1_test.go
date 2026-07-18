package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"reasonix/internal/eventwire"
	"reasonix/internal/runtimeapi"
)

// remoteBridgeRecordingV1 embeds the complete contract only to keep focused
// bridge tests small. Every method exercised by a test is overridden below;
// an accidental new call therefore panics instead of fabricating success.
type remoteBridgeRecordingV1 struct {
	runtimeapi.V1RuntimeAPI
	base *remoteWorkbenchTestRuntime

	mu              sync.Mutex
	composerInputs  []runtimeapi.ComposerSubmitInput
	fileInputs      []runtimeapi.FileListInput
	filePages       map[runtimeapi.Cursor]runtimeapi.FileListResult
	jobInputs       []runtimeapi.ListJobsInput
	jobPages        map[runtimeapi.Cursor]runtimeapi.JobPage
	researchInputs  []runtimeapi.ListResearchInput
	researchPages   map[runtimeapi.Cursor]runtimeapi.ResearchPage
	findingInputs   []runtimeapi.ResearchFindingsInput
	findingPages    map[runtimeapi.Cursor]runtimeapi.ResearchFindingsPage
	researchStatus  runtimeapi.ResearchStatusView
	changeInputs    []runtimeapi.WorkspaceChangesInput
	changePages     map[runtimeapi.Cursor]runtimeapi.WorkspaceChangesPage
	gitInputs       []runtimeapi.GitCommitDetailInput
	gitPages        map[runtimeapi.Cursor]runtimeapi.GitCommitDetail
	workspaceInputs []runtimeapi.ListWorkspacesInput
	workspacePages  map[runtimeapi.Cursor]runtimeapi.WorkspaceListPage
	sessionCatalog  runtimeapi.SessionCatalog
	catalogCalls    int
}

func newRemoteBridgeRecordingV1() *remoteBridgeRecordingV1 {
	return &remoteBridgeRecordingV1{
		base:           newRemoteWorkbenchTestRuntime(),
		filePages:      make(map[runtimeapi.Cursor]runtimeapi.FileListResult),
		jobPages:       make(map[runtimeapi.Cursor]runtimeapi.JobPage),
		researchPages:  make(map[runtimeapi.Cursor]runtimeapi.ResearchPage),
		findingPages:   make(map[runtimeapi.Cursor]runtimeapi.ResearchFindingsPage),
		changePages:    make(map[runtimeapi.Cursor]runtimeapi.WorkspaceChangesPage),
		gitPages:       make(map[runtimeapi.Cursor]runtimeapi.GitCommitDetail),
		workspacePages: make(map[runtimeapi.Cursor]runtimeapi.WorkspaceListPage),
	}
}

func (r *remoteBridgeRecordingV1) Connection(ctx context.Context) (runtimeapi.ConnectionView, error) {
	return r.base.Connection(ctx)
}
func (r *remoteBridgeRecordingV1) BrowseWorkspace(ctx context.Context, input runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	return r.base.BrowseWorkspace(ctx, input)
}
func (r *remoteBridgeRecordingV1) OpenWorkspace(ctx context.Context, input runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	return r.base.OpenWorkspace(ctx, input)
}
func (r *remoteBridgeRecordingV1) CreateSession(ctx context.Context, input runtimeapi.CreateSessionInput) (runtimeapi.CreatedSession, error) {
	return r.base.CreateSession(ctx, input)
}
func (r *remoteBridgeRecordingV1) AttachAndSubscribe(ctx context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	return r.base.AttachAndSubscribe(ctx, input)
}
func (r *remoteBridgeRecordingV1) ComposerSubmit(_ context.Context, input runtimeapi.ComposerSubmitInput) (runtimeapi.ComposerSubmitResult, error) {
	r.mu.Lock()
	r.composerInputs = append(r.composerInputs, input)
	r.mu.Unlock()
	return runtimeapi.ComposerSubmitResult{Kind: runtimeapi.SubmitTurn, TurnID: "turn_recorded", Session: input.Session}, nil
}
func (r *remoteBridgeRecordingV1) SteerTurn(ctx context.Context, input runtimeapi.SteerInput) error {
	return r.base.SteerTurn(ctx, input)
}
func (r *remoteBridgeRecordingV1) CancelTurn(ctx context.Context, input runtimeapi.CancelTurnInput) error {
	return r.base.CancelTurn(ctx, input)
}
func (r *remoteBridgeRecordingV1) ApprovePrompt(ctx context.Context, input runtimeapi.ApproveInput) error {
	return r.base.ApprovePrompt(ctx, input)
}
func (r *remoteBridgeRecordingV1) AnswerPrompt(ctx context.Context, input runtimeapi.AnswerInput) error {
	return r.base.AnswerPrompt(ctx, input)
}
func (r *remoteBridgeRecordingV1) Events() <-chan runtimeapi.Event { return r.base.Events() }
func (r *remoteBridgeRecordingV1) UnsubscribeSession(ctx context.Context, input runtimeapi.UnsubscribeSessionInput) error {
	return r.base.UnsubscribeSession(ctx, input)
}
func (r *remoteBridgeRecordingV1) ListFiles(_ context.Context, input runtimeapi.FileListInput) (runtimeapi.FileListResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fileInputs = append(r.fileInputs, input)
	page, ok := r.filePages[input.Cursor]
	if !ok {
		return runtimeapi.FileListResult{}, errors.New("unexpected file cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) ListJobs(_ context.Context, input runtimeapi.ListJobsInput) (runtimeapi.JobPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobInputs = append(r.jobInputs, input)
	page, ok := r.jobPages[input.Cursor]
	if !ok {
		return runtimeapi.JobPage{}, errors.New("unexpected job cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) ResearchStatus(context.Context, runtimeapi.ResearchInput) (runtimeapi.ResearchStatusView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.researchStatus, nil
}

func (r *remoteBridgeRecordingV1) ListResearch(_ context.Context, input runtimeapi.ListResearchInput) (runtimeapi.ResearchPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.researchInputs = append(r.researchInputs, input)
	page, ok := r.researchPages[input.Cursor]
	if !ok {
		return runtimeapi.ResearchPage{}, errors.New("unexpected research cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) ResearchFindings(_ context.Context, input runtimeapi.ResearchFindingsInput) (runtimeapi.ResearchFindingsPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findingInputs = append(r.findingInputs, input)
	page, ok := r.findingPages[input.Cursor]
	if !ok {
		return runtimeapi.ResearchFindingsPage{}, errors.New("unexpected findings cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) WorkspaceChanges(_ context.Context, input runtimeapi.WorkspaceChangesInput) (runtimeapi.WorkspaceChangesPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changeInputs = append(r.changeInputs, input)
	page, ok := r.changePages[input.Cursor]
	if !ok {
		return runtimeapi.WorkspaceChangesPage{}, errors.New("unexpected changes cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) GitCommitDetail(_ context.Context, input runtimeapi.GitCommitDetailInput) (runtimeapi.GitCommitDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gitInputs = append(r.gitInputs, input)
	page, ok := r.gitPages[input.Cursor]
	if !ok {
		return runtimeapi.GitCommitDetail{}, errors.New("unexpected git cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) ListWorkspaces(_ context.Context, input runtimeapi.ListWorkspacesInput) (runtimeapi.WorkspaceListPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceInputs = append(r.workspaceInputs, input)
	page, ok := r.workspacePages[input.Cursor]
	if !ok {
		return runtimeapi.WorkspaceListPage{}, errors.New("unexpected workspace cursor")
	}
	return page, nil
}

func (r *remoteBridgeRecordingV1) SessionCatalog(context.Context, runtimeapi.SessionCatalogInput) (runtimeapi.SessionCatalog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalogCalls++
	return r.sessionCatalog, nil
}

type remoteBridgeV1Adapter struct {
	target  TargetDescriptor
	runtime *remoteBridgeRecordingV1
}

func (a *remoteBridgeV1Adapter) Descriptor() TargetDescriptor      { return a.target }
func (a *remoteBridgeV1Adapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a.runtime }
func (a *remoteBridgeV1Adapter) CanRelease(context.Context) (ReleaseStatus, error) {
	return ReleaseStatus{}, nil
}
func (a *remoteBridgeV1Adapter) Detach(context.Context) error { return nil }
func (a *remoteBridgeV1Adapter) AbandonTarget() error         { return nil }

func newRemoteBridgeV1TestApp(t *testing.T, recording *remoteBridgeRecordingV1) (*App, RemoteWorkbenchStatusView) {
	t.Helper()
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_bridge_v1", Label: "Host bridge V1"}
	manager, err := NewTargetManager(
		TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
			return nil, errors.New("unexpected target switch")
		}),
		&remoteBridgeV1Adapter{target: target, runtime: recording},
		TargetManagerOptions{},
	)
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
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "dir_primary"})
	if err != nil {
		t.Fatal(err)
	}
	return app, status
}

func TestRemotePreviewOfOpenSessionDoesNotReplaceOrRemoveSubscription(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	app, _ := newRemoteBridgeV1TestApp(t, recording)

	beforeAttach := len(recording.base.attachInputs)
	beforeUnsubscribe := len(recording.base.unsubscribed)
	if _, err := app.PreviewSession(string(recording.base.created.Session.SessionID)); err != nil {
		t.Fatal(err)
	}
	if got := len(recording.base.attachInputs); got != beforeAttach {
		t.Fatalf("AttachAndSubscribe calls = %d, want unchanged %d", got, beforeAttach)
	}
	if got := len(recording.base.unsubscribed); got != beforeUnsubscribe {
		t.Fatalf("UnsubscribeSession calls = %d, want unchanged %d", got, beforeUnsubscribe)
	}
}

func TestRemoteToolResultUsesHydratedLiveEventsWithoutRefresh(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	app, status := newRemoteBridgeV1TestApp(t, recording)
	ref := recording.base.created.Session
	app.remote.workbenchMu.Lock()
	session := app.remote.workbench.Sessions[ref]
	session.Snapshot.Runtime.Running = true
	session.Snapshot.Runtime.LiveEvents = []eventwire.Event{
		{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "tool_opaque", Name: "bash", Args: `{"command":"pwd"}`}},
		{Kind: "tool_result", Tool: &eventwire.Tool{ID: "tool_opaque", Name: "bash", Output: "/srv/repo"}},
	}
	app.remote.workbenchMu.Unlock()

	beforeAttach := len(recording.base.attachInputs)
	result := app.ToolResultForTab(status.TabID, "tool_opaque")
	if result == nil || result.Args != `{"command":"pwd"}` || result.Output != "/srv/repo" {
		t.Fatalf("ToolResultForTab = %#v", result)
	}
	if got := len(recording.base.attachInputs); got != beforeAttach {
		t.Fatalf("live tool lookup refreshed subscription: calls = %d, want %d", got, beforeAttach)
	}
}

func TestRemoteFileListAggregatesOpaqueCursorPages(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	recording.filePages[""] = runtimeapi.FileListResult{
		Entries: []runtimeapi.FileEntry{{Name: "a.go", Path: "src/a.go"}}, HasMore: true, Next: "cursor_page_2",
	}
	recording.filePages["cursor_page_2"] = runtimeapi.FileListResult{
		Entries: []runtimeapi.FileEntry{{Name: "b.go", Path: "src/b.go"}},
	}
	app, status := newRemoteBridgeV1TestApp(t, recording)

	entries := app.ListDirForTab(status.TabID, "src")
	if len(entries) != 2 || entries[0].Path != "src/a.go" || entries[1].Path != "src/b.go" {
		t.Fatalf("ListDirForTab = %#v", entries)
	}
	recording.mu.Lock()
	inputs := append([]runtimeapi.FileListInput(nil), recording.fileInputs...)
	recording.mu.Unlock()
	if got := []runtimeapi.Cursor{inputs[0].Cursor, inputs[1].Cursor}; !reflect.DeepEqual(got, []runtimeapi.Cursor{"", "cursor_page_2"}) {
		t.Fatalf("file cursors = %v", got)
	}
	for _, input := range inputs {
		if input.Session != recording.base.created.Session || input.Path != "src" {
			t.Fatalf("file input crossed target boundary: %#v", input)
		}
	}
}

func TestRemoteLegacyPagedSurfacesExhaustAllPagesAndCatalogIsReadOnce(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	goalOne, goalTwo := "goal one", "goal two"
	more, done := true, false
	filesOne := []runtimeapi.GitCommitFile{{Path: "a.go"}}
	filesTwo := []runtimeapi.GitCommitFile{{Path: "b.go"}}
	recording.jobPages[""] = runtimeapi.JobPage{Jobs: []runtimeapi.Job{{ID: "job-1", Kind: runtimeapi.JobBash, Label: "one", Status: runtimeapi.JobRunning}}, HasMore: true, Next: "jobs-2"}
	recording.jobPages["jobs-2"] = runtimeapi.JobPage{Jobs: []runtimeapi.Job{{ID: "job-2", Kind: runtimeapi.JobTask, Label: "two", Status: runtimeapi.JobRunning}}}
	recording.researchPages[""] = runtimeapi.ResearchPage{Items: []runtimeapi.ResearchTask{{TaskID: "research-1", Goal: &goalOne, Status: "running", OpenCriteria: []runtimeapi.ResearchCriterion{}}}, HasMore: true, Next: "research-2"}
	recording.researchPages["research-2"] = runtimeapi.ResearchPage{Items: []runtimeapi.ResearchTask{{TaskID: "research-2", Goal: &goalTwo, Status: "complete", OpenCriteria: []runtimeapi.ResearchCriterion{}}}}
	recording.researchStatus = runtimeapi.ResearchStatusView{Available: true, Task: &runtimeapi.ResearchTask{TaskID: "research-live", Goal: &goalOne, Status: "running", OpenCriteria: []runtimeapi.ResearchCriterion{}}}
	recording.findingPages[""] = runtimeapi.ResearchFindingsPage{Items: []runtimeapi.ResearchFinding{{ID: "finding-1", Summary: &goalOne}}, HasMore: true, Next: "findings-2"}
	recording.findingPages["findings-2"] = runtimeapi.ResearchFindingsPage{Items: []runtimeapi.ResearchFinding{{ID: "finding-2", Summary: &goalTwo}}}
	recording.changePages[""] = runtimeapi.WorkspaceChangesPage{Files: []runtimeapi.ChangedFile{{Path: "a.go", Sources: []runtimeapi.ChangeSource{runtimeapi.ChangeGit}}}, GitAvailable: true, GitBranch: "main", HasMore: true, Next: "changes-2"}
	recording.changePages["changes-2"] = runtimeapi.WorkspaceChangesPage{Files: []runtimeapi.ChangedFile{{Path: "b.go", Sources: []runtimeapi.ChangeSource{runtimeapi.ChangeSession}}}, GitAvailable: true, GitBranch: "main"}
	recording.gitPages[""] = runtimeapi.GitCommitDetail{Kind: runtimeapi.GitDetailFiles, Files: &filesOne, HasMore: &more, Next: "git-2"}
	recording.gitPages["git-2"] = runtimeapi.GitCommitDetail{Kind: runtimeapi.GitDetailFiles, Files: &filesTwo, HasMore: &done}
	recording.workspacePages[""] = runtimeapi.WorkspaceListPage{Items: []runtimeapi.Workspace{{ID: "workspace-1", Name: "One"}}, HasMore: true, Next: "workspaces-2"}
	recording.workspacePages["workspaces-2"] = runtimeapi.WorkspaceListPage{Items: []runtimeapi.Workspace{{ID: "workspace-2", Name: "Two"}}}
	recording.sessionCatalog = runtimeapi.SessionCatalog{
		MCPServers: []runtimeapi.MCPServerCatalogItem{{Name: "mcp", Available: true, ToolCount: 2}},
		Skills:     []runtimeapi.SkillCatalogItem{{ID: "skill-1", Name: "audit", Scope: "project"}},
		Plugins:    []runtimeapi.PluginCatalogItem{{ID: "plugin-1", Name: "plugin", Enabled: true}},
	}
	app, status := newRemoteBridgeV1TestApp(t, recording)

	jobs, err := app.remoteJobsV1(status.TabID)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %#v, %v", jobs, err)
	}
	research, err := app.remoteResearchListV1(status.TabID)
	if err != nil || len(research) != 2 {
		t.Fatalf("research = %#v, %v", research, err)
	}
	findings, err := app.remoteResearchFindingsV1(status.TabID, 0)
	if err != nil || len(findings) != 2 {
		t.Fatalf("findings = %#v, %v", findings, err)
	}
	changes, err := app.remoteWorkspaceChangesV1(status.TabID)
	if err != nil || len(changes.Files) != 2 || changes.GitBranch != "main" {
		t.Fatalf("changes = %#v, %v", changes, err)
	}
	commit, err := app.remoteGitCommitDetailV1(status.TabID, "abc123", "")
	if err != nil || !reflect.DeepEqual(commit.Files, []string{"a.go", "b.go"}) {
		t.Fatalf("commit = %#v, %v", commit, err)
	}
	workspaces, err := app.remoteListWorkspacesV1()
	if err != nil || len(workspaces) != 2 {
		t.Fatalf("workspaces = %#v, %v", workspaces, err)
	}
	capabilities, err := app.remoteCapabilitiesV1(status.TabID)
	if err != nil || len(capabilities.Servers) != 1 || len(capabilities.Skills) != 1 || len(capabilities.Plugins) != 1 {
		t.Fatalf("capabilities = %#v, %v", capabilities, err)
	}

	recording.mu.Lock()
	defer recording.mu.Unlock()
	for name, got := range map[string]int{
		"jobs": len(recording.jobInputs), "research": len(recording.researchInputs),
		"findings": len(recording.findingInputs), "changes": len(recording.changeInputs),
		"git": len(recording.gitInputs), "workspaces": len(recording.workspaceInputs),
	} {
		if got != 2 {
			t.Errorf("%s page calls = %d, want 2", name, got)
		}
	}
	if recording.catalogCalls != 1 {
		t.Errorf("SessionCatalog calls = %d, want 1", recording.catalogCalls)
	}
}

func TestRemoteLegacyPagedSurfaceRejectsMalformedCursorContract(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	recording.filePages[""] = runtimeapi.FileListResult{Entries: []runtimeapi.FileEntry{}, HasMore: true}
	app, status := newRemoteBridgeV1TestApp(t, recording)
	if _, err := app.remoteListFilesV1(status.TabID, ""); err == nil || !containsError(err, "missing or non-advancing cursor") {
		t.Fatalf("malformed file cursor error = %v", err)
	}
}

func TestRemoteComposerBridgePreservesV1UnionAndOpaqueSession(t *testing.T) {
	recording := newRemoteBridgeRecordingV1()
	app, status := newRemoteBridgeV1TestApp(t, recording)

	if err := app.SubmitDeliveryRecoveryToTab(status.TabID, "display", "raw"); err != nil {
		t.Fatal(err)
	}
	if err := app.SubmitInvocationsToTab(status.TabID, "invoke", "/skill", []InvocationRequest{{Name: "audit", Kind: "skill", Offset: 99}}); err != nil {
		t.Fatal(err)
	}
	if err := app.SubmitEditedDisplayToTab(status.TabID, "edited", "new", "old"); err != nil {
		t.Fatal(err)
	}
	recording.mu.Lock()
	inputs := append([]runtimeapi.ComposerSubmitInput(nil), recording.composerInputs...)
	recording.mu.Unlock()
	if len(inputs) != 3 {
		t.Fatalf("composer calls = %d", len(inputs))
	}
	for _, input := range inputs {
		if input.Session != recording.base.created.Session {
			t.Fatalf("composer Session = %#v", input.Session)
		}
	}
	if !inputs[0].DeliveryRecovery || inputs[0].DisplayText != "display" || inputs[0].Input != "raw" {
		t.Fatalf("delivery recovery = %#v", inputs[0])
	}
	if len(inputs[1].Invocations) != 1 || inputs[1].Invocations[0].Kind != runtimeapi.InvocationSkill || inputs[1].Invocations[0].Name != "audit" {
		t.Fatalf("invocations = %#v", inputs[1])
	}
	if inputs[2].EditedOriginal != "old" || inputs[2].DisplayText != "edited" || inputs[2].Input != "new" {
		t.Fatalf("edited submit = %#v", inputs[2])
	}
}

func TestAdvanceRemoteLegacyCursorRejectsNonAdvancingAndCycles(t *testing.T) {
	seen := map[runtimeapi.Cursor]struct{}{"": {}, "seen": {}}
	for _, next := range []runtimeapi.Cursor{"", "seen"} {
		if _, _, err := advanceRemoteLegacyCursor("test/list", "", next, true, seen, 1); err == nil {
			t.Fatalf("next %q accepted", next)
		}
	}
}
