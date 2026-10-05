package agent

import (
	"strings"
	"testing"
)

func TestExecutorHandoffRetryMessageKeepsUserChoicesInteractive(t *testing.T) {
	msg := executorHandoffRetryMessage()
	lower := strings.ToLower(msg)
	for _, want := range []string{
		"ask tool",
		"wait for its tool result",
		"do not ask in prose",
		"do not claim the user answered",
	} {
		if !strings.Contains(lower, want) {
			t.Fatalf("executorHandoffRetryMessage() missing %q:\n%s", want, msg)
		}
	}
}
