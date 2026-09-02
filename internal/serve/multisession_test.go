package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
)

type replayStubController struct{ *control.Controller }

func (replayStubController) SessionPath() string { return "/sessions/a.jsonl" }
func (replayStubController) ReplayPendingPromptsWith(factory func() event.Sink) {
	factory().Emit(event.Event{Kind: event.ApprovalRequest})
}

func sessionPathFromFrame(t *testing.T, frame []byte) string {
	t.Helper()
	var wired eventwire.Event
	if err := json.Unmarshal(frame, &wired); err != nil {
		t.Fatalf("decode event frame: %v\nframe=%s", err, frame)
	}
	return wired.SessionPath
}

type replayRaceController struct {
	control.SessionAPI
	path     string
	onPath   func()
	replayed chan string
}

func (c *replayRaceController) SessionPath() string {
	if c.onPath != nil {
		c.onPath()
	}
	return c.path
}

func (c *replayRaceController) ReplayPendingPromptsWith(factory func() event.Sink) {
	c.replayed <- c.path
	factory().Emit(event.Event{Kind: event.ApprovalRequest})
}

type closeProbeController struct {
	*control.Controller
	closed atomic.Bool
}

func (c *closeProbeController) Close() {
	c.closed.Store(true)
	c.Controller.Close()
}

type retiringCloseProbeController struct {
	*control.Controller
	closeStarted chan struct{}
	closeRelease chan struct{}
}

func (c *retiringCloseProbeController) Close() {
	close(c.closeStarted)
	<-c.closeRelease
	c.Controller.Close()
}

func TestDetachedSessionRemainsBusyUntilCloseFinishes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retiring.jsonl")
	ctrl := &retiringCloseProbeController{
		Controller:   control.New(control.Options{SessionPath: path}),
		closeStarted: make(chan struct{}),
		closeRelease: make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-ctrl.closeRelease:
		default:
			close(ctrl.closeRelease)
		}
	})
	server := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	tag := NewSessionTagSink(server.bc)
	tag.SetPath(path)
	detached, err := server.registerDetached(ctrl, nil, tag)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctrl.closeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("detached controller did not start retiring")
	}
	if !server.detachedBusy(path) {
		t.Fatal("retiring session disappeared before controller Close finished")
	}
	if got := server.takeDetached(path); got != nil {
		t.Fatal("retiring controller remained reattachable")
	}
	close(ctrl.closeRelease)
	select {
	case <-detached.done:
	case <-time.After(2 * time.Second):
		t.Fatal("detached retirement did not finish")
	}
	if server.detachedBusy(path) {
		t.Fatal("retired session remained busy after controller Close finished")
	}
}

func TestServerCloseClosesPublishedForegroundReplacement(t *testing.T) {
	bc := NewBroadcaster()
	first := &closeProbeController{Controller: control.New(control.Options{Sink: bc})}
	replacement := &closeProbeController{Controller: control.New(control.Options{Sink: bc})}
	server := New(first, bc, config.ServeConfig{})
	if !server.publishControllerSwap(first, replacement, replacement.SessionPath()) {
		t.Fatal("replacement publication failed")
	}
	server.Close()
	if !replacement.closed.Load() {
		t.Fatal("server shutdown left the published foreground controller open")
	}
}

func TestStaleRecoveryCannotOverwritePublishedForegroundRoute(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	recoveryPath := filepath.Join(dir, "old-recovery.jsonl")
	bc := NewBroadcaster()
	old := control.New(control.Options{SessionPath: oldPath})
	next := control.New(control.Options{SessionPath: newPath})
	server := New(old, bc, config.ServeConfig{})
	tag := NewSessionTagSink(bc)
	server.RegisterSessionTag(old, tag)
	recoverOld := server.sessionRecoveryHandler(old, nil)
	if !server.publishControllerSwap(old, next, newPath) {
		t.Fatal("foreground replacement publication failed")
	}
	if err := recoverOld(control.SessionRecoveryInfo{RecoveryPath: recoveryPath}); err != nil {
		t.Fatal(err)
	}
	if got, want := bc.CurrentSession(), agent.CanonicalSessionPath(newPath); got != want {
		t.Fatalf("stale recovery changed foreground route to %q, want %q", got, want)
	}
}

