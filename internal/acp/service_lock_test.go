package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
)

type snapshotLockProbeController struct {
	*control.Controller
	onSnapshot func()
}

func TestACPRebuildSerializesCollaborationAndApprovalChanges(t *testing.T) {
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	factory := &configurableFactory{
		onBuild: func(index int, _ SessionParams) {
			if index != 0 {
				return
			}
			close(buildStarted)
			<-releaseBuild
		},
	}
	sink := newUpdateSink(&fakeNotifier{}, "sess-axis-race")
	sess := &acpSession{
		id:               "sess-axis-race",
		ctrl:             control.New(control.Options{}),
		sink:             sink,
		cwd:              t.TempDir(),
		model:            "fast",
		runtimeProfile:   "balanced",
		toolApprovalMode: control.ToolApprovalAsk,
		modeID:           sessionModeNormal,
	}
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	rebuildErr := make(chan error, 1)
	go func() {
		rebuildErr <- svc.rebuildSession(context.Background(), sess, SessionConfigState{
			Model:          "pro",
			RuntimeProfile: "delivery",
		}, []sessionConfigDelta{{axis: "work_mode", runtimeProfile: "delivery"}})
	}()
	select {
	case <-buildStarted:
	case <-time.After(time.Second):
		t.Fatal("controller rebuild did not reach blocked build")
	}

	modeRaw, err := json.Marshal(SessionSetModeParams{SessionID: sess.id, ModeID: sessionModePlan})
	if err != nil {
		t.Fatal(err)
	}
	modeDone := make(chan error, 1)
	approvalDone := make(chan error, 1)
	go func() {
		_, err := svc.sessionSetMode(context.Background(), modeRaw)
		modeDone <- err
	}()
	go func() {
		_, err := svc.switchSessionToolApproval(context.Background(), sess, control.ToolApprovalAuto)
		approvalDone <- err
	}()
	select {
	case err := <-modeDone:
		t.Fatalf("mode change completed before controller swap: %v", err)
	case err := <-approvalDone:
		t.Fatalf("approval change completed before controller swap: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseBuild)

	for name, ch := range map[string]<-chan error{
		"rebuild":  rebuildErr,
		"mode":     modeDone,
		"approval": approvalDone,
	} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
	ctrl := sess.currentCtrl()
	if !ctrl.PlanMode() || ctrl.ToolApprovalMode() != control.ToolApprovalAuto {
		t.Fatalf("post-rebuild axes = plan:%v approval:%q, want plan + auto", ctrl.PlanMode(), ctrl.ToolApprovalMode())
	}
	if sess.runtimeProfile != "delivery" || sess.currentModeID() != sessionModePlan {
		t.Fatalf("post-rebuild session = profile:%q mode:%q, want delivery + plan", sess.runtimeProfile, sess.currentModeID())
	}
}

func (c *snapshotLockProbeController) Snapshot() error {
	if c.onSnapshot != nil {
		c.onSnapshot()
	}
	return nil
}

func expectACPSessionMutexAvailableDuringSnapshot(t *testing.T, sess *acpSession, checks chan<- struct{}) func() {
	t.Helper()
	return func() {
		acquired := make(chan struct{})
		go func() {
			sess.mu.Lock()
			sess.mu.Unlock() //nolint:staticcheck // probe: lock must be immediately acquirable
			close(acquired)
		}()
		select {
		case <-acquired:
		case <-time.After(500 * time.Millisecond):
			t.Error("Snapshot ran while holding ACP session mutex")
		}
		if checks == nil {
			return
		}
		select {
		case checks <- struct{}{}:
		default:
		}
	}
}

func TestACPPersistAfterTurnSnapshotsWithoutSessionLock(t *testing.T) {
	sess := &acpSession{id: "sess-lock"}
	checks := make(chan struct{}, 1)
	sess.ctrl = &snapshotLockProbeController{
		Controller: control.New(control.Options{}),
		onSnapshot: expectACPSessionMutexAvailableDuringSnapshot(t, sess, checks),
	}

	sess.persistAfterTurn("hello from acp")

	select {
	case <-checks:
	case <-time.After(time.Second):
		t.Fatal("session was not snapshotted after turn")
	}
	if sess.title == "" {
		t.Fatal("session title was not updated after turn")
	}
}

func TestACPRebuildSessionSnapshotsWithoutSessionLock(t *testing.T) {
	sink := newUpdateSink(&fakeNotifier{}, "sess-lock")
	sess := &acpSession{
		id:    "sess-lock",
		sink:  sink,
		cwd:   t.TempDir(),
		model: "fast",
	}
	checks := make(chan struct{}, 1)
	oldCtrl := &snapshotLockProbeController{
		Controller: control.New(control.Options{}),
		onSnapshot: expectACPSessionMutexAvailableDuringSnapshot(t, sess, checks),
	}
	sess.ctrl = oldCtrl
	svc := &service{
		factory:  &configurableFactory{},
		sessions: map[string]*acpSession{sess.id: sess},
	}

	if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "pro"}, []sessionConfigDelta{{axis: "model", model: "pro"}}); err != nil {
		t.Fatalf("rebuildSession: %v", err)
	}
	select {
	case <-checks:
	case <-time.After(time.Second):
		t.Fatal("session was not snapshotted before rebuild")
	}
	if sess.ctrl == oldCtrl {
		t.Fatal("session controller was not replaced")
	}
	if sess.model != "pro" {
		t.Fatalf("session model = %q, want pro", sess.model)
	}
}

