package serve

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/backgroundwork"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func TestResumePreemptsProjectionButTreeRefreshDoesNot(t *testing.T) {
	f := newUnreadHarness(t, "plain")
	path := filepath.Join(SessionDirFor(f.root), "history.jsonl")
	session := sessionstore.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "saved history"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl, err := boot.Build(t.Context(), boot.Options{Sink: bc, Home: f.home, WorkspaceRoot: f.root, SessionDir: SessionDirFor(f.root)})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{})
	rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		ctrl.Close()
		t.Fatal(err)
	}
	t.Cleanup(h.Shutdown)
	job, release, err := backgroundwork.Shared.Acquire(context.Background(), backgroundwork.Rebuild)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h.tree(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/tree?progressive=1", nil))
	if job.Err() != nil {
		t.Fatal("tree refresh preempted its producer")
	}
	if _, err := rt.Server.resumeInto(path); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(job), backgroundwork.ErrPreempted) {
		t.Fatalf("cause=%v", context.Cause(job))
	}
	if got := rt.Server.Controller().History(); len(got) < 2 || got[len(got)-1].Content != "saved history" {
		t.Fatalf("history=%v", got)
	}
	release()
	mux := http.NewServeMux()
	rt.Server.registerHistoryRoutes(mux)
	for _, path := range []string{"/history", "/history/page", "/history/tool/missing"} {
		job, release, err := backgroundwork.Shared.Acquire(t.Context(), backgroundwork.Rebuild)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		release()
		if !errors.Is(context.Cause(job), backgroundwork.ErrPreempted) {
			t.Fatalf("%s did not preempt background projection: %v", path, context.Cause(job))
		}
		if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
