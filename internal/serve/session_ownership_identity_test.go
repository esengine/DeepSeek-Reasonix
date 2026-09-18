package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/session"
)

// retireExclusiveForeground releases every writer deterministically (the
// foreground plus any runtime a handoff rotated away from) so Windows temp-dir
// cleanup does not race the idle-retirement TTL.
func retireExclusiveForeground(t *testing.T, ctrl *control.Controller, service *session.Service) {
	t.Helper()
	ctrl.Close()
	if service != nil {
		_ = service.CloseAll(context.Background())
	}
}


// newIdentityLifecycleServe wraps the lifecycle test server with the frame
// tag an exclusive controller's production host would register, so identity
// transitions can assert how live frames get stamped.
func newIdentityLifecycleServe(t *testing.T, ctrl *control.Controller, ref session.SessionRef) *Server {
	t.Helper()
	bc := NewBroadcaster()
	lifecycle := newLifecycleTestServer(t, ctrl, bc, config.ServeConfig{})
	tag := newSessionTagSink(bc)
	tag.SetIdentity("", ref.SessionID)
	lifecycle.RegisterSessionTag(ctrl, tag)
	return lifecycle
}

// openIdentityWriter opens the final-format session's writer through a bare
// persistence handle, standing in for the local runtime that takes over. The
// returned session keeps the writer lock until Close.
func openIdentityWriter(t *testing.T, root string, ref session.SessionRef) *session.Session {
	t.Helper()
	handle, err := session.NewFilesystemPersistence(root).Open(ref.SessionID, session.ReadWrite)
	if err != nil {
		t.Fatalf("open identity writer: %v", err)
	}
	return handle
}

func identityRoot(t *testing.T, service *session.Service, ref session.SessionRef) string {
	t.Helper()
	dir, err := service.SessionDir(t.Context(), ref)
	if err != nil {
		t.Fatalf("resolve identity dir: %v", err)
	}
	return filepath.Dir(dir)
}

