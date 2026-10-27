package isolation

import (
	"context"
	"errors"
	"fmt"
)

// ErrRunnerPanic identifies a recovered panic in the host's isolated runner.
var ErrRunnerPanic = errors.New("isolated runner panicked")

// Recover only the host runner's stack. Its unwind does not establish process
// scope completion, so Execute retains an uninspected result without cleanup.
func invokeRunner(ctx context.Context, runner Runner, run Run) (out Outcome, err error) {
	returned := false
	defer func() {
		p := recover()
		if !returned {
			if cause, ok := p.(error); ok {
				err = fmt.Errorf("%w: %w", ErrRunnerPanic, cause)
			} else {
				err = fmt.Errorf("%w: %v", ErrRunnerPanic, p)
			}
		}
	}()
	out, err = runner(ctx, run)
	returned = true
	return out, err
}

func (s *Store) retainRunnerPanic(ctx context.Context, e *Entry, cause error) error {
	release, err := s.acquireResultOperation(ctx, e.ID, false)
	if err != nil {
		return s.rememberUnadmittedFinish(e, errors.Join(cause, err))
	}
	defer release()
	return s.retainInspection(e, cause)
}
