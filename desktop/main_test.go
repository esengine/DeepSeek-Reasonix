package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

func TestParseDesktopLaunchArgsStripsLegacySafeMode(t *testing.T) {
	got := parseDesktopLaunchArgs([]string{"launch", "--detach", "--safe-mode", "--other"})
	if !got.LegacySafeModeArg {
		t.Fatal("--safe-mode should still be recognized for stripping")
	}
	if parseDesktopLaunchArgs([]string{"--other"}).LegacySafeModeArg {
		t.Fatal("unrelated argument must not set legacy safe-mode flag")
	}
}

func TestParseDesktopLaunchArgsRemoteWindow(t *testing.T) {
	got := parseDesktopLaunchArgs([]string{
		"--other",
		remoteWindowTicketArgPrefix + ".remote-window-123",
		remoteWindowHostArgPrefix + "abcd1234",
		remoteWindowOwnerArgPrefix + "0123456789abcdef0123456789abcdef",
		remoteWindowParentArgPrefix + "4242",
	})
	if got.RemoteWindowTicket != ".remote-window-123" {
		t.Fatalf("RemoteWindowTicket = %q", got.RemoteWindowTicket)
	}
	if got.RemoteWindowHostKey != "abcd1234" {
		t.Fatalf("RemoteWindowHostKey = %q", got.RemoteWindowHostKey)
	}
	if got.RemoteWindowOwnerID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("RemoteWindowOwnerID = %q", got.RemoteWindowOwnerID)
	}
	if got.RemoteWindowParentPID != 4242 {
		t.Fatalf("RemoteWindowParentPID = %d", got.RemoteWindowParentPID)
	}
	if got.LegacySafeModeArg {
		t.Fatal("remote window args unexpectedly enabled legacy safe mode")
	}
}

func TestLifecycleDiagnosticsUsePreWailsOwnershipGate(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	beforeRun, _, ok := strings.Cut(string(mainSource), "err := wails.Run")
	if !ok {
		t.Fatal("main.go no longer contains the Wails run boundary")
	}
	if !strings.Contains(beforeRun, "prepareDesktopDiagnostics(app)") {
		t.Fatal("main process must claim diagnostics ownership before Wails starts")
	}

	appSource, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	_, afterStartup, ok := strings.Cut(string(appSource), "func (a *App) startup(ctx context.Context) {")
	if !ok {
		t.Fatal("app.go no longer contains App.startup")
	}
	startupBody, _, ok := strings.Cut(afterStartup, "\n}")
	if !ok || !strings.Contains(startupBody, "initializeLifecycleDiagnostics(a)") {
		t.Fatal("previous lifecycle consumption must remain owned by Wails OnStartup")
	}
}

// TestMain isolates user config/state/cache dirs for the whole package. Without
// this, tests that persist desktop state, sessions, cache, or CLI-style config
// can leak into the developer's real Reasonix directories.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reasonix-desktop-test")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv("HOME", dir)
	os.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	os.Setenv("USERPROFILE", dir)
	os.Setenv("XDG_CONFIG_HOME", dir+"/config")
	os.Setenv("REASONIX_STATE_HOME", dir+"/state")
	os.Setenv("REASONIX_CACHE_HOME", dir+"/cache")
	os.Setenv("AppData", dir)
	// Tests fail closed for telemetry. Any test that expects a request must
	// replace the relevant endpoint with an httptest.Server explicitly.
	crashEndpoint = "http://127.0.0.1:0/v1/report"
	pingEndpoint = "http://127.0.0.1:0/v1/ping"
	metricsEndpoint = "http://127.0.0.1:0/v1/metrics"
	// Neutralize the Wails runtime-event bridge for the whole test binary:
	// outside a running Wails app, runtime.EventsEmit log.Fatals on the plain
	// contexts tests use, killing the process from any emitting code path.
	// Tests that assert on runtime events install their own capture through
	// the per-instance runtimeEvents.emit hook, which takes precedence.
	runtimeEventsEmitFallback = func(context.Context, string, ...any) {}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestDesktopTestTelemetryEndpointsAreFailClosed(t *testing.T) {
	for name, endpoint := range map[string]string{
		"crash":   crashEndpoint,
		"ping":    pingEndpoint,
		"metrics": metricsEndpoint,
	} {
		if strings.Contains(endpoint, "crash.reasonix.io") {
			t.Fatalf("%s test endpoint targets production: %s", name, endpoint)
		}
	}
}

func TestWindowsWebview2GPUDisabled(t *testing.T) {
	oldChannel := channel
	t.Cleanup(func() {
		channel = oldChannel
		os.Unsetenv(disableWebview2GPUEnv)
		os.Unsetenv(legacyDisableWebview2GPUEnv)
	})

	tests := []struct {
		name    string
		channel string
		env     string
		want    bool
	}{
		{name: "stable default keeps gpu", channel: "stable", want: false},
		{name: "preview default disables gpu", channel: "preview", want: true},
		{name: "legacy canary default disables gpu", channel: "canary", want: true},
		{name: "env enables fallback", channel: "stable", env: "1", want: true},
		{name: "env disables canary fallback", channel: "canary", env: "0", want: false},
		{name: "truthy env", channel: "stable", env: "yes", want: true},
		{name: "falsey env", channel: "canary", env: "off", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel = tt.channel
			if tt.env == "" {
				os.Unsetenv(disableWebview2GPUEnv)
			} else {
				os.Setenv(disableWebview2GPUEnv, tt.env)
			}
			if got := windowsWebview2GPUDisabled(); got != tt.want {
				t.Fatalf("windowsWebview2GPUDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
	os.Unsetenv(disableWebview2GPUEnv)
	os.Setenv(legacyDisableWebview2GPUEnv, "1")
	if !windowsWebview2GPUDisabled() {
		t.Fatal("legacy WebView2 GPU override was not accepted")
	}
}

func TestLinuxWebviewGpuPolicyDisablesGpuWithoutAccessibleRenderNode(t *testing.T) {
	glob := filepath.Join(t.TempDir(), "renderD*")

	if got := linuxWebviewGpuPolicy(glob); got != linux.WebviewGpuPolicyNever {
		t.Fatalf("linuxWebviewGpuPolicy() = %v, want %v", got, linux.WebviewGpuPolicyNever)
	}
}

func TestLinuxWebviewGpuPolicyDisablesGpuForInaccessibleRenderNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "renderD128"), 0o700); err != nil {
		t.Fatal(err)
	}

	if got := linuxWebviewGpuPolicy(filepath.Join(dir, "renderD*")); got != linux.WebviewGpuPolicyNever {
		t.Fatalf("linuxWebviewGpuPolicy() = %v, want %v", got, linux.WebviewGpuPolicyNever)
	}
}

func TestLinuxWebviewGpuPolicyKeepsOnDemandWithAccessibleRenderNode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "renderD128"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := linuxWebviewGpuPolicy(filepath.Join(dir, "renderD*")); got != linux.WebviewGpuPolicyOnDemand {
		t.Fatalf("linuxWebviewGpuPolicy() = %v, want %v", got, linux.WebviewGpuPolicyOnDemand)
	}
}