func serveBody(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw := make([]byte, 0, 4<<10)
	buf := make([]byte, 4<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	return resp, string(raw)
}

// TestIdentityHandoffReleasesWriterAndOwnershipTracks proves the release half:
// /handoff on a final-format identity rotates the foreground, drops the writer
// lock before the grant is answered, and /ownership reports the external
// holder while the taker keeps the lock.
func TestIdentityHandoffReleasesWriterAndOwnershipTracks(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	lifecycle := newIdentityLifecycleServe(t, ctrl, current)
	ts := httptest.NewServer(lifecycle.Handler())
	defer ts.Close()
	route := "session-id:" + current.SessionID

	resp, raw := serveBody(t, http.MethodGet, ts.URL+"/ownership?session="+route, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ownership status = %d body %s", resp.StatusCode, raw)
	}
	var view ownershipView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if view.Holder != "serve" || view.Running {
		t.Fatalf("before handoff view = %+v, want serve holder idle", view)
	}
	defer retireExclusiveForeground(t, ctrl, service)

	resp, raw = serveBody(t, http.MethodPost, ts.URL+"/handoff", `{"sessionPath":"`+route+`","targetWriterId":"taker-writer","force":true,"mode":"wait","timeoutMs":2000}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handoff status = %d body %s", resp.StatusCode, raw)
	}
	var grant mirrorGrant
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		t.Fatal(err)
	}
	if grant.MirrorID == "" || grant.ReturnHandoffID == "" || grant.SourceWriterID == "" ||
		grant.TargetWriterID != "taker-writer" || grant.SessionPath != route {
		t.Fatalf("handoff grant = %+v", grant)
	}
	if ref, bound := ctrl.SessionRef(); bound && ref == current {
		t.Fatal("foreground still bound to the handed-off identity after handoff")
	}
	// The frame tag must follow the rotated foreground: a tag still pointing at
	// the handed-off identity misroutes every subsequent live frame.
	if tag := lifecycle.tagFor(ctrl); tag == nil {
		t.Fatal("frame tag missing after handoff rotation")
	} else if fresh, bound := ctrl.SessionRef(); !bound || tag.path != "" || (fresh.SessionID != "" && tag.sessionID != fresh.SessionID) {
		t.Fatalf("frame tag after handoff = %+v, want identity %q", tag, fresh.SessionID)
	}
	if session.ProbeWriterHeld(filepath.Join(root, current.SessionID)) {
		t.Fatal("writer lock still held after handoff grant")
	}

	// The taker acquires the released writer; ownership flips to external.
	writer := openIdentityWriter(t, root, current)
	defer writer.Close(t.Context())
	resp, raw = serveBody(t, http.MethodGet, ts.URL+"/ownership?session="+route, "")
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || view.Holder != "external" || !view.TakenOver {
		t.Fatalf("after takeover view = %+v (status %d)", view, resp.StatusCode)
	}
}

// TestIdentityResumeMountsSpectatorWhenWriterHeld proves the attach contract:
// /resume for an identity another runtime writes answers 204 with the
// taken-over header instead of a hard failure, and /history serves the cold
// event log so the spectator can render.
func TestIdentityResumeMountsSpectatorWhenWriterHeld(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	ts := httptest.NewServer(newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer ts.Close()
	route := "session-id:" + current.SessionID

	// Detach the foreground from the identity first so the resume path cannot
	// short-circuit onto the already-bound current session.
	if _, err := ctrl.BindFreshSession(t.Context(), "spectator-fresh"); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), current); err != nil {
		t.Fatalf("close rotated-out runtime: %v", err)
	}
	writer := openIdentityWriter(t, root, current)
	defer writer.Close(t.Context())

	resp, raw := serveBody(t, http.MethodPost, ts.URL+"/resume", `{"sessionId":"`+current.SessionID+`"}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("spectator resume status = %d body %s", resp.StatusCode, raw)
	}
	if resp.Header.Get(sessionTakenOverHeader) == "" {
		t.Fatal("spectator resume omitted the taken-over header")
	}
	if resp.Header.Get(sessionIDHeader) != current.SessionID {
		t.Fatalf("spectator resume session header = %q", resp.Header.Get(sessionIDHeader))
	}

	resp, raw = serveBody(t, http.MethodGet, ts.URL+"/history?session="+route, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spectator history status = %d body %s", resp.StatusCode, raw)
	}

	resp, raw = serveBody(t, http.MethodGet, ts.URL+"/status?session="+route, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("spectator status code = %d body %s", resp.StatusCode, raw)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	if taken, _ := status["takenOver"].(bool); !taken {
		t.Fatalf("spectator status missing takenOver: %v", status)
	}
	retireExclusiveForeground(t, ctrl, service)
}

// TestIdentityAdoptRegistersWriterAfterServeRestart pins the canonical
// re-registration path used when a CLI survives the resident serve restarting.
func TestIdentityAdoptRegistersWriterAfterServeRestart(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	lifecycle := newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{})
	ts := httptest.NewServer(lifecycle.Handler())
	defer ts.Close()
	defer retireExclusiveForeground(t, ctrl, service)
	route := "session-id:" + current.SessionID

	if _, err := ctrl.BindFreshSession(t.Context(), "adopt-fresh"); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), current); err != nil {
		t.Fatalf("close rotated-out runtime: %v", err)
	}
	writer := openIdentityWriter(t, root, current)
	defer writer.Close(t.Context())

	resp, raw := serveBody(t, http.MethodPost, ts.URL+"/adopt", `{"sessionPath":"`+route+`","writerId":"surviving-cli"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("identity adopt status = %d body %s", resp.StatusCode, raw)
	}
	var grant mirrorGrant
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		t.Fatal(err)
	}
	if grant.SessionPath != route || grant.MirrorID == "" || grant.TargetWriterID != "surviving-cli" {
		t.Fatalf("identity adopt grant = %+v", grant)
	}
	if mirrored, ok := lifecycle.mirroredEntry(route); !ok || mirrored.targetWriterID != "surviving-cli" {
		t.Fatalf("identity mirror after adopt = %+v, present=%v", mirrored, ok)
	}
}

// TestIdentityReclaimReattachesForeground proves the return half: once the
// local writer drops the lock, /reclaim re-owns the identity for the serve and
// clears the mirror bookkeeping.
func TestIdentityReclaimReattachesForeground(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	lifecycle := newIdentityLifecycleServe(t, ctrl, current)
	ts := httptest.NewServer(lifecycle.Handler())
	defer ts.Close()
	route := "session-id:" + current.SessionID

	resp, raw := serveBody(t, http.MethodPost, ts.URL+"/handoff", `{"sessionPath":"`+route+`","targetWriterId":"taker-writer","force":true,"mode":"wait","timeoutMs":2000}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handoff status = %d body %s", resp.StatusCode, raw)
	}
	writer := openIdentityWriter(t, root, current)
	if err := writer.Close(t.Context()); err != nil {
		t.Fatalf("taker release: %v", err)
	}

	resp, raw = serveBody(t, http.MethodPost, ts.URL+"/reclaim", `{"sessionPath":"`+route+`","mode":"wait","timeoutMs":5000}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reclaim status = %d body %s", resp.StatusCode, raw)
	}
	// After the reclaim the identity-selected status must answer ownership
	// explicitly false: clients apply present fields only, so an omitted
	// takenOver would pin the spectator banner forever.
	resp, raw = serveBody(t, http.MethodGet, ts.URL+"/status?session="+route, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-reclaim status code = %d body %s", resp.StatusCode, raw)
	}
	var reclaimed map[string]any
	if err := json.Unmarshal([]byte(raw), &reclaimed); err != nil {
		t.Fatal(err)
	}
	if taken, _ := reclaimed["takenOver"].(bool); taken {
		t.Fatalf("post-reclaim status still reports takenOver: %v", reclaimed)
	}
	if ref, bound := ctrl.SessionRef(); !bound || ref != current {
		t.Fatalf("foreground ref after reclaim = %+v (bound %v), want %+v", ref, bound, current)
	}
	// The frame tag must follow the re-owned identity: a stale tag stamps live
	// frames with another session and identity-routed subscribers drop them.
	if tag := lifecycle.tagFor(ctrl); tag == nil || tag.path != "" || tag.sessionID != current.SessionID {
		t.Fatalf("frame tag after reclaim = %+v, want identity %q", tag, current.SessionID)
	}
	if _, still := lifecycle.mirroredEntry(route); still {
		t.Fatal("mirror entry survived reclaim")
	}
	defer retireExclusiveForeground(t, ctrl, service)
	if !session.ProbeWriterHeld(filepath.Join(root, current.SessionID)) {
		t.Fatal("serve did not re-acquire the writer after reclaim")
	}
}

// TestIdentityHandoffRefusesForeignHolder pins the guard: a handoff for an
// identity this serve does not run is refused instead of granting a session
// the serve cannot release.
func TestIdentityHandoffRefusesForeignHolder(t *testing.T) {
	_, ctrl, service, _ := newExclusiveSessionServe(t)
	ts := httptest.NewServer(newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer ts.Close()
	resp, raw := serveBody(t, http.MethodPost, ts.URL+"/handoff", `{"sessionPath":"session-id:does-not-exist","targetWriterId":"taker","force":true}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown identity handoff status = %d body %s", resp.StatusCode, raw)
	}
	retireExclusiveForeground(t, ctrl, service)
}

