package config

import "strings"

// ModelActionPolicy tells a model to execute requested actions with its tools
// instead of describing them, and to ground completion claims in real tool
// results. Small and open-weight models that narrate work they never performed
// are the intended audience; providers opt in per model.
const ModelActionPolicy = `When the user requests an action, execute it with the available permitted tools instead of describing or simulating it. Base completion claims only on actual tool results; never invent a result. Use concrete tool errors to correct unexecuted invalid calls. Permission denial, cancellation, and unknown action outcomes are not retryable tool failures: do not bypass them or repeat completed actions. Respect read-only and plan-mode restrictions.`

// AppliesModelActionPolicy reports whether the resolved model opted into the
// action policy. Nothing is inferred from the model id: the paragraph costs
// prompt tokens on every turn, so enabling it stays the operator's choice.
func AppliesModelActionPolicy(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	ov, ok := e.modelOverrideForModel(e.Model)
	return ok && ov.ActionPolicy != nil && *ov.ActionPolicy
}

// ApplyModelActionPolicy appends the policy for an opted-in model and returns
// the prompt untouched otherwise, keeping a default build byte-identical.
func ApplyModelActionPolicy(prompt string, e *ProviderEntry) string {
	if !AppliesModelActionPolicy(e) {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return ModelActionPolicy
	}
	return prompt + "\n\n" + ModelActionPolicy
}

func IsKimiActionModel(entry *ProviderEntry) bool {
	if entry == nil {
		return false
	}
	if entry.ReasoningProtocol == ReasoningProtocolKimiK3 {
		return true
	}
	id := strings.ToLower(entry.Model)
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	return id == "kimi" || strings.HasPrefix(id, "kimi-") || strings.HasPrefix(id, "kimi_")
}
