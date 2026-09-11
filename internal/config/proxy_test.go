package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectProxyHostsFromNoProxyProviders(t *testing.T) {
	c := Default()
	c.Providers = append(c.Providers, ProviderEntry{Name: "domestic", Kind: "openai", BaseURL: "https://domestic.example/v1", Model: "chat", NoProxy: true})
	spec := c.NetworkProxySpec()
	hasDirectHost := false
	for _, h := range spec.DirectHosts {
		if h == "domestic.example" {
			hasDirectHost = true
		}
		if h == "api.deepseek.com" {
			t.Errorf("DeepSeek works through the proxy and must not be forced direct: %v", spec.DirectHosts)
		}
	}
	if !hasDirectHost {
		t.Errorf("a no_proxy provider's host should land in DirectHosts, got %v", spec.DirectHosts)
	}
}

func TestExplicitProxyOverridesProviderNoProxy(t *testing.T) {
	// An explicit custom proxy (e.g. a mandatory corporate proxy) must apply to
	// every provider, including no_proxy ones, so it isn't unreachable
	// behind the proxy (#3635).
	c := Default()
	c.Providers = append(c.Providers, ProviderEntry{Name: "domestic", Kind: "openai", BaseURL: "https://domestic.example/v1", Model: "chat", NoProxy: true})
	c.Network.ProxyMode = "custom"
	spec := c.NetworkProxySpec()
	for _, h := range spec.DirectHosts {
		if h == "domestic.example" {
			t.Fatalf("custom proxy must not force no_proxy providers direct; DirectHosts = %v", spec.DirectHosts)
		}
	}
}

func TestLoadForRootWithoutCredentialsReadOnlyUsesEffectiveProjectProxy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	const key = "REASONIX_TEST_EFFECTIVE_PROXY_KEY"
	t.Setenv(key, "inherited-value")

	if err := os.WriteFile(UserConfigPath(), []byte("[network]\nproxy_mode = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("PROJECT_PROXY_URL=http://127.0.0.1:9876\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[network]
proxy_mode = "custom"
proxy_url = "${PROJECT_PROXY_URL}"

[[providers]]
name = "project-provider"
kind = "openai"
base_url = "https://provider.invalid/v1"
model = "model"
api_key_env = "`+key+`"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := UserCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte(key+"=stored-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootWithoutCredentialsReadOnly(project)
	if err != nil {
		t.Fatalf("LoadForRootWithoutCredentialsReadOnly: %v", err)
	}
	spec := cfg.NetworkProxySpec()
	if spec.Mode != "custom" || spec.URL != "http://127.0.0.1:9876" {
		t.Fatalf("effective proxy = %+v, want project custom proxy with .env expansion", spec)
	}
	provider, ok := cfg.Provider("project-provider")
	if !ok {
		t.Fatal("project provider missing from effective config")
	}
	if provider.resolvedAPIKey != "" {
		t.Fatal("credential-free load eagerly resolved the provider key")
	}
	if got := os.Getenv(key); got != "inherited-value" {
		t.Fatalf("credential-free load changed process env to %q", got)
	}
}