func TestCapturedRecoveryCallbackFollowsDetachedKeeper(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	targetPath := filepath.Join(dir, "target.jsonl")
	recoveryPath := filepath.Join(dir, "old-recovery.jsonl")
	old := control.New(control.Options{SessionPath: oldPath})
	server := New(old, NewBroadcaster(), config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(oldPath); err != nil {
		t.Fatal(err)
	}
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	captured := server.sessionRecoveryHandler(old, leases)
	detached, err := leases.RebindDetaching(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer detached.Release()
	replacement := control.New(control.Options{SessionPath: targetPath})
	if err := leases.BindControllerAuthority(replacement); err != nil {
		t.Fatal(err)
	}
	if err := captured(control.SessionRecoveryInfo{RecoveryPath: recoveryPath}); err != nil {
		t.Fatal(err)
	}
	if got, want := detached.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("captured recovery moved detached keeper to %q, want %q", got, want)
	}
	if got, want := leases.HeldPath(), agent.CanonicalSessionPath(targetPath); got != want {
		t.Fatalf("captured recovery corrupted foreground keeper: got %q, want %q", got, want)
	}
}

func TestResumeActiveSessionTreatsSymlinkAliasAsCurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink alias identity is exercised on POSIX CI")
	}
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.jsonl")
	aliasPath := filepath.Join(dir, "alias.jsonl")
	saveServeTestSession(t, realPath)
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{Runner: blockingRunner{}, SessionDir: dir, SessionPath: aliasPath})
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	ctrl.Submit("keep running")
	waitRunning(t, ctrl)
	defer func() {
		ctrl.Cancel()
		waitNotRunning(t, ctrl)
	}()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resume", nil)
	if !server.resumeActiveSession(rec, req, ctrl, realPath) {
		t.Fatal("active current-session alias was not handled")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("resume through current-session symlink alias = %d, want 204", rec.Code)
	}
	if server.ctl() != control.SessionAPI(ctrl) || !ctrl.Running() {
		t.Fatal("current-session alias detached or replaced the active controller")
	}
}

func TestBusyNewRejectsUntaggedLegacyController(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: blockingRunner{}, Sink: bc, SessionDir: dir, SessionPath: path})
	server := New(ctrl, bc, config.ServeConfig{})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	defer ctrl.Close()
	ctrl.Submit("keep running")
	waitRunning(t, ctrl)
	resp, err := http.Post(httpServer.URL+"/new", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("untagged busy /new = %d, want 409: %s", resp.StatusCode, body)
	}
	if server.ctl() != control.SessionAPI(ctrl) || !ctrl.Running() {
		t.Fatal("legacy controller was detached without a session tag")
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
}

func TestReplayPendingPromptsBroadcastTagsFrames(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	server := New(replayStubController{ctrl}, bc, config.ServeConfig{})
	all, stop := bc.SubscribeAll()
	defer stop()
	server.replayPendingPromptsBroadcast()
	select {
	case frame := <-all:
		if got, want := sessionPathFromFrame(t, frame), agent.CanonicalSessionPath("/sessions/a.jsonl"); got != want {
			t.Fatalf("replayed frame session path = %q, want %q: %s", got, want, frame)
		}
	default:
		t.Fatal("pending prompt was not replayed")
	}
}

func TestEventsReplayUsesControllerCapturedWithPath(t *testing.T) {
	bc := NewBroadcaster()
	baseA := control.New(control.Options{Sink: bc})
	baseB := control.New(control.Options{Sink: bc})
	replayed := make(chan string, 2)
	a := &replayRaceController{SessionAPI: baseA, path: "/sessions/a.jsonl", replayed: replayed}
	b := &replayRaceController{SessionAPI: baseB, path: "/sessions/b.jsonl", replayed: replayed}
	server := New(a, bc, config.ServeConfig{})
	promotionStarted := make(chan struct{})
	promotionDone := make(chan struct{})
	a.onPath = func() {
		a.onPath = nil
		go func() {
			close(promotionStarted)
			server.bindMu.Lock()
			if !server.publishControllerSwap(a, b, b.path) {
				t.Error("controller promotion failed")
			}
			bc.Emit(event.Event{Kind: event.AskRequest, SessionPath: b.path})
			server.bindMu.Unlock()
			close(promotionDone)
		}()
		<-promotionStarted
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpServer.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	nextData := func() string {
		t.Helper()
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
				return strings.TrimPrefix(line, "data: ")
			}
		}
		t.Fatalf("event stream ended before the next frame: %v", scanner.Err())
		return ""
	}
	if first := nextData(); sessionPathFromFrame(t, []byte(first)) != agent.CanonicalSessionPath(a.path) {
		t.Fatalf("first frame = %s, want controller A replay", first)
	}
	select {
	case <-promotionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("controller promotion remained blocked after subscription")
	}
	if second := nextData(); sessionPathFromFrame(t, []byte(second)) != agent.CanonicalSessionPath(b.path) {
		t.Fatalf("second frame = %s, want promoted controller B prompt", second)
	}
}

