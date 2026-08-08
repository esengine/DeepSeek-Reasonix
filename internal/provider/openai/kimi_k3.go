package openai

import "strings"

var kimiK3EffortVocabulary = []string{"low", "high", "max"}

func usesKimiK3Contract(protocol, baseURL, model string) bool {
	return protocol == "kimi-k3" || (IsKimiAPI(baseURL) && strings.EqualFold(strings.TrimSpace(model), "kimi-k3"))
}

func reasoningEffortVocabulary(kimiK3 bool, configured []string) ([]string, bool) {
	if kimiK3 {
		return kimiK3EffortVocabulary, true
	}
	return configured, hasExplicitSupportedEfforts(configured)
}

func kimiK3ReasoningEffort(kimiK3 bool, effort string) string {
	if kimiK3 && effort == "" {
		return "max"
	}
	return effort
}
