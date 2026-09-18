package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/session"
)

func TestSessionsDeduplicatesMigratedLegacySource(t *testing.T) {
	legacyDir := t.TempDir()
	v4Root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("serve-test", session.NewFilesystemPersistence(v4Root))
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: legacyDir, SessionService: service, ExclusiveSession: true})
	if _, err := ctrl.BindFreshSession(t.Context(), "current"); err != nil {
		t.Fatal(err)
	}
	target, err := service.Create(t.Context(), session.CreateOptions{SessionID: "canonical-target"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), target.Ref()); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(legacyDir, "old.jsonl")
	if err := os.WriteFile(legacy, []byte(`{"role":"user","content":"old"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mapping := session.MigrationMapping{
		SchemaVersion: session.SchemaVersion,
		Entries:       []session.MigrationEntry{{SourcePath: agent.CanonicalSessionPath(legacy), TargetID: "canonical-target"}},
	}
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v4Root, "migration-map.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	// Service-backed controllers park their runtime in the idle cache after
	// close, so the lifecycle fixture's writer-retire wait does not apply;
	// these listing tests only need the HTTP surface.
	srv := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	t.Cleanup(srv.Close)
	recorder := httptest.NewRecorder()
	srv.sessions(recorder, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name == "old" || agent.CanonicalSessionPath(row.Path) == agent.CanonicalSessionPath(legacy) {
			t.Fatalf("migrated legacy row was not deduplicated: %+v", rows)
		}
	}
	found := false
	for _, row := range rows {
		if row.SessionID == "canonical-target" {
			found = true
		}
	}
	if !found {
		t.Fatalf("canonical target missing from sessions: %+v", rows)
	}
}

func TestDeleteSessionDeletesCanonicalIdentity(t *testing.T) {
	legacyDir := t.TempDir()
	v4Root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("serve-test", session.NewFilesystemPersistence(v4Root))
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: legacyDir, SessionService: service, ExclusiveSession: true})
	current, err := ctrl.BindFreshSession(t.Context(), "current")
	if err != nil {
		t.Fatal(err)
	}
	target, err := service.Create(t.Context(), session.CreateOptions{SessionID: "canonical-target"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), target.Ref()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	srv := httptest.NewServer(newLifecycleTestServer(t, ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()
	post := func(body string) int {
		resp, err := http.Post(srv.URL+"/delete-session", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(`{"name":"` + current.SessionID + `","sessionId":"` + current.SessionID + `"}`); got != http.StatusConflict {
		t.Fatalf("active canonical delete status = %d, want 409", got)
	}
	if got := post(`{"name":"canonical-target","sessionId":"canonical-target"}`); got != http.StatusNoContent {
		t.Fatalf("canonical delete status = %d, want 204", got)
	}
	if _, err := os.Stat(filepath.Join(v4Root, "canonical-target")); !os.IsNotExist(err) {
		t.Fatalf("canonical session still exists or stat failed unexpectedly: %v", err)
	}
}

func newExclusiveSessionServe(t *testing.T) (*Server, *control.Controller, *session.Service, session.SessionRef) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("serve-test", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown session service: %v", err)
		}
	})
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Executor: exec, SessionDir: t.TempDir(), SessionService: service, ExclusiveSession: true,
	})
	ref, err := ctrl.BindFreshSession(t.Context(), "current")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	bc := NewBroadcaster()
	srv := New(ctrl, bc, config.ServeConfig{})
	// Production serve hosts register a frame tag per controller; the takeover
	// tests assert its identity, so wire the sink the CLI server would.
	tag := newSessionTagSink(bc)
	if id, bound := ctrl.SessionRef(); bound {
		tag.SetIdentity("", id.SessionID)
	}
	srv.RegisterSessionTag(ctrl, tag)
	return srv, ctrl, service, ref
}

func TestExclusiveV3SessionsAndResumeUseImmutableIdentity(t *testing.T) {
	srv, ctrl, service, current := newExclusiveSessionServe(t)
	target, err := service.Create(t.Context(), session.CreateOptions{SessionID: "target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), target.Ref()); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	srv.sessions(list, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	foundCurrent, foundTarget := false, false
	for _, row := range rows {
		if row.SessionID == current.SessionID && row.HostID == current.HostID && row.Current && row.Path == "" {
			foundCurrent = true
		}
		if row.SessionID == "target" && row.HostID == current.HostID && row.Path == "" {
			foundTarget = true
		}
	}
	if !foundCurrent || !foundTarget {
		t.Fatalf("v3 rows = %+v", rows)
	}

	resume := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resume", strings.NewReader(`{"hostId":"serve-test","sessionId":"target"}`))
	srv.resume(resume, req)
	if resume.Code != http.StatusNoContent {
		t.Fatalf("resume status = %d: %s", resume.Code, resume.Body.String())
	}
	if got := resume.Header().Get(sessionIDHeader); got != "target" {
		t.Fatalf("resume session id = %q", got)
	}
	if got, ok := ctrl.SessionRef(); !ok || got.SessionID != "target" || ctrl.SessionPath() != "" {
		t.Fatalf("controller identity = %+v, bound=%v path=%q", got, ok, ctrl.SessionPath())
	}
}

func TestSessionsReportsFinalizingExclusiveRuntimeAsRunning(t *testing.T) {
	srv, ctrl, service, ref := newExclusiveSessionServe(t)
	runtime, ok := service.Runtime(ref)
	if !ok {
		t.Fatal("current runtime is not published")
	}
	generation := ctrl.ExecutionGeneration()
	runtime.NoteExecution(generation, session.RuntimeFinalizing, "terminal_commit")
	defer runtime.NoteExecution(generation, session.RuntimeIdle, "")

	list := httptest.NewRecorder()
	srv.sessions(list, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.SessionID == ref.SessionID {
			if !row.Running {
				t.Fatalf("finalizing session row = %+v, want running", row)
			}
			return
		}
	}
	t.Fatalf("session %q missing from rows %+v", ref.SessionID, rows)
}

func TestExclusiveV3MissingResumeDoesNotCreateOrReplaceCurrent(t *testing.T) {
	srv, ctrl, service, current := newExclusiveSessionServe(t)
	resume := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resume", strings.NewReader(`{"hostId":"serve-test","sessionId":"missing"}`))
	srv.resume(resume, req)
	if resume.Code != http.StatusConflict {
		t.Fatalf("resume status = %d, want 409", resume.Code)
	}
	if got, ok := ctrl.SessionRef(); !ok || got != current {
		t.Fatalf("current identity changed to %+v, bound=%v", got, ok)
	}
	if _, err := service.Query().Snapshot(t.Context(), session.SessionRef{HostID: "serve-test", SessionID: "missing"}); err == nil {
		t.Fatal("missing Open created a session")
	}
}

func TestExclusiveV3ResumeNameFallbackUsesCanonicalIdentity(t *testing.T) {
	srv, ctrl, service, current := newExclusiveSessionServe(t)
	target, err := service.Create(t.Context(), session.CreateOptions{SessionID: "named-target"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), target.Ref()); err != nil {
		t.Fatal(err)
	}

	resume := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/resume", strings.NewReader(`{"name":"named-target"}`))
	srv.resume(resume, req)
	if resume.Code != http.StatusNoContent {
		t.Fatalf("name fallback status = %d: %s", resume.Code, resume.Body.String())
	}
	if got := resume.Header().Get(sessionIDHeader); got != "named-target" {
		t.Fatalf("name fallback session id = %q", got)
	}
	if got, ok := ctrl.SessionRef(); !ok || got.HostID != current.HostID || got.SessionID != "named-target" {
		t.Fatalf("controller identity after name fallback = %+v, bound=%v", got, ok)
	}
}

func TestExclusiveV3RotationAndMutationFenceReturnSessionID(t *testing.T) {
	srv, ctrl, _, current := newExclusiveSessionServe(t)
	stale := httptest.NewRequest(http.MethodPost, "/cancel", nil)
	stale.Header.Set(expectedSessionIDHeader, "stale")
	if err := srv.expectedSessionErrorLocked(stale); err == nil {
		t.Fatal("stale immutable identity passed mutation fence")
	}
	matching := httptest.NewRequest(http.MethodPost, "/cancel", nil)
	matching.Header.Set(expectedSessionIDHeader, current.SessionID)
	if err := srv.expectedSessionErrorLocked(matching); err != nil {
		t.Fatalf("matching identity rejected: %v", err)
	}

	rotate := httptest.NewRecorder()
	srv.newSession(rotate, httptest.NewRequest(http.MethodPost, "/new", nil))
	if rotate.Code != http.StatusNoContent {
		t.Fatalf("new status = %d: %s", rotate.Code, rotate.Body.String())
	}
	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID == current.SessionID || ref.SessionID == "" {
		t.Fatalf("rotated identity = %+v, bound=%v", ref, ok)
	}
	if got := rotate.Header().Get(sessionIDHeader); got != ref.SessionID {
		t.Fatalf("new response session id = %q, want %q", got, ref.SessionID)
	}
	if got := rotate.Header().Get(sessionPathHeader); got != "" {
		t.Fatalf("exclusive rotation exposed legacy path %q", got)
	}
}

// The persistence list caps one page at 100 rows ordered by the random
// session id, so a workspace with more canonical sessions hid an arbitrary
// subset — including a session another runtime had just taken over. The
// handler must follow NextCursor and surface every row in one response.
func TestSessionsListsBeyondFirstHundredCanonicalSessions(t *testing.T) {
	v4Root := filepath.Join(robustTempDir(t), "sessions-v4")
	service, err := session.NewService("serve-test", session.NewFilesystemPersistence(v4Root))
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: robustTempDir(t), SessionService: service, ExclusiveSession: true})
	const total = 103
	for i := range total {
		created, err := service.Create(t.Context(), session.CreateOptions{SessionID: fmt.Sprintf("s%03d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Close(t.Context(), created.Ref()); err != nil {
			t.Fatal(err)
		}
	}
	// A catalog full of pending metadata keeps the query's background rebuild
	// queue busy, which races the lifecycle fixture's writer-retire check.
	// Drive the queue to quiescence before the handler under test runs.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		page, err := service.Query().List(t.Context(), "", 100)
		if err != nil {
			t.Fatal(err)
		}
		pending := 0
		for _, info := range page.Sessions {
			if info.MetadataStatus != session.MetadataReady {
				pending++
			}
		}
		if pending == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Cleanup(ctrl.Close)
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	// Service-backed controllers park their runtime in the idle cache after
	// close, so the lifecycle fixture's writer-retire wait does not apply;
	// these listing tests only need the HTTP surface.
	srv := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	t.Cleanup(srv.Close)
	recorder := httptest.NewRecorder()
	srv.sessions(recorder, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		seen[row.SessionID] = true
	}
	for i := range total {
		if !seen[fmt.Sprintf("s%03d", i)] {
			t.Fatalf("session s%03d missing from the list (%d rows returned)", i, len(rows))
		}
	}
}
