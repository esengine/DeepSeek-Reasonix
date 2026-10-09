package boot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/workspacelease"
)

// The mode a user file names is what the session's runtime is built with. This
// asserts it through the real session assembly rather than the lease alone,
// against a holder on the same workspace root: optimistic lets an undeclaring
// writer of this session run beside the holder while a declared path still
// waits, off takes no lease at all, and the default waits either way.
func TestWriteLeaseModeReachesTheSessionLease(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	// A session built with the given mode against the same root the holder
	// holds, so the two really contend for one workspace.
	session := func(t *testing.T, mode, root string) *workspacelease.Owner {
		t.Helper()
		cfg := config.Default()
		if mode != "" {
			cfg.Agent.WriteLease = mode
		}
		rt, err := startSessionRuntime(Options{SessionDir: robustTempDir(t)}, cfg, root, event.Discard)
		if err != nil {
			t.Fatalf("startSessionRuntime(%q): %v", mode, err)
		}
		t.Cleanup(func() { rt.jobs.Close() })
		return rt.lease
	}

	holds := func(t *testing.T, root string) {
		t.Helper()
		holder, err := workspacelease.New(root, config.WorkspaceLeaseDir(), nil)
		if err != nil {
			t.Fatalf("holder: %v", err)
		}
		holder.BeginRun()
		t.Cleanup(holder.EndRun)
		if err := holder.AcquireWrite(context.Background()); err != nil {
			t.Fatalf("holder acquire: %v", err)
		}
	}

	deadline := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 300*time.Millisecond)
	}

	t.Run("optimistic lets an undeclaring writer pass and a declared path wait", func(t *testing.T) {
		root := robustTempDir(t)
		holds(t, root)
		lease := session(t, config.WriteLeaseOptimistic, root)
		if err := lease.AcquireWrite(context.Background()); err != nil {
			t.Fatalf("an undeclaring writer must not queue behind the holder: %v", err)
		}
		ctx, cancel := deadline()
		defer cancel()
		if err := lease.AcquirePaths(ctx, []string{filepath.Join(root, "a.txt")}); err == nil {
			t.Fatal("a writer that declares a path must still wait behind the holder")
		}
	})

	t.Run("off takes no lease at all", func(t *testing.T) {
		root := robustTempDir(t)
		holds(t, root)
		lease := session(t, config.WriteLeaseOff, root)
		if err := lease.AcquireWrite(context.Background()); err != nil {
			t.Fatalf("with the lease off nothing should wait: %v", err)
		}
		if err := lease.AcquirePaths(context.Background(), []string{filepath.Join(root, "a.txt")}); err != nil {
			t.Fatalf("with the lease off nothing should wait: %v", err)
		}
	})

	t.Run("the default waits behind the holder either way", func(t *testing.T) {
		root := robustTempDir(t)
		holds(t, root)
		lease := session(t, "", root)
		ctx, cancel := deadline()
		defer cancel()
		if err := lease.AcquireWrite(ctx); err == nil {
			t.Fatal("the default must still wait behind the holder")
		}
		ctx2, cancel2 := deadline()
		defer cancel2()
		if err := lease.AcquirePaths(ctx2, []string{filepath.Join(root, "a.txt")}); err == nil {
			t.Fatal("the default must still wait behind the holder")
		}
	})
}
