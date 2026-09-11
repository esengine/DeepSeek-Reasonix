package taskcatalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedManagerRebuildRestartsWithRegisteredProjects(t *testing.T) {
	manager := &sharedManager{}
	databasePath := filepath.Join(t.TempDir(), "tasks.sqlite")
	manager.open = func(ctx context.Context, _ string) (*Catalog, error) {
		return Open(ctx, databasePath)
	}
	rebuildCalls := 0
	manager.rebuild = func(context.Context, string, []Project) (Status, error) {
		rebuildCalls++
		return Status{}, nil
	}
	manager.start()
	waitSharedManagerOpen(t, manager)
	project := Project{Root: t.TempDir(), Label: "Current"}
	if err := manager.rebuildShared(context.Background(), []Project{project}); err != nil {
		t.Fatal(err)
	}
	waitSharedManagerOpen(t, manager)
	if rebuildCalls != 1 {
		t.Fatalf("rebuild calls = %d, want 1", rebuildCalls)
	}
	manager.mu.RLock()
	restarted := manager.catalog
	manager.mu.RUnlock()
	if restarted == nil {
		t.Fatal("manager did not publish a fresh catalog after rebuild")
	}
	normalized := normalizeProject(project)
	if _, ok, err := restarted.Project(context.Background(), normalized.Key); err != nil || !ok {
		t.Fatalf("registered project missing after rebuild: ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSharedManagerRetainsRegistrationsWhileClosingForRebuild(t *testing.T) {
	manager := &sharedManager{closing: true, rebuilding: true}
	registeredRoot := t.TempDir()
	notifiedRoot := t.TempDir()
	manager.registerProject(registeredRoot, "Registered")
	if catalog := manager.catalogForNotification(notifiedRoot); catalog != nil {
		t.Fatal("notification unexpectedly reached a catalog during rebuild")
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.pending[registeredRoot] != "Registered" {
		t.Fatalf("registered project was not retained: %#v", manager.pending)
	}
	if manager.pending[notifiedRoot] != filepath.Base(notifiedRoot) {
		t.Fatalf("notified project was not retained: %#v", manager.pending)
	}
}

func waitSharedManagerOpen(t *testing.T, manager *sharedManager) {
	t.Helper()
	manager.mu.RLock()
	done := manager.openDone
	manager.mu.RUnlock()
	if done != nil {
		<-done
	}
}
