package config

import (
	"testing"
)

func TestSetDefaultModel(t *testing.T) {
	c := Default()
	if err := c.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatalf("set valid default: %v", err)
	}
	if c.DefaultModel != "deepseek-pro" {
		t.Errorf("default = %q, want deepseek-pro", c.DefaultModel)
	}
	if err := c.SetDefaultModel("nope"); err == nil {
		t.Error("expected error for unknown provider")
	}
	// "provider/model" form is also accepted: the /model picker stores the
	// full ref so a user can land on a non-default model under the same
	// provider across restarts.
	if err := c.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("set provider/model default: %v", err)
	}
	if c.DefaultModel != "deepseek-pro/deepseek-v4-pro" {
		t.Errorf("default = %q, want deepseek-pro/deepseek-v4-pro", c.DefaultModel)
	}
	if err := c.SetDefaultModel("deepseek-pro/missing"); err == nil {
		t.Error("expected error for unknown model under known provider")
	}
	if err := c.SetDefaultModel(""); err == nil {
		t.Error("expected error for empty name")
	}
}
