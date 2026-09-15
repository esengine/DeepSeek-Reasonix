package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
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
	ts := httptest.NewServer(newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
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

// TestIdentityReclaimReattachesForeground proves the return half: once the
// local writer drops the lock, /reclaim re-owns the identity for the serve and
// clears the mirror bookkeeping.
func TestIdentityReclaimReattachesForeground(t *testing.T) {
	_, ctrl, service, current := newExclusiveSessionServe(t)
	root := identityRoot(t, service, current)
	lifecycle := newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{})
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
	lifecycle := newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{})
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
