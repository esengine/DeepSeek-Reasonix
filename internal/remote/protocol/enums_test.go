package protocol

import "testing"

func TestPromptDecisionValues(t *testing.T) {
	values := []PromptDecision{
		DecisionAllowOnce,
		DecisionAllowSession,
		DecisionAllowPersistent,
		DecisionDeny,
		DecisionRunSandboxed,
		DecisionCancelCommand,
	}
	for _, v := range values {
		if string(v) == "" {
			t.Errorf("PromptDecision %v has empty string representation", v)
		}
	}
}

func TestPromptKindValues(t *testing.T) {
	values := []PromptKind{
		PromptApproval,
		PromptAsk,
		PromptCapabilityApproval,
	}
	for _, v := range values {
		if string(v) == "" {
			t.Errorf("PromptKind %v has empty string representation", v)
		}
	}
}
