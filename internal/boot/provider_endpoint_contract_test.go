package boot

import (
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
)

func TestRuntimeRejectsProtocolEndpointMismatch(t *testing.T) {
	entry := &config.ProviderEntry{
		Name: "deepseek-anthropic", PresetID: "deepseek-anthropic", Kind: "openai",
		BaseURL: "https://api.deepseek.com/anthropic/v1", RequestURL: "https://api.deepseek.com/anthropic/v1/chat/completions",
		Model: "deepseek-v4-flash",
	}
	_, err := NewProviderWithProxyAndModelInfo(entry, netclient.ProxySpec{}, nil)
	if err == nil || !strings.Contains(err.Error(), "https://api.deepseek.com/v1/chat/completions") {
		t.Fatalf("runtime mismatch error = %v", err)
	}
}
