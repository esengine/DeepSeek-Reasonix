package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

// newCanonicalTakeoverTUI builds an exclusive-session TUI over a fresh
// sessions-v4 catalog sharing the CLI's cached session service, plus a second
// ("held") identity the fake serve pretends to release.
func newCanonicalTakeoverTUI(t *testing.T) (*chatTUI, *control.Controller, *session.Service, session.SessionRef) {
	t.Helper()
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	service := cliSessionService(sessionDir)
	if service == nil {
		t.Fatal("session service unavailable for test workspace")
	}
	ctrl := newOwnedTestController(t, control.Options{
		Executor:   agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionDir: sessionDir, SessionService: service, ExclusiveSession: true,
	})
	if _, err := ctrl.BindFreshSession(t.Context(), "fresh-cli"); err != nil {
		t.Fatal(err)
	}
	held, err := service.Create(t.Context(), session.CreateOptions{SessionID: "held"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), held.Ref()); err != nil {
		t.Fatal(err)
	}
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	m.takeover = newCLITakeoverManager(nil, m.leases)
	t.Cleanup(func() {
		ctrl.Close()
		_ = service.CloseAll(context.Background())
	})
	return &m, ctrl, service, held.Ref()
}

// fakeCanonicalServe impersonates the resident serve: it grants the identity
// handoff, accepts mirrored frames, and can flag a reclaim.
type fakeCanonicalServe struct {
	mu          sync.Mutex
	base        string
	handoffBody map[string]any
	framesPath  []string
	mirrorEnd   []string
	reclaim     atomic.Bool
}

func newFakeCanonicalServe(t *testing.T, route string) *fakeCanonicalServe {
	t.Helper()
	f := &fakeCanonicalServe{}
	f.handoffBody = map[string]any{
		"sessionPath": route, "mirrorId": "mirror-1", "handoffId": "handoff-1",
		"returnHandoffId": "return-1", "sourceWriterId": "serve-writer",
		"targetWriterId": agent.SessionWriterID(), "status": "handed_off",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/handoff":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.handoffBody["__received"] = body
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(f.handoffBody)
		case "/external/frames":
			var body struct {
				SessionPath string `json:"sessionPath"`
				MirrorID    string `json:"mirrorId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.framesPath = append(f.framesPath, body.SessionPath)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"reclaimRequested": f.reclaim.Load(), "reclaimMode": "wait"})
		case "/mirror-end":
			var body struct {
				SessionPath string `json:"sessionPath"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.mirrorEnd = append(f.mirrorEnd, body.SessionPath)
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	f.base = srv.URL
	return f
}

func withFakeCanonicalDiscovery(t *testing.T, base string) {
	t.Helper()
	previous := discoverCLIServesForTakeover
	discoverCLIServesForTakeover = func() []cliServeRecord {
		return []cliServeRecord{{pid: 1, base: base, token: "test-token"}}
	}
	t.Cleanup(func() { discoverCLIServesForTakeover = previous })
}

// TestCanonicalTakeoverCommandTakesOverIdentity proves the /takeover command
// against an identity route: the grant is validated, the controller attaches
// through OpenSession, and the mirror manager activates on the route key.
func TestCanonicalTakeoverCommandTakesOverIdentity(t *testing.T) {
	route := cliCanonicalRoute("held")
	fake := newFakeCanonicalServe(t, route)
	withFakeCanonicalDiscovery(t, fake.base)
	m, ctrl, _, held := newCanonicalTakeoverTUI(t)

	m.runCanonicalTakeoverCommand(route)

	if got := m.pendingTakeoverPath; got != "" {
		t.Fatalf("pending takeover target = %q after success", got)
	}
	if ref, bound := ctrl.SessionRef(); !bound || ref != held {
		t.Fatalf("controller ref after takeover = %+v (bound %v), want %+v", ref, bound, held)
	}
	binding, _, _, _ := m.takeover.snapshot()
	if binding == nil || binding.path != route || !binding.canonical {
		t.Fatalf("mirror binding = %+v, want canonical route %q", binding, route)
	}
	fake.mu.Lock()
	received, _ := fake.handoffBody["__received"].(map[string]any)
	fake.mu.Unlock()
	if received == nil || received["sessionPath"] != route || received["targetWriterId"] != agent.SessionWriterID() {
		t.Fatalf("handoff request = %+v", received)
	}
	if err := m.takeover.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCanonicalTakeoverCommandReportsRefusedGrant proves a refusing serve
// surfaces the failure without touching the controller's session.
func TestCanonicalTakeoverCommandReportsRefusedGrant(t *testing.T) {
	route := cliCanonicalRoute("held")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "session is not held by this serve process", http.StatusConflict)
		}
	}))
	defer srv.Close()
	withFakeCanonicalDiscovery(t, srv.URL)
	m, ctrl, _, _ := newCanonicalTakeoverTUI(t)

	m.runCanonicalTakeoverCommand(route)

	if ref, bound := ctrl.SessionRef(); !bound || ref.SessionID != "fresh-cli" {
		t.Fatalf("controller ref after refused takeover = %+v (bound %v), want the original fresh session", ref, bound)
	}
	if binding, _, _, _ := m.takeover.snapshot(); binding != nil {
		t.Fatalf("mirror binding activated despite refusal: %+v", binding)
	}
}

