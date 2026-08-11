package taskcatalog

import (
	"context"
	"path/filepath"
	"sync"

	"reasonix/internal/taskmonitor"
)

type sharedManager struct {
	lifecycleMu sync.Mutex
	mu          sync.RWMutex
	catalog     *Catalog
	pending     map[string]string
	closing     bool
	rebuilding  bool
	generation  uint64
	opening     bool
	openDone    chan struct{}
	openCancel  context.CancelFunc
	open        func(context.Context, string) (*Catalog, error)
	rebuild     func(context.Context, string, []Project) (Status, error)
}

var shared sharedManager

func ensureShared() {
	shared.start()
}

func (m *sharedManager) start() {
	m.mu.Lock()
	if m.catalog != nil || m.opening || m.closing || m.rebuilding {
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
		openCatalog = Open
	}
	m.mu.Unlock()
	go m.openGeneration(ctx, generation, done, openCatalog)
}

func (m *sharedManager) openGeneration(ctx context.Context, generation uint64, done chan struct{}, openCatalog func(context.Context, string) (*Catalog, error)) {
	catalog, err := openCatalog(ctx, "")
	seen := map[string]bool{}
	for err == nil {
		m.mu.Lock()
		if m.closing || m.rebuilding || generation != m.generation || ctx.Err() != nil {
			m.mu.Unlock()
			_ = catalog.Close(context.Background())
			catalog = nil
			break
		}
		pending := map[string]string{}
		for root, label := range m.pending {
			if !seen[root] {
				seen[root] = true
				pending[root] = label
			}
		}
		if len(pending) == 0 {
			m.catalog = catalog
			m.pending = nil
			m.mu.Unlock()
			break
		}
		m.mu.Unlock()
		for root, label := range pending {
			_, _ = catalog.RegisterProject(ctx, root, label)
		}
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

func Shared() *Catalog {
	ensureShared()
	shared.mu.RLock()
	defer shared.mu.RUnlock()
	return shared.catalog
}

// ShutdownShared drains accepted notifications and cancels every shared task
// projection worker. It is only used during process shutdown; authoritative
// task snapshots and event logs have already committed before notifications.
func ShutdownShared(ctx context.Context) error {
	return shared.close(ctx)
}

func (m *sharedManager) close(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.closeLocked(ctx, false)
}

func (m *sharedManager) closeLocked(ctx context.Context, rebuild bool) error {
	m.mu.Lock()
	m.closing = true
	m.rebuilding = rebuild
	m.generation++
	if m.openCancel != nil {
		m.openCancel()
	}
	done := m.openDone
	catalog := m.catalog
	m.catalog = nil
	if !rebuild {
		m.pending = nil
	}
	m.mu.Unlock()
	var flushErr, closeErr error
	if catalog != nil {
		flushErr = catalog.Flush(ctx)
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
	m.mu.Lock()
	m.closing = false
	m.rebuilding = rebuild
	m.mu.Unlock()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func RegisterSharedProject(root, label string) string {
	return shared.registerProject(root, label)
}

func (m *sharedManager) registerProject(root, label string) string {
	m.start()
	key := ProjectKey(root)
	m.mu.Lock()
	if m.rebuilding {
		if m.pending == nil {
			m.pending = map[string]string{}
		}
		m.pending[root] = label
		m.mu.Unlock()
		return key
	}
	if m.closing {
		m.mu.Unlock()
		return key
	}
	if m.catalog == nil {
		if m.pending == nil {
			m.pending = map[string]string{}
		}
		m.pending[root] = label
		m.mu.Unlock()
		return key
	}
	catalog := m.catalog
	m.mu.Unlock()
	_, _ = catalog.RegisterProject(context.Background(), root, label)
	return key
}

type sharedSink struct{}

func (sharedSink) SnapshotChanged(projectRoot, taskID string) {
	catalog := sharedCatalogForNotification(projectRoot)
	if catalog != nil {
		catalog.SnapshotChanged(projectRoot, taskID)
	}
}

func (sharedSink) EventsChanged(projectRoot, taskID string) {
	catalog := sharedCatalogForNotification(projectRoot)
	if catalog != nil {
		catalog.EventsChanged(projectRoot, taskID)
	}
}

// sharedCatalogForNotification is deliberately SQLite-free. ProjectionSink is
// called after the authoritative task file lock is released, but task saves
// still must never wait for catalog I/O. A notification received while the
// catalog is opening is recovered by the pending project's initial reconcile.
func sharedCatalogForNotification(projectRoot string) *Catalog {
	return shared.catalogForNotification(projectRoot)
}

func (m *sharedManager) catalogForNotification(projectRoot string) *Catalog {
	m.start()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rebuilding {
		if m.pending == nil {
			m.pending = map[string]string{}
		}
		m.pending[projectRoot] = filepath.Base(projectRoot)
		return nil
	}
	if m.closing {
		return nil
	}
	if m.catalog == nil {
		if m.pending == nil {
			m.pending = map[string]string{}
		}
		m.pending[projectRoot] = filepath.Base(projectRoot)
		return nil
	}
	return m.catalog
}

// ObservedStore remains an authoritative FileStore; only its post-commit sink
// is shared with the disposable catalog.
func ObservedStore() taskmonitor.WriteStore {
	ensureShared()
	return taskmonitor.NewObservedFileStore(filepath.Join(".reasonix", "tasks"), sharedSink{})
}
