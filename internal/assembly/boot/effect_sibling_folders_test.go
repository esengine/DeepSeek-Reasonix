package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/workspacelease"
)

func siblingProjects(t *testing.T, bashMode string) (alpha, beta, root *workspacelease.Owner, confined bool) {
	t.Helper()
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	repo := robustTempDir(t)
	initEffectRepository(t, repo)
	for _, dir := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	open := func(folder string) *workspacelease.Owner {
		cfg := config.Default()
		cfg.Agent.WriteLease = "strict"
		cfg.Sandbox.Bash = bashMode
		session, err := startSessionRuntime(t.Context(), Options{SessionDir: robustTempDir(t)}, cfg, folder, event.Discard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.jobs.Close)
		session.lease.BeginRun()
		t.Cleanup(session.lease.EndRun)
		return session.lease
	}
	cfg := config.Default()
	cfg.Sandbox.Bash = bashMode
	confined = shellConfinedToFolder(Options{}, cfg)
	return open(filepath.Join(repo, "alpha")), open(filepath.Join(repo, "beta")), open(repo), confined
}

// Two projects under one repository are separate workspaces to the person who
// opened them, but only while an OS sandbox keeps each shell inside its own
// folder: then a writer that declared nothing holds that folder and the
// repository root still excludes both. Without the sandbox a shell can write
// beside its folder, so the whole repository stays claimed.
func TestSiblingProjectsWaitOnEachOtherExactlyWhenNoSandboxConfinesThem(t *testing.T) {
	for _, mode := range []string{"off", "enforce"} {
		t.Run(mode, func(t *testing.T) {
			alpha, beta, root, confined := siblingProjects(t, mode)
			if mode == "off" && confined {
				t.Fatal("a session with the bash sandbox off was counted as confined")
			}
			if err := alpha.AcquireWrite(context.Background()); err != nil {
				t.Fatalf("alpha: %v", err)
			}
			if got := waits(t, beta.AcquireWrite); got == confined {
				t.Fatalf("sibling project waited = %v with confined = %v (sandbox available here: %v); it must wait exactly when nothing confines it", got, confined, sandbox.Available())
			}
			if !waits(t, root.AcquireWrite) {
				t.Fatal("a session on the repository root did not wait on a writer in a project beneath it")
			}
		})
	}
}
