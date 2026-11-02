package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func postRemoveSessions(t *testing.T, srv *httptest.Server, paths []string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string][]string{"paths": paths})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/tree/sessions/remove-many", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A batch request erases every named transcript in one shot, and leaves nothing
// behind for the ones it removed.
func TestRemoveSessionsDeletesEachPath(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	paths := []string{
		filepath.Join(SessionDirFor(root), "a.jsonl"),
		filepath.Join(SessionDirFor(root), "b.jsonl"),
		filepath.Join(SessionDirFor(root), "c.jsonl"),
	}
	for _, p := range paths {
		writeSessionAt(t, p)
	}

	resp := postRemoveSessions(t, srv, paths)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 204: %s", resp.StatusCode, b)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("transcript survived the batch delete: %s", p)
		}
		if _, err := os.Stat(store.SessionEventLog(p)); !os.IsNotExist(err) {
			t.Errorf("event log survived the batch delete: %s", p)
		}
	}
}

// One held transcript must refuse the whole batch: nothing is deleted, not even
// the paths that were free. A partial delete would strand a reader mid-way.
func TestRemoveSessionsRefusesALeasedTranscript(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	a := filepath.Join(SessionDirFor(root), "a.jsonl")
	b := filepath.Join(SessionDirFor(root), "b.jsonl")
	writeSessionAt(t, a)
	writeSessionAt(t, b)

	lease, err := sessionstore.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	resp := postRemoveSessions(t, srv, []string{a, b})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 409 while one path is held: %s", resp.StatusCode, b)
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a refused batch deleted %s anyway: %v", p, err)
		}
	}
}

// A path that is not inside a known workspace is rejected, and the valid paths
// in the same request are left untouched.
func TestRemoveSessionsRefusesAnUnknownWorkspace(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	inside := filepath.Join(SessionDirFor(root), "inside.jsonl")
	writeSessionAt(t, inside)
	outside := filepath.Join(testenv.TempDir(t), "outside.jsonl")
	writeSessionAt(t, outside)

	resp := postRemoveSessions(t, srv, []string{inside, outside})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 403 for an out-of-workspace path: %s", resp.StatusCode, b)
	}
	if _, err := os.Stat(inside); err != nil {
		t.Errorf("a refused batch deleted the in-workspace session: %v", err)
	}
}

// An empty list is not a valid request; the endpoint answers before touching
// anything on disk.
func TestRemoveSessionsRefusesAnEmptyList(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	resp := postRemoveSessions(t, srv, nil)
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("POST /tree/sessions/remove-many = %d, want a client error for an empty list", resp.StatusCode)
	}
}

// Duplicates in the request are erased once, not repeatedly — the second mention
// must not turn into a second guard acquisition against an already-gone file.
func TestRemoveSessionsDeduplicatesPaths(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	p := filepath.Join(SessionDirFor(root), "dup.jsonl")
	writeSessionAt(t, p)

	resp := postRemoveSessions(t, srv, []string{p, p})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 204 for a deduplicated list: %s", resp.StatusCode, b)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("transcript survived the delete: %v", err)
	}
}

// A transcript that lives in the session directory of a project this hub does
// not list is refused, and the refusal is the whole batch: an in-workspace
// transcript in the same request must be left untouched, never deleted partway.
// The hub only lists the workspace it adopted, so a second project's session
// directory is a "different project" the delete may not reach into.
func TestRemoveSessionsRefusesAPathFromAnotherProject(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	other := testenv.TempDir(t)
	inside := filepath.Join(SessionDirFor(root), "inside.jsonl")
	outside := filepath.Join(SessionDirFor(other), "outside.jsonl")
	writeSessionAt(t, inside)
	writeSessionAt(t, outside)

	resp := postRemoveSessions(t, srv, []string{inside, outside})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 403 for a path in another project: %s", resp.StatusCode, b)
	}
	for _, p := range []string{inside, outside} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a refused batch deleted %s anyway: %v", p, err)
		}
	}
}

// A conversation a pane is actively running must refuse the whole batch, and the
// refusal must land before any transcript is erased: a session earlier in the
// list survives untouched, not deleted partway through validation. The running
// session sits in the middle so the handler has already accepted the one before
// it by the time it refuses.
func TestRemoveSessionsRefusesARunningSessionInTheMiddle(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	bc := NewBroadcaster()
	runRelease := make(chan struct{})
	// Release the running turn on the way out even if an assertion fails, so the
	// goroutine driving RunTurn never leaks into a later test.
	defer close(runRelease)
	ctrl := control.New(control.Options{
		WorkspaceRoot: root,
		SessionDir:    SessionDirFor(root),
		Runner:        runningPaneRunner{release: runRelease},
		Sink:          bc,
	})
	rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	defer h.Shutdown()
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	dir := SessionDirFor(root)
	before := filepath.Join(dir, "before.jsonl")
	mid := filepath.Join(dir, "mid.jsonl")
	after := filepath.Join(dir, "after.jsonl")
	writeSessionAt(t, before)
	writeSessionAt(t, mid)
	writeSessionAt(t, after)

	// Pin the running turn to `mid` so the delete sees it as a live session, and
	// release the turn only after the refusal so it is genuinely mid-delete.
	rt.Server.Controller().SetSessionPath(mid)
	go func() { _ = rt.Server.Controller().RunTurn(context.Background(), "go") }()
	deadline := time.Now().Add(5 * time.Second)
	for !rt.Server.Controller().Running() {
		if time.Now().After(deadline) {
			t.Fatal("the turn never started running")
		}
		time.Sleep(time.Millisecond)
	}
	if got := rt.Server.Controller().SessionPath(); got != mid {
		t.Fatalf("running session path = %q, want the pinned %q", got, mid)
	}

	resp := postRemoveSessions(t, srv, []string{before, mid, after})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("POST /tree/sessions/remove-many = %d, want 409 while a mid-list session runs: %s", resp.StatusCode, b)
	}
	for _, p := range []string{before, mid, after} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("a refused batch deleted %s before the running refusal: %v", p, err)
		}
	}
}

// runningPaneRunner holds a turn open: the controller reports Running() for the
// whole time the test is driving the delete around it, then the turn finishes
// once the channel is closed.
type runningPaneRunner struct{ release chan struct{} }

func (b runningPaneRunner) Run(_ context.Context, _ string) error {
	<-b.release
	return nil
}
