package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func TestHistoryPageAfterRealBootResume(t *testing.T) {
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
	if _, err := rt.Server.resumeInto(path); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	rt.Server.registerHistoryRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history/page?limit=1", nil))
	var page historyPageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(page.Messages) != 1 || page.Messages[0].Content != "saved history" || page.Messages[0].MsgIndex != page.Start || !page.HasMoreBefore || page.NextCursor == "" {
		t.Fatalf("paged resume=%d %+v", w.Code, page)
	}
}
