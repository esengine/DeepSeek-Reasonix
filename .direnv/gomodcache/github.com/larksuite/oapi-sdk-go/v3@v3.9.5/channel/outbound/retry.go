package outbound

import (
	"context"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"math"
	"time"
)

// RetryOptions configures the retry behavior.
type RetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// DefaultRetryOptions provides sensible defaults for retrying.
var DefaultRetryOptions = RetryOptions{
	MaxAttempts: 3,
	BaseDelay:   500 * time.Millisecond,
}

// Retry executes an operation with exponential backoff.
// Only retries errors classified as retryable (e.g., rate_limited / unknown).
func Retry(ctx context.Context, op func(attempt int) (interface{}, error), opts *RetryOptions) (interface{}, error) {
	if opts == nil {
		opts = &DefaultRetryOptions
	}
	max := opts.MaxAttempts
	if max <= 0 {
		max = 3
	}
	base := opts.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}

	var lastErr error
	for attempt := 1; attempt <= max; attempt++ {
		res, err := op(attempt)
		if err == nil {
			return res, nil
		}

		feishuErr := types.ClassifyError(err, map[string]interface{}{"attempt": attempt})
		lastErr = feishuErr

		if attempt >= max || !types.IsRetryable(feishuErr) {
			return nil, lastErr
		}

		// Calculate delay: base * 3^(attempt-1)
		delay := time.Duration(float64(base) * math.Pow(3, float64(attempt-1)))

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			// continue
		}
	}

	return nil, lastErr
}
