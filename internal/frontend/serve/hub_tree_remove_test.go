package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func writeSessionAt(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
}

func postExportSession(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/tree/sessions/export", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestExportSessionReadsOnlyAKnownWorkspaceTranscript(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	path := filepath.Join(SessionDirFor(root), "export-me.jsonl")
	writeSessionAt(t, path)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	resp := postExportSession(t, srv, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /tree/sessions/export = %d, want 200: %s", resp.StatusCode, got)
	}
	var out struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "export-me" || out.Content != string(want) {
		t.Errorf("export = (%q, %d bytes), want (export-me, %d bytes)", out.Name, len(out.Content), len(want))
	}

	outside := filepath.Join(testenv.TempDir(t), "outside.jsonl")
	writeSessionAt(t, outside)
	refused := postExportSession(t, srv, outside)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Errorf("outside export = %d, want 403", refused.StatusCode)
	}
}

func postRemoveSession(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/tree/sessions/remove", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A conversation something is still writing to must not be deleted, even when
// it is not the transcript any pane is currently pointed at — a recovery branch
// and a session mid-rotation are both held without being anyone's current path.
// The pane map cannot see either, so the lease is what has to answer.
func TestRemoveSessionRefusesALeasedTranscript(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	path := filepath.Join(SessionDirFor(root), "held.jsonl")
	writeSessionAt(t, path)

	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	resp := postRemoveSession(t, srv, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("POST /tree/sessions/remove = %d, want 409 while the lease is held", resp.StatusCode)
	}
	// The code is the only part a reader ever sees: the frontend looks up its
	// own wording by it, and falls back to printing this response otherwise.
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if body.Code != "session.in_use" {
		t.Errorf("refusal code = %q, want session.in_use", body.Code)
	}
	// Refused means refused: not one byte may be gone.
	for _, p := range []string{path, store.SessionEventLog(path)} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("artifact erased despite the refusal: %s (%v)", p, err)
		}
	}
	if sessionstore.IsCleanupPending(path) {
		t.Error("a refused delete must not mark the session for cleanup")
	}
}

// The same session deletes normally once nothing holds it, so the guard cannot
// be a session that can never be removed.
func TestRemoveSessionSucceedsOnceTheLeaseIsReleased(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	path := filepath.Join(SessionDirFor(root), "free.jsonl")
	writeSessionAt(t, path)

	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	lease.Release()

	resp := postRemoveSession(t, srv, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /tree/sessions/remove = %d, want 204 after release", resp.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("transcript survived the delete: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(path)); !os.IsNotExist(err) {
		t.Error("event log survived the delete, so the conversation could be resurrected")
	}
}

// A holder in another process is invisible to the pane map, so the sidebar
// offers no pane to close and the window no button that would close one. The
// refusal has to name the holder the guard already read.
func TestRemoveSessionNamesTheProcessHoldingIt(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	path := filepath.Join(SessionDirFor(root), "elsewhere.jsonl")
	writeSessionAt(t, path)

	// A holder this process cannot reach, written the way another one writes it.
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	stampForeignLeaseHolder(t, path, os.Getpid()+1)

	resp := postRemoveSession(t, srv, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /tree/sessions/remove = %d, want 409", resp.StatusCode)
	}
	var body struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if body.Code != "session.in_use_by" {
		t.Fatalf("refusal code = %q, want the one that carries a holder", body.Code)
	}
	if pid, ok := body.Params["pid"].(float64); !ok || int(pid) != os.Getpid()+1 {
		t.Fatalf("params = %v, want the holder's pid — without it the reader has nothing to act on", body.Params)
	}
}

// stampForeignLeaseHolder rewrites the lease's recorded holder. Only the info
// file names one; the lock itself does not.
func stampForeignLeaseHolder(t *testing.T, path string, pid int) {
	t.Helper()
	info, err := sessionstore.LoadSessionLeaseInfo(path)
	if err != nil || info == nil {
		t.Fatalf("LoadSessionLeaseInfo: %v", err)
	}
	info.PID = pid
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionLeaseInfo(path), raw, 0o600); err != nil {
		t.Fatalf("write lease info: %v", err)
	}
}

// A pane that is only showing the conversation is no reason to keep it: the
// pane closes, and the delete goes through.
func TestRemoveSessionClosesAnIdlePaneShowingIt(t *testing.T) {
	writeOpenableConfig(t)
	root := testenv.TempDir(t)
	path := filepath.Join(SessionDirFor(root), "shown.jsonl")
	writeSessionAt(t, path)
	rememberWorkspace(root)
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	if _, err := h.Open(context.Background(), OpenRequest{Root: root, SessionPath: path}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	resp := postRemoveSession(t, srv, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("remove = %d: %s", resp.StatusCode, b)
	}
	if n := len(h.Runtimes()); n != 0 {
		t.Fatalf("%d panes left on a deleted conversation, want none", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("transcript survived the delete: %v", err)
	}
}