// TestCanonicalReclaimYieldsWithoutLegacyLease proves the yield half: a
// reclaim signal against a canonical binding returns the mirror without any
// legacy path-lease reservation, and mirror-end carries the identity route.
func TestCanonicalReclaimYieldsWithoutLegacyLease(t *testing.T) {
	route := cliCanonicalRoute("held")
	fake := newFakeCanonicalServe(t, route)
	m, ctrl, _, held := newCanonicalTakeoverTUI(t)
	withFakeCanonicalDiscovery(t, fake.base)
	m.runCanonicalTakeoverCommand(route)
	if ref, bound := ctrl.SessionRef(); !bound || ref != held {
		t.Fatalf("takeover did not attach: %+v bound=%v", ref, bound)
	}

	exited := make(chan struct{}, 1)
	m.takeover.SetYieldCallback(func() { exited <- struct{}{} })
	fake.reclaim.Store(true)
	m.takeover.Emit(event.Event{Kind: event.Text, Text: "answer"})
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not yield the canonical mirror")
	}
	if !m.takeover.Returned() {
		t.Fatal("canonical mirror not marked returned")
	}
	fake.mu.Lock()
	ends := append([]string(nil), fake.mirrorEnd...)
	fake.mu.Unlock()
	if len(ends) != 1 || ends[0] != route {
		t.Fatalf("mirror-end requests = %v, want one for %q", ends, route)
	}
}

// TestCanonicalReclaimKeepsTUIAliveAndSwitchesSession proves that reclaiming
// one identity releases its writer while leaving the CLI process available to
// resume another identity.
func TestCanonicalReclaimKeepsTUIAliveAndSwitchesSession(t *testing.T) {
	route := cliCanonicalRoute("held")
	fake := newFakeCanonicalServe(t, route)
	withFakeCanonicalDiscovery(t, fake.base)
	m, ctrl, service, held := newCanonicalTakeoverTUI(t)
	if err := ctrl.RecordSessionMessages(t.Context(), "reclaim-test", []provider.Message{{
		ID: agent.NewMessageID(), Role: provider.RoleUser, Content: "existing session",
	}}); err != nil {
		t.Fatal(err)
	}
	m.runCanonicalTakeoverCommand(route)

	yielded := make(chan struct{}, 1)
	m.takeover.SetYieldCallback(func() { yielded <- struct{}{} })
	fake.reclaim.Store(true)
	m.takeover.Emit(event.Event{Kind: event.Text, Text: "answer"})
	select {
	case <-yielded:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not yield the identity")
	}

	if next, cmd := m.Update(tuiSessionReclaimedMsg{}); cmd != nil {
		t.Fatalf("reclaim updated TUI with quit command %T", cmd)
	} else {
		updated := next.(chatTUI)
		m = &updated
	}
	// The reclaimed conversation stays rendered with a notice instead of a
	// forced chooser; only the switch/takeover/exit commands are accepted.
	if !m.sessionReclaimed || m.resumePick != nil {
		t.Fatalf("reclaim state = %v picker=%v, want live TUI on the reclaimed session", m.sessionReclaimed, m.resumePick != nil)
	}
	if reclaimInputAllowed("hello") {
		t.Fatal("reclaim gate accepted plain text")
	}
	if !reclaimInputAllowed("/resume 2") {
		t.Fatal("reclaim gate rejected /resume")
	}
	dir, err := service.SessionDir(t.Context(), held)
	if err != nil {
		t.Fatal(err)
	}
	if session.ProbeWriterHeld(dir) {
		t.Fatal("reclaimed identity writer lock is still held by the CLI")
	}

	// /resume opens the chooser on demand and switching clears the reclaim state.
	m.runResumeCommand("/resume")
	if m.resumePick == nil {
		t.Fatal("/resume after reclaim did not open the session chooser")
	}
	entries := m.resumePick.entries
	for i, entry := range entries {
		if entry.target.canonical() && entry.target.ref.SessionID == "fresh-cli" {
			m.resumePick.sel = i
			if m.resumePick.quick != nil {
				m.resumePick.quick.selected = i
			}
			break
		}
	}
	if m.resumePick.sel < 0 || m.resumePick.sel >= len(entries) || !entries[m.resumePick.sel].target.canonical() || entries[m.resumePick.sel].target.ref.SessionID != "fresh-cli" {
		t.Fatalf("resume picker did not expose fresh-cli: %+v", entries)
	}
	// Confirm through the same key path used by the live picker. Calling
	// applyResumePick directly would miss routing or quick-picker regressions.
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("resume after reclaim returned unexpected command %T", cmd)
	}
	updated := next.(chatTUI)
	m = &updated
	if ref, bound := ctrl.SessionRef(); !bound || ref.SessionID != "fresh-cli" {
		t.Fatalf("controller after reclaim resume = %+v bound=%v, want fresh-cli", ref, bound)
	}
	if m.sessionReclaimed || m.takeover.Returned() {
		t.Fatal("reclaim marker remained after switching to another session")
	}
}

