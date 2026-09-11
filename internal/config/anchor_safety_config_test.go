package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLegacyAnchorSafetyGateConfigRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Agent.LegacyAnchorSafetyGate = true
	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, "legacy_anchor_safety_gate = true") {
		t.Fatalf("rendered config omitted legacy anchor safety switch:\n%s", rendered)
	}
	var decoded Config
	if _, err := toml.Decode(rendered, &decoded); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	if !decoded.Agent.LegacyAnchorSafetyGate {
		t.Fatal("legacy anchor safety switch did not round-trip")
	}
	if project := RenderTOMLForScope(cfg, RenderScopeProject); strings.Contains(project, "legacy_anchor_safety_gate") {
		t.Fatalf("project config exposed user-global anchor safety switch:\n%s", project)
	}

	var explicit Config
	if _, err := toml.Decode("[agent]\nlegacy_anchor_safety_gate = true\n", &explicit); err != nil {
		t.Fatalf("decode explicit switch: %v", err)
	}
	if !explicit.Agent.LegacyAnchorSafetyGate {
		t.Fatal("explicit TOML switch was ignored")
	}
}

func TestProjectCannotOverrideLegacyAnchorSafetyGate(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[agent]\nlegacy_anchor_safety_gate = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	if cfg.Agent.LegacyAnchorSafetyGate {
		t.Fatal("project config weakened the user-global anchor safety policy")
	}
}
