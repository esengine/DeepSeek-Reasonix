package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionstore"
)

func TestSyncPinsOnlyTouchesSessionsTheKernelLists(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	t.Cleanup(func() { pinsSynced.Store(false) })

	listed := filepath.Join(SessionDirFor(root), "listed.jsonl")
	writeSessionAt(t, listed)
	outside := filepath.Join(testenv.TempDir(t), "outside.jsonl")
	writeSessionAt(t, outside)
	missing := filepath.Join(SessionDirFor(root), "missing.jsonl")

	body, _ := json.Marshal(map[string][]string{"paths": {listed, outside, missing, "../../etc/passwd"}})
	resp, err := http.Post(srv.URL+"/tree/sessions/pins", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	pinned := func(p string) bool {
		m, ok, _ := sessionstore.LoadBranchMeta(p)
		return ok && m.Pinned
	}
	if !pinned(listed) {
		t.Error("a listed session was not pinned")
	}
	if pinned(outside) {
		t.Error("a session outside every workspace was pinned")
	}
	if _, ok, _ := sessionstore.LoadBranchMeta(missing); ok {
		t.Error("a session the kernel does not list gained a sidecar")
	}
	if !pinsSynced.Load() {
		t.Error("the sync was not recorded")
	}
}

func TestPinSessionOnlyTouchesSessionsTheKernelLists(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	listed := filepath.Join(SessionDirFor(root), "listed.jsonl")
	writeSessionAt(t, listed)
	outside := filepath.Join(testenv.TempDir(t), "outside.jsonl")
	writeSessionAt(t, outside)
	post := func(p string) int {
		body, _ := json.Marshal(map[string]any{"path": p, "pinned": true})
		resp, err := http.Post(srv.URL+"/tree/sessions/pin", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(listed); code != http.StatusNoContent {
		t.Fatalf("listed: %d", code)
	}
	if m, _, _ := sessionstore.LoadBranchMeta(listed); !m.Pinned {
		t.Error("a listed session was not pinned")
	}
	if code := post(outside); code != http.StatusForbidden {
		t.Fatalf("outside: %d, want 403", code)
	}
	if m, _, _ := sessionstore.LoadBranchMeta(outside); m.Pinned {
		t.Error("a session outside every workspace was pinned")
	}
}
