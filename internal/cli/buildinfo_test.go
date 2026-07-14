package cli

import (
	"strings"
	"testing"
)

func TestBuildInfoVersionTextIncludesLocalBuildIdentity(t *testing.T) {
	isolateCLIConfigHome(t)
	out := BuildInfo{
		Version:      "desktop-v1.17.3-4-gabcdef0-dirty",
		BuildNumber:  "20260706091213",
		BuildTimeUTC: "2026-07-06T09:12:13Z",
		GitCommit:    "abcdef0",
		GitDirty:     "dirty",
		BuildProfile: "release",
		BuildTarget:  "aarch64-apple-darwin",
	}.VersionText()

	for _, want := range []string{
		"reasonix desktop-v1.17.3-4-gabcdef0-dirty",
		"build_number: 20260706091213",
		"build_time_utc: 2026-07-06T09:12:13Z",
		"build_time_cst: 2026-07-06 17:12:13 CST",
		"git_commit: abcdef0",
		"git_dirty: dirty",
		"build_profile: release",
		"build_target: aarch64-apple-darwin",
		"go: go version ",
		"config_path: ",
		"config_mode: missing",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("VersionText missing %q:\n%s", want, out)
		}
	}
}

func TestBuildInfoVersionTextKeepsVersionFirstLine(t *testing.T) {
	if got := (BuildInfo{Version: "test-version"}).VersionText(); !strings.HasPrefix(got, "reasonix test-version") {
		t.Fatalf("VersionText = %q", got)
	}
}

func TestBuildInfoVersionTextShowsFallbackIdentityForPlainGoBuild(t *testing.T) {
	isolateCLIConfigHome(t)
	out := (BuildInfo{Version: "dev"}).VersionText()

	for _, want := range []string{
		"reasonix dev",
		"build_number: ",
		"build_time_utc: ",
		"git_commit: ",
		"git_dirty: ",
		"build_profile: debug",
		"build_target: ",
		"go: go version ",
		"config_path: ",
		"config_mode: missing",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("VersionText missing %q:\n%s", want, out)
		}
	}
}

func TestBuildNumberFromUTC(t *testing.T) {
	if got := buildNumberFromUTC("2026-07-08T04:05:06Z"); got != "20260708040506" {
		t.Fatalf("buildNumberFromUTC = %q", got)
	}
}
