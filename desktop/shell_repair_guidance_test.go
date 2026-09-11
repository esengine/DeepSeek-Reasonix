package main

import (
	"strings"
	"testing"
)

func TestShellRepairGuidancePerPlatform(t *testing.T) {
	if got := shellRepairGuidanceForGOOS("windows"); got != nil {
		t.Fatalf("Windows guidance = %+v, want nil because its install action owns repair", got)
	}
	if got := shellRepairGuidanceForGOOS("darwin"); got != nil {
		t.Fatalf("macOS shell guidance = %+v, want nil because zsh/sh are native fallbacks", got)
	}
	if got := gitRepairGuidanceForGOOS("darwin"); got == nil || got.Manager != "homebrew" || got.Command != "brew install git" {
		t.Fatalf("macOS Git guidance = %+v, want copy-only Homebrew Git command", got)
	}
}

func TestLinuxShellRepairGuidanceUsesAllowlistedCommandsWithoutSudo(t *testing.T) {
	tests := []struct {
		name      string
		osRelease string
		manager   string
		command   string
	}{
		{"ubuntu", "ID=ubuntu\nID_LIKE=debian\n", "apt", "apt-get install bash"},
		{"fedora-like", "ID=custom\nID_LIKE=\"rhel fedora\"\n", "dnf", "dnf install bash"},
		{"arch", "ID=arch\n", "pacman", "pacman -S bash"},
		{"opensuse", "ID='opensuse'\nID_LIKE=\"suse\"\n", "zypper", "zypper install bash"},
		{"alpine", "ID=alpine\n", "apk", "apk add bash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := linuxShellRepairGuidance([]byte(test.osRelease))
			if got.Manager != test.manager || got.Command != test.command {
				t.Fatalf("guidance = %+v, want manager=%q command=%q", got, test.manager, test.command)
			}
			if strings.Contains(strings.ToLower(got.Command), "sudo") {
				t.Fatalf("copy-only repair command must not prescribe sudo: %q", got.Command)
			}
		})
	}
}

func TestLinuxGitRepairGuidanceUsesAllowlistedCommandsWithoutSudo(t *testing.T) {
	got := linuxGitRepairGuidance([]byte("ID=ubuntu\nID_LIKE=debian\n"))
	if got.Manager != "apt" || got.Command != "apt-get install git" {
		t.Fatalf("Git guidance = %+v, want apt Git command", got)
	}
	if strings.Contains(strings.ToLower(got.Command), "sudo") {
		t.Fatalf("copy-only Git command must not prescribe sudo: %q", got.Command)
	}
}

func TestLinuxShellRepairGuidanceDoesNotInterpolateOSRelease(t *testing.T) {
	got := linuxShellRepairGuidance([]byte("ID=unknown; touch /tmp/not-allowed\n"))
	if got.Manager != "system" || got.Command != "" {
		t.Fatalf("unknown distribution guidance = %+v, want generic no-command fallback", got)
	}
}
