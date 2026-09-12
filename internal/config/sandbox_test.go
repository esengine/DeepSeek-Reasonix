package config

import "testing"

func TestBashModeDefaultsToEnforceOnWindows(t *testing.T) {
	cfg := Default()
	if got := cfg.BashModeForGOOS("windows"); got != "enforce" {
		t.Fatalf("empty Windows bash mode = %q, want enforce", got)
	}

	cfg.Sandbox.Bash = "enforce"
	if got := cfg.BashModeForGOOS("windows"); got != "enforce" {
		t.Fatalf("explicit Windows bash mode = %q, want enforce", got)
	}

	cfg.Sandbox.Bash = ""
	if got := cfg.BashModeForGOOS("darwin"); got != "enforce" {
		t.Fatalf("empty Darwin bash mode = %q, want enforce", got)
	}
}
