package config

import (
	"fmt"

	"reasonix/internal/contract/provider"
)

func isMiniMaxFlashEntry(e *ProviderEntry) bool {
	return explicitReasoningProtocol(e) == "" && isMiniMaxEntry(e) && provider.IsMiniMaxFlash(e.Model)
}

// normalizeMiniMaxBinaryEffort preserves the legacy M3 alias mappings.
func normalizeMiniMaxBinaryEffort(level string) (string, error) {
	switch level {
	case "adaptive", "disabled":
		return level, nil
	case "off", "xhigh", "max":
		return "disabled", nil
	case "low", "medium", "high":
		return "adaptive", nil
	default:
		return "", fmt.Errorf("usage: /effort auto|adaptive|disabled")
	}
}

func normalizeMiniMaxFlashEffort(level string) (string, error) {
	if containsString(provider.MiniMaxFlashEfforts(), level) {
		return level, nil
	}
	return "", fmt.Errorf("usage: /effort auto|low|medium|high|xhigh|max")
}
