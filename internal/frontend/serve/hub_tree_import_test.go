package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

type skipAnswer struct {
	Warnings int `json:"warnings"`
	Skipped  []struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func TestImportLegacyNamesEverySessionItCouldNotReadWithATypedReason(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	old := testenv.TempDir(t)
	store := v4fixture.New(t)
	const good, huge, newer, broken = "0123456789abcdef0123456789abcdef", "1123456789abcdef0123456789abcdef", "2123456789abcdef0123456789abcdef", "3123456789abcdef0123456789abcdef"
	store.Batch(store.Session(good, 3), v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "fine")},
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m2", "assistant", "ok")})
	oversized := &v4fixture.Ref{Digest: strings.Repeat("a", 64), Bytes: 300 << 20}
	store.Batch(store.Session(huge, 3), v4fixture.Ended, v4fixture.Event{Kind: "message/complete", Ref: oversized})
	store.Session(newer, 9)
	if err := os.MkdirAll(filepath.Join(store.Root, broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, broken, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.Root, filepath.Join(old, "sessions-v4")); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(old, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "odd.jsonl"), []byte(`{"schema":2,"kind":"header"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{"path": old, "workspace": root})
	resp, err := http.Post(srv.URL+"/tree/sessions/import-legacy", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got skipAnswer
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		huge:        "too_large",
		newer:       "schema_unsupported",
		broken:      "corrupt",
		"odd.jsonl": "unreadable_format",
	}
	if len(got.Skipped) != len(want) || got.Warnings != len(want) {
		t.Fatalf("answered %+v, want %d skipped sessions", got, len(want))
	}
	for _, s := range got.Skipped {
		if want[s.Name] != s.Reason {
			t.Errorf("%s skipped as %q, want %q", s.Name, s.Reason, want[s.Name])
		}
		if !filepath.IsAbs(s.Path) || filepath.Base(s.Path) != s.Name {
			t.Errorf("%s path %q does not name the untouched source entry", s.Name, s.Path)
		}
		if _, err := os.Stat(s.Path); err != nil {
			t.Errorf("%s: the source entry is gone: %v", s.Name, err)
		}
	}
}

func TestImportLegacyAttributesReadAndWriteFailuresToTheirOwnClass(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced here")
	}
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	old := testenv.TempDir(t)
	store := v4fixture.New(t)
	const denied, missing = "4123456789abcdef0123456789abcdef", "5123456789abcdef0123456789abcdef"
	store.Batch(store.Session(denied, 3), v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Payload: v4fixture.Msg("m1", "user", "x")})
	store.Batch(store.Session(missing, 3), v4fixture.Ended,
		v4fixture.Event{Kind: "message/complete", Ref: &v4fixture.Ref{Digest: strings.Repeat("b", 64), Bytes: 10}})
	if err := os.MkdirAll(filepath.Join(store.Root, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.Root, filepath.Join(old, "sessions-v4")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(old, "sessions-v4", denied, "manifest.json")
	if err := os.Chmod(manifest, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(manifest, 0o600) })
	sessions := filepath.Join(old, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	log := `{"role":"user","content":"hello"}` + "\n" + `{"role":"assistant","content":"hi"}` + "\n"
	if err := os.WriteFile(filepath.Join(sessions, "w.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := SessionDirFor(root)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dest, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dest, 0o755) })

	body, _ := json.Marshal(map[string]string{"path": old, "workspace": root})
	resp, err := http.Post(srv.URL+"/tree/sessions/import-legacy", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got skipAnswer
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{denied: "permission", missing: "corrupt", "w.jsonl": "copy_failed"}
	if len(got.Skipped) != len(want) {
		t.Fatalf("answered %+v, want exactly %v (a folder with no manifest is not a session)", got.Skipped, want)
	}
	for _, s := range got.Skipped {
		if want[s.Name] != s.Reason {
			t.Errorf("%s skipped as %q, want %q", s.Name, s.Reason, want[s.Name])
		}
	}
}
