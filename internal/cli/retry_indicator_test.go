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

	m.ingestEvent(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10})
	if m.retryAttempt != 3 || m.retryMax != 10 {
		t.Fatalf("retry fields = %d/%d, want 3/10", m.retryAttempt, m.retryMax)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer"})
	if m.retryAttempt != 0 || m.retryMax != 0 {
		t.Fatalf("a stream event should clear the retry indicator, got %d/%d", m.retryAttempt, m.retryMax)
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
}
