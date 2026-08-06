package telemetry

import "testing"

func clearPolicyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DO_NOT_TRACK", "REASONIX_TELEMETRY", "CI", "CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "JENKINS_URL",
		"TEAMCITY_VERSION", "TF_BUILD",
	} {
		t.Setenv(key, "")
	}
}

func TestEnabledPolicy(t *testing.T) {
	clearPolicyEnv(t)
	if !Enabled("auto", "v1.20.0", true) {
		t.Fatal("auto should enable a release build on an interactive terminal")
	}
	if Enabled("auto", "v1.20.0", false) {
		t.Fatal("auto should disable headless execution")
	}
	if !Enabled("on", "v1.20.0", false) {
		t.Fatal("on should permit a local headless release run")
	}
	for _, tc := range []struct {
		name    string
		mode    string
		version string
	}{
		{name: "off", mode: "off", version: "v1.20.0"},
		{name: "development", mode: "on", version: "dev"},
		{name: "test version", mode: "on", version: "test-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Enabled(tc.mode, tc.version, true) {
				t.Fatal("policy unexpectedly enabled telemetry")
			}
		})
	}
}

func TestEnabledHonorsEnvironmentOptOuts(t *testing.T) {
	clearPolicyEnv(t)
	for _, tc := range []struct{ key, value string }{
		{key: "DO_NOT_TRACK", value: "1"},
		{key: "REASONIX_TELEMETRY", value: "0"},
		{key: "CI", value: "true"},
		{key: "GITHUB_ACTIONS", value: "1"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			clearPolicyEnv(t)
			t.Setenv(tc.key, tc.value)
			if Enabled("on", "v1.20.0", true) {
				t.Fatal("environment opt-out was ignored")
			}
		})
	}
}
