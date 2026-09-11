package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const twoProviderConfig = `
[[providers]]
name        = "deepseek"
kind        = "openai"
base_url    = "https://api.deepseek.com"
models      = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name        = "kimi"
kind        = "openai"
base_url    = "https://api.moonshot.cn/v1"
models      = ["kimi-k2"]
api_key_env = "MOONSHOT_API_KEY"
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProviders(t *testing.T, dir string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read metered config: %v", err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse metered config: %v", err)
	}
	var out []map[string]any
	switch list := doc["providers"].(type) {
	case []map[string]any:
		out = list
	case []any:
		for _, entry := range list {
			p, _ := entry.(map[string]any)
			out = append(out, p)
		}
	}
	return out
}

func TestMeterUpstreamFindsTheProviderServingTheModel(t *testing.T) {
	path := writeConfig(t, twoProviderConfig)
	got, err := meterUpstream(path, "kimi-k2")
	if err != nil {
		t.Fatalf("meterUpstream: %v", err)
	}
	if got != "https://api.moonshot.cn/v1" {
		t.Fatalf("upstream = %q, want the provider that serves the model", got)
	}
}

func TestMeterUpstreamAcceptsAVendorQualifiedModel(t *testing.T) {
	path := writeConfig(t, twoProviderConfig)
	got, err := meterUpstream(path, "Kimi/kimi-k2")
	if err != nil {
		t.Fatalf("meterUpstream: %v", err)
	}
	if got != "https://api.moonshot.cn/v1" {
		t.Fatalf("upstream = %q, want the vendor prefix stripped before matching", got)
	}
}

func TestMeterUpstreamFallsBackToTheFirstProvider(t *testing.T) {
	path := writeConfig(t, twoProviderConfig)
	got, err := meterUpstream(path, "")
	if err != nil {
		t.Fatalf("meterUpstream: %v", err)
	}
	if got != "https://api.deepseek.com" {
		t.Fatalf("upstream = %q, want the first provider", got)
	}
}

// Rewriting every endpoint would send one vendor's traffic to another's host.
func TestWriteMeteredConfigRedirectsOnlyTheBenchmarkedProvider(t *testing.T) {
	path := writeConfig(t, twoProviderConfig)
	dir := t.TempDir()
	if err := writeMeteredConfig(path, dir, "kimi-k2", "http://127.0.0.1:9999"); err != nil {
		t.Fatalf("writeMeteredConfig: %v", err)
	}
	providers := readProviders(t, dir)
	if len(providers) != 2 {
		t.Fatalf("providers = %d, want both preserved", len(providers))
	}
	byName := map[string]map[string]any{}
	for _, p := range providers {
		name, _ := p["name"].(string)
		byName[name] = p
	}
	if got := byName["kimi"]["base_url"]; got != "http://127.0.0.1:9999" {
		t.Fatalf("kimi base_url = %v, want the meter", got)
	}
	if got := byName["deepseek"]["base_url"]; got != "https://api.deepseek.com" {
		t.Fatalf("deepseek base_url = %v, want it left alone", got)
	}
	if got := byName["kimi"]["no_proxy"]; got != true {
		t.Fatalf("no_proxy = %v, want true: the meter is loopback plaintext", got)
	}
}

// The key lives in an env var the child inherits, so metering must never need
// to read, copy, or rewrite a credential.
func TestWriteMeteredConfigKeepsCredentialsUntouched(t *testing.T) {
	path := writeConfig(t, twoProviderConfig)
	dir := t.TempDir()
	if err := writeMeteredConfig(path, dir, "deepseek-v4-flash", "http://127.0.0.1:1"); err != nil {
		t.Fatalf("writeMeteredConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "DEEPSEEK_API_KEY") {
		t.Fatalf("api_key_env was dropped:\n%s", raw)
	}
	if strings.Contains(string(raw), "api_key =") {
		t.Fatalf("a literal key appeared in the metered config:\n%s", raw)
	}
}

func TestMeterUpstreamRejectsAConfigWithNoProviders(t *testing.T) {
	path := writeConfig(t, "model = \"x\"\n")
	if _, err := meterUpstream(path, ""); err == nil {
		t.Fatal("a config with no providers must fail loudly, not meter nothing")
	}
}
