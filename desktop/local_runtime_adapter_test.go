package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/skill"
)

type localRuntimeTestController struct {
	control.SessionAPI

	mu          sync.Mutex
	status      control.RuntimeStatus
	root        string
	dir         string
	path        string
	slashSkills []skill.Skill

	snapshotErr   error
	snapshotCalls int
	closeCalls    int
	submitCalls   int
	editedCalls   int
	recoveryCalls int
	invocations   []control.InvocationRequest
	cancelCalls   int
	approveCalls  int
	answerCalls   int

	submitStarted chan struct{}
	submitRelease chan struct{}
	startOnce     sync.Once
}

func (c *localRuntimeTestController) RuntimeStatus() control.RuntimeStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *localRuntimeTestController) Running() bool { return c.RuntimeStatus().Running }

func (c *localRuntimeTestController) WorkspaceRoot() string      { return c.root }
func (c *localRuntimeTestController) SessionDir() string         { return c.dir }
func (c *localRuntimeTestController) SessionPath() string        { return c.path }
func (c *localRuntimeTestController) SetSessionPath(path string) { c.path = path }
func (c *localRuntimeTestController) Snapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshotCalls++
	return c.snapshotErr
}
func (c *localRuntimeTestController) Close() {
	c.mu.Lock()
	c.closeCalls++
	c.mu.Unlock()
}
func (c *localRuntimeTestController) SubmitDisplay(_, _ string) {
	c.mu.Lock()
	c.submitCalls++
	c.mu.Unlock()
	if c.submitStarted != nil {
		c.startOnce.Do(func() { close(c.submitStarted) })
	}
	if c.submitRelease != nil {
		<-c.submitRelease
	}
	c.mu.Lock()
	c.status.Running = true
	c.status.Cancellable = true
	c.mu.Unlock()
}
func (c *localRuntimeTestController) SubmitEditedDisplay(_, _, _ string) {
	c.mu.Lock()
	c.editedCalls++
	c.status.Running = true
	c.status.Cancellable = true
	c.mu.Unlock()
}
func (c *localRuntimeTestController) SubmitDeliveryRecovery(_, _ string) {
	c.mu.Lock()
	c.recoveryCalls++
	c.status.Running = true
	c.status.Cancellable = true
	c.mu.Unlock()
}
func (c *localRuntimeTestController) SubmitInvocationDisplay(_, _ string, values []control.InvocationRequest) {
	c.mu.Lock()
	c.invocations = append([]control.InvocationRequest(nil), values...)
	c.status.Running = true
	c.status.Cancellable = true
	c.mu.Unlock()
}
func (c *localRuntimeTestController) Commands() []command.Command { return []command.Command{} }
func (c *localRuntimeTestController) Host() *plugin.Host          { return nil }
func (c *localRuntimeTestController) SlashSkills() []skill.Skill {
	return append([]skill.Skill(nil), c.slashSkills...)
}
func (c *localRuntimeTestController) Cancel() {
	c.mu.Lock()
	c.cancelCalls++
	c.status.CancelRequested = true
	c.mu.Unlock()
}
func (c *localRuntimeTestController) Approve(string, bool, bool, bool) {
	c.mu.Lock()
	c.approveCalls++
	c.mu.Unlock()
}
func (c *localRuntimeTestController) AnswerQuestion(string, []event.AskAnswer) {
	c.mu.Lock()
	c.answerCalls++
	c.mu.Unlock()
}

func localRuntimeTestApp(t *testing.T, controllers ...*localRuntimeTestController) *App {
	t.Helper()
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.tabs = make(map[string]*WorkspaceTab, len(controllers))
	app.tabOrder = make([]string, 0, len(controllers))
	for index, ctrl := range controllers {
		id := "tab-" + string(rune('a'+index))
		tab := &WorkspaceTab{
			ID: id, Scope: "project", WorkspaceRoot: ctrl.root, TopicID: "topic-" + id,
			TopicTitle: "Topic " + id, SessionPath: ctrl.path, Ctrl: ctrl, Ready: true,
			disabledMCP: map[string]ServerView{},
		}
		tab.sink = &tabEventSink{tabID: id, app: app}
		app.tabs[id] = tab
		app.tabOrder = append(app.tabOrder, id)
	}
	if len(app.tabOrder) > 0 {
		app.activeTabID = app.tabOrder[0]
	}
	return app
}

