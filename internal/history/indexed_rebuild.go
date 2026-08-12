package history

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/historycatalog"
)

// RebuildSharedCatalog closes the process-local reader, atomically replaces the
// disposable FTS database, and starts a fresh reader over the same roots.
func RebuildSharedCatalog(ctx context.Context, roots []historycatalog.Root) error {
	return processHistoryCatalog.rebuildShared(ctx, roots)
}

func (m *indexedCatalogManager) rebuildShared(ctx context.Context, roots []historycatalog.Root) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	if m.roots == nil {
		m.roots = map[string]historycatalog.Root{}
	}
	for _, root := range roots {
		if strings.TrimSpace(root.Path) != "" {
			root.Path = filepath.Clean(root.Path)
			m.roots[root.Path] = root
		}
	}
	m.closing = true
	m.generation++
	catalog := m.catalog
	m.catalog = nil
	done := make([]chan struct{}, 0, len(m.opening))
	for generation, opening := range m.opening {
		m.openCancel[generation]()
		done = append(done, opening)
	}
	registered := make([]historycatalog.Root, 0, len(m.roots))
	for _, root := range m.roots {
		registered = append(registered, root)
	}
	rebuild := m.rebuild
	if rebuild == nil {
		rebuild = historycatalog.Rebuild
	}
	m.mu.Unlock()

	var lifecycleErr error
	if catalog != nil {
		_ = catalog.Flush(ctx)
		lifecycleErr = catalog.Close(ctx)
	}
	for _, opening := range done {
		select {
		case <-opening:
		case <-ctx.Done():
			if lifecycleErr == nil {
				lifecycleErr = ctx.Err()
			}
		}
	}
	if lifecycleErr == nil {
		_, lifecycleErr = rebuild(ctx, historycatalog.Options{}, registered)
	}
	m.mu.Lock()
	m.closing = false
	if ctx.Err() == nil {
		m.startOpenLocked()
	}
	m.mu.Unlock()
	return lifecycleErr
}
