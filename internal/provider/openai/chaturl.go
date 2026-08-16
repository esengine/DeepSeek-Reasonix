package openai

import "strings"

func resolveOpenAIChatURL(baseURL string, extra map[string]any) string {
	requestURL, _ := extra["request_url"].(string)
	requestURL = strings.TrimSpace(requestURL)
	if canonical, ok := canonicalTokenRhythmChatURL(requestURL); ok {
		return canonical
	}
	if requestURL != "" {
		return requestURL
	}
	legacyChatURL, _ := extra["chat_url"].(string)
	return normalizeChatURL(baseURL, legacyChatURL)
}

func normalizeChatURL(baseURL, chatURL string) string {
	if legacy := strings.TrimRight(strings.TrimSpace(chatURL), "/"); legacy != "" {
		if canonical, ok := canonicalTokenRhythmChatURL(legacy); ok {
			return canonical
		}
		return legacy
	}
	if canonical, ok := canonicalTokenRhythmChatURL(baseURL); ok {
		return canonical
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/chat/completions"
}
