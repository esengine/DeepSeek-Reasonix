package history

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/historycatalog"
)

func TestIndexedCatalogManagerCloseFencesOpenAndAllowsRestart(t *testing.T) {
	manager := &indexedCatalogManager{}
	started := make(chan struct{})
	release := make(chan struct{})
	manager.open = func(ctx context.Context, opts historycatalog.Options) (*historycatalog.Catalog, error) {
		close(started)
		<-release
		opts.Path, opts.InMemory = "", true
		return historycatalog.Open(ctx, opts)
	}
	manager.register([]historycatalog.Root{{Path: t.TempDir(), Scope: "global"}})
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
	if catalog := manager.get(); catalog != nil {
		t.Fatal("stale open published a catalog after close")
	}

	manager.mu.Lock()
	manager.open = func(ctx context.Context, opts historycatalog.Options) (*historycatalog.Catalog, error) {
		opts.Path, opts.InMemory = "", true
		return historycatalog.Open(ctx, opts)
	}
	manager.mu.Unlock()
	manager.register([]historycatalog.Root{{Path: t.TempDir(), Scope: "global"}})
	manager.mu.RLock()
	var reopened chan struct{}
	for _, done := range manager.opening {
		reopened = done
	}
	manager.mu.RUnlock()
	if reopened == nil {
		t.Fatal("manager did not start a new generation after close")
	}
	<-reopened
	if catalog := manager.get(); catalog == nil {
		t.Fatal("manager did not publish the restarted catalog")
	}
	if err := manager.close(ctx); err != nil {
		t.Fatalf("close restarted catalog: %v", err)
	}
}
