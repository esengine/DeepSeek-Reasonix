package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
)

// withForeignWriterLease models the local writer as a separate process. The
// real probe answers false for leases held by the calling process, so tests
// substitute one backed by the writer lease they hold in-process.
func withForeignWriterLease(t *testing.T, session string, held *atomic.Bool) {
	t.Helper()
	prev := leaseHeldByForeignRuntime
	canonical := agent.CanonicalSessionPath(session)
	leaseHeldByForeignRuntime = func(path string) bool {
		return held.Load() && agent.CanonicalSessionPath(path) == canonical
	}
	t.Cleanup(func() { leaseHeldByForeignRuntime = prev })
}

// extendSessionOnDisk appends a message as the local writer would: load the
// transcript (establishing its CAS baseline), add the turn, save.
func extendSessionOnDisk(t *testing.T, path, content string) {
	t.Helper()
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("writer load: %v", err)
	}
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: content})
	if err := loaded.Save(path); err != nil {
		t.Fatalf("writer save: %v", err)
	}
}

// runningForeverController keeps RuntimeStatus busy so drain-mode handoff has
// something to wait on.
type runningForeverController struct {
	*control.Controller
}

func (c *runningForeverController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{Running: true}
}

type snapshotFailController struct {
	*control.Controller
}

func (c *snapshotFailController) Snapshot() error { return fmt.Errorf("snapshot failed") }

type ownershipFixture struct {
	server *Server
	srv    *httptest.Server
	leases *control.SessionLeaseKeeper
	active string
	dir    string
	grant  mirrorGrant
}

func newOwnershipFixture(t *testing.T) *ownershipFixture {
	t.Helper()
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	saveServeTestSession(t, active)

	bc := NewBroadcaster()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, bc)
	ctrl := control.New(control.Options{Executor: exec, Sink: bc, SessionDir: dir, SessionPath: active})
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	t.Cleanup(func() {
		leases.Release()
		ctrl.Close()
	})
	fixture := &ownershipFixture{server: server, leases: leases, active: active, dir: dir}
	fixture.srv = httptest.NewServer(server.Handler())
	t.Cleanup(fixture.srv.Close)
	return fixture
}

func (f *ownershipFixture) post(t *testing.T, path string, body any) (int, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(f.srv.URL+path, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, strings.TrimSpace(buf.String())
}

func (f *ownershipFixture) get(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(f.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, strings.TrimSpace(buf.String())
}

func (f *ownershipFixture) ownershipView(t *testing.T, session string) ownershipView {
	t.Helper()
	status, body := f.get(t, "/ownership?session="+filepath.ToSlash(session))
	if status != http.StatusOK {
		t.Fatalf("GET /ownership status = %d (body %q)", status, body)
	}
	var view ownershipView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode ownership view: %v (body %q)", err, body)
	}
	return view
}

// handoffForce performs the takeover a confirmed local window would issue.
func (f *ownershipFixture) handoffForce(t *testing.T, mode string) (int, string) {
	t.Helper()
	status, body := f.post(t, "/handoff", map[string]any{
		"sessionPath":    f.active,
		"targetWriterId": agent.SessionWriterID(),
		"force":          true,
		"mode":           mode,
		"timeoutMs":      3000,
	})
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &f.grant); err != nil {
			t.Fatalf("decode handoff grant: %v", err)
		}
	}
	return status, body
}

func (f *ownershipFixture) acquireHandedOff(t *testing.T) *agent.SessionLease {
	t.Helper()
	lease, err := agent.TryAcquireSessionLeaseWithHandoff(f.active, f.grant.SourceWriterID, f.grant.HandoffID)
	if err != nil {
		t.Fatalf("acquire handed-off lease: %v", err)
	}
	return lease
}

