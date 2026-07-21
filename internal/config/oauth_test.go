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

func TestClearOAuthSecretStripsInlinePrivateKey(t *testing.T) {
	in := &MCPOAuthConfig{
		ClientID:                "c",
		TokenEndpointAuthMethod: "private_key_jwt",
		PrivateKeyPEM:           "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----",
		PrivateKeyPath:          "/etc/keys/svc.pem",
	}
	out, changed := ClearOAuthSecret(in)
	if !changed {
		t.Fatal("expected changed=true when inline key present")
	}
	if out.PrivateKeyPEM != "" {
		t.Fatal("inline private key PEM must be stripped")
	}
	// The path is a pointer, not a secret — it must survive clear-auth.
	if out.PrivateKeyPath != "/etc/keys/svc.pem" {
		t.Fatalf("private_key_path must be preserved: %q", out.PrivateKeyPath)
	}
	if out.TokenEndpointAuthMethod != "private_key_jwt" {
		t.Fatal("auth method must be preserved")
	}
}

func TestClearOAuthSecretStripsJWTBearerGrantKey(t *testing.T) {
	in := &MCPOAuthConfig{
		JWTBearerGrant: &MCPJWTBearerGrant{
			Issuer:         "svc",
			PrivateKeyPEM:  "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----",
			PrivateKeyPath: "/etc/keys/grant.pem",
		},
	}
	out, changed := ClearOAuthSecret(in)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if out.JWTBearerGrant.PrivateKeyPEM != "" {
		t.Fatal("grant inline key must be stripped")
	}
	if out.JWTBearerGrant.PrivateKeyPath != "/etc/keys/grant.pem" {
		t.Fatal("grant private_key_path must be preserved")
	}
}

func TestPluginEntryOAuthJWTFieldsFromTOML(t *testing.T) {
	src := `
[[plugins]]
name = "svc"
type = "http"
url = "https://mcp.example.com/mcp"
[plugins.oauth]
token_endpoint_auth_method = "private_key_jwt"
private_key_path = "/etc/keys/svc.pem"
client_assertion_signing_alg = "RS256"
[plugins.oauth.jwt_bearer_grant]
issuer = "service-account-1"
private_key_path = "/etc/keys/grant.pem"
signing_alg = "ES256"
scopes = ["read"]
`
	cfg, err := loadConfigForTest(t, src)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	o := cfg.Plugins[0].OAuth
	if o.TokenEndpointAuthMethod != "private_key_jwt" {
		t.Fatalf("method = %q", o.TokenEndpointAuthMethod)
	}
	if o.PrivateKeyPath != "/etc/keys/svc.pem" || o.ClientAssertionSigningAlg != "RS256" {
		t.Fatalf("jwt fields: %+v", o)
	}
	if o.JWTBearerGrant == nil || o.JWTBearerGrant.Issuer != "service-account-1" {
		t.Fatalf("grant not parsed: %+v", o.JWTBearerGrant)
	}
	if o.JWTBearerGrant.SigningAlg != "ES256" || len(o.JWTBearerGrant.Scopes) != 1 {
		t.Fatalf("grant fields: %+v", o.JWTBearerGrant)
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
