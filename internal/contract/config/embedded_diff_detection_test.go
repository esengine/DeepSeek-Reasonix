package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The detection is opt-in: an unset or nil setting leaves it off.
func TestEmbeddedDiffDetectionDefaultsOff(t *testing.T) {
	if Default().EmbeddedDiffDetectionEnabled() {
		t.Fatal("embedded_diff_detection is on by default")
	}
	var nilCfg *Config
	if nilCfg.EmbeddedDiffDetectionEnabled() {
		t.Fatal("a nil config reports the detection on")
	}
	off := false
	cfg := Default()
	cfg.Agent.EmbeddedDiffDetection = &off
	if cfg.EmbeddedDiffDetectionEnabled() {
		t.Fatal("an explicit false reports the detection on")
	}
}

// A set value round-trips through the rendered [agent] section.
func TestEmbeddedDiffDetectionRoundTrips(t *testing.T) {
	cfg := Default()
	on := true
	cfg.Agent.EmbeddedDiffDetection = &on

	rendered := RenderTOMLForScope(cfg, RenderScopeUser)
	if !strings.Contains(rendered, "embedded_diff_detection = true") {
		t.Fatalf("render missing embedded_diff_detection:\n%s", rendered)
	}
	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v", err)
	}
	if !got.EmbeddedDiffDetectionEnabled() {
		t.Fatal("round-trip lost embedded_diff_detection = true")
	}
}
