package openai

import (
	"strings"

	"reasonix/internal/contract/provider"
)

// resolveChatURL treats an override that only repeats base_url as unset:
// POSTing to the API root answers 404.
func resolveChatURL(baseURL string, extra map[string]any) string {
	requestURL, _ := extra["request_url"].(string)
	requestURL = strings.TrimSpace(requestURL)
	if requestURL != "" && !provider.EndpointOverrideRepeatsBase(requestURL, baseURL) {
		return requestURL
	}
	legacyChatURL, _ := extra["chat_url"].(string)
	return normalizeChatURL(baseURL, legacyChatURL)
}

func normalizeChatURL(baseURL, chatURL string) string {
	if legacy := strings.TrimRight(strings.TrimSpace(chatURL), "/"); legacy != "" && !provider.EndpointOverrideRepeatsBase(legacy, baseURL) {
		return legacy
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/chat/completions"
}
