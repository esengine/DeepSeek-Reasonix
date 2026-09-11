package taskcatalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedManagerCloseFencesOpenAndAllowsRestart(t *testing.T) {
	manager := &sharedManager{}
	started := make(chan struct{})
	release := make(chan struct{})
	databasePath := filepath.Join(t.TempDir(), "tasks.sqlite")
	manager.open = func(ctx context.Context, path string) (*Catalog, error) {
		close(started)
		<-release
		return Open(ctx, databasePath)
	}
	manager.start()
	<-started

	closed := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { closed <- manager.close(ctx) }()
	select {
	case err := <-closed:
		t.Fatalf("close returned before in-flight open exited: %v", err)
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatalf("close after open fence: %v", err)
	}
	manager.mu.RLock()
	stale := manager.catalog
	manager.mu.RUnlock()
	if stale != nil {
		t.Fatal("stale open published a task catalog after close")
	}

	manager.mu.Lock()
	manager.open = func(ctx context.Context, path string) (*Catalog, error) {
		return Open(ctx, databasePath)
	}
	manager.mu.Unlock()
	manager.start()
	manager.mu.RLock()
	reopened := manager.openDone
	manager.mu.RUnlock()
	if reopened == nil {
		t.Fatal("manager did not start a new generation after close")
	}
	<-reopened
	manager.mu.RLock()
	restarted := manager.catalog
	manager.mu.RUnlock()
	if restarted == nil {
		t.Fatal("manager did not publish the restarted task catalog")
	}
	if err := manager.close(ctx); err != nil {
		t.Fatalf("close restarted task catalog: %v", err)
	}
}
