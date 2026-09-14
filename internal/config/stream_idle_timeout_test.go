package config

import (
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestProviderEntryStreamIdleTimeout(t *testing.T) {
	eight, zero, negative := 8, 0, -5
	cases := []struct {
		name  string
		entry *ProviderEntry
		want  time.Duration
	}{
		{"unset", &ProviderEntry{}, 0},
		{"nil entry", nil, 0},
		{"zero disables", &ProviderEntry{StreamIdleTimeoutSeconds: &zero}, 0},
		{"negative disables", &ProviderEntry{StreamIdleTimeoutSeconds: &negative}, 0},
		{"positive seconds", &ProviderEntry{StreamIdleTimeoutSeconds: &eight}, 8 * time.Second},
	}
	for _, tc := range cases {
		if got := tc.entry.StreamIdleTimeout(); got != tc.want {
			t.Errorf("%s: StreamIdleTimeout() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestStreamIdleTimeoutRendersAndRoundTrips proves a configured window survives
// a RenderTOML -> load rewrite instead of being dropped as an unknown field.
func TestStreamIdleTimeoutRendersAndRoundTrips(t *testing.T) {
	eight := 8
	c := Default()
	c.Providers = append(c.Providers, ProviderEntry{
		Name: "p", Kind: "openai", BaseURL: "https://example.com/v1", Model: "m",
		StreamIdleTimeoutSeconds: &eight,
	})

	rendered := RenderTOML(c)
	if !strings.Contains(rendered, "stream_idle_timeout_seconds = 8") {
		t.Fatalf("rendered TOML omits stream_idle_timeout_seconds:\n%s", rendered)
	}
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}
	for _, p := range got.Providers {
		if p.Name != "p" {
			continue
		}
		if got := p.StreamIdleTimeout(); got != 8*time.Second {
			t.Errorf("round-tripped StreamIdleTimeout = %v, want 8s", got)
		}
		return
	}
	t.Fatal("provider p missing after round trip")
}
