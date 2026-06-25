package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

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
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestWindowsWebview2GPUDisabled(t *testing.T) {
	oldChannel := channel
	t.Cleanup(func() {
		channel = oldChannel
		os.Unsetenv(disableWebview2GPUEnv)
	})

	tests := []struct {
		name    string
		channel string
		env     string
		want    bool
	}{
		{name: "stable default keeps gpu", channel: "stable", want: false},
		{name: "canary default disables gpu", channel: "canary", want: true},
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
