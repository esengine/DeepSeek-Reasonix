package agent

import (
	"testing"

	"reasonix/internal/provider"
)

func TestLegacyOriginFallbackCoversCurrentHostMessageFamilies(t *testing.T) {
	for _, content := range []string{
		CompletionValidationContinuationPrefix + " the last message did not deliver a self-contained final result.",
		standardTodoContinuationMessage(),
		emptyFinalRetryMessage(),
		executorHandoffRetryMessage(),
		"This task has reached its token budget. Finalize now.",
		"Your tool-call round limit (max_steps) has been reached.",
		"The following tools are unavailable in the current workflow phase: ask.",
		"Auto recovery has reached its limit for this turn. Summarize now.",
		todoProgressNudgeMessage(8),
		"Host progress redirect: choose a different approach.",
	} {
		legacy := provider.Message{Role: provider.RoleUser, Content: content}
		if !IsHostGeneratedUserMessage(legacy) {
			t.Errorf("legacy host message was not recognized: %q", content)
		}
	}
}
