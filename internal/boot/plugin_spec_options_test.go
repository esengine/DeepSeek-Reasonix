package boot

import (
	"net/http"
	"testing"

	"reasonix/internal/config"
)

func TestPluginSpecsCarryOAuthHTTPClient(t *testing.T) {
	client := &http.Client{}
	specs := PluginSpecsForRootWithOptions([]config.PluginEntry{{
		Name: "remote", Type: "http", URL: "https://mcp.example.test",
	}}, "", PluginSpecOptions{OAuthHTTPClient: client})
	if len(specs) != 1 || specs[0].OAuthHTTPClient != client {
		t.Fatalf("OAuth HTTP client was not carried into plugin.Spec: %+v", specs)
	}
}
