package jobs

import (
	"context"
	"io"
)

// StartForSession launches a job owned by parentSession. Session-scoped readers
// only see jobs whose owner matches the active session.
func (m *Manager) StartForSession(parentSession, kind, label string, run func(ctx context.Context, out io.Writer) (string, error), opts ...StartOption) *Job {
	j, _ := m.StartOwnedForSession(parentSession, kind, label, run, opts...)
	return j
}
