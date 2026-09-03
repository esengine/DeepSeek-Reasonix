package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

func TestBillingSplitUpgradeV5ToV6FreezesProviderCurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `config_version = 5
[desktop]
currency = "CNY"
language = "zh"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected upgrade rewrite")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "config_version = 8") {
		t.Fatalf("missing v8:\n%s", text)
	}
	if !strings.Contains(text, "display_currency") && !strings.Contains(text, `currency = "CNY"`) {
		t.Fatalf("display currency not migrated:\n%s", text)
	}
	cfg := LoadForEdit(path)
	if got := cfg.DisplayCurrencyPref(); got != "CNY" {
		t.Fatalf("display pref = %q", got)
	}
	flash, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatal("missing flash")
	}
	// List price must stay USD official; display is CNY.
	if flash.Price == nil || flash.Price.Currency != "$" || flash.Price.CacheHit != 0.014 || flash.Price.Input != 0.44 || flash.Price.Output != 1.32 {
		t.Fatalf("list price rewritten: %+v", flash.Price)
	}
	if got := flash.ProviderBillingCurrency(); got != "USD" {
		t.Fatalf("billing_currency = %q, want USD frozen from price", got)
	}
}

func TestDeepSeekScheduledPricingUpgradeLeavesMixedOfficialTableUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `config_version = 6
[[providers]]
name = "deepseek"
kind = "responses"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v5-future"]
billing_currency = "USD"
supported_efforts = ["disabled", "high"]
prices = { deepseek-v4-flash = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }, deepseek-v4-pro = { cache_hit = 9, input = 9, output = 9, currency = "$" }, deepseek-v5-future = { cache_hit = 8, input = 8, output = 8, currency = "$" } }
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || !changed {
		t.Fatalf("upgrade changed=%v err=%v", changed, err)
	}
	cfg := LoadForEdit(path)
	p, _ := cfg.Provider("deepseek")
	if p.Prices["deepseek-v4-flash"].Input != 0.14 || p.Prices["deepseek-v4-pro"].Input != 9 || p.Prices["deepseek-v5-future"].Input != 8 {
		t.Fatalf("mixed/custom table changed: %+v", p.Prices)
	}
	if strings.Join(p.SupportedEfforts, ",") != "disabled,high" {
		t.Fatalf("custom supported_efforts changed: %v", p.SupportedEfforts)
	}
}

func TestDeepSeekPricingContextSchedulesOnlyTrustedProtocolsAndAnchor(t *testing.T) {
	anchor := deepSeekV4FlashPriceCNY()
	for _, endpoint := range []struct{ kind, baseURL string }{
		{kind: "openai", baseURL: "https://api.deepseek.com"},
		{kind: "responses", baseURL: "https://api.deepseek.com"},
		{kind: "anthropic", baseURL: "https://api.deepseek.com/anthropic"},
	} {
		p := &ProviderEntry{Kind: endpoint.kind, BaseURL: endpoint.baseURL, Model: "deepseek-v4-flash", Price: clonePricing(anchor), BillingCurrency: "CNY"}
		if got := p.PricingContextForModel(p.Model).ScheduleID; got != billing.ScheduleDeepSeekV4August2026 {
			t.Fatalf("%s schedule = %q", endpoint.kind, got)
		}
	}
	for _, p := range []*ProviderEntry{
		{Kind: "ollama", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Price: clonePricing(anchor), BillingCurrency: "CNY"},
		{Kind: "openai", BaseURL: "https://gateway.example/v1", Model: "deepseek-v4-flash", Price: clonePricing(anchor), BillingCurrency: "CNY"},
		{Kind: "openai", BaseURL: "https://api.deepseek.com/custom", Model: "deepseek-v4-flash", Price: clonePricing(anchor), BillingCurrency: "CNY"},
		{Kind: "anthropic", BaseURL: "https://api.deepseek.com/custom", Model: "deepseek-v4-flash", Price: clonePricing(anchor), BillingCurrency: "CNY"},
		{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Price: &provider.Pricing{CacheHit: 9, Input: 9, Output: 9, Currency: "CNY"}, BillingCurrency: "CNY"},
	} {
		if got := p.PricingContextForModel(p.Model).ScheduleID; got != "" {
			t.Fatalf("untrusted provider scheduled: %+v => %q", p, got)
		}
	}
}

func TestDeepSeekScheduledPricingUpgradeV6ToV7(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `config_version = 6
[[providers]]
name = "deepseek"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
billing_currency = "CNY"
prices = { deepseek-v4-flash = { cache_hit = 0.02, input = 1, output = 2, currency = "CNY" }, deepseek-v4-pro = { cache_hit = 0.025, input = 3, output = 6, currency = "¥" } }

[[providers]]
name = "custom-endpoint"
kind = "openai"
base_url = "https://gateway.example/v1"
model = "deepseek-v4-flash"
billing_currency = "USD"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }

[[providers]]
name = "custom-price"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
billing_currency = "USD"
price = { cache_hit = 9, input = 9, output = 9, currency = "$" }

[[providers]]
name = "custom-path"
kind = "openai"
base_url = "https://api.deepseek.com/custom"
model = "deepseek-v4-flash"
billing_currency = "USD"
price = { cache_hit = 0.0028, input = 0.14, output = 0.28, currency = "$" }
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := ApplyUserConfigUpgradesOnStartup(path)
	if err != nil || !changed {
		t.Fatalf("upgrade changed=%v err=%v", changed, err)
	}
	cfg := LoadForEdit(path)
	official, _ := cfg.Provider("deepseek")
	if got := official.Prices["deepseek-v4-flash"]; got == nil || got.CacheHit != 0.10 || got.Input != 3 || got.Output != 9 {
		t.Fatalf("flash = %+v", got)
	}
	if got := official.Prices["deepseek-v4-pro"]; got == nil || got.CacheHit != 0.30 || got.Input != 9 || got.Output != 27 {
		t.Fatalf("pro = %+v", got)
	}
	customEndpoint, _ := cfg.Provider("custom-endpoint")
	if customEndpoint.Price.Input != 0.14 {
		t.Fatalf("custom endpoint changed: %+v", customEndpoint.Price)
	}
	customPrice, _ := cfg.Provider("custom-price")
	if customPrice.Price.Input != 9 {
		t.Fatalf("custom price changed: %+v", customPrice.Price)
	}
	customPath, _ := cfg.Provider("custom-path")
	if customPath.Price.Input != 0.14 {
		t.Fatalf("custom path changed: %+v", customPath.Price)
	}
	if again, err := ApplyUserConfigUpgradesOnStartup(path); err != nil || again {
		t.Fatalf("second upgrade changed=%v err=%v", again, err)
	}
}
