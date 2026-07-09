package config

import "testing"

func TestBashModeWindowsAlwaysOff(t *testing.T) {
	cfg := Default()
	if got := cfg.BashModeForGOOS("windows"); got != "off" {
		t.Fatalf("empty Windows bash mode = %q, want off", got)
	}

	cfg.Sandbox.Bash = "enforce"
	if got := cfg.BashModeForGOOS("windows"); got != "off" {
		t.Fatalf("explicit Windows bash mode = %q, want off", got)
	}

	cfg.Sandbox.Bash = ""
	if got := cfg.BashModeForGOOS("darwin"); got != "enforce" {
		t.Fatalf("empty Darwin bash mode = %q, want enforce", got)
	}
}