type blockingConfigFactory struct {
	configurableFactory
	started      chan string
	releaseFirst chan struct{}
}

type blockingResolveFactory struct {
	configurableFactory
	proReached   chan struct{}
	releasePro   chan struct{}
	fastResolved chan struct{}
	proOnce      sync.Once
	fastOnce     sync.Once
}

func (f *blockingResolveFactory) SessionConfigState(ctx context.Context, p SessionConfigStateParams) (SessionConfigState, error) {
	switch p.Model {
	case "pro":
		f.proOnce.Do(func() { close(f.proReached) })
		select {
		case <-f.releasePro:
		case <-ctx.Done():
			return SessionConfigState{}, ctx.Err()
		}
	case "fast":
		f.fastOnce.Do(func() { close(f.fastResolved) })
	}
	return f.configurableFactory.SessionConfigState(ctx, p)
}

type failFirstBuildFactory struct {
	configurableFactory
	started  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	attempts int
}

func (f *failFirstBuildFactory) NewSession(ctx context.Context, p SessionParams) (*control.Controller, error) {
	f.mu.Lock()
	f.attempts++
	attempt := f.attempts
	f.mu.Unlock()
	if attempt == 1 {
		close(f.started)
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return nil, errors.New("first build failed")
	}
	return f.configurableFactory.NewSession(ctx, p)
}

func (f *blockingConfigFactory) NewSession(ctx context.Context, p SessionParams) (*control.Controller, error) {
	select {
	case f.started <- p.Model:
	default:
	}
	f.mu.Lock()
	buildNumber := len(f.builds) + 1
	f.mu.Unlock()
	if buildNumber == 1 {
		select {
		case <-f.releaseFirst:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.configurableFactory.NewSession(ctx, p)
}

func TestACPRebuildSessionAppliesPendingConfigAfterMaintenance(t *testing.T) {
	sink := newUpdateSink(&fakeNotifier{}, "sess-lock")
	sess := &acpSession{
		id:    "sess-lock",
		sink:  sink,
		cwd:   t.TempDir(),
		model: "fast",
		ctrl:  control.New(control.Options{}),
	}
	factory := &blockingConfigFactory{
		started:      make(chan string, 2),
		releaseFirst: make(chan struct{}),
	}
	svc := &service{
		factory:  factory,
		sessions: map[string]*acpSession{sess.id: sess},
	}

	errs := make(chan error, 1)
	go func() {
		errs <- svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "pro"}, []sessionConfigDelta{{axis: "model", model: "pro"}})
	}()
	select {
	case got := <-factory.started:
		if got != "pro" {
			t.Fatalf("first rebuild model = %q, want pro", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not start")
	}

	if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "fast"}, []sessionConfigDelta{{axis: "model", model: "fast"}}); err != nil {
		t.Fatalf("queue pending rebuild: %v", err)
	}
	close(factory.releaseFirst)
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("first rebuild: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not finish")
	}
	if sess.model != "fast" {
		t.Fatalf("session model = %q, want pending fast", sess.model)
	}
	if got := factory.buildCount(); got != 2 {
		t.Fatalf("factory builds = %d, want 2", got)
	}
}

