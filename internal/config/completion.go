package config

import (
	"fmt"
	"os"
	"strings"
)

// Completion-validation modes, mirrored from the agent layer.
const (
	CompletionValidationOff     = "off"
	CompletionValidationShadow  = "shadow"
	CompletionValidationEnforce = "enforce"
)

// CompletionValidationModeEnv overrides the configured completion-validation mode.
const CompletionValidationModeEnv = "REASONIX_COMPLETION_VALIDATION_MODE"

// CompletionValidationMode returns the normalized completion-validation mode.
// Empty (unconfigured) defaults to enforce so ordinary turns fail closed when
// the validator cannot confirm a self-contained result.
func (a AgentConfig) CompletionValidationMode() string {
	value := strings.TrimSpace(a.CompletionValidation)
	if override := strings.TrimSpace(os.Getenv(CompletionValidationModeEnv)); override != "" {
		value = override
	}
	switch value {
	case CompletionValidationOff, CompletionValidationShadow, CompletionValidationEnforce:
		return value
	default:
		return CompletionValidationEnforce
	}
}

// ValidateCompletionValidation rejects an explicit invalid enum at load time
// rather than silently defaulting, so a typo surfaces where it is configured.
func ValidateCompletionValidation(value string) error {
	switch strings.TrimSpace(value) {
	case "", CompletionValidationOff, CompletionValidationShadow, CompletionValidationEnforce:
		return nil
	default:
		return fmt.Errorf("agent.completion_validation %q: must be off, shadow, or enforce", value)
	}
}

func validateCompletionValidationModes(configured string) error {
	if err := ValidateCompletionValidation(configured); err != nil {
		return err
	}
	if err := ValidateCompletionValidation(os.Getenv(CompletionValidationModeEnv)); err != nil {
		return fmt.Errorf("%s: %w", CompletionValidationModeEnv, err)
	}
	return nil
}

// renderRecoveryAndCompletionValidation owns the adjacent evaluator settings
// in the full [agent] render without growing the legacy monolithic renderer.
func renderRecoveryAndCompletionValidation(b *strings.Builder, c *Config) {
	if strings.TrimSpace(c.Agent.RecoveryModel) != "" {
		fmt.Fprintf(b, "recovery_model = %q   # optional independent reviewer for low-risk automatic recovery\n", c.Agent.RecoveryModel)
	} else {
		b.WriteString("# recovery_model = \"deepseek-pro\"   # optional; falls back to guardian then main model\n")
	}
	if strings.TrimSpace(c.Agent.CompletionValidation) != "" {
		fmt.Fprintf(b, "completion_validation = %q   # off | shadow | enforce (empty defaults to enforce)\n", c.Agent.CompletionValidation)
	} else {
		b.WriteString("# completion_validation = \"enforce\"   # completion validator mode: off | shadow | enforce\n")
	}
	if strings.TrimSpace(c.Agent.CompletionEvaluatorModel) != "" {
		fmt.Fprintf(b, "completion_evaluator_model = %q   # optional; empty follows the working model\n", c.Agent.CompletionEvaluatorModel)
	} else {
		b.WriteString("# completion_evaluator_model = \"\"   # optional; empty follows the working model\n")
	}
}

// diffRecoveryAndCompletionValidation appends the changed evaluator settings
// to a project-scoped [agent] delta.
func diffRecoveryAndCompletionValidation(agentBuf *strings.Builder, c, d Config, anyAgent *bool) {
	if c.Agent.RecoveryModel != "" && c.Agent.RecoveryModel != d.Agent.RecoveryModel {
		fmt.Fprintf(agentBuf, "recovery_model = %q\n", c.Agent.RecoveryModel)
		*anyAgent = true
	}
	if c.Agent.CompletionValidation != "" && c.Agent.CompletionValidation != d.Agent.CompletionValidation {
		fmt.Fprintf(agentBuf, "completion_validation = %q\n", c.Agent.CompletionValidation)
		*anyAgent = true
	}
	if c.Agent.CompletionEvaluatorModel != "" && c.Agent.CompletionEvaluatorModel != d.Agent.CompletionEvaluatorModel {
		fmt.Fprintf(agentBuf, "completion_evaluator_model = %q\n", c.Agent.CompletionEvaluatorModel)
		*anyAgent = true
	}
}
