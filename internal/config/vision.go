package config

import (
	"net/url"
	"strings"
)

var mimoVisionModels = map[string]bool{
	"mimo-v2.5":    true,
	"mimo-v2-omni": true,
}

// EffectiveVision resolves whether the selected model accepts image input.
// Explicit provider vision still wins for custom vision-capable gateways; the
// built-in MiMo heuristic is deliberately limited to official MiMo endpoints so
// arbitrary OpenAI-compatible proxies do not get image payloads unexpectedly.
func EffectiveVision(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	if e.Vision {
		return true
	}
	return isOfficialMimoVisionEntry(e)
}

func isOfficialMimoVisionEntry(e *ProviderEntry) bool {
	if e == nil || e.Kind != "openai" || !mimoVisionModels[strings.ToLower(strings.TrimSpace(e.Model))] {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(e.BaseURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com":
		return true
	default:
		return false
	}
}
