package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

// forkThreeIdenticalRecoveryCopies builds one lineage whose parent went on to
// cover three identical conflict forks — the multiplying "Recovered unsaved
// changes" shape from #8525. Each fork gets its own isolated lane, so three
// distinct files share one recovery group.
func forkThreeIdenticalRecoveryCopies(t *testing.T, dir, name string) (parentPath string, copyPaths []string) {
	t.Helper()
	parentPath = filepath.Join(dir, name+".jsonl")
	disk := agent.NewSession("sys")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk " + name})
	if err := disk.Save(parentPath); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	var stale *agent.Session
	for range 3 {
		fork := agent.NewSession("sys")
		fork.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
		fork.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
		fork.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
		info, err := fork.SaveRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: parentPath})
		if err != nil {
			t.Fatalf("SaveRecoveryBranch: %v", err)
		}
		copyPaths = append(copyPaths, info.Path)
		if stale == nil {
			stale = fork
		}
	}
	// The parent goes on to contain everything every identical fork preserved.
	covering, err := agent.LoadSession(parentPath)
	if err != nil {
		t.Fatalf("Load covering parent: %v", err)
	}
	covering.Replace(append([]provider.Message(nil), stale.Snapshot()...))
	covering.Add(provider.Message{Role: provider.RoleAssistant, Content: "answered after recovery"})
	if err := covering.SaveRewrite(parentPath); err != nil {
		t.Fatalf("Save covering parent: %v", err)
	}
	return parentPath, copyPaths
}

func countTrashTranscripts(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	root := filepath.Join(dir, sessionTrashDir)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") && !strings.HasSuffix(entry.Name(), ".events.jsonl") {
			count++
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func openSweepTestCatalog(t *testing.T, dir string) *sessioncatalog.Catalog {
	t.Helper()
	ctx := context.Background()
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func sweepTestApp(catalog *sessioncatalog.Catalog) *App {
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	app.sessionCatalog.Store(catalog)
	return app
}

func TestRecoveryCopySweepTrashesExcessCoveredCopies(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentPath, copyPaths := forkThreeIdenticalRecoveryCopies(t, dir, "storm")
	// A second lineage below the threshold must never be touched.
	otherParent, otherCopy := forkCoveredRecoveryBranch(t, dir, "quiet")

	catalog := openSweepTestCatalog(t, dir)
	app := sweepTestApp(catalog)
	if got := app.sweepExcessRecoveryCopiesIn(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, time.Now(), 0); got != 2 {
		t.Fatalf("swept = %d, want 2 (3 copies, keep newest 1)", got)
	}

	remaining := 0
	for _, path := range copyPaths {
		if _, err := os.Stat(path); err == nil {
			remaining++
		}
	}
	if remaining != 1 {
		t.Fatalf("copies left in place = %d, want exactly the newest 1", remaining)
	}
	if got := countTrashTranscripts(t, dir); got != 2 {
		t.Fatalf("trashed transcripts = %d, want 2 recoverable copies", got)
	}
	for _, path := range []string{parentPath, otherParent, otherCopy} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("session outside the swept set must be untouched: %s: %v", path, err)
		}
	}

	// The catalog projection loses the swept copies but keeps the lineage.
	ctx := context.Background()
	groups, err := catalog.ListRecoveryGroups(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		for _, member := range group.Members {
			for _, swept := range copyPaths {
				if member.Path == swept {
					if _, err := os.Stat(swept); os.IsNotExist(err) {
						t.Fatalf("swept copy still projected: %s", swept)
					}
				}
			}
		}
	}

	// A repeat sweep is a no-op: only one copy remains.
	if got := app.sweepExcessRecoveryCopiesIn(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, time.Now(), 0); got != 0 {
		t.Fatalf("repeat sweep moved = %d, want 0", got)
	}
}

func TestRecoveryCopySweepKeepsNewestAndSkipsOpenCopy(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, copyPaths := forkThreeIdenticalRecoveryCopies(t, dir, "open")
	catalog := openSweepTestCatalog(t, dir)
	app := sweepTestApp(catalog)
	// The newest copy (last forked, highest recency) stays; hold the middle one
	// open so only the oldest can move.
	openPath := copyPaths[1]
	app.tabs["tab"] = &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: openPath, Ready: true}

	if got := app.sweepExcessRecoveryCopiesIn(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, time.Now(), 0); got != 1 {
		t.Fatalf("swept = %d, want 1 (open copy skipped)", got)
	}
	if _, err := os.Stat(openPath); err != nil {
		t.Fatalf("open copy must be untouched: %v", err)
	}
}

func TestRecoveryCopySweepRespectsConfigGate(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, copyPaths := forkThreeIdenticalRecoveryCopies(t, dir, "gated")
	catalog := openSweepTestCatalog(t, dir)
	app := sweepTestApp(catalog)

	configDir := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[recovery_cleanup]\nauto_enabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.sweepExcessRecoveryCopies(catalog, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"})
	for _, path := range copyPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("disabled sweep must not move copies: %v", err)
		}
	}
}
