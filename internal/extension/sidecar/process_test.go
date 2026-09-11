package sidecar

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/pluginpkg"
)

func TestResolveRuntimeCommandContract(t *testing.T) {
	root := t.TempDir()
	shellPath := "/bin/sh"
	if runtime.GOOS == "windows" {
		shellPath = `C:\Windows\System32\cmd.exe`
	}
	cases := []struct {
		name    string
		command string
		wantErr string
	}{
		{name: "empty", command: "   ", wantErr: "empty"},
		{name: "relative bare name", command: "node", wantErr: "not an absolute path"},
		{name: "relative path", command: "bin/sidecar", wantErr: "not an absolute path"},
		{name: "shell indirection", command: shellPath, wantErr: "exec form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveRuntimeCommand(&pluginpkg.RuntimeSpec{Command: tc.command}, root)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("resolveRuntimeCommand(%q) error = %v, want one containing %q", tc.command, err, tc.wantErr)
			}
		})
	}
}

func TestResolveRuntimeCommandExpandsPluginRoot(t *testing.T) {
	root := t.TempDir()
	got, err := resolveRuntimeCommand(&pluginpkg.RuntimeSpec{Command: "${REASONIX_PLUGIN_ROOT}/bin/sidecar"}, root)
	if err != nil {
		t.Fatalf("resolveRuntimeCommand: %v", err)
	}
	if !strings.HasPrefix(got, root) || !strings.HasSuffix(got, "sidecar") {
		t.Fatalf("expanded command = %q, want inside %q", got, root)
	}
}

// TestStartupFailureRedactsAndBoundsStderr floods stderr and fails the
// handshake: the surfaced diagnostics must carry a bounded, redacted tail.
func TestStartupFailureRedactsAndBoundsStderr(t *testing.T) {
	pkg, installed := fakeSidecarPackage(t, "fakeplugin", func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "stderr_flood"
		// Wrong major forces handshake failure after stderr flood.
		rt.Env[fakeEnvInitResult] = `{"protocolVersion":"1","name":"fake","version":"1","stateSchemaVersion":0}`
	})
	_, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
	if err == nil {
		t.Fatal("StartClient succeeded despite protocol mismatch")
	}
	var failure *startupFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error %T is not a startupFailure", err)
	}
	if failure.Stage != "handshake" {
		t.Fatalf("stage = %q, want handshake", failure.Stage)
	}
	if len(failure.Stderr) > stderrTailBytes {
		t.Fatalf("stderr tail is %d bytes, want <= %d", len(failure.Stderr), stderrTailBytes)
	}
	if strings.Contains(failure.Stderr, "sk-abcdef1234567890SECRETKEY") {
		t.Fatalf("stderr tail leaks the credential: %q", failure.Stderr)
	}
	if !strings.Contains(failure.Stderr, "***") {
		t.Fatalf("stderr tail shows no redaction mask: %q", failure.Stderr)
	}
}

func TestStartupFailureRedactsCauseWithoutLosingIdentity(t *testing.T) {
	const secret = "sk-abcdef1234567890SECRETKEY"
	cause := errors.New("initialize rejected api_key=" + secret)
	err := newStartupFailure("handshake", time.Now(), "", cause)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("startup failure leaked its cause: %q", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Fatalf("startup failure contains no redaction marker: %q", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("startup failure no longer unwraps to its original cause")
	}
}

// TestRuntimeEnvFullTrustContract pins the documented contract: the sidecar
// inherits the unfiltered environment, manifest env layers over it, and the
// plugin identity variables are always set.
func TestRuntimeEnvFullTrustContract(t *testing.T) {
	t.Setenv("REASONIX_TEST_INHERITED_MARKER", "present")
	root := t.TempDir()
	rt := &pluginpkg.RuntimeSpec{Command: "/bin/sidecar", Env: map[string]string{"MANIFEST_KEY": "manifest-value"}}
	pkg := pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Name: "p", Version: "2.0.0", Runtime: rt}}
	installed := pluginpkg.InstalledPlugin{Name: "p", Version: "1.0.0"}

	env := runtimeEnv(rt, pkg, installed)
	values := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["REASONIX_TEST_INHERITED_MARKER"] != "present" {
		t.Fatal("inherited environment was filtered")
	}
	if values["MANIFEST_KEY"] != "manifest-value" {
		t.Fatal("manifest env missing")
	}
	if values[envPluginRoot] != root || values[envPluginName] != "p" || values[envPluginVersion] != "1.0.0" {
		t.Fatalf("plugin identity env = %q %q %q", values[envPluginRoot], values[envPluginName], values[envPluginVersion])
	}
}
