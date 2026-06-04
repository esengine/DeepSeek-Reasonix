package config

import (
	"testing"
)

func TestMigrate_LegacyProviderEffort(t *testing.T) {
	c := Default()
	// Simulate old config with provider-level effort.
	c.Providers[0].Effort = "high"
	c.Session.Effort = ""

	normalizeLegacyEffort(c)

	if c.Session.Effort != "high" {
		t.Errorf("Session.Effort = %q, want migrated \"high\"", c.Session.Effort)
	}
	if c.MigrationHint == "" {
		t.Error("MigrationHint should be set after migration")
	}
	// Legacy field should be cleared.
	for i, p := range c.Providers {
		if p.Effort != "" {
			t.Errorf("Providers[%d].Effort = %q, should be cleared", i, p.Effort)
		}
	}
}

func TestMigrate_LegacyOff(t *testing.T) {
	c := Default()
	c.Providers[0].Effort = "off"
	c.Session.Effort = ""

	normalizeLegacyEffort(c)

	if c.Session.Effort != "" {
		t.Errorf("Session.Effort = %q, want empty (off migrated to auto)", c.Session.Effort)
	}
}

func TestMigrate_SessionEffortAlreadySet(t *testing.T) {
	c := Default()
	c.Providers[0].Effort = "max"
	c.Session.Effort = "low"

	normalizeLegacyEffort(c)

	// Session.Effort should not be overwritten if already set.
	if c.Session.Effort != "low" {
		t.Errorf("Session.Effort = %q, want preserved \"low\"", c.Session.Effort)
	}
	// Legacy field should still be cleared.
	if c.Providers[0].Effort != "" {
		t.Errorf("Providers[0].Effort = %q, should be cleared", c.Providers[0].Effort)
	}
}

func TestMigrate_DeepSeekWithExplicitSupportedEfforts(t *testing.T) {
	c := Default()
	// DeepSeek with explicit SupportedEfforts should keep them.
	ds := c.Providers[0] // deepseek-flash
	if ds.SupportedEfforts != nil {
		t.Skip("deepseek-flash already has SupportedEfforts in Default()")
	}
	// Add SupportedEfforts to DeepSeek.
	c.Providers[0].SupportedEfforts = []string{"auto", "high", "max"}
	c.Providers[0].DefaultEffort = "auto"

	caps := EffortCapabilityForEntry(&c.Providers[0])
	if len(caps.Levels) != 3 || caps.Default != "auto" {
		t.Errorf("DeepSeek with explicit caps = %+v", caps)
	}
}

func TestValidateEffortConfig(t *testing.T) {
	c := Default()
	// Valid config should pass.
	if err := c.ValidateEffortConfig(); err != nil {
		t.Errorf("ValidateEffortConfig() = %v, want nil", err)
	}

	// Invalid: DefaultEffort not in SupportedEfforts.
	c.Providers[2].DefaultEffort = "turbo" // mimo-pro
	if err := c.ValidateEffortConfig(); err == nil {
		t.Error("ValidateEffortConfig() should fail when DefaultEffort not in SupportedEfforts")
	}
}

func TestAdaptToProvider_SessionEffort(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}

	s := Session{Provider: "mimo", Effort: "high"}
	warning := s.AdaptToProvider(caps)
	if warning != "" {
		t.Errorf("AdaptToProvider(high) = %q, want empty", warning)
	}
	if s.Effort != "high" {
		t.Errorf("Session.Effort = %q, want high", s.Effort)
	}

	s.Effort = "max"
	warning = s.AdaptToProvider(caps)
	if warning == "" {
		t.Error("AdaptToProvider(max) should return warning")
	}
	if s.Effort == "max" {
		t.Errorf("Session.Effort should be degraded from max, got %q", s.Effort)
	}
}
