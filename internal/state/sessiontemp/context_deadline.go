package sessiontemp

import "context"

// nilContext returns a non-nil background context for lock acquisition that
// waits indefinitely. Owner locks for live generations must not time out while
// the process holds them; stale cleanup uses TryAcquire instead.
func nilContext() context.Context {
	return context.Background()
}