// TestOwnershipReportsHolderStates covers the free / serve / other triangle a
// takeover prompt is built from.
func TestOwnershipReportsHolderStates(t *testing.T) {
	f := newOwnershipFixture(t)

	if view := f.ownershipView(t, f.active); view.Holder != "serve" {
		t.Fatalf("foreground holder = %q, want serve", view.Holder)
	}

	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)
	if view := f.ownershipView(t, other); view.Holder != "free" {
		t.Fatalf("untouched session holder = %q, want free", view.Holder)
	}

	// A writer in another process holds the lease; the in-process test lease
	// would read as "self", so model it through the probe seam.
	var held atomic.Bool
	held.Store(true)
	withForeignWriterLease(t, other, &held)
	if view := f.ownershipView(t, other); view.Holder != "other" {
		t.Fatalf("foreign-held session holder = %q, want other", view.Holder)
	}
	held.Store(false)
}

// TestHandoffReleasesLeaseAndGatesMutations walks the core takeover: after a
// forced handoff the local side can acquire the lease, every foreground
// mutation is refused with the takeover wording, /history follows the file
// (the writer's turns, not Serve's frozen memory), and /status flags
// takenOver for the remote surface.
func TestHandoffReleasesLeaseAndGatesMutations(t *testing.T) {
	f := newOwnershipFixture(t)

	status, body := f.handoffForce(t, "wait")
	if status != http.StatusOK {
		t.Fatalf("handoff status = %d, want 200 (body %q)", status, body)
	}

	writerLease := f.acquireHandedOff(t)
	defer writerLease.Release()

	if view := f.ownershipView(t, f.active); view.Holder != "external" || !view.Mirrored || !view.TakenOver {
		t.Fatalf("post-handoff ownership = %+v, want external mirror", view)
	}

	status, body = f.post(t, "/submit", map[string]string{"input": "hello"})
	if status != http.StatusConflict || !strings.Contains(body, "taken over by a local Reasonix") {
		t.Fatalf("mirrored submit = %d %q, want 409 takeover refusal", status, body)
	}

	// The writer extends the transcript; Serve must serve the file's content.
	extendSessionOnDisk(t, f.active, "writer turn")
	status, body = f.get(t, "/history")
	if status != http.StatusOK || !strings.Contains(body, "writer turn") {
		t.Fatalf("mirrored history = %d %q, want the writer's turn from disk", status, body)
	}

	status, body = f.get(t, "/status?runtime=1")
	if status != http.StatusOK || !strings.Contains(body, `"takenOver":true`) {
		t.Fatalf("mirrored status = %d %q, want takenOver", status, body)
	}

	status, body = f.get(t, "/sessions")
	if status != http.StatusOK || !strings.Contains(body, `"takenOver":true`) {
		t.Fatalf("sessions list = %d %q, want takenOver row", status, body)
	}
}

