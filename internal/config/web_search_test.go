package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestEffectiveWebSearchDefaultsOnlySupportedOfficialDeepSeekAPIs(t *testing.T) {
	explicitTrue := true
	explicitFalse := false
	tests := []struct {
		name  string
		entry ProviderEntry
		want  bool
	}{
		{
			name:  "official responses omitted defaults on",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://api.deepseek.com"},
			want:  true,
		},
		{
			name:  "official anthropic omitted defaults on",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic"},
			want:  true,
		},
		{
			name:  "official responses explicit off wins",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://api.deepseek.com", WebSearch: &explicitFalse},
			want:  false,
		},
		{
			name:  "compatible third party omitted stays off",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://gateway.example/v1"},
			want:  false,
		},
		{
			name:  "compatible third party explicit on wins",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://gateway.example/anthropic", WebSearch: &explicitTrue},
			want:  true,
		},
		{
			name:  "deepseek subdomain is not treated as official",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://relay.deepseek.com"},
			want:  false,
		},
		{
			name:  "responses rejects anthropic path",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://api.deepseek.com/anthropic"},
			want:  false,
		},
		{
			name:  "anthropic requires documented path",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://api.deepseek.com"},
			want:  false,
		},
		{
			name:  "official trailing slash accepted",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic/"},
			want:  true,
		},
		{
			name:  "insecure scheme is not official",
			entry: ProviderEntry{Kind: "responses", BaseURL: "http://api.deepseek.com"},
			want:  false,
		},
		{
			name:  "official chat completions has no server tool wire",
			entry: ProviderEntry{Kind: "openai", BaseURL: "https://api.deepseek.com", WebSearch: &explicitTrue},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveWebSearch(&tt.entry); got != tt.want {
				t.Fatalf("EffectiveWebSearch(%+v) = %t, want %t", tt.entry, got, tt.want)
			}
		})
	}
}

func TestOpenCodeGoDeepSeekWebSearchCapabilityIsModelScoped(t *testing.T) {
	explicitTrue := true
	tests := []struct {
		name  string
		entry ProviderEntry
		want  bool
	}{
		{
			name:  "responses flash",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1", Models: []string{"deepseek-v4-flash"}, WebSearch: &explicitTrue},
			want:  true,
		},
		{
			name:  "anthropic flash trailing slash",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go/", Model: "deepseek-v4-flash", WebSearch: &explicitTrue},
			want:  true,
		},
		{
			name:  "mixed anthropic catalog remains unverified",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", Models: []string{"qwen3.7-plus", "deepseek-v4-flash"}, WebSearch: &explicitTrue},
			want:  false,
		},
		{
			name:  "pro remains unsupported",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4-pro", WebSearch: &explicitTrue},
			want:  false,
		},
		{
			name:  "wrong responses path",
			entry: ProviderEntry{Kind: "responses", BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-flash", WebSearch: &explicitTrue},
			want:  false,
		},
		{
			name:  "lookalike host",
			entry: ProviderEntry{Kind: "anthropic", BaseURL: "https://opencode.ai.attacker.example/zen/go", Model: "deepseek-v4-flash", WebSearch: &explicitTrue},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasServerWebSearchCapability(&tt.entry); got != tt.want {
				t.Fatalf("HasServerWebSearchCapability(%+v) = %t, want %t", tt.entry, got, tt.want)
			}
		})
	}
}

func TestWebSearchTOMLRoundTripPreservesExplicitOff(t *testing.T) {
	explicitFalse := false
	cfg := &Config{Providers: []ProviderEntry{{
		Name:      "deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://api.deepseek.com",
		Model:     "deepseek-v4-flash",
		WebSearch: &explicitFalse,
	}}}
	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, "web_search  = false") {
		t.Fatalf("rendered TOML lost explicit web_search=false:\n%s", rendered)
	}

	var roundTrip Config
	if _, err := toml.Decode(rendered, &roundTrip); err != nil {
		t.Fatalf("decode rendered TOML: %v", err)
	}
	entry, ok := roundTrip.Provider("deepseek-responses")
	if !ok {
		t.Fatal("round-tripped provider missing")
	}
	if entry.WebSearch == nil || *entry.WebSearch || EffectiveWebSearch(entry) {
		t.Fatalf("round-tripped web search = pointer:%v effective:%t, want explicit false", entry.WebSearch, EffectiveWebSearch(entry))
	}
}

func TestLegacyOfficialWebSearchOmissionDefaultsOnAcrossLoaders(t *testing.T) {
	const legacy = `config_version = 1
default_model = "deepseek-responses/deepseek-v4-flash"

[[providers]]
name = "deepseek-responses"
kind = "responses"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
`
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeConfig, err := LoadForRootReadOnly(workspace)
	if err != nil {
		t.Fatalf("runtime load: %v", err)
	}
	editConfig := LoadForEditWithoutCredentials(path)
	for name, cfg := range map[string]*Config{"runtime": runtimeConfig, "edit": editConfig} {
		entry, ok := cfg.Provider("deepseek-responses")
		if !ok {
			t.Fatalf("%s legacy provider missing", name)
		}
		if entry.WebSearch != nil || !EffectiveWebSearch(entry) {
			t.Fatalf("%s legacy omitted web search = pointer:%v effective:%t, want nil/true", name, entry.WebSearch, EffectiveWebSearch(entry))
		}
	}
}
