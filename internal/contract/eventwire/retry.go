package eventwire

import "reasonix/internal/contract/event"

// setRetry copies a Retrying event's payload. RetryCause says why the failed
// attempt is retried, RetryStatus is the HTTP status when that is the cause,
// RetryDelayMs the backoff before the next attempt and RetryTimeoutSecs how long
// it waits for response headers.
func (w *Event) setRetry(e event.Event) {
	w.RetryAttempt = e.RetryAttempt
	w.RetryMax = e.RetryMax
	w.RetryScope = string(e.RetryScope)
	w.RetryCause = string(e.RetryCause)
	w.RetryStatus = e.RetryStatus
	w.RetryDelayMs = e.RetryDelayMs
	w.RetryTimeoutSecs = e.RetryTimeoutSecs
}
