package config

import "strings"

// OfficialDeepSeekV4ProPersona is prepended to the cache-stable system prompt
// for official DeepSeek-V4-Pro. That model's agent post-training is peaked on
// this first-line persona and English "We need" thinking.
const OfficialDeepSeekV4ProPersona = `You are a helpful software engineer assistant. when you thought, thought in ENGLISH, start with "We need.."`

// AppliesOfficialDeepSeekV4ProPersona reports whether the resolved official
// DeepSeek endpoint is serving V4 Pro. Third-party hosts are excluded even
// when they reuse the same model id.
func AppliesOfficialDeepSeekV4ProPersona(e *ProviderEntry) bool {
	if e == nil || officialProviderKind(e) != "deepseek" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.Model), "deepseek-v4-pro")
}

// ApplyOfficialDeepSeekV4ProPersona keeps the persona as the first system-prompt
// paragraph for official V4 Pro and strips it when the session is not that model.
func ApplyOfficialDeepSeekV4ProPersona(prompt string, e *ProviderEntry) string {
	prompt = stripOfficialDeepSeekV4ProPersona(prompt)
	if !AppliesOfficialDeepSeekV4ProPersona(e) {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return OfficialDeepSeekV4ProPersona
	}
	return OfficialDeepSeekV4ProPersona + "\n\n" + prompt
}

func stripOfficialDeepSeekV4ProPersona(prompt string) string {
	switch {
	case strings.HasPrefix(prompt, OfficialDeepSeekV4ProPersona+"\n\n"):
		return strings.TrimPrefix(prompt, OfficialDeepSeekV4ProPersona+"\n\n")
	case strings.HasPrefix(prompt, OfficialDeepSeekV4ProPersona+"\n"):
		return strings.TrimPrefix(prompt, OfficialDeepSeekV4ProPersona+"\n")
	case prompt == OfficialDeepSeekV4ProPersona:
		return ""
	default:
		return prompt
	}
}

// ReasoningLanguageForEntry pins official V4 Pro auto mode to English so the
// per-turn Chinese auto-inference cannot override the persona.
func ReasoningLanguageForEntry(e *ProviderEntry, configured string) string {
	if AppliesOfficialDeepSeekV4ProPersona(e) && NormalizeReasoningLanguage(configured) == "auto" {
		return "en"
	}
	return configured
}
