package jobs

import (
	"context"
	"errors"

	"reasonix/internal/base/proc"
)

var ErrAdmissionClosed = errors.New("background job admission is closed")

type admissionSeal struct{ done chan struct{} }

type executionOwnerKey struct{}

// WithExecutionOwner binds only a fresh per-execution manager, never a caller's
// session manager. Child runtimes join this owner before releasing temp files.
func WithExecutionOwner(ctx context.Context, manager *Manager) context.Context {
	ctx = context.WithValue(ctx, executionOwnerKey{}, manager)
	var observe func(<-chan struct{})
	if manager != nil {
		ctx = WithSession(ctx, "execution-"+manager.ownerID)
		observe = func(done <-chan struct{}) {
			manager.mu.Lock()
			manager.waits = append(manager.waits, done)
			manager.mu.Unlock()
		}
	}
	return proc.WithCompletionObserver(ctx, observe)
}

// JoinExecution runs before execution-private resources are released.
func JoinExecution(ctx context.Context, manager *Manager, runErr *error) bool {
	owner, _ := ctx.Value(executionOwnerKey{}).(*Manager)
	if owner == nil || owner != manager {
		return false
	}
	caught := recover()
	if *runErr != nil || caught != nil {
		owner.Cancel()
	}
	*runErr = errors.Join(*runErr, owner.SealAndWait(ctx))
	if caught != nil {
		panic(caught)
	}
	return true
}

// Seal atomically closes admission and joins every previously registered job.
// The returned channel closes after run callbacks and their artifacts finish.
func (m *Manager) Seal() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admission != nil {
		return m.admission.done
	}
	m.admission = &admissionSeal{done: make(chan struct{})}
	pending := make([]<-chan struct{}, 0, len(m.jobs))
	for _, job := range m.jobs {
		pending = append(pending, job.done)
	}
	pending = append(pending, m.waits...)
	done := m.admission.done
	go func() {
		for _, exited := range pending {
			<-exited
		}
		close(done)
	}()
	return done
}

// SealAndWait retains execution ownership through cancellation until admitted
// callbacks return. It cannot prove containment of arbitrary OS descendants.
func (m *Manager) SealAndWait(ctx context.Context) error {
	done := m.Seal()
	select {
	case <-done:
		return ctx.Err()
	case <-ctx.Done():
		m.Cancel()
		<-done
		return ctx.Err()
	}
}

// Cancel stops owned jobs without releasing ownership or deleting artifacts.
func (m *Manager) Cancel() {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
}

func (m *Manager) admissionClosedLocked(session string) bool {
	return m.admission != nil || m.destroying[session]
}

// AdmissionError distinguishes a closed owner from a capacity rejection.
// StartOwnedForSession repeats admission under the registration lock.
func (m *Manager) AdmissionError(session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.root.Err(); err != nil {
		return err
	}
	if m.admissionClosedLocked(session) {
		return ErrAdmissionClosed
	}
	return nil
}
