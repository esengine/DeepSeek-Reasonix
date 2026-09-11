package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildInfoSingleLine(t *testing.T) {
	info := BuildInfo{Version: "v1.2.3"}
	if got, want := info.singleLine(), "reasonix v1.2.3"; got != want {
		t.Fatalf("singleLine = %q, want %q", got, want)
	}
}

func TestBuildInfoVerboseOmitsConfigAndCST(t *testing.T) {
	info := BuildInfo{
		Version:      "v9.9.9",
		GitCommit:    "abcdef0123456789",
		BuildTimeUTC: "2026-08-06T12:00:00Z",
		BuildTarget:  "darwin/arm64",
	}
	text := info.verboseText()
	if !strings.Contains(text, "reasonix v9.9.9") {
		t.Fatalf("verbose missing version line:\n%s", text)
	}
	if !strings.Contains(text, "git_commit: abcdef012345") {
		t.Fatalf("verbose missing short commit:\n%s", text)
	}
	if !strings.Contains(text, "build_time_utc: 2026-08-06T12:00:00Z") {
		t.Fatalf("verbose missing utc time:\n%s", text)
	}
	if !strings.Contains(text, "build_target: darwin/arm64") {
		t.Fatalf("verbose missing target:\n%s", text)
	}
	if strings.Contains(text, "config_path") || strings.Contains(text, "build_time_cst") || strings.Contains(text, "CST") {
		t.Fatalf("verbose must not include config path or CST:\n%s", text)
	}
}

func TestBuildInfoJSONShape(t *testing.T) {
	info := BuildInfo{
		Version:      "v1.0.0",
		GitCommit:    "deadbeefcafebabe",
		BuildTimeUTC: "2026-01-02T03:04:05Z",
		BuildTarget:  "linux/amd64",
	}
	raw, err := json.Marshal(info.jsonPayload())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "git_commit", "build_time_utc", "build_target", "go"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("json missing %q: %s", key, raw)
		}
	}
	for _, key := range []string{"config_path", "build_time_cst", "build_number"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("json must not include %q: %s", key, raw)
		}
	}
}

func TestVersionCommandFlags(t *testing.T) {
	info := BuildInfo{Version: "v2.0.0", GitCommit: "abc", BuildTimeUTC: "2026-08-06T00:00:00Z", BuildTarget: "test/arch"}
	out := captureStdout(t, func() {
		if code := versionCommand(nil, info, false); code != 0 {
			t.Fatalf("code = %d", code)
		}
	})
	if strings.TrimSpace(out) != "reasonix v2.0.0" {
		t.Fatalf("--version line = %q", out)
	}

	out = captureStdout(t, func() {
		if code := versionCommand([]string{"--verbose"}, info, true); code != 0 {
			t.Fatalf("verbose code = %d", code)
		}
	})
	if !strings.Contains(out, "git_commit:") {
		t.Fatalf("verbose output = %q", out)
	}

	out = captureStdout(t, func() {
		if code := versionCommand([]string{"--json"}, info, true); code != 0 {
			t.Fatalf("json code = %d", code)
		}
	})
	if !strings.Contains(out, `"version": "v2.0.0"`) && !strings.Contains(out, `"version":"v2.0.0"`) {
		t.Fatalf("json output = %q", out)
	}
}

func TestBuildInfoDoesNotUseVCSTimeAsBuildTime(t *testing.T) {
	// Empty BuildTimeUTC must stay "unknown" rather than silently adopting
	// vcs.time (commit timestamp) from the Go build info.
	info := BuildInfo{Version: "vdev", GitCommit: "deadbeef"}.withDefaults()
	if info.BuildTimeUTC != "unknown" {
		t.Fatalf("BuildTimeUTC without ldflags = %q, want unknown", info.BuildTimeUTC)
	}
}

func TestReleaseStyleLdflagsAppearInVerbose(t *testing.T) {
	// Simulates official release injection of commit + real UTC build time.
	info := BuildInfo{
		Version:      "vtest",
		GitCommit:    "cafebabef00d",
		BuildTimeUTC: "2026-08-06T12:34:56Z",
		BuildTarget:  "linux/amd64",
	}
	text := info.verboseText()
	if !strings.Contains(text, "git_commit: cafebabef00d") {
		t.Fatalf("missing injected commit:\n%s", text)
	}
	if !strings.Contains(text, "build_time_utc: 2026-08-06T12:34:56Z") {
		t.Fatalf("missing injected build time:\n%s", text)
	}
	if strings.Contains(text, "unknown") {
		t.Fatalf("injected identity still unknown:\n%s", text)
	}
}
