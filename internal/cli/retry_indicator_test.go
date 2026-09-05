package cli

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// TestRetryIndicatorShowsAndClears proves a Retrying event sets the transient
// retry fields the composer renders from, and that the next stream event clears
// them back to the normal thinking line.
func TestRetryIndicatorShowsAndClears(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning

	m.ingestEvent(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10,
		RetryDetail: event.RetryDetail{RetryReason: "HTTP 429", RetryDelayMs: 12500}})
	if m.retry != (retryStatus{attempt: 3, max: 10, reason: "HTTP 429", delayMs: 12500}) {
		t.Fatalf("retry fields = %+v, want 3/10 HTTP 429 12500ms", m.retry)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer"})
	if m.retry != (retryStatus{}) {
		t.Fatalf("a stream event should clear the retry indicator, got %+v", m.retry)
	}
}

// TestRetryIndicatorText guards the composer's retry line wording — the same
// format string View() renders when retryAttempt > 0.
func TestRetryIndicatorText(t *testing.T) {
	line := fmt.Sprintf(i18n.English.ChatStatusRetryingFmt, "⠋", 3, 10)
	if !strings.Contains(line, "retrying (3/10)") {
		t.Errorf("EN retry line = %q, want it to contain 'retrying (3/10)'", line)
	}
	zh := fmt.Sprintf(i18n.Chinese.ChatStatusRetryingFmt, "⠋", 3, 10)
	if !strings.Contains(zh, "正在重试 (3/10)") {
		t.Errorf("ZH retry line = %q, want it to contain '正在重试 (3/10)'", zh)
	}
	detail := (retryStatus{attempt: 3, max: 10, reason: "HTTP 429", delayMs: 12500}).line("⠋")
	if !strings.Contains(detail, "HTTP 429 · 13s") {
		t.Errorf("detailed retry line = %q, want rounded delay and reason", detail)
	}
}
