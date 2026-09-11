package stats

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/config"
	"reasonix/internal/usagecatalog"
)

type usageManager struct {
	catalog    atomic.Pointer[usagecatalog.Catalog]
	mu         sync.Mutex
	generation uint64
	opening    bool
	openDone   chan struct{}
	openCancel context.CancelFunc
	open       func(context.Context, string) (*usagecatalog.Catalog, error)
}

var usageManagers = struct {
	sync.Mutex
	byDir map[string]*usageManager
}{byDir: map[string]*usageManager{}}

func managerForUsage(dir string) *usageManager {
	dir = strings.TrimSpace(dir)
	if dir == "" || !sameUsageDirectory(dir, config.StatsDir()) {
		return nil
	}
	usageManagers.Lock()
	manager := usageManagers.byDir[dir]
	if manager == nil {
		manager = &usageManager{}
		usageManagers.byDir[dir] = manager
	}
	usageManagers.Unlock()
	manager.start(dir)
	return manager
}

func (m *usageManager) start(dir string) {
	m.mu.Lock()
	if m.catalog.Load() != nil || m.opening {
		m.mu.Unlock()
		return
	}
	m.generation++
	generation := m.generation
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.opening, m.openDone, m.openCancel = true, done, cancel
	openCatalog := m.open
	if openCatalog == nil {
		openCatalog = usagecatalog.Open
	}
	m.mu.Unlock()
	go m.openGeneration(ctx, generation, done, dir, openCatalog)
}

func (m *usageManager) openGeneration(ctx context.Context, generation uint64, done chan struct{}, dir string, openCatalog func(context.Context, string) (*usagecatalog.Catalog, error)) {
	catalog, err := openCatalog(ctx, "")
	if err == nil {
		_ = catalog.ReconcileDir(ctx, dir)
	}
	m.mu.Lock()
	stale := generation != m.generation || ctx.Err() != nil
	if err == nil && !stale {
		m.catalog.Store(catalog)
	}
	m.mu.Unlock()
	if catalog != nil && (err != nil || stale) {
		_ = catalog.Close(context.Background())
	}
	m.mu.Lock()
	if m.openDone == done {
		m.opening = false
		m.openDone = nil
		m.openCancel = nil
	}
	close(done)
	m.mu.Unlock()
}

func (m *usageManager) close(ctx context.Context) error {
	m.mu.Lock()
	m.generation++
	if m.openCancel != nil {
		m.openCancel()
	}
	done := m.openDone
	catalog := m.catalog.Swap(nil)
	m.mu.Unlock()
	var closeErr error
	if catalog != nil {
		closeErr = catalog.Close(ctx)
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			if closeErr == nil {
				closeErr = ctx.Err()
			}
		}
	}
	return closeErr
}

// The single usage catalog projects the single authoritative Reasonix stats
// directory. Test/custom writers retain the exact JSONL implementation rather
// than accidentally sharing rollups with the production cache database.
func sameUsageDirectory(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

// existingUsageManager returns an already-started projection without creating
// background work. Read-only commands, Query and Flush use this path so merely
// inspecting authoritative JSONL cannot make the process outlive the command.
func existingUsageManager(dir string) *usageManager {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	usageManagers.Lock()
	defer usageManagers.Unlock()
	return usageManagers.byDir[dir]
}

// CloseUsageCatalogs closes every process-local usage projection. Desktop
// shutdown and test isolation call this so Windows can delete TempDir cache
// files that would otherwise stay locked by open SQLite handles.
func CloseUsageCatalogs(ctx context.Context) error {
	usageManagers.Lock()
	managers := make([]*usageManager, 0, len(usageManagers.byDir))
	for dir, manager := range usageManagers.byDir {
		managers = append(managers, manager)
		delete(usageManagers.byDir, dir)
	}
	usageManagers.Unlock()
	var first error
	for _, manager := range managers {
		if err := manager.close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func usageEntry(day string, r record) usagecatalog.Entry {
	turns := 0
	if r.Turn {
		turns = 1
	}
	return usagecatalog.Entry{Day: day, Source: r.Source, ModelRef: r.ModelRef, Provider: providerOf(r.ModelRef),
		Prompt: r.Prompt, Completion: r.Completion, Reasoning: r.Reasoning, CacheHit: r.CacheHit,
		CacheMiss: r.CacheMiss, Total: r.Total, Requests: r.Requests, Turns: turns}
}
