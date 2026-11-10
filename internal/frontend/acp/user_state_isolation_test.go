package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
)

func TestACPInventoryIgnoresCallerSkills(t *testing.T) {
	if os.Getenv("REASONIX_TEST_CALLER_HOME") != "" {
		ctrl := control.New(control.Options{})
		defer ctrl.Close()
		catalog, err := ctrl.AgentPresets()
		if err != nil {
			t.Fatal(err)
		}
		for _, choice := range catalog.Modules["skills"] {
			if choice.ID == "caller-only-skill" {
				t.Fatal("ACP fixture scanned the caller's installed skill")
			}
		}
		return
	}
	home := testenv.TempDir(t)
	dir := filepath.Join(home, ".reasonix", "skills", "caller-only-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: caller-only-skill\ndescription: Caller fixture that must not enter a test inventory.\n---\nfixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestACPInventoryIgnoresCallerSkills$")
	cmd.Env = append(os.Environ(), "REASONIX_TEST_CALLER_HOME="+home, "HOME="+home, "USERPROFILE="+home, "REASONIX_HOME="+filepath.Join(home, ".reasonix"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("caller-state isolation failed: %v\n%s", err, out)
	}
}