func TestSlashNewRefreshesControllerTagAndForegroundRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "current.jsonl")
	saveServeTestSession(t, path)
	bc := NewBroadcaster()
	tag := NewSessionTagSink(bc)
	tag.SetPath(path)
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, tag)
	ctrl := control.New(control.Options{Executor: exec, Sink: tag, SessionDir: dir, SessionPath: path, Label: "test"})
	server := New(ctrl, bc, config.ServeConfig{})
	server.RegisterSessionTag(ctrl, tag)
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(path); err != nil {
		t.Fatal(err)
	}
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	all, stop := bc.SubscribeAll()
	defer stop()
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	resp, err := http.Post(httpServer.URL+"/submit", "application/json", strings.NewReader(`{"input":"/new"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("slash /new submit status = %d, want 204", resp.StatusCode)
	}
	select {
	case frame := <-all:
		deadline := time.After(2 * time.Second)
		for !strings.Contains(string(frame), `"kind":"notice"`) || !strings.Contains(string(frame), `"text":"new session"`) {
			select {
			case frame = <-all:
			case <-deadline:
				t.Fatal("slash /new did not publish its completion notice")
			}
		}
		newPath := agent.CanonicalSessionPath(ctrl.SessionPath())
		if newPath == agent.CanonicalSessionPath(path) || tag.Path() != newPath || bc.CurrentSession() != newPath {
			t.Fatalf("slash /new routing = controller %q tag %q broadcaster %q frame=%s", newPath, tag.Path(), bc.CurrentSession(), frame)
		}
		if got := sessionPathFromFrame(t, frame); got != newPath {
			t.Fatalf("slash /new notice session path = %q, want %q: %s", got, newPath, frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slash /new emitted no event")
	}
}

type backgroundJobOnlyController struct{ *control.Controller }

func (c *backgroundJobOnlyController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{BackgroundJobs: 1, Cancellable: true}
}

type detachedProviderHealProbe struct {
	*control.Controller
	closed atomic.Bool
}

func (c *detachedProviderHealProbe) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{BackgroundJobs: 1, Cancellable: true}
}

func (c *detachedProviderHealProbe) Close() {
	c.closed.Store(true)
	c.Controller.Close()
}

func TestProviderHealSynchronouslyRetiresDetachedControllers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background.jsonl")
	ctrl := &detachedProviderHealProbe{Controller: control.New(control.Options{SessionPath: path})}
	server := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	tag := NewSessionTagSink(server.bc)
	tag.SetPath(path)
	if _, err := server.registerDetached(ctrl, nil, tag); err != nil {
		t.Fatal(err)
	}
	server.retireDetachedForProviderHeal()
	if !ctrl.closed.Load() {
		t.Fatal("provider heal returned before the detached controller closed")
	}
	if server.detachedBusy(path) {
		t.Fatal("provider heal left the detached controller reattachable")
	}
}

func TestSessionsReportsForegroundBackgroundJobsAsRunning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.jsonl")
	saveServeTestSession(t, path)
	ctrl := &backgroundJobOnlyController{Controller: control.New(control.Options{SessionDir: dir, SessionPath: path})}
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	rec := httptest.NewRecorder()
	server.sessions(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Current || !rows[0].Running {
		t.Fatalf("foreground background-job session = %+v, want current and running", rows)
	}
}

func TestBuildTaggedInheritsWorkspacePlacement(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: sessionDir, WorkspaceRoot: root})
	server := New(ctrl, bc, config.ServeConfig{})
	server.SetControllerBuildOptions(boot.Options{MaxSteps: 7, MaxStepsKey: "--max-steps", AgentPreset: "delivery"})
	var got boot.Options
	server.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		got = opts
		return control.New(control.Options{Sink: opts.Sink, SessionDir: opts.SessionDir, WorkspaceRoot: opts.WorkspaceRoot}), nil
	}
	built, _, err := server.buildTagged(context.Background(), "provider/model", false)
	if err != nil {
		t.Fatal(err)
	}
	defer built.Close()
	if got.SessionDir != sessionDir || got.WorkspaceRoot != root {
		t.Fatalf("build placement = (%q, %q), want (%q, %q)", got.SessionDir, got.WorkspaceRoot, sessionDir, root)
	}
	if got.MaxSteps != 7 || got.MaxStepsKey != "--max-steps" || got.AgentPreset != "delivery" {
		t.Fatalf("process build options = max:%d key:%q preset:%q, want 7/--max-steps/delivery", got.MaxSteps, got.MaxStepsKey, got.AgentPreset)
	}
}

func TestSessionTagSinkBuffersUntilReplacementCommit(t *testing.T) {
	bc := NewBroadcaster()
	all, stop := bc.SubscribeAll()
	defer stop()
	tag := NewSessionTagSink(bc)
	path := filepath.Join(t.TempDir(), "replacement.jsonl")

	tag.Emit(event.Event{Kind: event.Notice, Text: "booted"})
	tag.PrimePath(path)
	if len(all) != 0 {
		t.Fatal("replacement boot event leaked before publication committed")
	}
	tag.Activate()
	if len(all) != 1 {
		t.Fatalf("activation flushed %d frames, want 1", len(all))
	}
	var frame eventwire.Event
	if err := json.Unmarshal(<-all, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Kind != "notice" || frame.SessionPath != agent.CanonicalSessionPath(path) {
		t.Fatalf("activated boot frame = %+v", frame)
	}
}

func TestBuildTaggedFailureDiscardsBufferedBootEvents(t *testing.T) {
	bc := NewBroadcaster()
	all, stop := bc.SubscribeAll()
	defer stop()
	server := New(control.New(control.Options{}), bc, config.ServeConfig{})
	server.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		opts.Sink.Emit(event.Event{Kind: event.Notice, Text: "booting replacement"})
		return nil, errors.New("build failed")
	}
	if _, _, err := server.buildTagged(context.Background(), "provider/model", false); err == nil {
		t.Fatal("buildTagged unexpectedly succeeded")
	}
	if len(all) != 0 {
		t.Fatal("failed replacement leaked a buffered boot event")
	}
}

type balanceProbeController struct {
	*control.Controller
	calls atomic.Int32
}

func (c *balanceProbeController) Balance(context.Context) (*billing.Balance, error) {
	c.calls.Add(1)
	return &billing.Balance{Available: true}, nil
}

func (c *balanceProbeController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{Running: true, PendingPrompt: true, Cancellable: true}
}

func TestStatusRuntimeQuerySkipsBalance(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := &balanceProbeController{Controller: control.New(control.Options{Sink: bc})}
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	full, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, full.Body)
	full.Body.Close()
	before := ctrl.calls.Load()
	if before == 0 {
		t.Fatal("full status did not fetch balance")
	}
	lite, err := http.Get(srv.URL + "/status?runtime=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(lite.Body)
	lite.Body.Close()
	if ctrl.calls.Load() != before {
		t.Fatal("runtime status fetched balance")
	}
	for _, want := range []string{`"running":true`, `"pendingPrompt":true`, `"cancellable":true`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("runtime status missing %s: %s", want, body)
		}
	}
}

func TestDetachedRecoveryMovesRegistryKey(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	recoveryPath := filepath.Join(dir, "old-recovery.jsonl")
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: oldPath})
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	detached := &detachedSession{path: oldPath, ctrl: ctrl}
	server.detached[oldPath] = detached
	if err := server.moveDetachedRecovery(ctrl, recoveryPath); err != nil {
		t.Fatal(err)
	}
	canonical := agent.CanonicalSessionPath(recoveryPath)
	if got := server.detached[canonical]; got != detached || detached.path != canonical {
		t.Fatalf("recovery registry = %+v path=%q", got, detached.path)
	}
	if server.detached[oldPath] != nil {
		t.Fatal("old detached registry key was retained")
	}
}

func TestRegisterDetachedRevalidatesPathAtPublication(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "recovery.jsonl")
	saveServeTestSession(t, oldPath)
	saveServeTestSession(t, newPath)
	ctrl := control.New(control.Options{Runner: blockingRunner{}, SessionDir: dir, SessionPath: oldPath})
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	tag := NewSessionTagSink(server.bc)
	server.RegisterSessionTag(ctrl, tag)
	started, release := make(chan struct{}), make(chan struct{})
	registerDetachedHookForTest = func() { close(started); <-release }
	t.Cleanup(func() { registerDetachedHookForTest = nil })
	result := make(chan *detachedSession, 1)
	go func() { detached, _ := server.registerDetached(ctrl, nil, tag); result <- detached }()
	<-started
	loaded, err := agent.LoadSession(newPath)
	if err != nil {
		t.Fatal(err)
	}
	ctrl.Resume(loaded, newPath)
	ctrl.Submit("keep running")
	waitRunning(t, ctrl)
	close(release)
	detached := <-result
	canonical := agent.CanonicalSessionPath(newPath)
	server.detachedMu.Lock()
	registered := server.detached[canonical]
	server.detachedMu.Unlock()
	if detached == nil || detached.path != canonical || registered != detached {
		t.Fatalf("detached publication path = %q entry=%v, want %q", detached.path, registered == detached, canonical)
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
	server.CloseBackground()
}

func TestBusyResumeDetachesAndReattachesRunningController(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.jsonl")
	bPath := filepath.Join(dir, "b.jsonl")
	saveServeTestSession(t, aPath)
	saveServeTestSession(t, bPath)

	bc := NewBroadcaster()
	tag := NewSessionTagSink(bc)
	tag.SetPath(aPath)
	ctrlA := control.New(control.Options{Runner: blockingRunner{}, Sink: tag, SessionDir: dir, SessionPath: aPath, Label: "test"})
	server := New(ctrlA, bc, config.ServeConfig{})
	server.RegisterSessionTag(ctrlA, tag)
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(aPath); err != nil {
		t.Fatal(err)
	}
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	server.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		return control.New(control.Options{Runner: blockingRunner{}, Sink: opts.Sink, SessionDir: opts.SessionDir, WorkspaceRoot: opts.WorkspaceRoot, Label: "test"}), nil
	}
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	defer server.CloseBackground()

	ctrlA.Submit("keep running")
	waitRunning(t, ctrlA)
	postResume := func(path string) {
		payload, _ := json.Marshal(map[string]string{"path": path})
		resp, err := http.Post(srv.URL+"/resume", "application/json", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("resume %q = %d: %s", path, resp.StatusCode, body)
		}
	}

	postResume(bPath)
	wantB, err := filepath.EvalSymlinks(bPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Clean(server.ctl().SessionPath()); got != filepath.Clean(wantB) {
		t.Fatalf("foreground session = %q, want b", got)
	}
	if !ctrlA.Running() {
		t.Fatal("switched-away session stopped instead of running in background")
	}
	postResume(aPath)
	if server.ctl() != control.SessionAPI(ctrlA) {
		t.Fatal("reattach did not restore the original controller")
	}
	if !ctrlA.Running() {
		t.Fatal("running turn was lost during reattach")
	}
	ctrlA.Cancel()
	waitNotRunning(t, ctrlA)
}

func TestDetachedRecoveryKeepsServeRoutingWrapper(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.jsonl")
	bPath := filepath.Join(dir, "b.jsonl")
	saveServeTestSession(t, aPath)
	saveServeTestSession(t, bPath)
	loaded, err := agent.LoadSession(aPath)
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	tag := NewSessionTagSink(bc)
	tag.SetPath(aPath)
	exec := agent.New(nil, nil, loaded, agent.Options{}, tag)
	ctrlA := control.New(control.Options{Runner: blockingRunner{}, Executor: exec, Sink: tag, SessionDir: dir, SessionPath: aPath, Label: "test"})
	server := New(ctrlA, bc, config.ServeConfig{})
	server.RegisterSessionTag(ctrlA, tag)
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(aPath); err != nil {
		t.Fatal(err)
	}
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	server.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		return control.New(control.Options{Sink: opts.Sink, SessionDir: opts.SessionDir, Label: "test"}), nil
	}
	ctrlA.Submit("keep running")
	waitRunning(t, ctrlA)
	if err := server.busyDetach(context.Background(), ctrlA, bPath, func(next *control.Controller) error {
		session, loadErr := agent.LoadSession(bPath)
		if loadErr == nil {
			next.Resume(session, bPath)
		}
		return loadErr
	}); err != nil {
		t.Fatal(err)
	}
	disk, err := agent.LoadSession(aPath)
	if err != nil {
		t.Fatal(err)
	}
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk diverged"})
	if err := disk.Save(aPath); err != nil {
		t.Fatal(err)
	}
	ctrlA.Executor().Session().Add(provider.Message{Role: provider.RoleUser, Content: "local diverged"})
	if err := ctrlA.Snapshot(); err != nil {
		t.Fatal(err)
	}
	recoveryPath := agent.CanonicalSessionPath(ctrlA.SessionPath())
	if recoveryPath == agent.CanonicalSessionPath(aPath) {
		t.Fatal("detached controller did not move to a recovery transcript")
	}
	server.detachedMu.Lock()
	detached := server.detached[recoveryPath]
	oldEntry := server.detached[agent.CanonicalSessionPath(aPath)]
	server.detachedMu.Unlock()
	if detached == nil || detached.ctrl != control.SessionAPI(ctrlA) || oldEntry != nil || tag.Path() != recoveryPath {
		t.Fatalf("detached recovery routing = entry %v old %v tag %q want %q", detached != nil, oldEntry != nil, tag.Path(), recoveryPath)
	}
	ctrlA.Cancel()
	waitNotRunning(t, ctrlA)
	server.CloseBackground()
}
