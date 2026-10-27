package jobs

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
)

func runRecovered(ctx context.Context, out io.Writer, run func(context.Context, io.Writer) (string, error)) (result string, err error) {
	err = withPanicRecovery(func() error {
		var runErr error
		result, runErr = run(ctx, out)
		return runErr
	}, func(value any) error {
		return fmt.Errorf("%w\n%s", panicCause("internal error: panic", value), debug.Stack())
	})
	return result, err
}

// withPanicRecovery distinguishes a normal return from legacy nil-valued panic.
// It does not recover Goexit or a panic on another goroutine.
func withPanicRecovery(call func() error, onPanic func(any) error) (err error) {
	returned := false
	defer func() {
		value := recover()
		if !returned {
			err = onPanic(value)
		}
	}()
	err = call()
	returned = true
	return err
}

func panicCause(prefix string, value any) error {
	if cause, ok := value.(error); ok {
		return fmt.Errorf("%s: %w", prefix, cause)
	}
	return fmt.Errorf("%s: %v", prefix, value)
}
