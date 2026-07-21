package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPluginEntryOAuthFromTOML(t *testing.T) {
	src := `
[[plugins]]
name = "remote"
type = "http"
url = "https://mcp.example.com/mcp"
[plugins.oauth]
client_id = "abc-123"
client_secret = "supersecret"
scopes = ["read", "write"]
redirect_port = 8765
skip_browser = true
skip_dynamic_registration = true
trusted_origins = ["https://auth.example.com"]
`
	cfg, err := loadConfigForTest(t, src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("want 1 plugin, got %d", len(cfg.Plugins))
	}
	o := cfg.Plugins[0].OAuth
	if o == nil {
		t.Fatal("OAuth config not parsed")
	}
	if o.ClientID != "abc-123" || o.ClientSecret != "supersecret" {
		t.Errorf("client id/secret = %q/%q", o.ClientID, o.ClientSecret)
	}
	if o.RedirectPort != 8765 || !o.SkipBrowser || !o.SkipDynamicRegistration {
		t.Errorf("flags wrong: %+v", o)
	}
	if len(o.Scopes) != 2 || len(o.TrustedOrigins) != 1 {
		t.Errorf("slices wrong: %+v", o)
	}
}

func TestClearOAuthSecretStripsSecret(t *testing.T) {
	in := &MCPOAuthConfig{ClientID: "abc", ClientSecret: "secret", Scopes: []string{"read"}}
	out, changed := ClearOAuthSecret(in)
	if !changed {
		t.Fatal("expected changed=true when a secret is present")
	}
	if out.ClientSecret != "" {
		t.Fatalf("secret not cleared: %q", out.ClientSecret)
	}
	// Non-secret fields are preserved.
	if out.ClientID != "abc" || len(out.Scopes) != 1 {
		t.Fatalf("non-secret fields lost: %+v", out)
	}
}

func TestClearOAuthSecretNoopForPublicClient(t *testing.T) {
	// A public PKCE client has no secret; clearing must be a no-op.
	in := &MCPOAuthConfig{ClientID: "abc", Scopes: []string{"read"}}
	out, changed := ClearOAuthSecret(in)
	if changed {
		t.Fatal("expected changed=false for a public client")
	}
	if out != in {
		t.Fatal("expected the same pointer for a no-op clear")
	}
}

func TestClearOAuthSecretNil(t *testing.T) {
	out, changed := ClearOAuthSecret(nil)
	if changed || out != nil {
		t.Fatalf("nil clear should be a no-op: %+v changed=%v", out, changed)
	}
}

// loadConfigForTest decodes a TOML document into a Config the same way the
// loader does, without touching the filesystem.
func loadConfigForTest(t *testing.T, src string) (*Config, error) {
	t.Helper()
	_ = strings.TrimSpace // keep strings import for readability of failures
	var cfg Config
	_, err := toml.Decode(src, &cfg)
	return &cfg, err
}