func newLocalRuntimeTestController(t *testing.T, name string) *localRuntimeTestController {
	t.Helper()
	root := t.TempDir()
	return &localRuntimeTestController{
		root: root, dir: filepath.Join(root, ".reasonix", "sessions"),
		path: filepath.Join(root, ".reasonix", "sessions", name+".jsonl"),
	}
}

func TestLocalTargetCanReleaseChecksEveryRunningAndPendingTab(t *testing.T) {
	running := newLocalRuntimeTestController(t, "running")
	running.status = control.RuntimeStatus{Running: true}
	prompt := newLocalRuntimeTestController(t, "prompt")
	prompt.status = control.RuntimeStatus{PendingPrompt: true}
	background := newLocalRuntimeTestController(t, "background")
	background.status = control.RuntimeStatus{BackgroundJobs: 2}
	app := localRuntimeTestApp(t, running, prompt, background)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)

	status, err := adapter.CanRelease(context.Background())
	if err != nil {
		t.Fatalf("CanRelease: %v", err)
	}
	if len(status.Blockers) != 3 || status.Blockers[0].Kind != ReleaseRuntimeRunning || status.Blockers[1].Kind != ReleasePromptPending || status.Blockers[2].Kind != ReleaseRuntimeRunning {
		t.Fatalf("blockers = %+v, want running, pending prompt, and background job", status.Blockers)
	}
	if err := adapter.Detach(context.Background()); !errors.Is(err, ErrTargetBusy) {
		t.Fatalf("Detach error = %v, want ErrTargetBusy", err)
	}
	if running.closeCalls != 0 || prompt.closeCalls != 0 || background.closeCalls != 0 {
		t.Fatalf("busy Detach closed controllers: running=%d prompt=%d background=%d", running.closeCalls, prompt.closeCalls, background.closeCalls)
	}
	if err := app.cancelLocalTab("tab-a"); err != nil {
		t.Fatalf("busy Detach left Local admission closed: %v", err)
	}
}

func TestLocalTargetDetachRechecksTurnStartedAfterPreflight(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "race")
	ctrl.submitStarted = make(chan struct{})
	ctrl.submitRelease = make(chan struct{})
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	status, err := adapter.CanRelease(context.Background())
	if err != nil || status.Busy() {
		t.Fatalf("initial CanRelease = %+v, %v", status, err)
	}

	submitDone := make(chan error, 1)
	go func() { submitDone <- app.SubmitToTab("tab-a", "start after preflight") }()
	select {
	case <-ctrl.submitStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("submit did not reach controller")
	}
	detachDone := make(chan error, 1)
	go func() { detachDone <- adapter.Detach(context.Background()) }()
	select {
	case err := <-detachDone:
		t.Fatalf("Detach returned before admitted submit completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(ctrl.submitRelease)
	if err := <-submitDone; err != nil {
		t.Fatalf("SubmitToTab: %v", err)
	}
	if err := <-detachDone; !errors.Is(err, ErrTargetBusy) {
		t.Fatalf("Detach error = %v, want new running turn blocker", err)
	}
	if ctrl.closeCalls != 0 {
		t.Fatalf("controller closed despite post-preflight turn: %d", ctrl.closeCalls)
	}
}

func TestLocalTargetSuspendResumePreservesLayoutAndSessionIdentity(t *testing.T) {
	first := newLocalRuntimeTestController(t, "first")
	second := newLocalRuntimeTestController(t, "second")
	app := localRuntimeTestApp(t, first, second)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	beforeA, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab(a): %v", err)
	}
	beforeB, err := adapter.SessionRefForTab("tab-b")
	if err != nil {
		t.Fatalf("SessionRefForTab(b): %v", err)
	}
	originalOrder := append([]string(nil), app.tabOrder...)
	originalActive := app.activeTabID
	originalPaths := []string{app.tabs["tab-a"].SessionPath, app.tabs["tab-b"].SessionPath}

	if err := adapter.Detach(context.Background()); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if first.snapshotCalls != 1 || second.snapshotCalls != 1 || first.closeCalls != 1 || second.closeCalls != 1 {
		t.Fatalf("suspend calls first snapshot/close=%d/%d second=%d/%d", first.snapshotCalls, first.closeCalls, second.snapshotCalls, second.closeCalls)
	}
	for index, id := range originalOrder {
		tab := app.tabs[id]
		if tab == nil || tab.Ctrl != nil || tab.Ready || tab.SessionPath != originalPaths[index] {
			t.Fatalf("suspended tab %s = %+v, want preserved layout/path without controller", id, tab)
		}
	}
	if app.activeTabID != originalActive {
		t.Fatalf("active tab = %q, want %q", app.activeTabID, originalActive)
	}
	if err := app.SubmitToTab("tab-a", "must not reach stale Local"); !errors.Is(err, ErrLocalTargetSuspended) {
		t.Fatalf("submit while suspended = %v", err)
	}

	rebuilt := map[string]*localRuntimeTestController{}
	app.localTarget.resumeBuildHook = func(_ context.Context, app *App, tab *WorkspaceTab) error {
		ctrl := &localRuntimeTestController{root: tab.WorkspaceRoot, dir: filepath.Dir(tab.SessionPath), path: tab.SessionPath}
		rebuilt[tab.ID] = ctrl
		app.mu.Lock()
		tab.Ctrl = ctrl
		tab.Ready = true
		clearTabStartupError(tab)
		app.mu.Unlock()
		return nil
	}
	resumed, err := ResumeLocalTargetAdapter(context.Background(), app)
	if err != nil {
		t.Fatalf("ResumeLocalTargetAdapter: %v", err)
	}
	t.Cleanup(resumed.closeAdapter)
	afterA, _ := resumed.SessionRefForTab("tab-a")
	afterB, _ := resumed.SessionRefForTab("tab-b")
	if beforeA != afterA || beforeB != afterB {
		t.Fatalf("session identity changed: before=%+v/%+v after=%+v/%+v", beforeA, beforeB, afterA, afterB)
	}
	if len(rebuilt) != 2 || app.tabs["tab-a"].Ctrl == first || app.tabs["tab-b"].Ctrl == second {
		t.Fatalf("resume did not rebuild both Local controllers")
	}
	if app.activeTabID != originalActive || len(app.tabOrder) != len(originalOrder) {
		t.Fatalf("resume changed layout: active=%q order=%v", app.activeTabID, app.tabOrder)
	}
}

