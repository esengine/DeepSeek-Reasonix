package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
)

// TestWorkspaceRootForDir covers the --dir plumbing: no --dir yields no override
// (empty, so boot falls back to git-root detection), while a --dir run returns
// the post-chdir working directory as the explicit root. A Getwd failure surfaces
// as an error rather than a silent empty fallback.
func TestWorkspaceRootForDir(t *testing.T) {
	// No --dir: no explicit override, no error.
	if got, err := workspaceRootForDir(""); err != nil || got != "" {
		t.Fatalf("workspaceRootForDir(\"\") = %q, %v; want \"\", nil", got, err)
	}

	// With --dir: chdirTo has already switched in, so the root is the CWD.
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := workspaceRootForDir(dir)
	if err != nil {
		t.Fatalf("workspaceRootForDir: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("workspaceRootForDir returned unusable path %q: %v", got, err)
	}
	if gotResolved != want {
		t.Fatalf("workspaceRootForDir(%q) = %q, want cwd %q", dir, got, dir)
	}
}

const minimalTestModelTOML = `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`

// TestSetupProfilePinsExplicitDirOverGitRoot drives the real chdirTo ->
// workspaceRootForDir -> setupProfile -> boot.Build wiring shared by the
// initial CLI controller (runAgent, chatREPL), not just the helpers in
// isolation. An explicit --dir root inside a git repo must reach the built
// controller unchanged; an empty --dir must still fall back to the nearest
// git root. This proves the seam actually forwards the value end to end.
func TestSetupProfilePinsExplicitDirOverGitRoot(t *testing.T) {
	isolateCLIConfigHome(t)

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{repo, sub} {
		if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(minimalTestModelTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	// Explicit --dir: workspaceRootForDir pins sub; setupProfile must not widen
	// it back to the repo's git root.
	workspaceRoot, err := workspaceRootForDir(sub)
	if err != nil {
		t.Fatalf("workspaceRootForDir: %v", err)
	}
	ctrl, err := setupProfile(context.Background(), "", 0, false, event.Discard, "", workspaceRoot)
	if err != nil {
		t.Fatalf("setupProfile with explicit --dir: %v", err)
	}
	defer ctrl.Close()
	wantSub, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	gotSub, err := filepath.EvalSymlinks(ctrl.WorkspaceRoot())
	if err != nil {
		t.Fatalf("controller has unusable workspace root %q: %v", ctrl.WorkspaceRoot(), err)
	}
	if gotSub != wantSub {
		t.Fatalf("setupProfile with --dir %s: controller workspace root = %q, want explicit dir (must not widen to repo root)", sub, ctrl.WorkspaceRoot())
	}

	// No --dir: still falls back to the nearest git root from the CWD (sub is
	// still inside repo, which has the .git marker).
	ctrlFallback, err := setupProfile(context.Background(), "", 0, false, event.Discard, "", "")
	if err != nil {
		t.Fatalf("setupProfile with no --dir: %v", err)
	}
	defer ctrlFallback.Close()
	wantRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	gotRepo, err := filepath.EvalSymlinks(ctrlFallback.WorkspaceRoot())
	if err != nil {
		t.Fatalf("fallback controller has unusable workspace root %q: %v", ctrlFallback.WorkspaceRoot(), err)
	}
	if gotRepo != wantRepo {
		t.Fatalf("setupProfile with no --dir: controller workspace root = %q, want git root %q", ctrlFallback.WorkspaceRoot(), repo)
	}
}
