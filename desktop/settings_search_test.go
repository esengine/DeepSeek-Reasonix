package main

import (
	"reasonix/internal/config"
	"testing"
)

func TestProviderViewFromEntryUsesEffectiveWebSearch(t *testing.T) {
	view := providerViewFromEntry(config.ProviderEntry{
		Name:    "deepseek-responses",
		Kind:    "responses",
		BaseURL: "https://api.deepseek.com",
	}, false, true)
	if !view.WebSearch {
		t.Fatal("official DeepSeek Responses omission did not default web search on")
	}

	chat := providerViewFromEntry(config.ProviderEntry{Name: "deepseek-chat", Kind: "openai", BaseURL: "https://api.deepseek.com/v1"}, false, true)
	if !chat.WebSearch || !chat.ServerWebSearchCapability {
		t.Fatal("official Chat Completions account did not expose independent search")
	}

	disabled := false
	explicitOff := providerViewFromEntry(config.ProviderEntry{
		Name:      "deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://api.deepseek.com",
		WebSearch: &disabled,
	}, false, true)
	if explicitOff.WebSearch {
		t.Fatal("explicit web_search=false was not preserved")
	}

	custom := providerViewFromEntry(config.ProviderEntry{
		Name:    "custom-responses",
		Kind:    "responses",
		BaseURL: "https://gateway.example/v1",
	}, false, true)
	if custom.WebSearch {
		t.Fatal("custom provider unexpectedly enabled web search")
	}
	if custom.ServerWebSearchCapability {
		t.Fatal("unverified custom Responses provider unexpectedly exposed server web search in Settings")
	}

	openAI := providerViewFromEntry(config.ProviderEntry{
		Name:      "custom-openai",
		Kind:      "openai",
		BaseURL:   "https://gateway.example/v1",
		WebSearch: func() *bool { enabled := true; return &enabled }(),
	}, false, true)
	if openAI.ServerWebSearchCapability || openAI.WebSearch {
		t.Fatal("OpenAI Chat Completions unexpectedly reported server web-search support")
	}
}
