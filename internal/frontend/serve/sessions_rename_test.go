package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestRenameSessionAtTitlesASavedConversationAndRefusesUnknownIDs(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "20260101-000001-model.jsonl")
	s := sessionstore.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	post := func(body string) int {
		resp, err := http.Post(srv.URL+"/sessions/rename", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(`{"id":"20260101-000001-model","title":"Release notes"}`); got != http.StatusNoContent {
		t.Fatalf("rename status = %d", got)
	}
	meta, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != "Release notes" {
		t.Fatalf("title not stored: %+v ok=%v err=%v", meta, ok, err)
	}
	list, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	var shown []struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(list.Body).Decode(&shown); err != nil || len(shown) != 1 || shown[0].Title != "Release notes" {
		t.Fatalf("the session list ignores the name the person gave: %+v err=%v", shown, err)
	}
	if got := post(`{"id":"../../etc/passwd","title":"x"}`); got != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", got)
	}
}
