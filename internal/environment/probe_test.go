package environment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/secrets"
)

func TestFormatSectionSortsAndRedacts(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	section := FormatSection([]ProbeResult{
		{Binary: "python3", Found: true, Output: "Python 3.12.0"},
		{Binary: "go", Found: true, Output: "go version go1.24 darwin/arm64"},
		{Binary: "docker", Error: "not found"},
	}, "darwin/arm64", filepath.Join(home, "bin", "bash"), map[string]string{
		"python3": filepath.Join(home, ".pyenv", "shims", "python3"),
		"go":      "/opt/homebrew/bin/go",
	})

	for _, want := range []string{
		"## Environment",
		"- OS: darwin/arm64",
		"- Shell: ~/bin/bash",
		"Configured tools:\n- go: /opt/homebrew/bin/go\n- python3: ~/.pyenv/shims/python3",
		"Detected tools:\n- go: go version go1.24 darwin/arm64\n- python3: Python 3.12.0",
		"Not found or unavailable:\n- docker: not found",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("section missing %q:\n%s", want, section)
		}
	}
}

func TestRunProbesReportsMissingCommand(t *testing.T) {
	results := RunProbes(context.Background(), []string{"__reasonix_missing_probe__ --version"})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Found {
		t.Fatalf("missing command marked found: %+v", results[0])
	}
	if results[0].Error != "not found" {
		t.Fatalf("Error = %q, want not found", results[0].Error)
	}
}

func TestRunProbesUsesOverridePathAndFirstLine(t *testing.T) {
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "mytool")
	toolPath = writeProbeTool(t, toolPath, "custom version\nignored")

	results := RunProbesWithOverrides(context.Background(), []string{"mytool --version"}, map[string]string{"mytool": toolPath})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if !results[0].Found {
		t.Fatalf("override command not found: %+v", results[0])
	}
	if results[0].Output != "custom version" {
		t.Fatalf("Output = %q, want first line", results[0].Output)
	}
}

func TestRunProbesParsesQuotedStaticArgs(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(25, 0))
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "quotedtool")
	toolPath = writeProbeTool(t, toolPath, "quoted version")

	results := RunProbesWithOverrides(context.Background(), []string{`quotedtool "--version with spaces"`}, map[string]string{"quotedtool": toolPath})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if !results[0].Found || results[0].Output != "quoted version" {
		t.Fatalf("quoted probe result = %+v", results[0])
	}
}

func TestRunProbesAllowsStaticEnvAssignment(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(35, 0))
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "envtool")
	toolPath = writeEnvProbeTool(t, toolPath)

	results := RunProbesWithOverrides(context.Background(), []string{`REASONIX_PROBE_ENV=ok envtool --version`}, map[string]string{"envtool": toolPath})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if !results[0].Found || results[0].Output != "ok" {
		t.Fatalf("env probe result = %+v", results[0])
	}
}

func TestRunProbesAllowsStaticStderrMerge(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(40, 0))
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "stderrtool")
	toolPath = writeStderrProbeTool(t, toolPath, "stderr version")

	results := RunProbesWithOverrides(context.Background(), []string{`stderrtool --version 2>&1`}, map[string]string{"stderrtool": toolPath})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if !results[0].Found || results[0].Output != "stderr version" {
		t.Fatalf("stderr merge probe result = %+v", results[0])
	}
}

func TestRunProbesRejectsDeniedOverridePath(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(50, 0))
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "deniedtool")
	toolPath = writeProbeTool(t, toolPath, "should not run")

	results := RunProbesWithOptions(context.Background(), []string{"deniedtool --version"}, ProbeOptions{
		Overrides: map[string]string{"deniedtool": toolPath},
		DenyRoots: []string{dir},
	})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Found || results[0].Error != "not trusted" {
		t.Fatalf("denied override result = %+v, want not trusted", results[0])
	}
}

func TestRunProbesRejectsDeniedPathHit(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(75, 0))
	dir := t.TempDir()
	writeProbeTool(t, filepath.Join(dir, "pathtool"), "should not run")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	results := RunProbesWithOptions(context.Background(), []string{"pathtool --version"}, ProbeOptions{
		DenyRoots: []string{dir},
	})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Found || results[0].Error != "not trusted" {
		t.Fatalf("denied PATH result = %+v, want not trusted", results[0])
	}
}

