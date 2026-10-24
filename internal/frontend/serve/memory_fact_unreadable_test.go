package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/session/control"
	"reasonix/internal/state/memory"
)

func denyServeFactFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
	if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
		t.Skip("file permissions do not deny reads on this platform or for this user")
	}
}

// TestTheMemoryPanelShowsAnUnreadableFact is the desktop surface: the listing
// keeps the readable fact and carries the unreadable one as its own entry, and
// a save over it refuses with a typed code instead of rewriting bytes nobody
// read.
func TestTheMemoryPanelShowsAnUnreadableFact(t *testing.T) {
	store := memory.Store{Dir: t.TempDir()}
	if _, err := store.Save(memory.Memory{Name: "good", Body: "good body"}); err != nil {
		t.Fatal(err)
	}
	locked, err := store.Save(memory.Memory{Name: "locked", Body: "locked body"})
	if err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, locked)
	denyServeFactFile(t, locked)
	c := control.New(control.Options{Memory: &memory.Set{Store: store}})
	t.Cleanup(c.Close)
	s := &Server{ctrl: c}

	rec := httptest.NewRecorder()
	s.memories(rec, httptest.NewRequest(http.MethodGet, "/memories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s, want 200 with the readable fact", rec.Code, rec.Body)
	}
	var catalog struct {
		Memories   []memoryEntry `json:"memories"`
		Unreadable []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Error string `json:"error"`
		} `json:"unreadable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Memories) != 1 || catalog.Memories[0].Name != "good" {
		t.Fatalf("memories = %+v, want only the readable fact", catalog.Memories)
	}
	if len(catalog.Unreadable) != 1 || catalog.Unreadable[0].Name != "locked" || catalog.Unreadable[0].Path != locked {
		t.Fatalf("unreadable = %+v, want the fact at %s", catalog.Unreadable, locked)
	}
	if !strings.Contains(catalog.Unreadable[0].Error, "read memory fact") {
		t.Fatalf("unreadable error = %q, want the store's fact read error", catalog.Unreadable[0].Error)
	}

	save := httptest.NewRecorder()
	s.saveMemory(save, httptest.NewRequest(http.MethodPost, "/memory/save", strings.NewReader(`{"name":"locked","body":"overwritten"}`)))
	var reason Reason
	if err := json.Unmarshal(save.Body.Bytes(), &reason); err != nil {
		t.Fatal(err)
	}
	if save.Code != http.StatusInternalServerError || reason.Code != "memory.fact_unreadable" {
		t.Fatalf("save = %d %s, want 500 memory.fact_unreadable", save.Code, save.Body)
	}
	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, locked); got != before {
		t.Fatalf("save rewrote the unreadable fact:\n%s\nwant:\n%s", got, before)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, ".revisions")); !os.IsNotExist(err) {
		t.Fatalf("save started writing history over an unreadable fact: %v", err)
	}

	// An ordinary save still works, and a genuinely missing name is still 404.
	ok := httptest.NewRecorder()
	s.saveMemory(ok, httptest.NewRequest(http.MethodPost, "/memory/save", strings.NewReader(`{"name":"good","body":"edited"}`)))
	if ok.Code != http.StatusOK {
		t.Fatalf("ordinary save = %d %s", ok.Code, ok.Body)
	}
	missing := httptest.NewRecorder()
	s.saveMemory(missing, httptest.NewRequest(http.MethodPost, "/memory/save", strings.NewReader(`{"name":"missing","body":"x"}`)))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing save = %d %s, want 404", missing.Code, missing.Body)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
