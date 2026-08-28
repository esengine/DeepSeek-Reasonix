package main

import (
	"os"
	"strconv"
	"strings"
)

// ShellRepairGuidanceView is a read-only repair hint for platforms where
// Reasonix must not run the system package manager. Command is an allowlisted,
// copy-only suggestion; it is never passed to a shell by the desktop backend.
type ShellRepairGuidanceView struct {
	Manager string `json:"manager"`
	Command string `json:"command,omitempty"`
}

func shellRepairGuidanceForGOOS(goos string) *ShellRepairGuidanceView {
	switch goos {
	case "darwin":
		return &ShellRepairGuidanceView{Manager: "homebrew", Command: "brew install bash"}
	case "linux":
		contents, _ := os.ReadFile("/etc/os-release")
		return linuxShellRepairGuidance(contents)
	default:
		return nil
	}
}

// linuxShellRepairGuidance maps trusted OS identity metadata to a fixed command.
// No os-release value is interpolated into the result, so a modified file cannot
// turn the Settings copy button into an arbitrary-command delivery surface.
func linuxShellRepairGuidance(osRelease []byte) *ShellRepairGuidanceView {
	identities := linuxOSReleaseIdentities(osRelease)
	for _, candidate := range []struct {
		ids     []string
		manager string
		command string
	}{
		{[]string{"debian", "ubuntu", "linuxmint", "pop"}, "apt", "apt-get install bash"},
		{[]string{"fedora", "rhel", "centos", "rocky", "almalinux"}, "dnf", "dnf install bash"},
		{[]string{"arch", "manjaro", "endeavouros"}, "pacman", "pacman -S bash"},
		{[]string{"opensuse", "suse", "sles"}, "zypper", "zypper install bash"},
		{[]string{"alpine"}, "apk", "apk add bash"},
	} {
		if intersectsIdentity(identities, candidate.ids) {
			return &ShellRepairGuidanceView{Manager: candidate.manager, Command: candidate.command}
		}
	}
	return &ShellRepairGuidanceView{Manager: "system"}
}

func linuxOSReleaseIdentities(contents []byte) []string {
	values := map[string]string{}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "ID" && key != "ID_LIKE" {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, "'\"")
		}
		values[key] = strings.ToLower(value)
	}
	return strings.Fields(values["ID"] + " " + values["ID_LIKE"])
}

func intersectsIdentity(available, candidates []string) bool {
	for _, actual := range available {
		for _, candidate := range candidates {
			if actual == candidate {
				return true
			}
		}
	}
	return false
}
