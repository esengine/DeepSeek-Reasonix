package taskcatalog

import "context"

// RebuildSharedCatalog fences the process-local reader, atomically replaces
// the disposable database, and reopens it with every project registered during
// or before the rebuild.
func RebuildSharedCatalog(ctx context.Context, projects []Project) error {
	return shared.rebuildShared(ctx, projects)
}

func (m *sharedManager) rebuildShared(ctx context.Context, projects []Project) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if err := m.closeLocked(ctx, true); err != nil {
		m.mu.Lock()
		m.rebuilding = false
		if m.pending == nil {
			m.pending = map[string]string{}
		}
		for _, project := range projects {
			project = normalizeProject(project)
			m.pending[project.Root] = project.Label
		}
		m.mu.Unlock()
		if ctx.Err() == nil {
			m.start()
		}
		return err
	}
	m.mu.Lock()
	if m.pending == nil {
		m.pending = map[string]string{}
	}
	for _, project := range projects {
		project = normalizeProject(project)
		m.pending[project.Root] = project.Label
	}
	rebuild := m.rebuild
	if rebuild == nil {
		rebuild = Rebuild
	}
	m.mu.Unlock()
	_, rebuildErr := rebuild(ctx, "", projects)
	m.mu.Lock()
	m.rebuilding = false
	m.mu.Unlock()
	if ctx.Err() == nil {
		m.start()
	}
	return rebuildErr
}
