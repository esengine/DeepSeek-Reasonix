package cli

import (
	"context"
	"errors"
)

// turnFailureWorthShowing separates a turn the user stopped from one that
// broke. Cancellation used to be recognised by looking for "context canceled"
// in the message, which a reworded or differently wrapped error walks past —
// and what the reader then sees is their own Escape reported as a failure.
func turnFailureWorthShowing(err error) bool {
	if err == nil || err.Error() == "" {
		return false
	}
	return !errors.Is(err, context.Canceled)
}