// TestIdentityMirrorEndAcceptsLiveWriter pins the farewell contract: the
// writer's return transaction sends mirror-end before process exit, so the
// writer lock is still held and the serve must accept (204) instead of 409ing
// a call its own protocol ordering requires.
func TestIdentityMirrorEndAcceptsLiveWriter(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	lifecycle := newIdentityLifecycleServe(t, ctrl, current)
	ts := httptest.NewServer(lifecycle.Handler())
	defer ts.Close()
	route := "session-id:" + current.SessionID

	resp, raw := serveBody(t, http.MethodPost, ts.URL+"/handoff", `{"sessionPath":"`+route+`","targetWriterId":"taker-writer","force":true,"mode":"wait","timeoutMs":2000}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handoff status = %d body %s", resp.StatusCode, raw)
	}
	var grant mirrorGrant
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		t.Fatal(err)
	}
	writer := openIdentityWriter(t, root, current)
	defer writer.Close(t.Context())

	resp, raw = serveBody(t, http.MethodPost, ts.URL+"/mirror-end", `{"sessionPath":"`+route+`","mirrorId":"`+grant.MirrorID+`"}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mirror-end with live writer status = %d body %s", resp.StatusCode, raw)
	}
	if ref, bound := ctrl.SessionRef(); bound && ref == current {
		t.Fatal("mirror-end re-owned the identity under a live writer")
	}
	retireExclusiveForeground(t, ctrl, service)
}

