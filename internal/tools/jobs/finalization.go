package jobs

import (
	"errors"
	"fmt"
	"sync"
)

// ErrFinalization identifies failed cleanup of a job's host-owned resources.
var ErrFinalization = errors.New("job resource finalization failed")

// Finalization describes the managed closure, not any descendant process scope.
// Invoked is false only when the Manager proves it will not call that closure.
type Finalization struct {
	Invoked bool
	Err     error
}

type jobFinalizers struct {
	once      sync.Once
	callbacks []func(Finalization) error
	err       error
}

// WithFinalizer registers host cleanup before terminal publication, including
// no-launch paths. Callbacks run without Manager/Job locks; all are attempted.
func WithFinalizer(fn func(Finalization) error) StartOption {
	return func(j *Job) {
		if fn == nil {
			return
		}
		if j.finalization == nil {
			j.finalization = &jobFinalizers{}
		}
		j.finalization.callbacks = append(j.finalization.callbacks, fn)
	}
}

func (j *Job) finalize(state Finalization) error {
	f := j.finalization
	if f == nil {
		return nil
	}
	f.once.Do(func() {
		for _, fn := range f.callbacks {
			f.err = errors.Join(f.err, callFinalizer(fn, state))
		}
		f.callbacks = nil
	})
	return f.err
}

func callFinalizer(fn func(Finalization) error, state Finalization) error {
	err := withPanicRecovery(func() error { return fn(state) }, func(value any) error {
		return panicCause("panic", value)
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFinalization, err)
	}
	return nil
}

func (m *Manager) finishUninvokedJob(j *Job, st Status, cause error) {
	cleanupErr := j.finalize(Finalization{Err: cause})
	err := errors.Join(cause, cleanupErr)
	j.mu.Lock()
	j.times.finished = nowMs()
	j.artifact.close()
	if cleanupErr != nil {
		j.noteArtifactErr(cleanupErr.Error())
	}
	if j.artifact.metaPath != "" {
		if metaErr := m.writeJobMetaLocked(j, st); metaErr != nil {
			j.noteArtifactErr(metaErr.Error())
		}
	}
	resultRef := j.artifactRefLocked()
	j.mu.Unlock()
	ev := m.recordCompletion(j.SessionID, j.ID, j.Kind, j.Label, resultRef, st, err)
	j.mu.Lock()
	j.runReturned = true
	if j.status != Killed && j.status != Interrupted {
		j.status = st
	}
	j.mu.Unlock()
	close(j.done)
	m.publishCompletion(ev)
}
