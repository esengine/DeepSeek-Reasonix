package safety

import (
	"time"
)

// IsStale checks if an event is older than the given window.
func IsStale(createTimeMs int64, window time.Duration) bool {
	if createTimeMs <= 0 {
		return false
	}

	createTime := time.UnixMilli(createTimeMs)
	return time.Since(createTime) > window
}
