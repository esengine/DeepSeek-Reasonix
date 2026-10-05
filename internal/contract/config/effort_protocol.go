package config

import "fmt"

func normalizeOpenAIReasoningEffort(e *ProviderEntry, level string) (string, error) {
	if isMimoEntry(e) {
		switch level {
		case "none", "low", "medium", "high":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|none|low|medium|high")
		}
	}
	switch level {
	case "low", "medium", "high":
		return level, nil
	default:
		return "", fmt.Errorf("usage: /effort auto|low|medium|high")
	}
}

func normalizeKimiK3ReasoningEffort(level string) (string, error) {
	if isKimiK3ReasoningEffort(level) {
		return level, nil
	}
	return "", fmt.Errorf("usage: /effort auto|low|high|max")
}

func isKimiK3ReasoningEffort(level string) bool {
	switch level {
	case "low", "high", "max":
		return true
	default:
		return false
	}
}

// normalizeDeepSeekReasoningEffort maps a level onto DeepSeek's vocabulary.
// "max" is the vendor's own extension: its endpoint takes it, and anywhere else
// it degrades onto the deepest standard level rather than being sent to an
// endpoint that answers 400 for as long as the setting stands.
func normalizeDeepSeekReasoningEffort(e *ProviderEntry, level string) (string, error) {
	vendor := servedByVendor(e, deepSeekVendor)
	switch level {
	case "disabled", "off": // "off" is the retired spelling of no thinking
		return "disabled", nil
	case "low", "medium", "high":
		return "high", nil
	case "max", "xhigh":
		if vendor {
			return "max", nil
		}
		return "high", nil
	}
	if vendor {
		return "", fmt.Errorf("usage: /effort auto|disabled|high|max")
	}
	return "", fmt.Errorf("usage: /effort auto|disabled|high")
}