func TestHandoffSnapshotFailureKeepsServeLeaseAndMirrorUnpublished(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	saveServeTestSession(t, active)
	bc := NewBroadcaster()
	base := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	ctrl := &snapshotFailController{Controller: base}
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	defer base.Close()
	if err := leases.Rebind(active); err != nil {
		t.Fatal(err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	payload, _ := json.Marshal(map[string]any{
		"sessionPath": active, "targetWriterId": "target", "force": true, "mode": "wait",
	})
	resp, err := http.Post(srv.URL+"/handoff", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	body := readAllOrFatal(t, resp)
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(body, "snapshot") {
		t.Fatalf("handoff snapshot failure = %d %q, want 500", resp.StatusCode, body)
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(active) {
		t.Fatalf("held path = %q, want active", got)
	}
	if _, ok := server.mirroredEntry(active); ok {
		t.Fatal("snapshot failure published a mirror")
	}
}

// TestHandoffRefusedWhileAttachedWithoutForce proves an unconfirmed takeover
// cannot yank the session while a remote client is watching.
func TestHandoffRefusedWhileAttachedWithoutForce(t *testing.T) {
	f := newOwnershipFixture(t)
	events := subscribeServeEvents(t, f.srv.URL+"/events?all=1")
	defer events.close()

	status, body := f.post(t, "/handoff", map[string]any{"sessionPath": f.active, "targetWriterId": agent.SessionWriterID()})
	if status != http.StatusConflict || !strings.Contains(body, "force") {
		t.Fatalf("unforced handoff with subscriber = %d %q, want 409 force guidance", status, body)
	}
	if view := f.ownershipView(t, f.active); view.Holder != "serve" {
		t.Fatalf("holder after refused handoff = %q, want serve", view.Holder)
	}
}

// TestExternalFramesReachSubscriber proves the mirror: after takeover the
// writer's frames land on the remote SSE stream tagged to the mirrored
// session and marked current, and heartbeats surface reclaim requests.
func TestExternalFramesReachSubscriber(t *testing.T) {
	f := newOwnershipFixture(t)
	events := subscribeServeEvents(t, f.srv.URL+"/events?all=1")
	defer events.close()

	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff status = %d (body %q)", status, body)
	}
	// Drain the takeover notice, then push a writer frame.
	var notice eventwire.Event
	if err := events.next(&notice, 3*time.Second); err != nil || notice.Code != "session_taken_over" {
		t.Fatalf("expected taken_over notice first, got %+v (%v)", notice, err)
	}

	status, body := f.post(t, "/external/frames", map[string]any{
		"sessionPath": f.active,
		"mirrorId":    f.grant.MirrorID,
		"frames":      []map[string]any{{"kind": "text", "text": "writer says hi"}},
	})
	if status != http.StatusOK {
		t.Fatalf("external frames status = %d (body %q)", status, body)
	}

	var frame eventwire.Event
	if err := events.next(&frame, 3*time.Second); err != nil {
		t.Fatalf("subscriber did not receive mirrored frame: %v", err)
	}
	canonical := agent.CanonicalSessionPath(f.active)
	if frame.Kind != "text" || frame.Text != "writer says hi" || frame.SessionPath != canonical || !frame.SessionCurrent {
		t.Fatalf("mirrored frame = %+v, want current text frame on %q", frame, canonical)
	}

	// A heartbeat (empty frames) reports no pending reclaim.
	var resp externalFramesResponse
	status, body = f.post(t, "/external/frames", map[string]any{"sessionPath": f.active, "mirrorId": f.grant.MirrorID, "frames": []map[string]any{}})
	if status != http.StatusOK || json.Unmarshal([]byte(body), &resp) != nil || resp.ReclaimRequested {
		t.Fatalf("heartbeat = %d %q, want reclaimRequested=false", status, body)
	}
}

// TestReclaimRestoresRemoteOwnership covers the reverse transition: the
// remote side reclaims, the local writer sees the request on its heartbeat,
// yields the lease, and Serve re-owns the session with the writer's turns
// reloaded from disk.
func TestReclaimRestoresRemoteOwnership(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff status = %d (body %q)", status, body)
	}

	writerLease := f.acquireHandedOff(t)
	var writerHeld atomic.Bool
	writerHeld.Store(true)
	withForeignWriterLease(t, f.active, &writerHeld)
	extendSessionOnDisk(t, f.active, "writer turn")

	type reclaimResult struct {
		status int
		body   string
	}
	done := make(chan reclaimResult, 1)
	go func() {
		status, body := f.post(t, "/reclaim", map[string]any{
			"sessionPath": f.active,
			"mode":        "wait",
			"timeoutMs":   5000,
		})
		done <- reclaimResult{status, body}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.ownershipView(t, f.active).ReclaimRequested {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if view := f.ownershipView(t, f.active); !view.ReclaimRequested {
		t.Fatal("reclaim request never became visible to the writer")
	}
	var heartbeat externalFramesResponse
	status, body := f.post(t, "/external/frames", map[string]any{"sessionPath": f.active, "mirrorId": f.grant.MirrorID, "frames": []map[string]any{}})
	if status != http.StatusOK || json.Unmarshal([]byte(body), &heartbeat) != nil || !heartbeat.ReclaimRequested {
		t.Fatalf("writer heartbeat = %d %q, want reclaimRequested=true", status, body)
	}

	writerHeld.Store(false)
	writerLease.Release()
	select {
	case res := <-done:
		if res.status != http.StatusNoContent {
			t.Fatalf("reclaim status = %d (body %q)", res.status, res.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not complete after the writer yielded")
	}

	if got := f.leases.HeldPath(); got != agent.CanonicalSessionPath(f.active) {
		t.Fatalf("post-reclaim lease = %q, want the reclaimed session", got)
	}
	if view := f.ownershipView(t, f.active); view.Holder != "serve" || view.Mirrored {
		t.Fatalf("post-reclaim ownership = %+v, want serve without mirror", view)
	}
	status, body = f.get(t, "/history")
	if status != http.StatusOK || !strings.Contains(body, "writer turn") {
		t.Fatalf("post-reclaim history = %d %q, want the writer's turn reloaded", status, body)
	}
}

// TestMirrorEndRequiresReleasedLease proves the writer's farewell only ends
// the mirror once its lease is actually gone, then hands speaking rights
// straight back to the remote side.
func TestMirrorEndRequiresReleasedLease(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff status = %d (body %q)", status, body)
	}

	writerLease := f.acquireHandedOff(t)
	var writerHeld atomic.Bool
	writerHeld.Store(true)
	withForeignWriterLease(t, f.active, &writerHeld)
	if status, body := f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": f.grant.MirrorID}); status != http.StatusConflict {
		t.Fatalf("mirror-end with live writer = %d %q, want 409", status, body)
	}
	if view := f.ownershipView(t, f.active); !view.Mirrored {
		t.Fatal("mirror was cleared under a live writer")
	}

	writerHeld.Store(false)
	writerLease.Release()
	if status, body := f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": f.grant.MirrorID}); status != http.StatusNoContent {
		t.Fatalf("mirror-end after release = %d %q, want 204", status, body)
	}
	if got := f.leases.HeldPath(); got != agent.CanonicalSessionPath(f.active) {
		t.Fatalf("post mirror-end lease = %q, want the session re-owned", got)
	}
	if view := f.ownershipView(t, f.active); view.Holder != "serve" || view.Mirrored {
		t.Fatalf("post mirror-end ownership = %+v, want serve without mirror", view)
	}
}

func TestMirrorEndLoadFailureKeepsMirrorRetryable(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff = %d %q", status, body)
	}
	writerLease := f.acquireHandedOff(t)
	if err := writerLease.ReleaseForHandoff(f.grant.SourceWriterID, f.grant.ReturnHandoffID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.SessionEventLogPath(f.active), []byte(`{"schema_version":999,"type":"replace"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, body := f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": f.grant.MirrorID})
	if status < http.StatusBadRequest {
		t.Fatalf("mirror-end with unloadable session = %d %q, want failure", status, body)
	}
	if _, ok := f.server.mirroredEntry(f.active); !ok {
		t.Fatal("load failure cleared mirror generation")
	}
}

func TestMirrorEndCommitFailureRestoresPreviousServeSession(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff = %d %q", status, body)
	}
	writerLease := f.acquireHandedOff(t)
	previousPath := filepath.Join(f.dir, "previous.jsonl")
	saveServeTestSession(t, previousPath)
	previousLoaded, err := agent.LoadSession(previousPath)
	if err != nil {
		t.Fatal(err)
	}
	previousCtrl := f.server.ctl()
	if err := f.leases.Rebind(previousPath); err != nil {
		t.Fatal(err)
	}
	if err := f.leases.BindSessionAuthority(previousLoaded); err != nil {
		t.Fatal(err)
	}
	previousCtrl.Resume(previousLoaded, previousPath)
	if concrete, ok := previousCtrl.(*control.Controller); ok {
		if err := f.leases.BindControllerAuthority(concrete); err != nil {
			t.Fatal(err)
		}
		f.server.setControllerPath(concrete, previousPath)
	}
	previousPath = f.leases.HeldPath()
	if err := writerLease.ReleaseForHandoff(f.grant.SourceWriterID, f.grant.ReturnHandoffID); err != nil {
		t.Fatal(err)
	}

	replacement := control.New(control.Options{SessionDir: f.dir, SessionPath: filepath.Join(f.dir, "replacement.jsonl")})
	defer replacement.Close()
	resumeBindHookForTest = func() {
		f.server.mu.Lock()
		f.server.ctrl = replacement
		f.server.mu.Unlock()
	}
	t.Cleanup(func() { resumeBindHookForTest = nil })
	status, _ := f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": f.grant.MirrorID})
	resumeBindHookForTest = nil
	f.server.mu.Lock()
	f.server.ctrl = previousCtrl
	f.server.mu.Unlock()
	if status != http.StatusConflict {
		t.Fatalf("commit-raced mirror-end status = %d, want 409", status)
	}
	if got := f.leases.HeldPath(); got != previousPath {
		t.Fatalf("restored lease = %q, want %q", got, previousPath)
	}
	if got := agent.CanonicalSessionPath(previousCtrl.SessionPath()); got != previousPath {
		t.Fatalf("restored controller path = %q, want %q", got, previousPath)
	}
	if err := previousCtrl.Snapshot(); err != nil {
		t.Fatalf("restored controller lost authority: %v", err)
	}
	if !f.server.sessionMirrored(f.active) {
		t.Fatal("commit failure cleared mirror generation")
	}
	if status, body := f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": f.grant.MirrorID}); status != http.StatusNoContent {
		t.Fatalf("retry mirror-end = %d %q, want 204", status, body)
	}
}

// TestHandoffWaitsOnRunningForeground proves drain mode refuses (rather than
// interrupting) while the foreground turn is still running, and that the
// refusal names the interrupt escape hatch.
func TestHandoffWaitsOnRunningForeground(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	saveServeTestSession(t, active)

	bc := NewBroadcaster()
	ctrl := &runningForeverController{Controller: control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})}
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatal(err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	payload, _ := json.Marshal(map[string]any{"sessionPath": active, "targetWriterId": agent.SessionWriterID(), "force": true, "mode": "wait", "timeoutMs": 400})
	resp, err := http.Post(srv.URL+"/handoff", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	body := readAllOrFatal(t, resp)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "mode=interrupt") {
		t.Fatalf("busy wait handoff = %d %q, want 409 with interrupt hint", resp.StatusCode, body)
	}
	if view := ownershipViewFromServer(t, srv, active); view.Holder != "serve" {
		t.Fatalf("holder after refused busy handoff = %q, want serve", view.Holder)
	}
}

// TestNewOnMirroredForegroundRotates proves /new still works for the remote
// user after a takeover: the mirrored controller cannot rotate in place, so
// Serve publishes a replacement and keeps the mirrored transcript untouched.
func TestNewOnMirroredForegroundRotates(t *testing.T) {
	f := newOwnershipFixture(t)
	// busyDetach needs a tagged foreground controller and an injectable
	// replacement builder (production would boot real providers).
	foreground, ok := f.server.ctl().(*control.Controller)
	if !ok {
		t.Fatal("foreground controller is not a *control.Controller")
	}
	f.server.RegisterSessionTag(foreground, NewSessionTagSink(f.server.bc))
	f.server.buildControllerWithOptions = func(_ context.Context, _ string, opts boot.Options) (*control.Controller, error) {
		return control.New(control.Options{Sink: opts.Sink, SessionDir: opts.SessionDir}), nil
	}

	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff status = %d (body %q)", status, body)
	}
	status, body := f.post(t, "/new", map[string]any{})
	if status != http.StatusNoContent {
		t.Fatalf("/new on mirrored foreground = %d %q, want 204", status, body)
	}

	canonical := agent.CanonicalSessionPath(f.active)
	if got := agent.CanonicalSessionPath(f.server.ctl().SessionPath()); got == canonical {
		t.Fatalf("/new kept the foreground on the mirrored session %q", got)
	}
	if !f.server.sessionMirrored(f.active) {
		t.Fatal("/new cleared the mirror; the local writer was cut off")
	}
	if held := f.leases.HeldPath(); held == canonical || held == "" {
		t.Fatalf("post-rotation lease = %q, want the replacement session", held)
	}
}

func readAllOrFatal(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return strings.TrimSpace(buf.String())
}

func ownershipViewFromServer(t *testing.T, srv *httptest.Server, session string) ownershipView {
	t.Helper()
	resp, err := http.Get(srv.URL + "/ownership?session=" + filepath.ToSlash(session))
	if err != nil {
		t.Fatal(err)
	}
	body := readAllOrFatal(t, resp)
	var view ownershipView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decode ownership view: %v (body %q)", err, body)
	}
	return view
}

// serveEventStream reads an SSE endpoint frame by frame.
type serveEventStream struct {
	lines chan string
	stop  chan struct{}
}

func subscribeServeEvents(t *testing.T, url string) *serveEventStream {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("subscribe %s status = %d", url, resp.StatusCode)
	}
	stream := &serveEventStream{lines: make(chan string, 64), stop: make(chan struct{})}
	go func() {
		defer close(stream.lines)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-stream.stop:
				return
			default:
			}
			line := scanner.Text()
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				select {
				case stream.lines <- data:
				case <-stream.stop:
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		select {
		case <-stream.stop:
		default:
			close(stream.stop)
		}
	})
	return stream
}

func (s *serveEventStream) next(out any, timeout time.Duration) error {
	select {
	case line := <-s.lines:
		return json.Unmarshal([]byte(line), out)
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for an SSE frame")
	}
}

func (s *serveEventStream) close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// TestAdoptRegistersDirectlyOpenedSession proves a local runtime that opened a
// session without any handoff can announce it to Serve, making it watchable
// and reclaimable by the remote side.
func TestAdoptRegistersDirectlyOpenedSession(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)

	// The local runtime owns it; Serve does not. Adopt succeeds.
	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Release()
	status, body := f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": agent.SessionWriterID()})
	if status != http.StatusOK || !strings.Contains(body, "adopted") {
		t.Fatalf("adopt status = %d %q, want 200 adopted", status, body)
	}
	if view := f.ownershipView(t, other); view.Holder != "external" || !view.Mirrored {
		t.Fatalf("post-adopt ownership = %+v, want external mirror", view)
	}
	// Idempotent second adopt.
	status, _ = f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": agent.SessionWriterID()})
	if status != http.StatusOK {
		t.Fatalf("second adopt status = %d, want 200", status)
	}
}

// TestAdoptRefusedForServeHeldSession proves sessions Serve itself holds go
// through /handoff, not /adopt.
func TestAdoptRefusedForServeHeldSession(t *testing.T) {
	f := newOwnershipFixture(t)
	status, body := f.post(t, "/adopt", map[string]string{"sessionPath": f.active, "writerId": agent.SessionWriterID()})
	if status != http.StatusConflict || !strings.Contains(body, "handoff") {
		t.Fatalf("adopt of serve-held session = %d %q, want 409 handoff hint", status, body)
	}
}

func TestAdoptRequiresLiveClaimedWriter(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)
	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Release()

	status, body := f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": "not-the-owner"})
	if status != http.StatusConflict || !strings.Contains(body, "claimed writer") {
		t.Fatalf("adopt with false owner = %d %q, want 409", status, body)
	}
	if view := f.ownershipView(t, other); view.Mirrored {
		t.Fatalf("false adoption published mirror: %+v", view)
	}
}

func TestMirrorGenerationFencesFramesAndEnd(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff = %d %q", status, body)
	}
	writerLease := f.acquireHandedOff(t)
	defer writerLease.Release()

	status, _ := f.post(t, "/external/frames", map[string]any{
		"sessionPath": f.active, "mirrorId": "old-generation", "frames": []eventwire.Event{},
	})
	if status != http.StatusConflict {
		t.Fatalf("stale frames status = %d, want 409", status)
	}
	status, _ = f.post(t, "/mirror-end", map[string]string{"sessionPath": f.active, "mirrorId": "old-generation"})
	if status != http.StatusConflict {
		t.Fatalf("stale mirror-end status = %d, want 409", status)
	}
	if view := f.ownershipView(t, f.active); !view.Mirrored || view.Holder != "external" {
		t.Fatalf("stale generation changed ownership: %+v", view)
	}
}

func TestExternalFramesRequestLimits(t *testing.T) {
	f := newOwnershipFixture(t)
	if status, body := f.handoffForce(t, "wait"); status != http.StatusOK {
		t.Fatalf("handoff = %d %q", status, body)
	}
	writerLease := f.acquireHandedOff(t)
	defer writerLease.Release()

	frames := make([]eventwire.Event, externalFramesMaxCount+1)
	status, _ := f.post(t, "/external/frames", map[string]any{
		"sessionPath": f.active, "mirrorId": f.grant.MirrorID, "frames": frames,
	})
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("too many frames status = %d, want 413", status)
	}

	oversized := `{"sessionPath":` + fmt.Sprintf("%q", f.active) + `,"mirrorId":` + fmt.Sprintf("%q", f.grant.MirrorID) + `,"padding":"` + strings.Repeat("x", externalFramesMaxBody) + `"}`
	resp, err := http.Post(f.srv.URL+"/external/frames", "application/json", strings.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", resp.StatusCode)
	}
}

// TestMirroredStatusAndHistoryBySession proves the read-only endpoints answer
// a spectator selecting a mirrored session with the file-backed view.
func TestMirroredStatusAndHistoryBySession(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)
	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Release()
	if status, body := f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": agent.SessionWriterID()}); status != http.StatusOK {
		t.Fatalf("adopt failed: %d %q", status, body)
	}
	extendSessionOnDisk(t, other, "writer turn")

	status, body := f.get(t, "/history?session="+filepath.ToSlash(other))
	if status != http.StatusOK || !strings.Contains(body, "writer turn") {
		t.Fatalf("spectator history = %d %q, want the writer's turn", status, body)
	}
	status, body = f.get(t, "/status?runtime=1&session="+filepath.ToSlash(other))
	if status != http.StatusOK || !strings.Contains(body, `"takenOver":true`) {
		t.Fatalf("spectator status = %d %q, want takenOver", status, body)
	}
}

// TestResumeSpectatorMountOnMirroredSession proves /resume on a session a
// local runtime owns returns 200 (read-only spectator mount) without taking
// ownership, so any client version can attach and render the mirrored view.
func TestResumeSpectatorMountOnMirroredSession(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)
	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Release()
	if status, body := f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": agent.SessionWriterID()}); status != http.StatusOK {
		t.Fatalf("adopt failed: %d %q", status, body)
	}
	status, body := f.post(t, "/resume", map[string]string{"path": other})
	if status != http.StatusNoContent {
		t.Fatalf("spectator resume = %d %q, want 204", status, body)
	}
	// Ownership must stay external: no lease transfer happened.
	if view := f.ownershipView(t, other); view.Holder != "external" || !view.Mirrored {
		t.Fatalf("post-resume ownership = %+v, want external mirror", view)
	}
	// The foreground controller must not have moved onto the spectator target.
	if got := f.server.ctl().SessionPath(); got == other {
		t.Fatalf("spectator resume switched the foreground onto %q", got)
	}
	// The spectator's writes are refused by the expected-path fence: it is
	// pinned to a session the foreground controller does not own.
	payload, _ := json.Marshal(map[string]string{"input": "hello"})
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/submit", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Reasonix-Expected-Session-Path", agent.CanonicalSessionPath(other))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = readAll(resp)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "taken over by a local Reasonix") {
		t.Fatalf("spectator submit = %d %q, want 409 takeover refusal", resp.StatusCode, body)
	}
}

// TestSpectatorSwitchCommandsPassTheFence proves a read-only spectator pinned
// to a local-owned session can still run foreground-switch commands (/new) —
// that is how the remote side leaves its pin and regains the ability to act.
func TestSpectatorSwitchCommandsPassTheFence(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "other.jsonl")
	saveServeTestSession(t, other)
	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLease.Release()
	if status, body := f.post(t, "/adopt", map[string]string{"sessionPath": other, "writerId": agent.SessionWriterID()}); status != http.StatusOK {
		t.Fatalf("adopt failed: %d %q", status, body)
	}
	// Spectator mounts on the mirrored session.
	if status, body := f.post(t, "/resume", map[string]string{"path": other}); status != http.StatusNoContent {
		t.Fatalf("spectator resume = %d %q", status, body)
	}
	// /new as the spectator: expected path is the mirrored pin, foreground is
	// elsewhere — the switch fence must let it through and rotate the
	// foreground to a fresh session.
	payload, _ := json.Marshal(map[string]any{})
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/new", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Reasonix-Expected-Session-Path", agent.CanonicalSessionPath(other))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := readAll(resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("spectator /new = %d %q, want 204", resp.StatusCode, body)
	}
	if got := f.server.ctl().SessionPath(); got == other {
		t.Fatalf("/new did not rotate the foreground off the mirrored pin")
	}
	// The mirrored session stays mirrored (local owner untouched).
	if view := f.ownershipView(t, other); !view.Mirrored || view.Holder != "external" {
		t.Fatalf("post-/new ownership = %+v, want external mirror", view)
	}
}

// TestAutoReclaimCompletesOutstandingReclaim proves a writer that vanished
// after a reclaim was requested cannot leave the session mirrored forever:
// once the entry goes stale and the lease is free, the outstanding reclaim
// completes on the recovery sweep instead of being skipped by the flag.
func TestAutoReclaimCompletesOutstandingReclaim(t *testing.T) {
	f := newOwnershipFixture(t)
	other := filepath.Join(f.dir, "vanished.jsonl")
	saveServeTestSession(t, other)

	writerLease, err := agent.TryAcquireSessionLease(other)
	if err != nil {
		t.Fatal(err)
	}
	held := &atomic.Bool{}
	held.Store(true)
	withForeignWriterLease(t, other, held)

	if status, body := f.post(t, "/adopt", map[string]any{"sessionPath": other, "writerId": agent.SessionWriterID()}); status != http.StatusOK {
		t.Fatalf("adopt = %d %q", status, body)
	}
	// The writer ignores the reclaim: the wait times out, the flag stays set.
	status, body := f.post(t, "/reclaim", map[string]any{"sessionPath": other, "timeoutMs": 200})
	if status != http.StatusConflict {
		t.Fatalf("reclaim against silent writer = %d %q, want 409", status, body)
	}
	if view := f.ownershipView(t, other); !view.Mirrored || !view.ReclaimRequested {
		t.Fatalf("post-timeout ownership = %+v, want mirrored with reclaim requested", view)
	}

	canonical := agent.CanonicalSessionPath(other)
	backdate := func() {
		f.server.mirrorMu.Lock()
		defer f.server.mirrorMu.Unlock()
		if m, ok := f.server.mirrored[canonical]; ok {
			m.lastContact = time.Now().Add(-2 * mirrorStaleAfter)
			f.server.mirrored[canonical] = m
		}
	}

	// Stale but still leased: the recovery sweep must stand down.
	backdate()
	f.server.maybeAutoReclaimMirrored(other)
	if view := f.ownershipView(t, other); !view.Mirrored {
		t.Fatal("auto-reclaim cleared a mirror whose lease is still held")
	}

	// The writer dies silently: lease gone, no mirror-end, no heartbeats. The
	// outstanding reclaim completes and the remote side can own the session.
	held.Store(false)
	writerLease.Release()
	backdate()
	f.server.maybeAutoReclaimMirrored(other)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if view := f.ownershipView(t, other); !view.Mirrored {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stale mirror with outstanding reclaim was never cleared: %+v", f.ownershipView(t, other))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
