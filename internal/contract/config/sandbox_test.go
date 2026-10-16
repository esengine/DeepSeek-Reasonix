package config

import "testing"

// Windows and Android both lack an OS-level Bash sandbox, so both force the
// effective mode off however the config file is written. Grouped in one test
// because the two are one behavior: a platform with no backend runs the shell
// unconfined rather than refusing every command.
func TestBashModeSandboxlessPlatformsAlwaysOff(t *testing.T) {
	for _, goos := range []string{"windows", "android"} {
		cfg := Default()
		if got := cfg.BashModeForGOOS(goos); got != "off" {
			t.Fatalf("empty %s bash mode = %q, want off", goos, got)
		}

		cfg.Sandbox.Bash = "enforce"
		if got := cfg.BashModeForGOOS(goos); got != "off" {
			t.Fatalf("explicit %s bash mode = %q, want off", goos, got)
		}
	}
}

func TestBashModeKeepsExplicitModeWhereSandboxExists(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Bash = ""
	if got := cfg.BashModeForGOOS("darwin"); got != "enforce" {
		t.Fatalf("empty Darwin bash mode = %q, want enforce", got)
	}
	if got := cfg.BashModeForGOOS("linux"); got != "enforce" {
		t.Fatalf("empty Linux bash mode = %q, want enforce", got)
	}
}
