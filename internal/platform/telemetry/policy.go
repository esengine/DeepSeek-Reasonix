// Package telemetry collects and uploads bounded, content-free CLI counters.
package telemetry

import (
	"os"
	"regexp"
	"strings"
)

var releaseVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z._-]+)?$`)

// Retired is the project's end of service: no counter is queued and nothing is
// uploaded whatever the configuration says. Tests of the reporting machinery
// lift it; the shipped value is true.
var Retired = true

// Enabled applies the fail-closed CLI telemetry policy. Explicit "on" permits
// local headless runs, but never overrides CI, development builds, or
// environment opt-outs.
func Enabled(mode, version string, interactive bool) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if Retired || mode == "off" || !releaseVersionPattern.MatchString(strings.TrimSpace(version)) {
		return false
	}
	if envOptOut() || inCI() {
		return false
	}
	if mode == "on" {
		return true
	}
	return interactive
}

// OptedOut reports the environment-level refusals every anonymous report
// honours whatever the configuration says: DO_NOT_TRACK, REASONIX_TELEMETRY=off
// and CI.
func OptedOut() bool { return Retired || envOptOut() || inCI() }

// ReleaseVersion reports whether version names a release build; development
// builds report nothing.
func ReleaseVersion(version string) bool {
	return releaseVersionPattern.MatchString(strings.TrimSpace(version))
}

func envOptOut() bool {
	if strings.TrimSpace(os.Getenv("DO_NOT_TRACK")) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REASONIX_TELEMETRY"))) {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

func inCI() bool {
	for _, key := range []string{
		"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "GITLAB_CI",
		"BUILDKITE", "CIRCLECI", "JENKINS_URL", "TEAMCITY_VERSION", "TF_BUILD",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}