func TestRunProbesReportsTimeout(t *testing.T) {
	setProbeTimeoutForTest(t, 200*time.Millisecond)
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "slowtool")
	body := "#!/bin/sh\nsleep 3\n"
	if runtime.GOOS == "windows" {
		toolPath += ".bat"
		body = "@ping 127.0.0.1 -n 4 > nul\r\n"
	}
	if err := os.WriteFile(toolPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	results := RunProbesWithOverrides(context.Background(), []string{"slowtool --version"}, map[string]string{"slowtool": toolPath})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Found {
		t.Fatalf("timeout command marked found: %+v", results[0])
	}
	if results[0].Error != "timeout" {
		t.Fatalf("Error = %q, want timeout", results[0].Error)
	}
}

func TestPrepareProbeCommandSetsCancellationBudget(t *testing.T) {
	cmd := exec.Command("reasonix-test-probe")
	prepareProbeCommand(cmd)
	if cmd.Cancel == nil {
		t.Fatal("probe command must install a cancellation hook")
	}
	if cmd.WaitDelay != probeWaitDelay {
		t.Fatalf("WaitDelay = %v, want %v", cmd.WaitDelay, probeWaitDelay)
	}
	if runtime.GOOS == "windows" && cmd.SysProcAttr == nil {
		t.Fatal("probe command must hide console windows on Windows")
	}
}

func TestRunProbesCachesByFingerprint(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(100, 0))
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "cachedtool")
	toolPath = writeProbeTool(t, toolPath, "version one")

	results := RunProbesWithOverrides(context.Background(), []string{"cachedtool --version"}, map[string]string{"cachedtool": toolPath})
	if got := results[0].Output; got != "version one" {
		t.Fatalf("first Output = %q, want version one", got)
	}
	results[0].Output = "mutated"
	toolPath = writeProbeTool(t, toolPath, "version two")

	results = RunProbesWithOverrides(context.Background(), []string{"cachedtool --version"}, map[string]string{"cachedtool": toolPath})
	if got := results[0].Output; got != "version one" {
		t.Fatalf("cached Output = %q, want version one", got)
	}
}

func TestRunProbesCacheExpires(t *testing.T) {
	now := time.Unix(200, 0)
	resetProbeCacheForTest(t, now)
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "expiringtool")
	toolPath = writeProbeTool(t, toolPath, "version one")

	results := RunProbesWithOverrides(context.Background(), []string{"expiringtool --version"}, map[string]string{"expiringtool": toolPath})
	if got := results[0].Output; got != "version one" {
		t.Fatalf("first Output = %q, want version one", got)
	}
	toolPath = writeProbeTool(t, toolPath, "version two")
	setProbeNowForTest(now.Add(probeCacheTTL + time.Second))

	results = RunProbesWithOverrides(context.Background(), []string{"expiringtool --version"}, map[string]string{"expiringtool": toolPath})
	if got := results[0].Output; got != "version two" {
		t.Fatalf("expired Output = %q, want version two", got)
	}
}

func TestRunProbesCacheSeparatesOverrides(t *testing.T) {
	resetProbeCacheForTest(t, time.Unix(300, 0))
	dir := t.TempDir()
	toolOne := filepath.Join(dir, "override-one")
	toolTwo := filepath.Join(dir, "override-two")
	toolOne = writeProbeTool(t, toolOne, "version one")
	toolTwo = writeProbeTool(t, toolTwo, "version two")

	results := RunProbesWithOverrides(context.Background(), []string{"overridetool --version"}, map[string]string{"overridetool": toolOne})
	if got := results[0].Output; got != "version one" {
		t.Fatalf("first override Output = %q, want version one", got)
	}
	results = RunProbesWithOverrides(context.Background(), []string{"overridetool --version"}, map[string]string{"overridetool": toolTwo})
	if got := results[0].Output; got != "version two" {
		t.Fatalf("second override Output = %q, want version two", got)
	}
}