// TestCanonicalReclaimCanTakeSameSessionAgain covers the complete ownership
// round trip: CLI takeover, desktop reclaim, then CLI takeover once more.
func TestCanonicalReclaimCanTakeSameSessionAgain(t *testing.T) {
	route := cliCanonicalRoute("held")
	fake := newFakeCanonicalServe(t, route)
	withFakeCanonicalDiscovery(t, fake.base)
	m, ctrl, _, held := newCanonicalTakeoverTUI(t)
	m.runCanonicalTakeoverCommand(route)

	yielded := make(chan struct{}, 1)
	m.takeover.SetYieldCallback(func() { yielded <- struct{}{} })
	fake.reclaim.Store(true)
	m.takeover.Emit(event.Event{Kind: event.Text, Text: "answer"})
	select {
	case <-yielded:
	case <-time.After(5 * time.Second):
		t.Fatal("reclaim did not yield the identity")
	}
	if next, cmd := m.Update(tuiSessionReclaimedMsg{}); cmd != nil {
		t.Fatalf("reclaim updated TUI with quit command %T", cmd)
	} else {
		updated := next.(chatTUI)
		m = &updated
	}
	if _, bound := ctrl.SessionRef(); bound {
		t.Fatal("controller remained bound after desktop reclaim")
	}

	fake.reclaim.Store(false)
	// The user-facing path: after a reclaim the notice says "/takeover takes
	// it back" — the command must resolve the remembered reclaimed target
	// without a prior /resume (which would re-populate pendingTakeoverPath).
	m.runTakeoverCommand("/takeover")
	if ref, bound := ctrl.SessionRef(); !bound || ref != held {
		t.Fatalf("controller after second takeover = %+v bound=%v, want %+v", ref, bound, held)
	}
	if m.sessionReclaimed || m.takeover.Returned() {
		t.Fatal("second takeover left the CLI in reclaimed mode")
	}
}

// TestDiscoverCLIServesSkipsDeadPIDs pins the discovery prune: state files
// whose recorded process is gone must not surface as takeover candidates —
// dialing their stale ports only produces connection-refused noise.
func TestDiscoverCLIServesSkipsDeadPIDs(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "remote")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", home)
	write := func(slug, addr string, pid int) {
		t.Helper()
		state := map[string]any{"pid": pid, "addr": addr, "workspace": "/tmp/" + slug}
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "serve-"+slug+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "serve-"+slug+".token"), []byte("tok-"+slug), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("dead-one", "127.0.0.1:44173", 111)
	write("alive-one", "127.0.0.1:33863", 222)

	previous := cliServeProcessAlive
	cliServeProcessAlive = func(pid int) bool { return pid == 222 }
	t.Cleanup(func() { cliServeProcessAlive = previous })

	records := discoverCLIServes()
	if len(records) != 1 || records[0].base != "http://127.0.0.1:33863" || records[0].token != "tok-alive-one" {
		t.Fatalf("discovery after prune = %+v, want only the alive record", records)
	}
}
