package runtimefactory

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/sessiondisplay"
	"reasonix/internal/tool"
)

type cancellationProvider struct {
	started chan struct{}
	once    sync.Once
}

func (p *cancellationProvider) Name() string { return "runtimefactory-cancellation" }

func (p *cancellationProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	stream := make(chan provider.Chunk)
	go func() {
		<-ctx.Done()
		close(stream)
	}()
	return stream, nil
}

type fakeResolver struct {
	resolved catalog.ResolvedSession
	err      error
}

func (r *fakeResolver) ResolveRuntimeTarget(context.Context, protocol.RuntimeTarget) (catalog.ResolvedSession, error) {
	return r.resolved, r.err
}

type factoryController struct {
	control.SessionAPI

	mu              sync.Mutex
	enabled         int
	plan            bool
	approval        string
	resumePath      string
	resumeMessages  []provider.Message
	closeCalls      int
	releaseCalls    int
	destroyCalls    int
	displayRecorder func(content, display string)
	submitCalls     int
	durableEnabled  int
	recoveryErr     error
}

type panicCloseController struct{ control.SessionAPI }

func (c *panicCloseController) EnableDurableTurnAdmission() error { return nil }
func (c *panicCloseController) PrepareDurableTurn(control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	return func() control.DurableTurnAdmissionResult {
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}, nil
}

func (c *panicCloseController) ResumeWithRecovery(session *agent.Session, path string) (control.SessionResumeState, error) {
	c.SessionAPI.Resume(session, path)
	return control.SessionResumeState{}, nil
}

func (c *panicCloseController) PrepareRuntimeShutdown() {
	if recovery, ok := c.SessionAPI.(control.RecoveryLifecycle); ok {
		recovery.PrepareRuntimeShutdown()
	}
}

func (c *panicCloseController) Close() { panic("close failed") }

type durableOnlyController struct{ control.SessionAPI }

func (c *durableOnlyController) EnableDurableTurnAdmission() error { return nil }
func (c *durableOnlyController) PrepareDurableTurn(control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	return func() control.DurableTurnAdmissionResult {
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}, nil
}

func newFactoryController() *factoryController {
	return &factoryController{SessionAPI: control.New(control.Options{Sink: event.Discard})}
}

func (c *factoryController) EnableInteractiveApproval() {
	c.mu.Lock()
	c.enabled++
	c.mu.Unlock()
}

func (c *factoryController) SetPlanMode(value bool) {
	c.mu.Lock()
	c.plan = value
	c.mu.Unlock()
}

func (c *factoryController) SetToolApprovalMode(value string) {
	c.mu.Lock()
	c.approval = value
	c.mu.Unlock()
}

func (c *factoryController) SetDisplayRecorder(record func(content, display string)) {
	c.mu.Lock()
	c.displayRecorder = record
	c.mu.Unlock()
}

func (c *factoryController) EnableDurableTurnAdmission() error {
	c.mu.Lock()
	c.durableEnabled++
	c.mu.Unlock()
	return nil
}

func (c *factoryController) PrepareDurableTurn(control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	return func() control.DurableTurnAdmissionResult {
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}, nil
}

func (c *factoryController) ResumeWithRecovery(session *agent.Session, path string) (control.SessionResumeState, error) {
	c.Resume(session, path)
	c.mu.Lock()
	err := c.recoveryErr
	c.mu.Unlock()
	return control.SessionResumeState{}, err
}

func (c *factoryController) PrepareRuntimeShutdown() {
	if recovery, ok := c.SessionAPI.(control.RecoveryLifecycle); ok {
		recovery.PrepareRuntimeShutdown()
	}
}

func (c *factoryController) SubmitUserTurn(input, display string) {
	c.mu.Lock()
	c.submitCalls++
	record := c.displayRecorder
	c.mu.Unlock()
	if record != nil {
		record(input, display)
	}
}

func (c *factoryController) Resume(session *agent.Session, path string) {
	c.mu.Lock()
	c.resumePath = path
	c.resumeMessages = session.Snapshot()
	c.mu.Unlock()
}

func (c *factoryController) Close() {
	c.mu.Lock()
	c.closeCalls++
	c.mu.Unlock()
}

func (c *factoryController) ReleaseResources() {
	c.mu.Lock()
	c.releaseCalls++
	c.mu.Unlock()
}

func (c *factoryController) CloseAfterDestroy() {
	c.mu.Lock()
	c.destroyCalls++
	c.mu.Unlock()
}

