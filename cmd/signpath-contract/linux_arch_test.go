package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLinuxReleaseBuildsAndCollectsBothNativeArchitectures(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/release-studio.yml")
	jobs := jobsOf(t, root)
	build := mappingValue(jobs, "build")
	entries := mappingValue(mappingValue(mappingValue(build, "strategy"), "matrix"), "include")
	want := map[string]string{"amd64": "ubuntu-22.04", "arm64": "ubuntu-22.04-arm"}
	seen := map[string]bool{}
	for _, entry := range entries.Content {
		platform := mappingScalar(entry, "platform")
		if !strings.HasPrefix(platform, "linux/") {
			continue
		}
		arch := strings.TrimPrefix(platform, "linux/")
		if seen[arch] || mappingScalar(entry, "runner") != want[arch] {
			t.Fatalf("Linux %s does not have one native runner", arch)
		}
		seen[arch] = true
		artifact := mappingScalar(entry, "artifact")
		if artifact != "studio-build-linux-"+arch {
			t.Fatalf("unexpected Linux artifact %q", artifact)
		}
		downloaded := false
		for _, step := range mappingValue(mappingValue(jobs, "publish"), "steps").Content {
			if mappingScalar(mappingValue(step, "with"), "name") == artifact {
				downloaded = true
			}
		}
		if !downloaded {
			t.Errorf("publish never downloads %s", artifact)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("Linux release architectures = %v, want amd64 and arm64", seen)
	}
	step := stepNamed(t, build, "Use a native fpm on ARM64 Linux")
	if mappingScalar(step, "if") != "runner.os == 'Linux' && runner.arch == 'ARM64'" || !strings.Contains(mappingScalar(step, "run"), "USE_SYSTEM_FPM=true") {
		t.Fatal("Linux ARM64 must retain the upstream native FPM setup")
	}
	for _, name := range []string{"Verify both Linux package formats", "Accept the installed Linux package"} {
		step := stepNamed(t, build, name)
		if mappingScalar(step, "if") != "runner.os == 'Linux'" || mappingScalar(step, "continue-on-error") == "true" {
			t.Errorf("%s must gate every Linux build", name)
		}
	}
	collect := mappingScalar(stepNamed(t, mappingValue(jobs, "publish"), "Collect artifacts"), "run")
	if !strings.Contains(collect, "bash scripts/verify-studio-artifacts.sh dist") {
		t.Fatal("publication does not require the complete platform artifact set")
	}
}

func TestLinuxCIPackageMatrixPreservesUpstreamScope(t *testing.T) {
	root := parseWorkflowFile(t, "../../.github/workflows/studio.yml")
	job := mappingValue(jobsOf(t, root), "scope")
	run := ""
	for _, step := range mappingValue(job, "steps").Content {
		if mappingScalar(step, "id") == "pick" {
			run = mappingScalar(step, "run")
		}
	}
	if run == "" {
		t.Fatal("no executable CI matrix selector")
	}
	for _, tc := range []struct {
		event, file string
		arm64       bool
	}{
		{"push", "docs/test.md", true},
		{"pull_request", "docs/test.md", false},
		{"pull_request", "scripts/linux-install-smoke.sh", true},
		{"pull_request", "scripts/verify-linux-package.sh", true},
		{"pull_request", "scripts/verify-studio-artifacts.sh", true},
		{"pull_request", "desktop/cmd/update-helper/main_linux.go", true},
		{"pull_request", "desktop/electron/test/unit.mjs", true},
	} {
		t.Run(tc.event+"/"+tc.file, func(t *testing.T) {
			dir := testenv.TempDir(t)
			// A shell function owns command lookup on every host, including Git
			// Bash on Windows; a PATH shim can resolve the runner's real git.
			git := "git() { case \"$*\" in 'diff -z --name-only --no-renames fixture HEAD') printf '%s\\0' \"$MATRIX_TEST_FILE\" ;; 'diff --name-only --no-renames fixture HEAD') printf '%s\\n' \"$MATRIX_TEST_FILE\" ;; *) return 1 ;; esac; };\n"
			output := filepath.Join(dir, "output")
			cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+git+run)
			cmd.Dir = "../.."
			cmd.Env = append(os.Environ(), "EVENT="+tc.event, "MATRIX_TEST_FILE="+tc.file, "BASE_SHA=fixture", "GITHUB_OUTPUT="+output)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("matrix selector: %v: %s", err, out)
			}
			body, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var matrix struct {
				Include []map[string]string `json:"include"`
			}
			for line := range strings.SplitSeq(string(body), "\n") {
				if value, ok := strings.CutPrefix(line, "matrix="); ok {
					if err := json.Unmarshal([]byte(value), &matrix); err != nil {
						t.Fatal(err)
					}
				}
			}
			seen := map[string]string{}
			for _, leg := range matrix.Include {
				if leg["platform"] == "linux" {
					seen[leg["arch"]] = leg["os"]
				}
			}
			if seen["amd64"] != "ubuntu-22.04" || (seen["arm64"] == "ubuntu-22.04-arm") != tc.arm64 {
				t.Fatalf("Linux CI matrix = %v; selector output: %s", seen, out)
			}
		})
	}
}