// TestACPRebuildSessionQueuedCrossAxisChangeDoesNotRollbackCompletedAxis pins
// the fix for a race where a queued config change resolved its full
// SessionConfigState snapshot at enqueue time from sess.model/effortOverride/
// runtimeProfile — fields that only update once an in-flight rebuild for a
// *different* axis lands. Queuing a work-mode (profile) switch while a model
// switch was still rebuilding used to restore the pre-switch model as soon as
// the queued profile switch drained.
func TestACPRebuildSessionQueuedCrossAxisChangeDoesNotRollbackCompletedAxis(t *testing.T) {
	sink := newUpdateSink(&fakeNotifier{}, "sess-cross-axis")
	sess := &acpSession{
		id:             "sess-cross-axis",
		sink:           sink,
		cwd:            t.TempDir(),
		model:          "fast",
		runtimeProfile: "balanced",
		ctrl:           control.New(control.Options{}),
	}
	factory := &blockingConfigFactory{
		started:      make(chan string, 2),
		releaseFirst: make(chan struct{}),
	}
	svc := &service{
		factory:  factory,
		sessions: map[string]*acpSession{sess.id: sess},
	}

	type switchResult struct {
		state SessionConfigState
		err   error
	}
	results := make(chan switchResult, 1)
	go func() {
		state, err := svc.switchSessionModel(context.Background(), sess, "pro")
		results <- switchResult{state: state, err: err}
	}()
	select {
	case got := <-factory.started:
		if got != "pro" {
			t.Fatalf("first rebuild model = %q, want pro", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not start")
	}

	// Work Mode -> delivery queues while Model -> pro is still rebuilding, so
	// sess.model still reads "fast" at this instant. Before the fix, the
	// queued change stored that stale full snapshot; the fix stores only the
	// work_mode delta and re-resolves the baseline when it actually applies.
	if _, err := svc.switchSessionRuntimeProfile(context.Background(), sess, "delivery"); err != nil {
		t.Fatalf("queue work mode switch: %v", err)
	}

	close(factory.releaseFirst)
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("model switch: %v", result.err)
		}
		if result.state.Model != "pro" || result.state.RuntimeProfile != "delivery" {
			t.Fatalf("model switch response = model %q, profile %q; want final pro/delivery state after pending drain", result.state.Model, result.state.RuntimeProfile)
		}
	case <-time.After(time.Second):
		t.Fatal("model switch did not finish")
	}

	if got, want := factory.buildCount(), 2; got != want {
		t.Fatalf("factory builds = %d, want %d", got, want)
	}
	if sess.model != "pro" {
		t.Fatalf("session model = %q, want pro (queued profile switch must not roll back a completed model switch)", sess.model)
	}
	if sess.runtimeProfile != "delivery" {
		t.Fatalf("session runtime profile = %q, want delivery", sess.runtimeProfile)
	}
}