func TestLocalAdmissionZeroValueDoesNotRequireTargetManager(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "ordinary")
	app := localRuntimeTestApp(t, ctrl)
	if app.localRuntimeAdapter != nil {
		t.Fatal("test unexpectedly installed TargetManager/Local adapter")
	}
	if err := app.SubmitToTab("tab-a", "ordinary Local turn"); err != nil {
		t.Fatalf("SubmitToTab without TargetManager: %v", err)
	}
	if ctrl.submitCalls != 1 || !ctrl.RuntimeStatus().Running {
		t.Fatalf("Local submission calls/running = %d/%v", ctrl.submitCalls, ctrl.RuntimeStatus().Running)
	}
}

func TestLocalRuntimeAdapterAdvertisesOnlyImplementedV1Domains(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "capabilities")
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	view, err := adapter.Connection(context.Background())
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if !view.Capabilities.HostConfig || !view.Capabilities.WorkspaceBrowse || !view.Capabilities.SessionCreate ||
		!view.Capabilities.SessionAttach || !view.Capabilities.ComposerSubmit || !view.Capabilities.TurnCancel ||
		!view.Capabilities.Features.PrimaryFileQueries || !view.Capabilities.Features.UserShell ||
		view.Capabilities.Features.Attachments || view.Capabilities.Features.SFTP || view.Capabilities.Features.GitWrite {
		t.Fatalf("Local V1 capability projection = %+v", view.Capabilities)
	}
	if err := view.Config.RequireAvailable(); err != nil {
		t.Fatalf("HostConfigSummary: %v", err)
	}
	page, err := adapter.BrowseWorkspace(context.Background(), runtimeapi.BrowseWorkspaceInput{TypedPath: ctrl.root, Limit: 1})
	if err != nil || page.Directory.Ref == "" || page.Directory.DisplayPath != ctrl.root {
		t.Fatalf("BrowseWorkspace = %+v, %v", page, err)
	}
	if _, err := adapter.HostCapabilities(context.Background()); err != nil {
		t.Fatalf("HostCapabilities: %v", err)
	}
}