func TestFormatSectionLimitsToolOutput(t *testing.T) {
	overrides := map[string]string{}
	var results []ProbeResult
	for i := 0; i < maxRenderedTools+2; i++ {
		name := fmt.Sprintf("tool%02d", i)
		overrides[name] = "/bin/" + name
		results = append(results, ProbeResult{Binary: name, Found: true, Output: "ok"})
		results = append(results, ProbeResult{Binary: "missing" + name, Error: "not found"})
	}

	section := FormatSection(results, "test/os", "", overrides)
	for _, want := range []string{
		"- ... 2 more configured tools omitted",
		"- ... 2 more detected tools omitted",
		"- ... 2 more unavailable tools omitted",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("section missing %q:\n%s", want, section)
		}
	}
}

func writeProbeTool(t *testing.T, path, output string) string {
	t.Helper()
	setProbeTimeoutForTest(t, 10*time.Second)
	body := "#!/bin/sh\nprintf '%s\\n'\n"
	body = fmt.Sprintf(body, strings.ReplaceAll(output, "'", "'\\''"))
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(path, ".bat") {
			path += ".bat"
		}
		body = "@echo " + strings.ReplaceAll(output, "\n", "\r\n@echo ") + "\r\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	return path
}

func writeEnvProbeTool(t *testing.T, path string) string {
	t.Helper()
	setProbeTimeoutForTest(t, 10*time.Second)
	body := "#!/bin/sh\nprintf '%s\\n' \"$REASONIX_PROBE_ENV\"\n"
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(path, ".bat") {
			path += ".bat"
		}
		body = "@echo %REASONIX_PROBE_ENV%\r\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write env tool: %v", err)
	}
	return path
}

func writeStderrProbeTool(t *testing.T, path, output string) string {
	t.Helper()
	setProbeTimeoutForTest(t, 10*time.Second)
	body := "#!/bin/sh\nprintf '%s\\n' >&2\n"
	body = fmt.Sprintf(body, strings.ReplaceAll(output, "'", "'\\''"))
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(path, ".bat") {
			path += ".bat"
		}
		body = "@echo " + strings.ReplaceAll(output, "\n", "\r\n@echo ") + " 1>&2\r\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stderr tool: %v", err)
	}
	return path
}

func resetProbeCacheForTest(t *testing.T, now time.Time) {
	t.Helper()
	setProbeNowForTest(now)
	probeCacheMu.Lock()
	probeCache = map[string]probeCacheEntry{}
	probeInflightCalls = map[string]*probeInflight{}
	probeCacheMu.Unlock()
	t.Cleanup(func() {
		probeCacheMu.Lock()
		probeCache = map[string]probeCacheEntry{}
		probeInflightCalls = map[string]*probeInflight{}
		probeNow = time.Now
		probeTimeout = ProbeTimeout
		probeCacheMu.Unlock()
	})
}

func setProbeNowForTest(now time.Time) {
	probeCacheMu.Lock()
	probeNow = func() time.Time { return now }
	probeCacheMu.Unlock()
}

func setProbeTimeoutForTest(t *testing.T, timeout time.Duration) {
	t.Helper()
	probeCacheMu.Lock()
	probeTimeout = timeout
	probeCacheMu.Unlock()
	t.Cleanup(func() {
		probeCacheMu.Lock()
		probeTimeout = ProbeTimeout
		probeCacheMu.Unlock()
	})
}

func TestRunProbesFilterSubprocessEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe tool")
	}
	resetProbeCacheForTest(t, time.Unix(90, 0))
	setProbeTimeoutForTest(t, 10*time.Second)
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "envtool")
	body := "#!/bin/sh\nprintf 'tok=%s' \"${REASONIX_TEST_SECRET_TOKEN:-none}\"\n"
	if err := os.WriteFile(toolPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_TEST_SECRET_TOKEN", "ghp_abcdefghijklmnopqrstuvwxyz")
	secrets.SetFilterSubprocessEnv(true)
	t.Cleanup(func() { secrets.SetFilterSubprocessEnv(false) })

	results := RunProbesWithOverrides(context.Background(), []string{"envtool --version"}, map[string]string{"envtool": toolPath})
	if len(results) != 1 || !results[0].Found {
		t.Fatalf("probe result = %+v", results)
	}
	// Probes declaring no extra env of their own must still get the filtered
	// environment, not inherit the full one.
	if results[0].Output != "tok=none" {
		t.Fatalf("probe leaked filtered env: output = %q", results[0].Output)
	}
}
