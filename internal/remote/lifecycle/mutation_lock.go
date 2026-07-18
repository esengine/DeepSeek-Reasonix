package lifecycle

import (
	"context"
	"fmt"
)

type mutationLock interface {
	Release() error
}

func (m *SystemdManager) withMutationLock(ctx context.Context, operation func(context.Context) (ActionResult, error)) (result ActionResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	lock, err := m.acquireMutationLock(ctx)
	if err != nil {
		return ActionResult{}, fmt.Errorf("acquire Remote lifecycle mutation lock: %w", err)
	}
	defer func() {
		if releaseErr := lock.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release Remote lifecycle mutation lock: %w", releaseErr)
		}
	}()
	return operation(ctx)
}
