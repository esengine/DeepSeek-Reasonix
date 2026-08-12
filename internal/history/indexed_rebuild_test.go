package history

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/historycatalog"
)

func TestIndexedCatalogManagerRebuildPreservesRootsAndRestarts(t *testing.T) {
	manager := &indexedCatalogManager{}
	manager.open = func(ctx context.Context, opts historycatalog.Options) (*historycatalog.Catalog, error) {
		opts.Path, opts.InMemory = "", true
		return historycatalog.Open(ctx, opts)
	}
	var rebuilt []historycatalog.Root
	manager.rebuild = func(_ context.Context, _ historycatalog.Options, roots []historycatalog.Root) (historycatalog.Status, error) {
		rebuilt = append([]historycatalog.Root{}, roots...)
		return historycatalog.Status{}, nil
	}
	firstRoot := historycatalog.Root{Path: t.TempDir(), Scope: "global"}
	secondRoot := historycatalog.Root{Path: t.TempDir(), Scope: "global"}
	manager.register([]historycatalog.Root{firstRoot})
	waitManagerOpen(t, manager)
	if err := manager.rebuildShared(context.Background(), []historycatalog.Root{secondRoot}); err != nil {
		t.Fatal(err)
	}
	waitManagerOpen(t, manager)
	if len(rebuilt) != 2 {
		t.Fatalf("rebuilt roots = %#v, want both registered roots", rebuilt)
	}
	if manager.get() == nil {
		t.Fatal("manager did not publish a fresh catalog after rebuild")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.close(ctx); err != nil {
		t.Fatal(err)
	}
}

func waitManagerOpen(t *testing.T, manager *indexedCatalogManager) {
	t.Helper()
	manager.mu.RLock()
	var done chan struct{}
	for _, candidate := range manager.opening {
		done = candidate
	}
	manager.mu.RUnlock()
	if done != nil {
		<-done
	}
}
