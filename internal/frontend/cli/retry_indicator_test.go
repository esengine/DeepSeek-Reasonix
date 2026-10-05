package cli

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
)

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
