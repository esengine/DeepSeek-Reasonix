package config

import (
	"fmt"
	"strings"
)

// Completion-validation modes, mirrored from the agent layer.
const (
	CompletionValidationOff     = "off"
	CompletionValidationShadow  = "shadow"
	CompletionValidationEnforce = "enforce"
)

// CompletionValidationModeEnv is retained so older config readers and process
// launchers continue to recognize the historical setting. It no longer
// changes runtime behavior.
const CompletionValidationModeEnv = "REASONIX_COMPLETION_VALIDATION_MODE"

// CompletionValidationMode returns off because the completion validator was
// removed. The method remains as a compatibility shim for old callers.
func (a AgentConfig) CompletionValidationMode() string {
	return CompletionValidationOff
}

// ValidateCompletionValidation accepts the retired setting so old config files
// continue to load. The value is ignored by the runtime.
func ValidateCompletionValidation(value string) error {
	return nil
}

func validateCompletionValidationModes(configured string) error {
	return ValidateCompletionValidation(configured)
}

// renderRecoveryAndCompletionValidation keeps the renderer call stable while
// deliberately omitting the retired completion-validator settings.
func renderRecoveryAndCompletionValidation(b *strings.Builder, c *Config) {
	if strings.TrimSpace(c.Agent.RecoveryModel) != "" {
		fmt.Fprintf(b, "recovery_model = %q   # optional independent reviewer for low-risk automatic recovery\n", c.Agent.RecoveryModel)
	} else {
		b.WriteString("# recovery_model = \"deepseek-pro\"   # optional; empty leaves rule-only recovery\n")
	}
}

// diffRecoveryAndCompletionValidation retains the historical renderer hook;
// retired completion-validator settings are intentionally never emitted.
func diffRecoveryAndCompletionValidation(agentBuf *strings.Builder, c, d Config, anyAgent *bool) {
	if c.Agent.RecoveryModel != "" && c.Agent.RecoveryModel != d.Agent.RecoveryModel {
		fmt.Fprintf(agentBuf, "recovery_model = %q\n", c.Agent.RecoveryModel)
		*anyAgent = true
	}
}
