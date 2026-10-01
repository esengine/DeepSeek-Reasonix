package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestSaveAutoArchivePersistsAndRefusesOutOfRange(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readJSON[control.AutoArchiveSettings](t, srv.URL, "/auto-archive")
	if got.Enabled || got.Days != config.DefaultAutoArchiveDays {
		t.Fatalf("unset section reads %+v, want off at the default days", got)
	}
	bad := postProvider(t, srv.URL, "/auto-archive", `{"enabled":true,"days":99999}`)
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(bad.Body).Decode(&refusal)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest || refusal.Code != "auto_archive.out_of_range" {
		t.Fatalf("out-of-range save = %d %q, want 400 auto_archive.out_of_range", bad.StatusCode, refusal.Code)
	}
	if config.LoadForEdit(config.UserConfigPath()).AutoArchive.Enabled {
		t.Fatal("a refused save still reached the file")
	}
	ok := postProvider(t, srv.URL, "/auto-archive", `{"enabled":true,"days":3}`)
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", ok.StatusCode)
	}
	saved := config.LoadForEdit(config.UserConfigPath()).AutoArchive
	if !saved.Enabled || saved.Days != 3 {
		t.Fatalf("user file holds %+v", saved)
	}
}

func TestSweepWaitsForAWindowToSyncItsPins(t *testing.T) {
	s := newProviderEditServer(t)
	dir := s.ctl().SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "old.jsonl")
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-90 * 24 * time.Hour)
	if err := sessionstore.UpdateBranchMeta(path, false, func(m *sessionstore.BranchMeta) error {
		m.UpdatedAt = old
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, store.SessionEventLog(path)} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chtimes(p, old, old)
		}
	}
	if err := s.ctl().SaveAutoArchiveSettings(control.AutoArchiveSettings{Enabled: true, Days: 3}); err != nil {
		t.Fatal(err)
	}
	pinsSynced.Store(false)
	t.Cleanup(func() { pinsSynced.Store(false) })
	if n := s.sweepInactiveSessions(); n != 0 {
		t.Fatalf("archived %d before any window synced its pins", n)
	}
	pinsSynced.Store(true)
	if n := s.sweepInactiveSessions(); n != 1 {
		t.Fatalf("archived %d after the sync, want 1", n)
	}
}
