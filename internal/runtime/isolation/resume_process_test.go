package isolation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResumedEnvironmentCrashHelper(t *testing.T) {
	repo := os.Getenv("REASONIX_RESUME_CRASH_REPO")
	if repo == "" {
		return
	}
	managed := os.Getenv("REASONIX_RESUME_CRASH_MANAGED")
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "first\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e, err = s.Resume(t.Context(), e.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "b.txt", "before crash\n")
	if err := os.WriteFile(filepath.Join(managed, "result-id"), []byte(e.ID), 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(77)
}

func TestResumedEnvironmentRecoversAfterProcessDeath(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResumedEnvironmentCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_RESUME_CRASH_REPO="+repo, "REASONIX_RESUME_CRASH_MANAGED="+managed)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("child exit: %v\n%s", err, out)
	}
	id, err := os.ReadFile(filepath.Join(managed, "result-id"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.Resume(t.Context(), string(id), "owner")
	if err != nil {
		t.Fatalf("running mutation mistaken for sealed-result modification: %v", err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	if got := read(t, e.Cand.WorkspaceRoot, "b.txt"); got != "before crash\n" {
		t.Fatal(got)
	}
	write(t, e.Cand.WorkspaceRoot, "b.txt", "after resume\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "first\n" {
		t.Fatal(got)
	}
	if got := read(t, repo, "b.txt"); got != "after resume\n" {
		t.Fatal(got)
	}
}
