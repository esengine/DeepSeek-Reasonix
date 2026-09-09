//go:build live

package websearch

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/anthropic"
)

// Explicitly opt in: one bounded Flash search on the canonical official search wire.
// Credentials remain in memory; only result counts and usage are logged.
func TestLiveIndependentWebSearch(t *testing.T) {
	if os.Getenv("REASONIX_LIVE_WEB_SEARCH") != "1" {
		t.Skip("set REASONIX_LIVE_WEB_SEARCH=1 to run the bounded live probe")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("no official DeepSeek credential available")
	}
	entry := &config.ProviderEntry{Name: "deepseek", Kind: "responses", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"}
	entry.ResolveAPIKeyFromProcessEnvForProbe()
	// Pin only this process credential; do not read or write global credentials.
	route := (&config.Config{}).ResolveWebSearchProvider(entry)
	if route == nil {
		t.Fatal("official search route was not resolved")
	}
	if route.Kind != "anthropic" {
		t.Fatalf("unexpected official search wire: %s", route.Kind)
	}
	tool := &Tool{Factory: func() (provider.Provider, error) {
		return provider.New(route.Kind, provider.Config{Name: route.Name, BaseURL: route.BaseURL, Model: route.Model, APIKey: key, Extra: map[string]any{"web_search": true, "reject_redirects": true, "thinking": "enabled", "effort": "low"}})
	}, ReportUsage: func(u *provider.Usage) {
		t.Logf("prompt=%d completion=%d requests=%d", u.PromptTokens, u.CompletionTokens, u.RequestCount)
	}}
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Find the official DeepSeek API thinking mode documentation and summarize its reasoning passback requirement with a source URL."}`))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) == 0 || result.Summary == "" {
		t.Fatal("search did not return a summary and structured sources")
	}
	t.Logf("sources=%d summary_bytes=%d", len(result.Sources), len(result.Summary))
}
