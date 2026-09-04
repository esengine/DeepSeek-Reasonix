package agent

import (
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func headerRetryEvent(info provider.RetryInfo) event.Event {
	return event.Event{
		Kind: event.Retrying, RetryAttempt: info.Attempt, RetryMax: info.Max,
		RetryScope: event.RetryScopeHeaders,
		RetryDetail: event.RetryDetail{
			RetryReason: provider.RetryReason(info.Err), RetryDelayMs: info.Delay.Milliseconds(),
		},
	}
}