func testResolvedSession(t *testing.T) catalog.ResolvedSession {
	t.Helper()
	workspace := t.TempDir()
	sessionDir := filepath.Join(workspace, ".reasonix-sessions")
	if err := mkdirTestDir(sessionDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "opaque.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "persisted Remote prompt"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	return catalog.ResolvedSession{
		Target:         protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"},
		WorkspaceRoot:  workspace,
		AdditionalDirs: []string{t.TempDir()},
		SessionDir:     sessionDir,
		SessionPath:    path,
		ResolvedProfile: protocol.ResolvedProfile{
			Model: "provider/model", Effort: "high", CollaborationMode: protocol.CollaborationPlan,
			TokenMode: protocol.TokenEconomy, ToolApprovalMode: protocol.ToolApprovalAuto,
		},
	}
}

func mkdirTestDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

func TestFactoryBuildsAndResumesRealResolvedSessionWithFrozenAxes(t *testing.T) {
	resolved := testResolvedSession(t)
	resolver := &fakeResolver{resolved: resolved}
	controller := newFactoryController()
	var captured boot.Options
	factory, err := New(Options{
		Resolver: resolver,
		Builder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			captured = options
			return controller, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := factory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Model != resolved.ResolvedProfile.Model || captured.RequireKey || captured.WorkspaceRoot != resolved.WorkspaceRoot ||
		captured.SessionDir != resolved.SessionDir || captured.TokenMode != string(protocol.TokenEconomy) ||
		captured.HeadlessApprovalMode != string(protocol.ToolApprovalAuto) || captured.EffortOverride == nil || *captured.EffortOverride != "high" ||
		!reflect.DeepEqual(captured.AdditionalDirs, resolved.AdditionalDirs) {
		t.Fatalf("boot options = %+v", captured)
	}
	controller.mu.Lock()
	if controller.enabled != 1 || controller.durableEnabled != 1 || !controller.plan || controller.approval != string(protocol.ToolApprovalAuto) ||
		controller.resumePath != resolved.SessionPath || len(controller.resumeMessages) != 2 || controller.resumeMessages[1].Content != "persisted Remote prompt" {
		t.Fatalf("Controller configuration enabled=%d plan=%v approval=%q path=%q messages=%+v",
			controller.enabled, controller.plan, controller.approval, controller.resumePath, controller.resumeMessages)
	}
	controller.mu.Unlock()
	created.Close()
	controller.mu.Lock()
	if controller.closeCalls != 1 {
		t.Fatalf("Controller Close calls = %d", controller.closeCalls)
	}
	controller.mu.Unlock()
}

func TestFactoryRejectsControllerWithoutDurableTurnAdmission(t *testing.T) {
	resolved := testResolvedSession(t)
	if err := agent.MarkSessionInFlightTurn(resolved.SessionPath, 1, true); err != nil {
		t.Fatal(err)
	}
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return &struct{ control.SessionAPI }{SessionAPI: control.New(control.Options{Sink: event.Discard})}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.CreateController(context.Background(), resolved.Target, event.Discard); err == nil ||
		!strings.Contains(err.Error(), "does not support durable Turn admission") {
		t.Fatalf("unsupported Controller error = %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		t.Fatalf("unsupported Controller consumed recovery marker: ok=%v err=%v meta=%+v", ok, err, meta)
	}
}

func TestFactoryRejectsControllerWithoutRecoveryLifecycle(t *testing.T) {
	resolved := testResolvedSession(t)
	if err := agent.MarkSessionInFlightTurn(resolved.SessionPath, 1, true); err != nil {
		t.Fatal(err)
	}
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return &durableOnlyController{SessionAPI: control.New(control.Options{Sink: event.Discard})}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.CreateController(context.Background(), resolved.Target, event.Discard); err == nil ||
		!strings.Contains(err.Error(), "does not support crash recovery lifecycle") {
		t.Fatalf("unsupported Controller error = %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		t.Fatalf("unsupported Controller consumed recovery marker: ok=%v err=%v meta=%+v", ok, err, meta)
	}
}

func TestFactoryRecoveryFailureClosesControllerReleasesLeaseAndPreservesMarker(t *testing.T) {
	resolved := testResolvedSession(t)
	if err := agent.MarkSessionInFlightTurn(resolved.SessionPath, 1, true); err != nil {
		t.Fatal(err)
	}
	recoveryErr := errors.New("injected recovery rewrite failure")
	controller := newFactoryController()
	controller.recoveryErr = recoveryErr
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return controller, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, state, err := factory.CreateControllerWithRecovery(context.Background(), resolved.Target, event.Discard)
	if created != nil || state != (control.SessionResumeState{}) || !errors.Is(err, recoveryErr) ||
		!strings.Contains(err.Error(), "recover Remote Session") {
		t.Fatalf("recovery failure = controller=%T state=%+v err=%v", created, state, err)
	}
	controller.mu.Lock()
	closeCalls := controller.closeCalls
	controller.mu.Unlock()
	if closeCalls != 1 {
		t.Fatalf("Controller Close calls after recovery failure = %d, want 1", closeCalls)
	}
	factory.leaseMu.Lock()
	leaseCount := len(factory.leases)
	factory.leaseMu.Unlock()
	if leaseCount != 0 {
		t.Fatalf("retained target leases after recovery failure = %d, want 0", leaseCount)
	}
	meta, ok, loadErr := agent.LoadBranchMeta(resolved.SessionPath)
	if loadErr != nil || !ok || meta.InFlightTurn == nil {
		t.Fatalf("recovery failure consumed marker: ok=%v err=%v meta=%+v", ok, loadErr, meta)
	}
}

func TestFactoryPreservesBootSystemPromptForFreshZeroByteSession(t *testing.T) {
	resolved := testResolvedSession(t)
	if err := os.Truncate(resolved.SessionPath, 0); err != nil {
		t.Fatal(err)
	}
	const systemPrompt = "fresh Remote system prompt"
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			executor := agent.New(nil, nil, agent.NewSession(systemPrompt), agent.Options{}, options.Sink)
			return control.New(control.Options{
				Executor: executor, Sink: options.Sink, SessionDir: options.SessionDir,
				WorkspaceRoot: options.WorkspaceRoot,
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, recovery, err := factory.CreateControllerWithRecovery(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	if recovery.PreviousTurnInterrupted {
		t.Fatal("fresh zero-byte Session was marked interrupted")
	}
	history := controller.History()
	if len(history) != 1 || history[0].Role != provider.RoleSystem || history[0].Content != systemPrompt {
		t.Fatalf("fresh Remote history = %+v, want boot system prefix retained", history)
	}
}

func TestFactoryRefreshesPersistedSystemPromptOnRemoteRebuild(t *testing.T) {
	resolved := testResolvedSession(t)
	const builtSystemPrompt = "new boot/profile system prompt"
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			executor := agent.New(nil, nil, agent.NewSession(builtSystemPrompt), agent.Options{}, options.Sink)
			return control.New(control.Options{
				Executor: executor, Sink: options.Sink, SessionDir: options.SessionDir,
				SessionPath: resolved.SessionPath, WorkspaceRoot: options.WorkspaceRoot,
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := factory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	history := controller.History()
	if len(history) != 2 || history[0].Role != provider.RoleSystem || history[0].Content != builtSystemPrompt ||
		history[1].Role != provider.RoleUser || history[1].Content != "persisted Remote prompt" {
		t.Fatalf("resumed Remote history = %+v, want refreshed system contract with preserved user history", history)
	}
}

func TestFactoryDisplayRecorderRoundTripsCanonicalPromptAtStableResolvedPath(t *testing.T) {
	resolved := testResolvedSession(t)
	controller := newFactoryController()
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return controller, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := factory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Close()

	const canonical = "expanded canonical body from @guide.md"
	const display = "@guide.md explain this"
	created.SubmitUserTurn(canonical, display)
	if got := sessiondisplay.Resolve(resolved.SessionDir, resolved.SessionPath, canonical); got != display {
		t.Fatalf("display sidecar roundtrip = %q, want %q", got, display)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.submitCalls != 1 {
		t.Fatalf("SubmitUserTurn calls = %d, want 1", controller.submitCalls)
	}
}

func TestFactoryDisplayRecordFailureIsReportedWithoutFailingTurn(t *testing.T) {
	resolved := testResolvedSession(t)
	// A directory at the sidecar destination makes the atomic replacement fail
	// deterministically without making the Session directory itself unusable.
	if err := os.Mkdir(sessiondisplay.Path(resolved.SessionDir), 0o700); err != nil {
		t.Fatal(err)
	}
	controller := newFactoryController()
	var reported error
	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return controller, nil
		},
		OnDisplayRecordError: func(err error) { reported = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := factory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Close()

	created.SubmitUserTurn("canonical body", "short display")
	controller.mu.Lock()
	submitCalls := controller.submitCalls
	controller.mu.Unlock()
	if submitCalls != 1 {
		t.Fatalf("display persistence failure prevented turn submission: calls=%d", submitCalls)
	}
	if reported == nil {
		t.Fatal("display persistence failure was not reported")
	}
}

func TestFactoryCapturesRecoveryStateBeforeResumeClearsMarker(t *testing.T) {
	resolved := testResolvedSession(t)
	persisted, err := agent.LoadSession(resolved.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	persisted.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial daemon tail"})
	if err := persisted.Save(resolved.SessionPath); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(resolved.SessionPath, 1, true); err != nil {
		t.Fatal(err)
	}

	factory, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder: func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
			executor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, options.Sink)
			return control.New(control.Options{
				Executor: executor, Sink: options.Sink, SessionDir: options.SessionDir,
				SessionPath: resolved.SessionPath, WorkspaceRoot: options.WorkspaceRoot,
			}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, recovery, err := factory.CreateControllerWithRecovery(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	if !recovery.PreviousTurnInterrupted {
		t.Fatal("factory lost the recovery result after Controller.Resume cleared the marker")
	}
	history := controller.History()
	if len(history) != 2 || history[1].Role != provider.RoleUser || history[1].Content != "persisted Remote prompt" {
		t.Fatalf("repaired Controller history = %+v", history)
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatalf("Resume left the durable marker behind: %+v", meta.InFlightTurn)
	}
}

func TestProductionFactoryShutdownAndColdRestoreRepairsInterruptedTurn(t *testing.T) {
	resolved := testResolvedSession(t)
	providerValue := &cancellationProvider{started: make(chan struct{})}
	build := func(_ context.Context, options boot.Options) (control.SessionAPI, error) {
		executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, options.Sink)
		return control.New(control.Options{
			Runner: executor, Executor: executor, Sink: options.Sink,
			SessionDir: options.SessionDir, SessionPath: resolved.SessionPath,
			WorkspaceRoot: options.WorkspaceRoot,
		}), nil
	}
	factory, err := New(Options{Resolver: &fakeResolver{resolved: resolved}, Builder: build})
	if err != nil {
		t.Fatal(err)
	}
	controller, recovery, err := factory.CreateControllerWithRecovery(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.PreviousTurnInterrupted {
		t.Fatal("fresh runtime was marked interrupted")
	}
	durable, ok := controller.(control.DurableTurnAdmission)
	if !ok {
		t.Fatal("leased production Controller does not expose DurableTurnAdmission")
	}
	completeAdmission, err := durable.PrepareDurableTurn(control.DurableTurnInput{
		Input: "accepted before daemon shutdown", DisplayText: "accepted before daemon shutdown",
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.SubmitUserTurn("accepted before daemon shutdown", "accepted before daemon shutdown")
	if admission := completeAdmission(); !admission.Claimed || !admission.SemanticCommit || admission.Err != nil {
		t.Fatalf("production durable admission = %+v", admission)
	}
	select {
	case <-providerValue.started:
	case <-time.After(3 * time.Second):
		t.Fatal("production Controller turn did not reach the provider")
	}
	waitFactoryMarker(t, resolved.SessionPath, true)
	lifecycle, ok := controller.(control.RecoveryLifecycle)
	if !ok {
		t.Fatal("leased production Controller does not expose RecoveryLifecycle")
	}
	lifecycle.PrepareRuntimeShutdown()
	controller.Close()
	waitFactoryMarker(t, resolved.SessionPath, true)

	// Model a durable mid-turn tool/assistant tail captured by periodic autosave.
	// The real shutdown above already persisted the accepted user prompt.
	persisted, err := agent.LoadSession(resolved.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if last := persisted.Messages[len(persisted.Messages)-1]; last.Role != provider.RoleUser || !strings.Contains(last.Content, "accepted before daemon shutdown") {
		t.Fatalf("shutdown transcript tail = %+v, want accepted real user prompt", last)
	}
	persisted.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial tool dispatch", ToolCalls: []provider.ToolCall{{ID: "tool-partial", Name: "read_file", Arguments: `{}`}}})
	persisted.Add(provider.Message{Role: provider.RoleTool, Content: "partial result", ToolCallID: "tool-partial", Name: "read_file"})
	if err := persisted.Save(resolved.SessionPath); err != nil {
		t.Fatal(err)
	}

	restoredFactory, err := New(Options{Resolver: &fakeResolver{resolved: resolved}, Builder: build})
	if err != nil {
		t.Fatal(err)
	}
	restored, restoredState, err := restoredFactory.CreateControllerWithRecovery(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if !restoredState.PreviousTurnInterrupted {
		t.Fatal("cold runtime did not receive the consumed interruption state")
	}
	history := restored.History()
	if len(history) != 3 || history[2].Role != provider.RoleUser || !strings.Contains(history[2].Content, "accepted before daemon shutdown") {
		t.Fatalf("cold-restored history = %+v, want partial assistant/tool tail removed and user preserved", history)
	}
	waitFactoryMarker(t, resolved.SessionPath, false)
}

func waitFactoryMarker(t *testing.T, path string, present bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		meta, ok, err := agent.LoadBranchMeta(path)
		if err == nil && ok && (meta.InFlightTurn != nil) == present {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	t.Fatalf("marker present=%v, want %v (ok=%v err=%v marker=%+v)", meta.InFlightTurn != nil, present, ok, err, meta.InFlightTurn)
}

func TestFactoryTargetLeaseAllowsAtomicReplacementButExcludesOtherFactory(t *testing.T) {
	resolved := testResolvedSession(t)
	resolver := &fakeResolver{resolved: resolved}
	build := func(context.Context, boot.Options) (control.SessionAPI, error) { return newFactoryController(), nil }
	firstFactory, err := New(Options{Resolver: resolver, Builder: build})
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstFactory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := firstFactory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatalf("same-factory replacement could not share target lease: %v", err)
	}
	otherFactory, err := New(Options{Resolver: resolver, Builder: build})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherFactory.CreateController(context.Background(), resolved.Target, event.Discard); err == nil {
		t.Fatal("second factory acquired an already-held Session writer lease")
	}
	first.Close()
	if _, err := otherFactory.CreateController(context.Background(), resolved.Target, event.Discard); err == nil {
		t.Fatal("replacement reference did not retain Session writer lease")
	}
	replacement.ReleaseResources()
	afterRelease, err := otherFactory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatalf("Session writer lease was not released after last runtime: %v", err)
	}
	afterRelease.CloseAfterDestroy()
}

func TestFactoryRejectsResolverDriftAndReleasesLeaseAfterBuildFailure(t *testing.T) {
	resolved := testResolvedSession(t)
	drifted := resolved
	drifted.Target.SessionID = "session-other"
	factory, err := New(Options{Resolver: &fakeResolver{resolved: drifted}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.CreateController(context.Background(), resolved.Target, event.Discard); err == nil {
		t.Fatal("resolver target drift was accepted")
	}

	wantErr := errors.New("build failed")
	failing, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder:  func(context.Context, boot.Options) (control.SessionAPI, error) { return nil, wantErr },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.CreateController(context.Background(), resolved.Target, event.Discard); !errors.Is(err, wantErr) {
		t.Fatalf("build failure = %v", err)
	}
	succeeding, err := New(Options{
		Resolver: &fakeResolver{resolved: resolved},
		Builder:  func(context.Context, boot.Options) (control.SessionAPI, error) { return newFactoryController(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := succeeding.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatalf("build failure leaked Session lease: %v", err)
	}
	controller.Close()
}

func TestFactoryReleasesTargetLeaseWhenControllerClosePanics(t *testing.T) {
	resolved := testResolvedSession(t)
	resolver := &fakeResolver{resolved: resolved}
	panicking, err := New(Options{
		Resolver: resolver,
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return &panicCloseController{SessionAPI: control.New(control.Options{Sink: event.Discard})}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := panicking.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Controller Close did not propagate the panic")
			}
		}()
		controller.Close()
	}()

	succeeding, err := New(Options{
		Resolver: resolver,
		Builder: func(context.Context, boot.Options) (control.SessionAPI, error) {
			return newFactoryController(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	afterPanic, err := succeeding.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatalf("panicking Close leaked Session lease: %v", err)
	}
	afterPanic.Close()
}

func TestFactoryDefaultBuilderUsesProductionBootBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	resolved := testResolvedSession(t)
	resolved.ResolvedProfile.Model = "remote-test/remote-model"
	resolved.ResolvedProfile.Effort = "auto"
	configBody := `default_model = "remote-test/remote-model"

[[providers]]
name = "remote-test"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
model = "remote-model"
`
	if err := os.WriteFile(filepath.Join(resolved.WorkspaceRoot, "reasonix.toml"), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	factory, err := New(Options{Resolver: &fakeResolver{resolved: resolved}, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := factory.CreateController(context.Background(), resolved.Target, event.Discard)
	if err != nil {
		t.Fatalf("production boot.Build Controller: %v", err)
	}
	controller.Close()
}
