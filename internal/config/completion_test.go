package config

import (
	"strings"
	"testing"
)

func TestCompletionValidationModeDefaultsToEnforce(t *testing.T) {
	t.Setenv(CompletionValidationModeEnv, "")
	if got := (AgentConfig{}).CompletionValidationMode(); got != CompletionValidationEnforce {
		t.Fatalf("CompletionValidationMode() = %q, want %q", got, CompletionValidationEnforce)
	}
	for _, mode := range []string{CompletionValidationOff, CompletionValidationShadow, CompletionValidationEnforce} {
		if got := (AgentConfig{CompletionValidation: mode}).CompletionValidationMode(); got != mode {
			t.Errorf("CompletionValidationMode(%q) = %q", mode, got)
		}
	}
}

func TestCompletionValidationEnvironmentOverridesConfig(t *testing.T) {
	t.Setenv(CompletionValidationModeEnv, CompletionValidationOff)
	if got := (AgentConfig{CompletionValidation: CompletionValidationEnforce}).CompletionValidationMode(); got != CompletionValidationOff {
		t.Fatalf("CompletionValidationMode() = %q, want environment override %q", got, CompletionValidationOff)
	}
}

func TestCompletionValidationRejectsInvalidExplicitMode(t *testing.T) {
	if err := ValidateCompletionValidation("sometimes"); err == nil {
		t.Fatal("ValidateCompletionValidation accepted an invalid mode")
	}
}

func TestCompletionValidationRejectsInvalidEnvironmentMode(t *testing.T) {
	t.Setenv(CompletionValidationModeEnv, "sometimes")
	if err := validateCompletionValidationModes(""); err == nil || !strings.Contains(err.Error(), CompletionValidationModeEnv) {
		t.Fatalf("validateCompletionValidationModes() error = %v, want named environment error", err)
	}
}

func TestRenderCompletionValidationDocumentsEnforceDefault(t *testing.T) {
	rendered := RenderTOML(Default())
	if !strings.Contains(rendered, `# completion_validation = "enforce"`) {
		t.Fatalf("rendered config does not document the enforce default:\n%s", rendered)
	}
}
