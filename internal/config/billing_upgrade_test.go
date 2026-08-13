package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(text, "config_version = 6") {
		t.Fatalf("missing v6:\n%s", text)
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
	if flash.Price == nil || flash.Price.Currency != "$" {
		t.Fatalf("list price rewritten: %+v", flash.Price)
	}
	if got := flash.ProviderBillingCurrency(); got != "USD" {
		t.Fatalf("billing_currency = %q, want USD frozen from price", got)
	}
}
