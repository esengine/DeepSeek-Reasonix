package config

import (
	"testing"

	"reasonix/internal/contract/provider"
)

// The shape a real config lands in: an OpenAI "deepseek" entry the user added,
// beside the legacy per-model pair migrated to the Anthropic protocol. The pair
// cannot fold into the canonical entry — different protocol, different address —
// so before this it stayed two rows against the OpenAI side's one.
func deepSeekPeerConfig() *Config {
	c := Default()
	c.Providers = []ProviderEntry{
		{
			Name: "deepseek-flash", Kind: "anthropic",
			BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash",
			APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance",
			ContextWindow: 1_000_000, Thinking: "enabled",
			Price:            &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
			SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "high",
		},
		{
			Name: "deepseek-pro", Kind: "anthropic",
			BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-pro",
			APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance",
			ContextWindow: 1_000_000, Thinking: "enabled",
			Price:            &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"},
			SupportedEfforts: []string{"disabled", "high", "max"}, DefaultEffort: "high",
		},
		{
			Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com",
			Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash",
			APIKeyEnv: "DEEPSEEK_API_KEY",
		},
	}
	return c
}

func TestLegacyDeepSeekPeersFoldIntoOneSource(t *testing.T) {
	c := deepSeekPeerConfig()
	if !foldLegacyDeepSeekPeers(c) {
		t.Fatal("the anthropic pair did not fold")
	}

	names := make([]string, 0, len(c.Providers))
	for i := range c.Providers {
		names = append(names, c.Providers[i].Name)
	}
	if len(names) != 2 || names[0] != "deepseek-flash" || names[1] != "deepseek" {
		t.Fatalf("providers = %v, want the merged peer in its old slot beside deepseek", names)
	}

	merged, _ := c.Provider("deepseek-flash")
	if got := merged.ModelList(); len(got) != 2 || got[0] != "deepseek-v4-flash" || got[1] != "deepseek-v4-pro" {
		t.Fatalf("merged models = %v", got)
	}
	// The OpenAI route it could not fold into is untouched.
	if openai, ok := c.Provider("deepseek"); !ok || openai.Kind != "openai" {
		t.Fatal("folding the anthropic pair disturbed the OpenAI entry")
	}
}

// A price and an effort list someone edited have to survive the merge, or the
// first entry's numbers silently answer for both models.
func TestLegacyDeepSeekPeerFoldKeepsPerModelPriceAndEfforts(t *testing.T) {
	c := deepSeekPeerConfig()
	if !foldLegacyDeepSeekPeers(c) {
		t.Fatal("the anthropic pair did not fold")
	}

	pro, ok := c.ResolveModel("deepseek-flash/deepseek-v4-pro")
	if !ok {
		t.Fatal("the merged entry does not resolve the pro model")
	}
	if pro.Price == nil || pro.Price.Input != 3 || pro.Price.Output != 6 {
		t.Fatalf("pro price = %+v, want the pro entry's own", pro.Price)
	}
	if got := EffortCapabilityForEntry(pro); len(got.Levels) != 4 {
		t.Fatalf("pro efforts = %v, want auto plus its own three", got.Levels)
	}

	flash, ok := c.ResolveModel("deepseek-flash/deepseek-v4-flash")
	if !ok {
		t.Fatal("the merged entry does not resolve the flash model")
	}
	if flash.Price == nil || flash.Price.Input != 1 {
		t.Fatalf("flash price = %+v, want the flash entry's own", flash.Price)
	}
	if got := EffortCapabilityForEntry(flash); len(got.Levels) != 5 {
		t.Fatalf("flash efforts = %v, want auto plus its own four", got.Levels)
	}
}

// Refs outlive the config file — shell history, scripts, a memorised --model.
func TestRefsToTheFoldedPeerStillResolve(t *testing.T) {
	c := deepSeekPeerConfig()
	foldLegacyDeepSeekPeers(c)

	entry, ok := c.ResolveModel("deepseek-pro/deepseek-v4-pro")
	if !ok {
		t.Fatal("a ref naming the folded peer stopped resolving")
	}
	if entry.Model != "deepseek-v4-pro" || entry.Name != "deepseek-flash" {
		t.Fatalf("resolved to %s/%s", entry.Name, entry.Model)
	}
}

// A curated model list is a choice; carrying both models would re-add the one
// the user removed.
func TestLegacyDeepSeekPeerFoldLeavesCuratedListsAlone(t *testing.T) {
	c := deepSeekPeerConfig()
	c.Providers[0].Models = []string{"deepseek-v4-flash"}
	if foldLegacyDeepSeekPeers(c) {
		t.Fatal("a curated model list was folded away")
	}
	if len(c.Providers) != 3 {
		t.Fatalf("providers = %d, want the three left as they were", len(c.Providers))
	}
}

// Peers that disagree on a provider-wide field are two sources, not one.
func TestLegacyDeepSeekPeerFoldRefusesMismatchedEndpoints(t *testing.T) {
	c := deepSeekPeerConfig()
	c.Providers[1].BaseURL = "https://proxy.example.com/anthropic"
	if foldLegacyDeepSeekPeers(c) {
		t.Fatal("peers at different endpoints were merged")
	}
}
