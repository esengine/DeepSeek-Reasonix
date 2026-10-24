package boot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/workspacelease"
)

func leaseSession(t *testing.T, root string, mode string) *workspacelease.Owner {
	t.Helper()
	cfg := config.Default()
	cfg.Agent.WriteLease = mode
	session, err := startSessionRuntime(t.Context(), Options{SessionDir: robustTempDir(t)}, cfg, root, event.Discard)
	if err != nil {
		t.Fatalf("startSessionRuntime(%q): %v", mode, err)
	}
	t.Cleanup(session.jobs.Close)
	session.lease.BeginRun()
	t.Cleanup(session.lease.EndRun)
	return session.lease
}

func waits(t *testing.T, acquire func(ctx context.Context) error) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := acquire(ctx)
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var conflict *workspacelease.ConflictError
	if errors.As(err, &conflict) {
		return true
	}
	t.Fatalf("unexpected error: %v", err)
	return false
}

// The mode a user chose reaches the lease each session is built with: under
// optimistic an undeclared writer (bash, MCP) no longer holds the whole
// workspace against another session, a writer that declared a path still
// excludes it, and a strict session beside it is unchanged.
func TestWriteLeaseModeShapesTheSessionsLease(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	root := robustTempDir(t)

	holder := leaseSession(t, root, "strict")
	if err := holder.AcquirePaths(context.Background(), []string{filepath.Join(root, "a.txt")}); err != nil {
		t.Fatalf("holder takes a declared path: %v", err)
	}

	optimistic := leaseSession(t, root, "optimistic")
	strict := leaseSession(t, root, "strict")

	if waits(t, optimistic.AcquireWrite) {
		t.Error("an undeclared writer waited behind a declared path under optimistic")
	}
	if !waits(t, func(ctx context.Context) error {
		return optimistic.AcquirePaths(ctx, []string{filepath.Join(root, "a.txt")})
	}) {
		t.Error("a declared overlapping path did not wait under optimistic")
	}
	if !waits(t, strict.AcquireWrite) {
		t.Error("an undeclared writer did not wait under strict")
	}
}

// A declared path the host cannot resolve is not an undeclared writer: it keeps
// holding the workspace under optimistic, exactly as under strict.
func TestAnUnresolvableDeclaredPathStillHoldsTheWorkspaceUnderOptimistic(t *testing.T) {
	root := leaseHome(t)
	holder := leaseSession(t, root, "strict")
	if err := holder.AcquirePaths(context.Background(), []string{filepath.Join(root, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	optimistic := leaseSession(t, root, "optimistic")
	if !waits(t, func(ctx context.Context) error { return optimistic.AcquirePaths(ctx, []string{"  "}) }) {
		t.Error("a declared path the host could not resolve was treated as no claim at all")
	}
}

// A declared path that resolves outside the workspace, such as one in an extra
// writable folder, claims only itself and never the whole workspace.
func TestADeclaredPathOutsideTheWorkspaceDoesNotHoldItUnderOptimistic(t *testing.T) {
	root := leaseHome(t)
	holder := leaseSession(t, root, "strict")
	if err := holder.AcquirePaths(context.Background(), []string{filepath.Join(root, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	optimistic := leaseSession(t, root, "optimistic")
	outside := filepath.Join(robustTempDir(t), "elsewhere.txt")
	if waits(t, func(ctx context.Context) error { return optimistic.AcquirePaths(ctx, []string{outside}) }) {
		t.Error("a declared path outside the workspace waited behind a file inside it")
	}
}

func leaseHome(t *testing.T) string {
	t.Helper()
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	return robustTempDir(t)
}