func TestLocalRuntimePromptResolutionIsOneShot(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "prompt-once")
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	ref, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab: %v", err)
	}

	adapter.publish("tab-a", event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "raw-approval", Tool: "bash", Subject: "go test ./..."}})
	adapter.mu.Lock()
	approval := adapter.sessions[ref].pendingPrompt.Approval.ID
	adapter.mu.Unlock()
	input := runtimeapi.ApproveInput{Session: ref, PromptID: approval, Decision: runtimeapi.DecisionAllowOnce}
	if err := adapter.ApprovePrompt(context.Background(), input); err != nil {
		t.Fatalf("ApprovePrompt: %v", err)
	}
	if err := adapter.ApprovePrompt(context.Background(), input); err == nil {
		t.Fatal("duplicate ApprovePrompt unexpectedly succeeded")
	}
	adapter.mu.Lock()
	pendingAfterApproval := adapter.sessions[ref].pendingPrompt
	adapter.mu.Unlock()
	if ctrl.approveCalls != 1 || pendingAfterApproval != nil {
		t.Fatalf("approval calls/pending = %d/%+v, want 1/nil", ctrl.approveCalls, pendingAfterApproval)
	}

	adapter.publish("tab-a", event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "raw-ask", Questions: []event.AskQuestion{{ID: "raw-question", Prompt: "Continue?"}}}})
	adapter.mu.Lock()
	ask := adapter.sessions[ref].pendingPrompt.Ask
	adapter.mu.Unlock()
	answer := runtimeapi.AnswerInput{Session: ref, PromptID: ask.ID, Answers: []runtimeapi.QuestionAnswer{{QuestionID: ask.Questions[0].ID, Selected: []string{"yes"}}}}
	if err := adapter.AnswerPrompt(context.Background(), answer); err != nil {
		t.Fatalf("AnswerPrompt: %v", err)
	}
	if err := adapter.AnswerPrompt(context.Background(), answer); err == nil {
		t.Fatal("duplicate AnswerPrompt unexpectedly succeeded")
	}
	adapter.mu.Lock()
	pendingAfterAnswer := adapter.sessions[ref].pendingPrompt
	adapter.mu.Unlock()
	if ctrl.answerCalls != 1 || pendingAfterAnswer != nil {
		t.Fatalf("answer calls/pending = %d/%+v, want 1/nil", ctrl.answerCalls, pendingAfterAnswer)
	}
}

func TestLocalRuntimeComposerSubmitUsesExactLocalTurnPrimitives(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "composer-union")
	ctrl.slashSkills = []skill.Skill{{Name: "review", RunAs: skill.RunInline}}
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	ref, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab: %v", err)
	}
	finishTurn := func() {
		ctrl.mu.Lock()
		ctrl.status = control.RuntimeStatus{}
		ctrl.mu.Unlock()
		app.tabs["tab-a"].sink.Emit(event.Event{Kind: event.TurnDone})
	}

	edited, err := adapter.ComposerSubmit(context.Background(), runtimeapi.ComposerSubmitInput{
		Session: ref, Input: "new body", DisplayText: "visible body", EditedOriginal: "old body",
	})
	if err != nil || edited.Kind != runtimeapi.SubmitTurn || edited.TurnID == "" || ctrl.editedCalls != 1 {
		t.Fatalf("edited ComposerSubmit = %+v, %v; edited calls=%d", edited, err, ctrl.editedCalls)
	}
	finishTurn()

	recovery, err := adapter.ComposerSubmit(context.Background(), runtimeapi.ComposerSubmitInput{
		Session: ref, Input: "continue delivery", DeliveryRecovery: true,
	})
	if err != nil || recovery.Kind != runtimeapi.SubmitTurn || recovery.TurnID == "" || ctrl.recoveryCalls != 1 {
		t.Fatalf("recovery ComposerSubmit = %+v, %v; recovery calls=%d", recovery, err, ctrl.recoveryCalls)
	}
	finishTurn()

	invoked, err := adapter.ComposerSubmit(context.Background(), runtimeapi.ComposerSubmitInput{
		Session: ref, Input: "review this", Invocations: []runtimeapi.Invocation{{Name: "review", Kind: runtimeapi.InvocationSkill}},
	})
	if err != nil || invoked.Kind != runtimeapi.SubmitTurn || invoked.TurnID == "" || len(ctrl.invocations) != 1 || ctrl.invocations[0].Name != "review" {
		t.Fatalf("invocation ComposerSubmit = %+v, %v; invocations=%+v", invoked, err, ctrl.invocations)
	}
	finishTurn()

	if _, err := adapter.ComposerSubmit(context.Background(), runtimeapi.ComposerSubmitInput{
		Session: ref, Input: "invalid union", EditedOriginal: "old", DeliveryRecovery: true,
	}); err == nil {
		t.Fatal("invalid composer union unexpectedly reached Local controller")
	}
}

