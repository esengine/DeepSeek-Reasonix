package plugin

import (
	"context"
	"errors"
	"time"
)

// A request the host abandons keeps running unless the server is told: a long
// query holds its transaction, and whatever that locked, until it finishes — so
// an abandoned turn can wedge the next one against its own leftovers.

const (
	initializeMethod  = "initialize"
	cancelledMethod   = "notifications/cancelled"
	cancelNotifyLimit = 5 * time.Second
)

// cancelInFlight tells the server to drop a request the host walked away from.
// It runs off the caller's goroutine because the call it belongs to is already
// returning, and it carries its own deadline so a wedged transport cannot hold
// the notification's goroutine forever.
func cancelInFlight(t transport, method string, id int, cause error) {
	if t == nil || method == initializeMethod {
		return // spec: a client MUST NOT cancel initialize
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cancelNotifyLimit)
		defer cancel()
		// Fire and forget: the spec has the receiver ignore an id it has already
		// finished or never knew, so a delivery failure changes nothing here.
		_ = t.notify(ctx, cancelledMethod, map[string]any{
			"requestId": id,
			"reason":    cancelReason(cause),
		})
	}()
}

// cancelReason is what the server logs. Go's own wording ("context canceled")
// says nothing to an operator reading a Python server's log.
func cancelReason(cause error) string {
	if errors.Is(cause, context.DeadlineExceeded) {
		return "client deadline exceeded"
	}
	return "client cancelled the call"
}
