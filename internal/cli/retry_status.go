package cli

import (
	"fmt"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

type retryStatus struct {
	attempt, max int
	reason       string
	delayMs      int64
}

func (s retryStatus) active() bool { return s.attempt > 0 }

func (s retryStatus) line(spinner string) string {
	if s.reason != "" && s.delayMs > 0 {
		return fmt.Sprintf("  "+i18n.M.ChatStatusRetryingDetailFmt, spinner, s.attempt, s.max, s.reason, (s.delayMs+999)/1000)
	}
	return fmt.Sprintf("  "+i18n.M.ChatStatusRetryingFmt, spinner, s.attempt, s.max)
}

func (s *retryStatus) set(e event.Event) {
	s.attempt, s.max, s.reason, s.delayMs = e.RetryAttempt, e.RetryMax, e.RetryReason, e.RetryDelayMs
}

func (s *retryStatus) clear() { *s = retryStatus{} }
