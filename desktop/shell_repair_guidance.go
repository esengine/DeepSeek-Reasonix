package main

import (
	"os"
	"slices"
	"strconv"
	"strings"
)

// RepairGuidanceView is a read-only repair hint for platforms where
// Reasonix must not run the system package manager. Command is an allowlisted,
// copy-only suggestion; it is never passed to a shell by the desktop backend.
type RepairGuidanceView struct {
	Manager string `json:"manager"`
	Command string `json:"command,omitempty"`
}

func shellRepairGuidanceForGOOS(goos string) *RepairGuidanceView {
	switch goos {
	case "linux":
		contents, _ := os.ReadFile("/etc/os-release")
		return linuxShellRepairGuidance(contents)
	default:
		return nil
	}
}

func gitRepairGuidanceForGOOS(goos string) *RepairGuidanceView {
	switch goos {
	case "darwin":
		return &RepairGuidanceView{Manager: "homebrew", Command: "brew install git"}
	case "linux":
		contents, _ := os.ReadFile("/etc/os-release")
		return linuxGitRepairGuidance(contents)
	default:
		return nil
	}
}

// linuxShellRepairGuidance maps trusted OS identity metadata to a fixed command.
// No os-release value is interpolated into the result, so a modified file cannot
// turn the Settings copy button into an arbitrary-command delivery surface.
func linuxShellRepairGuidance(osRelease []byte) *RepairGuidanceView {
	return linuxPackageRepairGuidance(osRelease, "bash")
}

func linuxGitRepairGuidance(osRelease []byte) *RepairGuidanceView {
	return linuxPackageRepairGuidance(osRelease, "git")
}

func linuxPackageRepairGuidance(osRelease []byte, packageName string) *RepairGuidanceView {
	identities := linuxOSReleaseIdentities(osRelease)
	for _, candidate := range []struct {
		ids     []string
		manager string
		bash    string
		git     string
	}{
		{[]string{"debian", "ubuntu", "linuxmint", "pop"}, "apt", "apt-get install bash", "apt-get install git"},
		{[]string{"fedora", "rhel", "centos", "rocky", "almalinux"}, "dnf", "dnf install bash", "dnf install git"},
		{[]string{"arch", "manjaro", "endeavouros"}, "pacman", "pacman -S bash", "pacman -S git"},
		{[]string{"opensuse", "suse", "sles"}, "zypper", "zypper install bash", "zypper install git"},
		{[]string{"alpine"}, "apk", "apk add bash", "apk add git"},
	} {
		if intersectsIdentity(identities, candidate.ids) {
			command := candidate.bash
			if packageName == "git" {
				command = candidate.git
			} else if packageName != "bash" {
				return &RepairGuidanceView{Manager: "system"}
			}
			return &RepairGuidanceView{Manager: candidate.manager, Command: command}
		}
	}
	return &RepairGuidanceView{Manager: "system"}
}

func linuxOSReleaseIdentities(contents []byte) []string {
	values := map[string]string{}
	for line := range strings.SplitSeq(string(contents), "\n") {
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
		if slices.Contains(candidates, actual) {
			return true
		}
	}
	return false
}
