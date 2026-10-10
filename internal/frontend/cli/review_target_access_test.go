package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// Run the command rather than supplying a policy directly in a test Options.
// Reader-level confinement alone must not hide missing admission wiring.
func TestReviewCommandTargetAccessUsesUserPolicy(t *testing.T) {
	for _, forbidden := range []bool{true, false} {
		name := "allowed"
		if forbidden {
			name = "forbidden"
		}
		t.Run(name, func(t *testing.T) {
			isolateCLIConfigHome(t)
			dir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			reviewHookRegister.Do(func() {
				provider.Register(reviewHookProviderKind, func(provider.Config) (provider.Provider, error) {
					reviewHookMu.Lock()
					defer reviewHookMu.Unlock()
					return reviewHookCurrent, nil
				})
			})
			probe := &reviewHookProvider{}
			reviewHookMu.Lock()
			reviewHookCurrent = probe
			reviewHookMu.Unlock()
			t.Cleanup(func() {
				reviewHookMu.Lock()
				reviewHookCurrent = nil
				reviewHookMu.Unlock()
			})
			userConfig := "[sandbox]\nforbid_read = []\n"
			if forbidden {
				userConfig = "[sandbox]\nforbid_read = [\"secret.txt\"]\n"
			}
			writeTestFile(t, config.UserConfigPath(), `default_model = "reviewer"
[[providers]]
name = "reviewer"
kind = "`+reviewHookProviderKind+`"
model = "x"
base_url = "http://127.0.0.1:1"
`+userConfig, 0o600)
			// A checkout must neither weaken nor expand the user's review policy.
			projectPolicy := "[sandbox]\nforbid_read = [\"secret.txt\"]\n"
			if forbidden {
				projectPolicy = "[sandbox]\nforbid_read = []\n"
			}
			writeTestFile(t, filepath.Join(dir, "reasonix.toml"), projectPolicy, 0o600)
			approveWorkspace(t, dir)
			writeTestFile(t, filepath.Join(dir, "secret.txt"), reviewHookSecret+"\n", 0o600)
			writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n", 0o600)
			for _, args := range [][]string{
				{"init", "-q"},
				{"add", "main.go"},
				{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init"},
			} {
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			writeTestFile(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n", 0o600)
			var rc int
			captureStdout(t, func() { rc = reviewCommand(nil) })
			if rc != 0 {
				t.Fatalf("review command failed: %d", rc)
			}
			probe.mu.Lock()
			defer probe.mu.Unlock()
			var results []string
			for _, req := range probe.reqs {
				for _, msg := range req.Messages {
					if msg.Role == provider.RoleTool && msg.ToolCallID == "review-read" {
						results = append(results, msg.Content)
					}
					if forbidden && strings.Contains(msg.Content, reviewHookSecret) {
						t.Fatal("review model received forbidden file contents")
					}
				}
			}
			if len(results) != 1 {
				t.Fatalf("expected one completed read, got %q", results)
			}
			if forbidden {
				if results[0] != "Cannot read target file: permission denied." {
					t.Fatalf("review skipped unified target admission: %q", results[0])
				}
			} else if !strings.Contains(results[0], reviewHookSecret) {
				t.Fatalf("user-allowed read was blocked by checkout policy: %q", results[0])
			}
		})
	}
}
