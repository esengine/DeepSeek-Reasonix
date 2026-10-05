package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionv4/v4fixture"
)

type importAnswer struct {
	Imported   int  `json:"imported"`
	Warnings   int  `json:"warnings"`
	Recognised bool `json:"recognised"`
}

func importLegacy(t *testing.T, srv *httptest.Server, path, workspace string) importAnswer {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"path": path, "workspace": workspace})
	resp, err := http.Post(srv.URL+"/tree/sessions/import-legacy", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import-legacy = %d, want 200", resp.StatusCode)
	}
	var out importAnswer
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImportLegacyReportsWhetherTheFolderWasRecognised(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	empty := testenv.TempDir(t)
	if got := importLegacy(t, srv, empty, root); got.Recognised || got.Imported != 0 {
		t.Fatalf("empty folder answered %+v, want unrecognised", got)
	}

	old := testenv.TempDir(t)
	sessions := filepath.Join(old, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	log := `{"role":"user","content":"hello"}` + "\n" + `{"role":"assistant","content":"hi"}` + "\n"
	if err := os.WriteFile(filepath.Join(sessions, "a.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := importLegacy(t, srv, old, root); !got.Recognised || got.Imported != 1 {
		t.Fatalf("first import answered %+v, want recognised with 1", got)
	}
	if got := importLegacy(t, srv, old, root); !got.Recognised || got.Imported != 0 {
		t.Fatalf("second import answered %+v, want recognised with 0", got)
	}
}

func TestEveryRowAParentFolderImportListsOpens(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	rt := hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	old := testenv.TempDir(t)
	log := `{"role":"user","content":"hello"}` + "\n" + `{"role":"assistant","content":"hi"}` + "\n"
	for _, dir := range []string{filepath.Join(old, "sessions"), filepath.Join(old, "projects", "p", "sessions")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(filepath.Dir(dir))+".jsonl"), []byte(log), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := v4fixture.New(t)
	v4 := store.Session("0123456789abcdef0123456789abcdef", 3)
	store.Batch(v4, v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "from v4")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "assistant", "ok")})
	if err := os.Rename(store.Root, filepath.Join(old, "sessions-v4")); err != nil {
		t.Fatal(err)
	}

	if got := importLegacy(t, srv, old, root); got.Imported != 3 {
		t.Fatalf("imported %d, want 3", got.Imported)
	}
	rows := 0
	for _, ws := range hubGet[[]treeWorkspace](t, srv, "/tree") {
		for _, s := range ws.Sessions {
			rows++
			if status, err := rt.Server.resumeInto(s.Path); err != nil {
				t.Errorf("row %q does not open: %d %v", s.Title, status, err)
			}
		}
	}
	if rows == 0 {
		t.Fatal("the import listed no rows")
	}
}
