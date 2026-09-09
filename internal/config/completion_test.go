package config

import (
	"strings"
	"testing"
)

func TestRetiredCompletionValidationSettingsAreNoOp(t *testing.T) {
	for _, mode := range []string{"", CompletionValidationOff, CompletionValidationShadow, CompletionValidationEnforce, "legacy-value"} {
		if got := (AgentConfig{CompletionValidation: mode}).CompletionValidationMode(); got != CompletionValidationOff {
			t.Errorf("CompletionValidationMode(%q) = %q, want off", mode, got)
		}
		if err := ValidateCompletionValidation(mode); err != nil {
			t.Errorf("ValidateCompletionValidation(%q) = %v, want nil for retired setting", mode, err)
		}
	}
}

func TestRetiredCompletionValidationEnvironmentDoesNotEnableValidator(t *testing.T) {
	t.Setenv(CompletionValidationModeEnv, CompletionValidationEnforce)
	if got := (AgentConfig{CompletionValidation: CompletionValidationShadow}).CompletionValidationMode(); got != CompletionValidationOff {
		t.Fatalf("CompletionValidationMode() = %q, want off even when legacy environment setting is present", got)
	}
	if err := validateCompletionValidationModes("legacy-value"); err != nil {
		t.Fatalf("validateCompletionValidationModes() = %v, want nil", err)
	}
}

func TestRenderOmitsRetiredCompletionValidationSettings(t *testing.T) {
	cfg := Default()
	cfg.Agent.CompletionValidation = CompletionValidationEnforce
	cfg.Agent.CompletionEvaluatorModel = "legacy-evaluator"
	rendered := RenderTOML(cfg)
	if strings.Contains(rendered, "completion_validation") || strings.Contains(rendered, "completion_evaluator_model") {
		t.Fatalf("rendered config contains retired completion settings:\n%s", rendered)
	}
}
