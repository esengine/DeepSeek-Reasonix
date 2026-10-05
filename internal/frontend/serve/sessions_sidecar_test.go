package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// GET /sessions must answer from the session sidecars. Decoding each transcript
// to count turns made the listing O(sessions x transcript size) on every
// refresh, which is the sidebar load the sidecars exist to avoid. Corrupting
// the transcript bodies is how the test tells the two apart: a reader that
// still parses them cannot report these turn counts.
func TestSessionsListReadsSidecarsNotTranscripts(t *testing.T) {
	dir := testenv.TempDir(t)
	paths := []string{
		filepath.Join(dir, "20260101-000001-model.jsonl"),
		filepath.Join(dir, "20260101-000002-model.jsonl"),
	}
	for i, path := range paths {
		s := sessionstore.NewSession("system")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
		if i == 1 {
			s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
			s.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
		}
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("not json at all\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Name  string `json:"name"`
		Turns int    `json:"turns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("GET /sessions returned %d entries, want 2: %+v", len(got), got)
	}
	turns := map[string]int{}
	for _, entry := range got {
		turns[entry.Name] = entry.Turns
	}
	if turns["20260101-000001-model"] != 1 || turns["20260101-000002-model"] != 2 {
		t.Fatalf("turn counts came from the corrupt transcripts, not the sidecars: %+v", got)
	}
}
