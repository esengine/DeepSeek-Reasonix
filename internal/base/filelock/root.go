package filelock

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func waitForFileLock(ctx context.Context, try func() (func(), error)) (func(), error) {
	for {
		release, err := try()
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, ErrHeld) {
			return nil, fmt.Errorf("acquire file lock: %w", err)
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, fmt.Errorf("acquire file lock: %w", ctx.Err())
		}
	}
}