func TestLocalRuntimeFileQueriesUseSharedWorkspaceService(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "file-queries")
	if err := os.WriteFile(filepath.Join(ctrl.root, "note.txt"), []byte("local runtime file body\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	ref, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab: %v", err)
	}

	files, err := adapter.ListFiles(context.Background(), runtimeapi.FileListInput{Session: ref})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	found := false
	for _, entry := range files.Entries {
		found = found || entry.Path == "note.txt"
	}
	if !found {
		t.Fatalf("ListFiles entries = %+v, want real note.txt", files.Entries)
	}
	preview, err := adapter.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: ref, Path: "note.txt"})
	if err != nil || preview.Body == nil || *preview.Body != "local runtime file body\n" {
		t.Fatalf("PreviewFile = %+v, %v", preview, err)
	}
}

func TestLocalRuntimeUnsubscribeStopsSessionEvents(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "unsubscribe")
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	ref, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab: %v", err)
	}
	adapter.mu.Lock()
	adapter.sessions[ref].subscribed = true
	adapter.mu.Unlock()
	if err := adapter.UnsubscribeSession(context.Background(), runtimeapi.UnsubscribeSessionInput{Session: ref}); err != nil {
		t.Fatalf("UnsubscribeSession: %v", err)
	}
	adapter.publish("tab-a", event.Event{Kind: event.Notice, Text: "must stay local"})
	select {
	case got := <-adapter.Events():
		t.Fatalf("event delivered after unsubscribe: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestLocalRuntimeContentIsExplicitlyUnavailableWithoutIssuedRef(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "content-ref")
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	_, err = adapter.SessionContent(context.Background(), runtimeapi.ContentInput{ContentRef: "not-issued-locally"})
	if !errors.Is(err, runtimeapi.ErrUnavailable) {
		t.Fatalf("SessionContent error = %v, want explicit unavailable", err)
	}
}

func TestLocalRuntimeCheckpointIDsAreOpaqueStableAndNeverReusedAfterRewrite(t *testing.T) {
	ctrl := newLocalRuntimeTestController(t, "checkpoint-ids")
	app := localRuntimeTestApp(t, ctrl)
	adapter, err := NewLocalTargetAdapter(app)
	if err != nil {
		t.Fatalf("NewLocalTargetAdapter: %v", err)
	}
	t.Cleanup(adapter.closeAdapter)
	ref, err := adapter.SessionRefForTab("tab-a")
	if err != nil {
		t.Fatalf("SessionRefForTab: %v", err)
	}
	values := []CheckpointMeta{{Turn: 4, Prompt: "change code", Files: []string{"a.go"}, FileCount: 1, CanCode: true, CanConversation: true}}

	adapter.mu.Lock()
	first, err := adapter.syncLocalCheckpointsLocked(ref, values)
	second, secondErr := adapter.syncLocalCheckpointsLocked(ref, values)
	adapter.invalidateLocalCheckpointsLocked(ref)
	third, thirdErr := adapter.syncLocalCheckpointsLocked(ref, values)
	_, oldStillValid := adapter.v1.checkpointIDs[first[4]]
	adapter.mu.Unlock()
	if err != nil || secondErr != nil || thirdErr != nil {
		t.Fatalf("checkpoint projection errors = %v, %v, %v", err, secondErr, thirdErr)
	}
	if first[4] == "" || second[4] != first[4] {
		t.Fatalf("stable checkpoint ids = %q then %q", first[4], second[4])
	}
	if third[4] == first[4] || oldStillValid {
		t.Fatalf("checkpoint id reused or retained after rewrite: first=%q third=%q oldValid=%v", first[4], third[4], oldStillValid)
	}
}
