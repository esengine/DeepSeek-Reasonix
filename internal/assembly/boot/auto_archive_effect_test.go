package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func seedIdleSession(t *testing.T, dir, name string, idleFor time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name+".jsonl")
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello " + name})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-idleFor)
	if err := sessionstore.UpdateBranchMeta(path, false, func(m *sessionstore.BranchMeta) error {
		m.UpdatedAt = old
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, store.SessionEventLog(path)} {
		if _, err := os.Stat(p); err == nil {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func archivedSession(t *testing.T, path string) bool {
	t.Helper()
	m, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	return m.Archived
}

func buildAutoArchiveController(t *testing.T, userConfig, projectExtra string) *control.Controller {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if userConfig != "" {
		path := config.UserConfigPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(userConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kind := "boot-auto-archive-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return &stallScriptProvider{script: func(int) *provider.ToolCall { return nil }}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
`+projectExtra+`
[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: &watchSink{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	return ctrl
}

func TestEffectAutoArchiveArchivesOnlyWhatTheUserAllowed(t *testing.T) {
	ctrl := buildAutoArchiveController(t, "[auto_archive]\nenabled = true\ndays = 3\n", "[auto_archive]\nenabled = false\ndays = 3650\n")
	dir := ctrl.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	week := 7 * 24 * time.Hour
	idle := seedIdleSession(t, dir, "idle", week)
	fresh := seedIdleSession(t, dir, "fresh", time.Hour)
	pinned := seedIdleSession(t, dir, "pinned", week)
	midTurn := seedIdleSession(t, dir, "midturn", week)
	if _, err := sessionstore.BeginSessionInFlightTurn(midTurn, 0, false); err != nil {
		t.Fatal(err)
	}

	if err := sessionstore.PinSessions([]string{pinned}); err != nil {
		t.Fatal(err)
	}
	if n := ctrl.ArchiveInactiveSessions(); n != 1 {
		t.Fatalf("archived %d, want 1", n)
	}
	if !archivedSession(t, idle) {
		t.Error("the idle conversation stayed in the list")
	}
	for name, p := range map[string]string{"fresh": fresh, "pinned": pinned, "midturn": midTurn} {
		if archivedSession(t, p) {
			t.Errorf("%s was archived", name)
		}
	}
	if err := sessionstore.SetSessionArchived(idle, false); err != nil {
		t.Fatal(err)
	}
	if n := ctrl.ArchiveInactiveSessions(); n != 0 {
		t.Fatalf("a restored conversation was archived again (%d)", n)
	}
}

func TestEffectAutoArchiveIsOffUntilTheUserTurnsItOn(t *testing.T) {
	ctrl := buildAutoArchiveController(t, "", "[auto_archive]\nenabled = true\ndays = 1\n")
	dir := ctrl.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := seedIdleSession(t, dir, "old", 90*24*time.Hour)
	if n := ctrl.ArchiveInactiveSessions(); n != 0 || archivedSession(t, old) {
		t.Fatalf("archived %d with the setting off, project file must not turn it on", n)
	}
	if err := ctrl.SaveAutoArchiveSettings(control.AutoArchiveSettings{Enabled: true, Days: 30}); err != nil {
		t.Fatal(err)
	}
	if n := ctrl.ArchiveInactiveSessions(); n != 1 || !archivedSession(t, old) {
		t.Fatalf("archived %d after the user turned it on, want the 90-day-old one", n)
	}
}
