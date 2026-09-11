package main

import (
	"testing"

	"reasonix/internal/config"
)

func TestProviderViewFromEntryUsesOpenCodeGoDeepSeekWebSearch(t *testing.T) {
	enabled := true
	view := providerViewFromEntry(config.ProviderEntry{
		Name:      "opencode-go-deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://opencode.ai/zen/go/v1",
		Models:    []string{"deepseek-v4-flash"},
		WebSearch: &enabled,
	}, false, true)
	if !view.ServerWebSearchCapability || !view.WebSearch {
		t.Fatalf("OpenCode Go DeepSeek web search view = %+v, want enabled verified capability", view)
	}
}
