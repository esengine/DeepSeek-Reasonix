package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A launcher's declared environment must not reach the pre-approval ls-remote:
// GIT_* variables there can name a program even under an https or ssh URL.
func TestLocatorSpecEnvDoesNotReachLsRemote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX payload")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, tc := range []struct {
		name, locator string
		env           map[string]string
	}{
		{"GIT_SSH_COMMAND", "git+ssh://example.invalid/x.git@main", map[string]string{"GIT_SSH_COMMAND": "%P"}},
		{"GIT_CONFIG_COUNT insteadOf ext", "git+https://example.invalid/x.git@main", map[string]string{
			"GIT_CONFIG_COUNT": "2", "GIT_CONFIG_KEY_0": "url.ext::%P .insteadOf", "GIT_CONFIG_VALUE_0": "https://",
			"GIT_CONFIG_KEY_1": "protocol.ext.allow", "GIT_CONFIG_VALUE_1": "always"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "executed")
			payload := filepath.Join(dir, "p.sh")
			if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{}
			for k, v := range tc.env {
				if v == "%P" {
					v = payload
				}
				if k == "GIT_CONFIG_KEY_0" {
					v = "url.ext::" + payload + " .insteadOf"
				}
				env[k] = v
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _, err := resolveGitLocator(ctx, Spec{Name: "probe", Env: env}, tc.locator)
			t.Logf("err: %v", err)
			if _, statErr := os.Stat(marker); statErr == nil {
				t.Fatal("ls-remote ran a program named by the spec env")
			}
		})
	}
}
