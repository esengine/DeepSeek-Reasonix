package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// unreadModel answers every round with a short text, fails it, or blocks until
// the turn is cancelled, depending on mode.
type unreadModel struct {
	mode    string
	started chan struct{}
	once    sync.Once
}

func (m *unreadModel) Name() string { return "unread-model" }

func (m *unreadModel) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	switch m.mode {
	case "block":
		m.once.Do(func() { close(m.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	case "fail":
		return nil, errors.New("model refused the request")
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var unreadKinds atomic.Int64

type unreadHarness struct {
	t     *testing.T
	root  string
	home  string
	model *unreadModel
}

func newUnreadHarness(t *testing.T, mode string) *unreadHarness {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root := testenv.TempDir(t)
	model := &unreadModel{mode: mode, started: make(chan struct{})}
	kind := fmt.Sprintf("unread-%s-%d", t.Name(), unreadKinds.Add(1))
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return model, nil })
	toml := "default_model = \"test-model\"\n\n[agent]\nsystem_prompt = \"BASE\"\n\n[[providers]]\nname = \"test-model\"\nkind = \"" + kind + "\"\nmodel = \"x\"\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.RootsForHome(home).ApproveWorkspacePrograms(root); err != nil {
		t.Fatal(err)
	}
	return &unreadHarness{t: t, root: root, home: home, model: model}
}

// pane builds one runtime through the real assembly on the harness workspace,
// bound to path, behind a fresh hub: what a restarted window gets.
func (h *unreadHarness) pane(path string) (*Hub, *Runtime, *Broadcaster) {
	h.t.Helper()
	bc := NewBroadcaster()
	ctrl, err := boot.Build(h.t.Context(), boot.Options{Sink: bc, Home: h.home, WorkspaceRoot: h.root})
	if err != nil {
		h.t.Fatalf("Build: %v", err)
	}
	h.t.Cleanup(ctrl.Close)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	ctrl.SetFreshSessionPath(path)
	hub := NewHub(HubOptions{})
	rt, err := hub.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		h.t.Fatalf("adopt: %v", err)
	}
	return hub, rt, bc
}

// runTurn submits one turn and waits for its turn_done frame.
func (h *unreadHarness) runTurn(rt *Runtime, bc *Broadcaster, cancelAfterStart bool) {
	h.t.Helper()
	ch, unsubscribe := bc.Subscribe()
	defer unsubscribe()
	rt.Server.Controller().Send("hello")
	if cancelAfterStart {
		select {
		case <-h.model.started:
		case <-time.After(testenv.Budget(h.t) / 2):
			h.t.Fatal("the turn never reached the model")
		}
		rt.Server.Controller().Cancel()
	}
	deadline := time.After(testenv.Budget(h.t) / 2)
	for {
		select {
		case frame := <-ch:
			var e struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal(frame.Data, &e) == nil && e.Kind == "turn_done" {
				return
			}
		case <-deadline:
			h.t.Fatal("the turn never closed")
		}
	}
}

func (h *unreadHarness) treeRow(srv *httptest.Server, path string) treeSession {
	h.t.Helper()
	for _, ws := range hubGet[[]treeWorkspace](h.t, srv, "/tree") {
		for _, s := range ws.Sessions {
			if filepath.Clean(s.Path) == filepath.Clean(path) {
				return s
			}
		}
	}
	h.t.Fatalf("no sidebar row for %s", path)
	return treeSession{}
}

