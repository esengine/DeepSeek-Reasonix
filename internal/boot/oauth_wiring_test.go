package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/mcpauth"
)

// newTestAuthorizer builds an mcpauth.Client backed by a temp credentials file.
func newTestAuthorizer(t *testing.T) *mcpauth.Client {
	t.Helper()
	c, err := mcpauth.New(mcpauth.Options{StorePath: filepath.Join(t.TempDir(), "creds.json")})
	if err != nil {
		t.Fatalf("mcpauth.New: %v", err)
	}
	return c
}

// TestPluginSpecInjectsAuthorizerForHTTP verifies that remote servers receive the
// OAuth authorizer and their per-server config is registered, while stdio
// servers do not.
func TestPluginSpecInjectsAuthorizerForHTTP(t *testing.T) {
	auth := newTestAuthorizer(t)
	opts := PluginSpecOptions{Authorizer: auth}

	httpEntry := config.PluginEntry{
		Name:  "remote",
		Type:  "http",
		URL:   "https://mcp.example.com/mcp",
		OAuth: &config.MCPOAuthConfig{ClientID: "abc", Scopes: []string{"read"}},
	}
	spec := pluginSpecFromEntryWithOptions(httpEntry, "", opts)
	if spec.Authorizer == nil {
		t.Fatal("http server should have an Authorizer injected")
	}

	stdioSpec := pluginSpecFromEntryWithOptions(config.PluginEntry{Name: "local", Type: "stdio", Command: "echo"}, "", opts)
	if stdioSpec.Authorizer != nil {
		t.Fatal("stdio server should NOT receive an Authorizer")
	}
}

// TestPluginSpecNoAuthorizerKeepsNil verifies that a nil authorizer option
// leaves http servers without one (OAuth fully disabled) and never panics.
func TestPluginSpecNoAuthorizerKeepsNil(t *testing.T) {
	spec := pluginSpecFromEntryWithOptions(config.PluginEntry{Name: "remote", Type: "http", URL: "https://mcp.example.com"}, "", PluginSpecOptions{})
	if spec.Authorizer != nil {
		t.Fatal("nil authorizer option should leave Spec.Authorizer nil")
	}
}
