package main

import (
	"os"
	"os/exec"
	"slices"
	"testing"
)

func TestWorkspaceGitCommitDetailPreservesUnicodeFilenames(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	runGit(t, "init")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Test User")
	runGit(t, "config", "core.quotepath", "true")

	if err := os.WriteFile("seed.txt", []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "seed.txt")
	runGit(t, "commit", "-m", "seed")

	const unicodeFilename = "中文文件名测试.txt"
	if err := os.WriteFile("plain.txt", []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unicodeFilename, []byte("unicode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "plain.txt", unicodeFilename)
	runGit(t, "commit", "-m", "add files")

	detail, err := (&App{}).WorkspaceGitCommitDetail("", gitOutput(t, "rev-parse", "HEAD"), "")
	if err != nil {
		t.Fatalf("WorkspaceGitCommitDetail err = %v", err)
	}
	want := []string{"plain.txt", unicodeFilename}
	if !slices.Equal(detail.Files, want) {
		t.Fatalf("files = %q, want %q", detail.Files, want)
	}
}