func postViewed(t *testing.T, srv *httptest.Server, prefix string) int {
	t.Helper()
	resp, err := http.Post(srv.URL+prefix+"/sessions/viewed", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestFinishedTurnShowsUnreadUntilViewedAndSurvivesARestart(t *testing.T) {
	h := newUnreadHarness(t, "text")
	path := filepath.Join(SessionDirFor(h.root), "20260101-000001-m.jsonl")
	hub, rt, bc := h.pane(path)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()

	h.runTurn(rt, bc, false)
	if !h.treeRow(srv, path).Unread {
		t.Fatal("a turn finished and the sidebar row is not unread")
	}
	if err := hub.Close(rt.ID); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	hub2, rt2, _ := h.pane(path)
	srv2 := httptest.NewServer(operatorHandler(hub2))
	defer srv2.Close()
	if !h.treeRow(srv2, path).Unread {
		t.Fatal("the unread mark did not survive a restart")
	}
	if got := postViewed(t, srv2, "/rt/"+rt2.ID); got != http.StatusNoContent {
		t.Fatalf("POST /sessions/viewed = %d, want 204", got)
	}
	if h.treeRow(srv2, path).Unread {
		t.Fatal("the row is still unread after the session was viewed")
	}

	if err := hub2.Close(rt2.ID); err != nil {
		t.Fatal(err)
	}
	hub3, _, _ := h.pane(path)
	srv3 := httptest.NewServer(operatorHandler(hub3))
	defer srv3.Close()
	if h.treeRow(srv3, path).Unread {
		t.Fatal("a viewed session came back unread after another restart")
	}
}

func TestListedSessionCarriesUnreadAndTheNextTurnMarksItAgain(t *testing.T) {
	h := newUnreadHarness(t, "text")
	path := filepath.Join(config.SessionDir(), "20260101-000001-m.jsonl")
	hub, rt, bc := h.pane(path)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	listed := func() bool {
		rows := hubGet[[]struct {
			Path   string `json:"path"`
			Unread bool   `json:"unread"`
		}](t, srv, "/rt/"+rt.ID+"/sessions")
		for _, r := range rows {
			if filepath.Clean(r.Path) == filepath.Clean(path) {
				return r.Unread
			}
		}
		t.Fatal("session missing from GET /sessions")
		return false
	}
	h.runTurn(rt, bc, false)
	if !listed() {
		t.Fatal("GET /sessions does not carry unread after a finished turn")
	}
	postViewed(t, srv, "/rt/"+rt.ID)
	if listed() {
		t.Fatal("GET /sessions still unread after viewing")
	}
	h.runTurn(rt, bc, false)
	if !listed() {
		t.Fatal("a second finished turn did not mark the session unread again")
	}
}

func TestSessionsThatNoTurnFinishedInAreNotUnread(t *testing.T) {
	h := newUnreadHarness(t, "text")
	dir := SessionDirFor(h.root)
	old := filepath.Join(dir, "20250101-000001-m.jsonl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "old prompt"})
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"20250101-000001-m","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z","turns":1,"preview":"old prompt","schema_version":2}`
	if err := os.WriteFile(sessionstore.BranchMetaPath(old), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	hub, _, _ := h.pane(filepath.Join(dir, "20260101-000001-m.jsonl"))
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	if h.treeRow(srv, old).Unread {
		t.Fatal("a session from before the field existed shows unread")
	}
}

func TestTurnTheUserCancelledIsNotUnreadButAFailedOneIs(t *testing.T) {
	cancelled := newUnreadHarness(t, "block")
	cPath := filepath.Join(SessionDirFor(cancelled.root), "20260101-000001-m.jsonl")
	seedTranscript(t, cPath)
	hub, rt, bc := cancelled.pane(cPath)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	cancelled.runTurn(rt, bc, true)
	if cancelled.treeRow(srv, cPath).Unread {
		t.Fatal("a turn the person cancelled was left unread")
	}

	failed := newUnreadHarness(t, "fail")
	fPath := filepath.Join(SessionDirFor(failed.root), "20260101-000001-m.jsonl")
	seedTranscript(t, fPath)
	hub2, rt2, bc2 := failed.pane(fPath)
	srv2 := httptest.NewServer(operatorHandler(hub2))
	defer srv2.Close()
	failed.runTurn(rt2, bc2, false)
	if !failed.treeRow(srv2, fPath).Unread {
		t.Fatal("a turn that failed on its own was not marked unread")
	}
}

func seedTranscript(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "earlier"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "earlier answer"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}

func TestSeveralWindowsViewingOneSessionAtOnceAllSucceed(t *testing.T) {
	h := newUnreadHarness(t, "text")
	path := filepath.Join(SessionDirFor(h.root), "20260101-000001-m.jsonl")
	hub, rt, bc := h.pane(path)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	h.runTurn(rt, bc, false)

	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Go(func() { codes <- postViewed(t, srv, "/rt/"+rt.ID) })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusNoContent {
			t.Fatalf("a concurrent view answered %d, want 204", code)
		}
	}
	if h.treeRow(srv, path).Unread {
		t.Fatal("concurrent views left the row unread")
	}
}

func TestViewingASessionWithNothingFinishedChangesNothing(t *testing.T) {
	h := newUnreadHarness(t, "text")
	path := filepath.Join(SessionDirFor(h.root), "20260101-000001-m.jsonl")
	seedTranscript(t, path)
	hub, rt, _ := h.pane(path)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	before, err := os.ReadFile(sessionstore.BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := postViewed(t, srv, "/rt/"+rt.ID); got != http.StatusNoContent {
		t.Fatalf("POST /sessions/viewed = %d, want 204", got)
	}
	after, err := os.ReadFile(sessionstore.BranchMetaPath(path))
	if err != nil || string(before) != string(after) {
		t.Fatalf("viewing a read session rewrote its sidecar (err=%v)", err)
	}
}

func TestDeletingASessionTakesItsUnreadMarkWithIt(t *testing.T) {
	h := newUnreadHarness(t, "text")
	path := filepath.Join(SessionDirFor(h.root), "20260101-000001-m.jsonl")
	hub, rt, bc := h.pane(path)
	srv := httptest.NewServer(operatorHandler(hub))
	defer srv.Close()
	h.runTurn(rt, bc, false)
	row := h.treeRow(srv, path)
	if !row.Unread {
		t.Fatal("fixture: the finished turn is not unread")
	}
	remember, err := http.Post(srv.URL+"/tree/workspaces", "application/json", strings.NewReader(`{"path":`+strconv.Quote(h.root)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	remember.Body.Close()
	body, err := json.Marshal(map[string]string{"path": row.Path})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/tree/sessions/remove", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /tree/sessions/remove = %d, want 204: %s", resp.StatusCode, got)
	}
	if _, err := os.Stat(sessionstore.BranchMetaPath(path)); !os.IsNotExist(err) {
		t.Fatalf("the sidecar holding the mark outlived its session (err=%v)", err)
	}
}