// TestACPCtrlReadPathsDoNotRaceWithRebuild drives the lock-free read surfaces
// that used to read sess.ctrl outside sess.mu — info(), service.sessionDir(),
// sendAvailableCommands, and resolveSlashPrompt — while a rebuild goroutine
// keeps swapping the controller. Under -race this fails without currentCtrl().
func TestACPCtrlReadPathsDoNotRaceWithRebuild(t *testing.T) {
	sink := newUpdateSink(&fakeNotifier{}, "sess-race")
	sess := &acpSession{
		id:    "sess-race",
		sink:  sink,
		cwd:   t.TempDir(),
		model: "fast",
		ctrl:  control.New(control.Options{}),
	}
	factory := &configurableFactory{}
	svc := &service{
		factory:  factory,
		sessions: map[string]*acpSession{sess.id: sess},
	}

	const rebuilds = 50
	models := [...]string{"pro", "fast"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rebuilds; i++ {
			if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: models[i%len(models)]}, []sessionConfigDelta{{axis: "model", model: models[i%len(models)]}}); err != nil {
				t.Errorf("rebuildSession %d: %v", i, err)
				return
			}
		}
	}()

	for rebuilding := true; rebuilding; {
		select {
		case <-done:
			rebuilding = false
		default:
		}
		if got := sess.info().SessionID; got != sess.id {
			t.Fatalf("info().SessionID = %q, want %q", got, sess.id)
		}
		_ = svc.sessionDir()
		svc.sendAvailableCommands(sess)
		if got := svc.resolveSlashPrompt(context.Background(), sess, "/no-such-command args"); got != "/no-such-command args" {
			t.Fatalf("resolveSlashPrompt rewrote unknown command to %q", got)
		}
	}

	if sess.currentCtrl() == nil {
		t.Fatal("session controller is nil after rebuilds")
	}
	if got := factory.buildCount(); got != rebuilds {
		t.Fatalf("factory builds = %d, want %d", got, rebuilds)
	}
}

// TestACPBeginRefusesWhilePendingConfigQueued pins the invariant begin relies
// on: a session with a queued (not yet applied) config switch must not start a
// new turn, or the prompt would run on the outgoing config.
func TestACPBeginRefusesWhilePendingConfigQueued(t *testing.T) {
	sess := &acpSession{id: "sess-pending", ctrl: control.New(control.Options{})}
	sess.mu.Lock()
	sess.pendingConfig = []sessionConfigDelta{{axis: "model", model: "pro"}}
	sess.mu.Unlock()

	if _, _, ok := sess.begin(context.Background()); ok {
		t.Fatal("begin succeeded while a pending config switch was queued")
	}

	sess.mu.Lock()
	sess.pendingConfig = nil
	sess.mu.Unlock()
	_, cancel, ok := sess.begin(context.Background())
	if !ok {
		t.Fatal("begin failed on an idle session with no pending config")
	}
	cancel()
	sess.finish()
}

// TestACPBeginRefusesDuringPendingConfigApplyWindow drives the exact
// interleaving begin used to lose: rebuildSession's defer first finishes
// maintenance (maintenanceDone back to nil) and only then applies the queued
// pendingConfig. Holding service.mu parks applyPendingSessionConfig on its
// initial s.session lookup, so the session sits in that window with the queue
// still set; begin must keep refusing until the pending config has landed.
func TestACPBeginRefusesDuringPendingConfigApplyWindow(t *testing.T) {
	sink := newUpdateSink(&fakeNotifier{}, "sess-window")
	sess := &acpSession{
		id:    "sess-window",
		sink:  sink,
		cwd:   t.TempDir(),
		model: "fast",
		ctrl:  control.New(control.Options{}),
	}
	factory := &blockingConfigFactory{
		started:      make(chan string, 2),
		releaseFirst: make(chan struct{}),
	}
	svc := &service{
		factory:  factory,
		sessions: map[string]*acpSession{sess.id: sess},
	}

	errs := make(chan error, 1)
	go func() {
		errs <- svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "pro"}, []sessionConfigDelta{{axis: "model", model: "pro"}})
	}()
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not start")
	}

	// Queue a second switch while the first build is blocked in maintenance.
	if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "fast"}, []sessionConfigDelta{{axis: "model", model: "fast"}}); err != nil {
		t.Fatalf("queue pending rebuild: %v", err)
	}
	sess.mu.Lock()
	maintenanceDone := sess.maintenanceDone
	queued := len(sess.pendingConfig) > 0
	sess.mu.Unlock()
	if maintenanceDone == nil || !queued {
		t.Fatalf("maintenance in flight = %v, pending queued = %v, want both", maintenanceDone != nil, queued)
	}

	svc.mu.Lock()
	close(factory.releaseFirst)
	select {
	case <-maintenanceDone: // closed after maintenanceDone is reset to nil
	case <-time.After(time.Second):
		svc.mu.Unlock()
		t.Fatal("maintenance did not finish")
	}
	if _, _, ok := sess.begin(context.Background()); ok {
		svc.mu.Unlock()
		t.Fatal("begin succeeded between maintenance end and pending config apply; the turn would run on the outgoing config")
	}
	svc.mu.Unlock()

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("first rebuild: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not finish")
	}

	_, cancel, ok := sess.begin(context.Background())
	if !ok {
		t.Fatal("begin failed after the pending config was applied")
	}
	cancel()
	sess.finish()
	if sess.model != "fast" {
		t.Fatalf("session model = %q, want pending fast", sess.model)
	}
	if got := factory.buildCount(); got != 2 {
		t.Fatalf("factory builds = %d, want 2", got)
	}
}