// TestSessionsFoldsEngineMirrorOfLegacyTranscript pins the listing fold: the
// engine mirrors an in-flight legacy transcript into a final-format event log
// keyed by the legacy branch id; the /sessions view must keep one row for that
// conversation instead of a transcript row plus its mirror.
func TestSessionsFoldsEngineMirrorOfLegacyTranscript(t *testing.T) {
	legacyDir := t.TempDir()
	legacy := filepath.Join(legacyDir, "mirror-me.jsonl")
	if err := os.WriteFile(legacy, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v4Root := filepath.Join(t.TempDir(), "sessions-v4")
	persistence := session.NewFilesystemPersistence(v4Root)
	mirror, err := persistence.Create(session.CreateOptions{SessionID: "mirror-me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mirror.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, err := session.NewService("serve-test", persistence)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: legacyDir, SessionService: service, ExclusiveSession: true})
	if _, err := ctrl.BindFreshSession(t.Context(), "foreground"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	srv := newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{})
	recorder := httptest.NewRecorder()
	srv.sessions(recorder, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	legacyKey := agent.CanonicalSessionPath(legacy)
	sawLegacy, sawMirror := false, false
	for _, row := range rows {
		if agent.CanonicalSessionPath(row.Path) == legacyKey {
			sawLegacy = true
		}
		if row.SessionID == "mirror-me" {
			sawMirror = true
		}
	}
	if !sawLegacy {
		t.Fatalf("legacy row missing from listing: %+v", rows)
	}
	if sawMirror {
		t.Fatalf("engine mirror listed beside its transcript: %+v", rows)
	}
	retireExclusiveForeground(t, ctrl, service)
}

// TestIdentityStatusAnswersFreeWriterWithRouteMatch pins the serve-restart
// recovery: a spectator identity whose writer exited and whose mirror entry
// was lost to the restart must get an explicit route-matching status with
// takenOver=false — the foreground snapshot names a different session and a
// pinned tab would discard it, leaving the banner stuck until re-attach.
func TestIdentityStatusAnswersFreeWriterWithRouteMatch(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	lifecycle := newIdentityLifecycleServe(t, ctrl, current)
	ts := httptest.NewServer(lifecycle.Handler())
	defer ts.Close()
	route := "session-id:" + current.SessionID

	// Move the foreground off the identity and free its writer, mimicking a
	// post-restart world where nothing holds the session.
	if _, err := ctrl.BindFreshSession(t.Context(), "elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), current); err != nil {
		t.Fatalf("release identity writer: %v", err)
	}

	resp, raw := serveBody(t, http.MethodGet, ts.URL+"/status?session="+route, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d body %s", resp.StatusCode, raw)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	if sid, _ := status["sessionId"].(string); sid != current.SessionID {
		t.Fatalf("status sessionId = %v, want the queried identity", status["sessionId"])
	}
	if taken, _ := status["takenOver"].(bool); taken {
		t.Fatalf("free-writer identity status still reports takenOver: %v", status)
	}
	retireExclusiveForeground(t, ctrl, service)
}