// planModeDriftProbeController lets a test pause emitModeDrift's read of
// PlanMode() at the exact point a concurrent config switch could otherwise
// race in: after finish() would have exposed the session as idle but before
// the drift correction lands on sess.modeID.
type planModeDriftProbeController struct {
	*control.Controller
	onPlanMode func()
}

func (c *planModeDriftProbeController) PlanMode() bool {
	if c.onPlanMode != nil {
		c.onPlanMode()
	}
	return c.Controller.PlanMode()
}

// TestACPFinishTurnReconcilesModeDriftBeforeExposingIdle pins the fix for the
// race where finish() exposed the session as idle before emitModeDrift
// corrected a controller-side Plan auto-exit. A concurrent work-mode switch
// landing in that window used to see sess.running already false, rebuild
// immediately from the stale "plan" modeID, and resurrect Plan mode on the
// replacement controller even though the controller had already exited it.
func TestACPFinishTurnReconcilesModeDriftBeforeExposingIdle(t *testing.T) {
	reachedDrift := make(chan struct{})
	releaseDrift := make(chan struct{})
	var once sync.Once
	realCtrl := control.New(control.Options{})
	realCtrl.SetPlanMode(false) // the turn already auto-exited Plan mode
	probe := &planModeDriftProbeController{
		Controller: realCtrl,
		onPlanMode: func() {
			once.Do(func() {
				close(reachedDrift)
				<-releaseDrift
			})
		},
	}

	sink := newUpdateSink(&fakeNotifier{}, "sess-drift-race")
	sess := &acpSession{
		id:     "sess-drift-race",
		ctrl:   probe,
		sink:   sink,
		cwd:    t.TempDir(),
		model:  "fast",
		modeID: sessionModePlan, // stale: not yet reconciled to the controller's actual state
	}
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{sess.id: sess}}

	if _, _, ok := sess.begin(context.Background()); !ok {
		t.Fatal("begin failed")
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		svc.finishTurn(context.Background(), sess)
	}()

	select {
	case <-reachedDrift:
	case <-time.After(time.Second):
		t.Fatal("mode drift check did not run")
	}

	// A concurrent work-mode switch races in here. Before the fix this landed
	// while sess.running was already false (finish() ran first), so it read
	// the stale "plan" modeID and rebuilt with Plan mode re-enabled. The drift
	// pass now holds stateChangeMu, so the switch runs from a goroutine: it
	// either queues behind the still-running turn or rebuilds only after the
	// drift correction landed — never from the stale modeID.
	switchDone := make(chan error, 1)
	go func() {
		_, err := svc.switchSessionRuntimeProfile(context.Background(), sess, "delivery")
		switchDone <- err
	}()

	close(releaseDrift)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("finishTurn did not complete")
	}
	select {
	case err := <-switchDone:
		if err != nil {
			t.Fatalf("switchSessionRuntimeProfile: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("work-mode switch did not complete")
	}

	if sess.currentCtrl().PlanMode() {
		t.Fatal("concurrent work-mode switch resurrected Plan mode after it had already exited")
	}
	if got := sess.currentModeID(); got != sessionModeNormal {
		t.Fatalf("session modeID = %q, want normal", got)
	}
}

// TestACPPendingConfigMergesAxesQueuedDuringActiveTurn pins the per-axis
// pending-config queue: a model change and a work-mode change both requested
// during one active turn must both apply when the turn ends and the queue
// drains. With the old single-slot queue the second request silently
// overwrote the first even though both RPCs had already reported success and
// announced their config_option_update to the client.
func TestACPPendingConfigMergesAxesQueuedDuringActiveTurn(t *testing.T) {
	factory := &configurableFactory{}
	sink := newUpdateSink(&fakeNotifier{}, "sess-pending-merge")
	sess := &acpSession{
		id:             "sess-pending-merge",
		ctrl:           control.New(control.Options{}),
		sink:           sink,
		cwd:            t.TempDir(),
		model:          "pro",
		runtimeProfile: "balanced",
	}
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	if _, _, ok := sess.begin(context.Background()); !ok {
		t.Fatal("begin failed")
	}

	if _, err := svc.switchSessionModel(context.Background(), sess, "fast"); err != nil {
		t.Fatalf("switchSessionModel during turn: %v", err)
	}
	if _, err := svc.switchSessionRuntimeProfile(context.Background(), sess, "delivery"); err != nil {
		t.Fatalf("switchSessionRuntimeProfile during turn: %v", err)
	}
	sess.mu.Lock()
	queued := len(sess.pendingConfig)
	sess.mu.Unlock()
	if queued != 2 {
		t.Fatalf("pending deltas = %d, want one per axis (2)", queued)
	}

	svc.finishTurn(context.Background(), sess)

	sess.mu.Lock()
	model, profile := sess.model, sess.runtimeProfile
	sess.mu.Unlock()
	if model != "fast" || profile != "delivery" {
		t.Fatalf("after drain model = %q, profile = %q; want fast/delivery (an axis queued during the turn was dropped)", model, profile)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("factory builds = %d, want a single merged rebuild", got)
	}
}

// TestACPApplyPendingClaimsStateBeforeResolving pins request order for one
// axis. The pending drain must own stateChangeMu before it clones/resolves the
// old value; otherwise a newer explicit switch can rebuild first and the stale
// clone then queues behind it, making the older request win last.
func TestACPApplyPendingClaimsStateBeforeResolving(t *testing.T) {
	factory := &blockingResolveFactory{
		proReached:   make(chan struct{}),
		releasePro:   make(chan struct{}),
		fastResolved: make(chan struct{}),
	}
	sess := &acpSession{
		id:             "sess-pending-order",
		ctrl:           control.New(control.Options{}),
		sink:           newUpdateSink(&fakeNotifier{}, "sess-pending-order"),
		cwd:            t.TempDir(),
		model:          "fast",
		runtimeProfile: "balanced",
		pendingConfig: []sessionConfigDelta{
			{axis: "model", model: "pro"},
			{axis: "work_mode", runtimeProfile: "delivery"},
		},
	}
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	applyDone := make(chan error, 1)
	go func() { applyDone <- svc.applyPendingSessionConfig(context.Background(), sess) }()
	select {
	case <-factory.proReached:
	case <-time.After(time.Second):
		t.Fatal("pending config did not reach blocked resolution")
	}

	claimed := !sess.stateChangeMu.TryLock()
	if !claimed {
		sess.stateChangeMu.Unlock()
	}

	newerDone := make(chan error, 1)
	go func() {
		_, err := svc.switchSessionModel(context.Background(), sess, "fast")
		newerDone <- err
	}()
	select {
	case <-factory.fastResolved:
	case <-time.After(time.Second):
		close(factory.releasePro)
		t.Fatal("newer model request did not resolve")
	}
	close(factory.releasePro)
	if !claimed {
		t.Fatal("pending apply resolved without stateChangeMu; a newer same-axis request can overtake it")
	}

	select {
	case err := <-applyDone:
		if err != nil {
			t.Fatalf("applyPendingSessionConfig: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending apply did not finish")
	}
	select {
	case err := <-newerDone:
		if err != nil {
			t.Fatalf("newer switchSessionModel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("newer model request did not finish")
	}
	if got := sess.model; got != "fast" {
		t.Fatalf("session model = %q, want latest requested value fast", got)
	}
	if got := sess.runtimeProfile; got != "delivery" {
		t.Fatalf("runtime profile = %q, want pending different-axis value delivery preserved", got)
	}
}

// TestACPFailedRebuildStillDrainsNewerPendingConfig covers a failed build with
// a newer request queued during maintenance. The newer request already returned
// success, so it must still apply and clear the queue even though the older
// rebuild reports its own failure.
func TestACPFailedRebuildStillDrainsNewerPendingConfig(t *testing.T) {
	factory := &failFirstBuildFactory{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	sess := &acpSession{
		id:             "sess-failed-drain",
		ctrl:           control.New(control.Options{}),
		sink:           newUpdateSink(&fakeNotifier{}, "sess-failed-drain"),
		cwd:            t.TempDir(),
		model:          "fast",
		runtimeProfile: "balanced",
	}
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.switchSessionModel(context.Background(), sess, "pro")
		firstDone <- err
	}()
	select {
	case <-factory.started:
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not start")
	}

	if _, err := svc.switchSessionModel(context.Background(), sess, "fast"); err != nil {
		t.Fatalf("queue newer model request: %v", err)
	}
	close(factory.release)
	select {
	case err := <-firstDone:
		if err == nil || !strings.Contains(err.Error(), "first build failed") {
			t.Fatalf("first rebuild error = %v, want first build failed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not finish")
	}

	if got := sess.model; got != "fast" {
		t.Fatalf("session model = %q, want newer pending value fast", got)
	}
	sess.mu.Lock()
	queued := len(sess.pendingConfig)
	sess.mu.Unlock()
	if queued != 0 {
		t.Fatalf("pending config entries = %d, want drained after failed maintenance", queued)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("successful replacement builds = %d, want one pending rebuild", got)
	}
	_, cancel, ok := sess.begin(context.Background())
	if !ok {
		t.Fatal("session stayed blocked after failed rebuild drained its pending request")
	}
	cancel()
	sess.finish()
}

func TestACPReportPendingConfigFailureRestoresClientState(t *testing.T) {
	notifier := &fakeNotifier{}
	sess := &acpSession{
		id:               "sess-pending-failure-update",
		ctrl:             control.New(control.Options{}),
		sink:             newUpdateSink(notifier, "sess-pending-failure-update"),
		cwd:              t.TempDir(),
		model:            "fast",
		runtimeProfile:   "balanced",
		toolApprovalMode: control.ToolApprovalAsk,
	}
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{sess.id: sess}}

	svc.reportPendingSessionConfigError(context.Background(), sess, errors.New("replacement build failed"), "after maintenance")

	notifier.mu.Lock()
	notifs := append([]capturedNotif(nil), notifier.notifs...)
	notifier.mu.Unlock()
	found := false
	for _, notif := range notifs {
		raw, err := json.Marshal(notif.params)
		if err != nil {
			t.Fatalf("marshal notification: %v", err)
		}
		var payload struct {
			Update struct {
				SessionUpdate string                `json:"sessionUpdate"`
				ConfigOptions []SessionConfigOption `json:"configOptions"`
			} `json:"update"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode notification: %v", err)
		}
		if payload.Update.SessionUpdate != "config_option_update" {
			continue
		}
		model, ok := findConfigOption(payload.Update.ConfigOptions, "model")
		if !ok {
			t.Fatal("rollback config update omitted model option")
		}
		if model.CurrentValue != "fast" {
			t.Fatalf("rollback model = %q, want live value fast", model.CurrentValue)
		}
		found = true
	}
	if !found {
		t.Fatal("pending config failure did not restore the client's live config state")
	}
}

// staleModeReadController reads PlanMode before pausing, modelling the drift
// emitter capturing controller state that a concurrent session/set_mode then
// changes before the emitter swaps it into the session.
type staleModeReadController struct {
	*control.Controller
	onPlanMode func()
}

func (c *staleModeReadController) PlanMode() bool {
	v := c.Controller.PlanMode()
	if c.onPlanMode != nil {
		c.onPlanMode()
	}
	return v
}

// TestACPFinishTurnModeDriftDoesNotRevertConcurrentSetMode pins the fix for
// the drift emitters racing explicit user selections: emitModeDrift reads the
// controller without stateChangeMu, so a session/set_mode completing between
// that read and the modeID swap was read back as drift, rolled the session
// metadata back to the pre-selection mode, and the pending-config rebuild
// riding the same finishTurn re-applied the stale mode to the replacement
// controller — silently undoing the user's choice.
func TestACPFinishTurnModeDriftDoesNotRevertConcurrentSetMode(t *testing.T) {
	reachedDrift := make(chan struct{})
	releaseDrift := make(chan struct{})
	var once sync.Once
	realCtrl := control.New(control.Options{})
	probe := &staleModeReadController{
		Controller: realCtrl,
		onPlanMode: func() {
			once.Do(func() {
				close(reachedDrift)
				<-releaseDrift
			})
		},
	}

	sink := newUpdateSink(&fakeNotifier{}, "sess-setmode-race")
	sess := &acpSession{
		id:             "sess-setmode-race",
		ctrl:           probe,
		sink:           sink,
		cwd:            t.TempDir(),
		model:          "fast",
		runtimeProfile: "balanced",
		modeID:         sessionModeNormal,
	}
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{sess.id: sess}}

	if _, _, ok := sess.begin(context.Background()); !ok {
		t.Fatal("begin failed")
	}
	// A work-mode change queued during the turn makes finishTurn rebuild the
	// controller, which re-applies the session's modeID — the step that turned
	// the stale drift write-back into a durable loss of the user's selection.
	sess.mu.Lock()
	sess.pendingConfig = []sessionConfigDelta{{axis: "work_mode", runtimeProfile: "delivery"}}
	sess.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		svc.finishTurn(context.Background(), sess)
	}()

	select {
	case <-reachedDrift:
	case <-time.After(time.Second):
		t.Fatal("mode drift check did not run")
	}

	// The user picks Plan mode while the drift pass is between its controller
	// read and its swap. With stateChangeMu held by the drift pass this blocks
	// until the pass completes; without it, it lands here and gets reverted.
	setModeDone := make(chan error, 1)
	go func() {
		raw, err := json.Marshal(SessionSetModeParams{SessionID: sess.id, ModeID: sessionModePlan})
		if err != nil {
			setModeDone <- err
			return
		}
		_, err = svc.sessionSetMode(context.Background(), raw)
		setModeDone <- err
	}()
	// Bias the pre-fix interleaving: give set_mode time to complete inside the
	// paused window. Post-fix it is blocked on stateChangeMu regardless, so
	// this sleep cannot make the fixed behavior flaky.
	time.Sleep(50 * time.Millisecond)

	close(releaseDrift)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("finishTurn did not complete")
	}
	select {
	case err := <-setModeDone:
		if err != nil {
			t.Fatalf("sessionSetMode: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session/set_mode did not complete")
	}

	if got := sess.currentModeID(); got != sessionModePlan {
		t.Fatalf("session modeID = %q, want plan (drift pass reverted the user's set_mode)", got)
	}
	if !sess.currentCtrl().PlanMode() {
		t.Fatal("rebuilt controller lost Plan mode after concurrent set_mode")
	}
}

// TestACPDriftEmittersSerializeWithStateChanges pins the lock contract behind
// the fix above: both drift emitters must hold stateChangeMu, or they can race
// every other holder (session/set_mode, tool-approval switches, controller
// rebuilds) between their controller read and session-state swap.
func TestACPDriftEmittersSerializeWithStateChanges(t *testing.T) {
	sess := &acpSession{
		id:     "sess-drift-lock",
		ctrl:   control.New(control.Options{}),
		sink:   newUpdateSink(&fakeNotifier{}, "sess-drift-lock"),
		cwd:    t.TempDir(),
		model:  "fast",
		modeID: sessionModeNormal,
	}
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{sess.id: sess}}

	sess.stateChangeMu.Lock()
	done := make(chan struct{})
	go func() {
		svc.emitModeDrift(sess)
		svc.emitToolApprovalDrift(context.Background(), sess)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("drift emitters completed while stateChangeMu was held; they can race set_mode/tool-approval swaps")
	case <-time.After(100 * time.Millisecond):
	}
	sess.stateChangeMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drift emitters did not finish after stateChangeMu was released")
	}
}
